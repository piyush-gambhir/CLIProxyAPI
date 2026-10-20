package quota

import (
	"testing"
	"time"
)

var rankBase = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

// readingOf builds a reading from a compact window description.
func readingOf(provider string, windows map[string]Window) Reading {
	out := Reading{Provider: provider, Windows: make(map[string]Window, len(windows))}
	for id, window := range windows {
		if window.LearnedAt.IsZero() {
			window.LearnedAt = rankBase
		}
		window.Known = true
		out.Windows[id] = window
		if out.FetchedAt.Before(window.LearnedAt) {
			out.FetchedAt = window.LearnedAt
		}
	}
	return out
}

// longWindowOf is shorthand for a credential whose only reading is its long window.
func longWindowOf(usedPercent float64, resetIn time.Duration) Reading {
	return readingOf("claude", map[string]Window{
		WindowSevenDay: {UsedPercent: usedPercent, ResetAt: rankBase.Add(resetIn)},
	})
}

func orderOf(ranked []Ranked) []string {
	out := make([]string, 0, len(ranked))
	for _, entry := range ranked {
		out = append(out, entry.AuthID)
	}
	return out
}

func TestRankUrgencyOrdering(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		candidates []Candidate
		want       []string
	}{
		{
			// The design's first worked example: 40 percent left resetting in 3 h
			// beats 90 percent left resetting in 4 days.
			name: "little left and resetting soon beats plenty left and far off",
			candidates: []Candidate{
				{AuthID: "a", Provider: "claude", Reading: longWindowOf(60, 3*time.Hour)},
				{AuthID: "b", Provider: "claude", Reading: longWindowOf(10, 4*24*time.Hour)},
			},
			want: []string{"a", "b"},
		},
		{
			// The design's second worked example: urgency is not earliest reset.
			// 80 percent left in 2 h beats 1 percent left in 10 min.
			name: "urgency beats earliest reset",
			candidates: []Candidate{
				{AuthID: "c", Provider: "claude", Reading: longWindowOf(99, 10*time.Minute)},
				{AuthID: "d", Provider: "claude", Reading: longWindowOf(20, 2*time.Hour)},
			},
			want: []string{"d", "c"},
		},
		{
			name: "percentages compare across providers and plan sizes",
			candidates: []Candidate{
				{AuthID: "codex", Provider: "codex", Reading: readingOf("codex", map[string]Window{
					WindowWeekly: {UsedPercent: 50, ResetAt: rankBase.Add(24 * time.Hour)},
				})},
				{AuthID: "claude", Provider: "claude", Reading: longWindowOf(50, 12*time.Hour)},
			},
			want: []string{"claude", "codex"},
		},
		{
			name: "equal urgency breaks on the earlier reset",
			candidates: []Candidate{
				{AuthID: "later", Provider: "claude", Reading: longWindowOf(50, 4*time.Hour)},
				{AuthID: "sooner", Provider: "claude", Reading: longWindowOf(75, 2*time.Hour)},
			},
			want: []string{"sooner", "later"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ranked := Rank(testCase.candidates, RankOptions{Now: rankBase})
			got := orderOf(ranked)
			if len(got) != len(testCase.want) {
				t.Fatalf("order = %v, want %v", got, testCase.want)
			}
			for i := range got {
				if got[i] != testCase.want[i] {
					t.Fatalf("order = %v, want %v", got, testCase.want)
				}
			}
		})
	}
}

func TestRankFableWindowUsesWhicheverHasLessLeft(t *testing.T) {
	t.Parallel()

	// The Fable window is nearly spent while the main weekly window is fresh.
	// The design deliberately ranks on the window with LESS left, departing from
	// bandoyer #8's "more urgent of the two".
	candidate := Candidate{AuthID: "a", Provider: "claude", Reading: readingOf("claude", map[string]Window{
		WindowSevenDay: {UsedPercent: 10, ResetAt: rankBase.Add(6 * 24 * time.Hour)},
		WindowFable:    {UsedPercent: 95, ResetAt: rankBase.Add(6 * 24 * time.Hour)},
	})}

	withoutFableModel := Rank([]Candidate{candidate}, RankOptions{Model: "claude-opus-5", Now: rankBase})
	if withoutFableModel[0].RankingWindow != WindowSevenDay {
		t.Fatalf("non-fable model ranking window = %q, want %q", withoutFableModel[0].RankingWindow, WindowSevenDay)
	}

	withFableModel := Rank([]Candidate{candidate}, RankOptions{Model: "claude-fable-5-1", Now: rankBase})
	if withFableModel[0].RankingWindow != WindowFable {
		t.Fatalf("fable model ranking window = %q, want %q", withFableModel[0].RankingWindow, WindowFable)
	}
	if withFableModel[0].Urgency >= withoutFableModel[0].Urgency {
		t.Fatalf("fable urgency %.3f should be below the weekly urgency %.3f",
			withFableModel[0].Urgency, withoutFableModel[0].Urgency)
	}

	// When the main window has less left than the Fable window, the main one wins
	// even for a Fable request.
	mainTighter := Candidate{AuthID: "b", Provider: "claude", Reading: readingOf("claude", map[string]Window{
		WindowSevenDay: {UsedPercent: 80, ResetAt: rankBase.Add(6 * 24 * time.Hour)},
		WindowFable:    {UsedPercent: 20, ResetAt: rankBase.Add(6 * 24 * time.Hour)},
	})}
	ranked := Rank([]Candidate{mainTighter}, RankOptions{Model: "claude-fable-5-1", Now: rankBase})
	if ranked[0].RankingWindow != WindowSevenDay {
		t.Fatalf("ranking window = %q, want %q", ranked[0].RankingWindow, WindowSevenDay)
	}
}

func TestRankFiveHourGuardDemotesWithoutBlocking(t *testing.T) {
	t.Parallel()

	// "guarded" is the most urgent credential on paper but is past the guard, so
	// it ranks behind the calmer one. It stays usable: the guard is a penalty.
	guarded := Candidate{AuthID: "guarded", Provider: "claude", Reading: readingOf("claude", map[string]Window{
		WindowFiveHour: {UsedPercent: 95, ResetAt: rankBase.Add(time.Hour)},
		WindowSevenDay: {UsedPercent: 60, ResetAt: rankBase.Add(3 * time.Hour)},
	})}
	calm := Candidate{AuthID: "calm", Provider: "claude", Reading: readingOf("claude", map[string]Window{
		WindowFiveHour: {UsedPercent: 10, ResetAt: rankBase.Add(time.Hour)},
		WindowSevenDay: {UsedPercent: 10, ResetAt: rankBase.Add(5 * 24 * time.Hour)},
	})}

	ranked := Rank([]Candidate{guarded, calm}, RankOptions{Now: rankBase})
	if got := orderOf(ranked); got[0] != "calm" || got[1] != "guarded" {
		t.Fatalf("order = %v, want [calm guarded]", got)
	}
	if !ranked[1].Guarded || ranked[1].Reason != ReasonGuarded {
		t.Fatalf("guarded entry = %+v, want Guarded with reason %q", ranked[1], ReasonGuarded)
	}
	if !ranked[1].Usable {
		t.Fatal("the five-hour guard must demote, not block")
	}

	// Raising the guard above the reading restores the urgency order.
	relaxed := Rank([]Candidate{guarded, calm}, RankOptions{Now: rankBase, FiveHourGuardPercent: 99})
	if got := orderOf(relaxed); got[0] != "guarded" {
		t.Fatalf("relaxed order = %v, want guarded first", got)
	}
}

func TestRankHardLimitedAndExhaustedAreUnusable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		model      string
		reading    Reading
		wantReason string
	}{
		{
			name: "hard limited short window",
			reading: readingOf("claude", map[string]Window{
				WindowFiveHour: {UsedPercent: 40, HardLimited: true, ResetAt: rankBase.Add(time.Hour)},
				WindowSevenDay: {UsedPercent: 10, ResetAt: rankBase.Add(5 * 24 * time.Hour)},
			}),
			wantReason: ReasonShortWindowHardLimited,
		},
		{
			name: "exhausted short window",
			reading: readingOf("claude", map[string]Window{
				WindowFiveHour: {UsedPercent: 100, ResetAt: rankBase.Add(time.Hour)},
				WindowSevenDay: {UsedPercent: 10, ResetAt: rankBase.Add(5 * 24 * time.Hour)},
			}),
			wantReason: ReasonShortWindowExhausted,
		},
		{
			name:  "exhausted per-model window for a request on that model",
			model: "claude-fable-5-1",
			reading: readingOf("claude", map[string]Window{
				WindowSevenDay: {UsedPercent: 10, ResetAt: rankBase.Add(5 * 24 * time.Hour)},
				WindowFable:    {UsedPercent: 100, ResetAt: rankBase.Add(5 * 24 * time.Hour)},
			}),
			wantReason: ReasonModelWindowExhausted,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			healthy := Candidate{AuthID: "healthy", Provider: "claude", Reading: longWindowOf(50, 2*time.Hour)}
			blocked := Candidate{AuthID: "blocked", Provider: "claude", Reading: testCase.reading}
			ranked := Rank([]Candidate{blocked, healthy}, RankOptions{Model: testCase.model, Now: rankBase})
			if ranked[0].AuthID != "healthy" {
				t.Fatalf("order = %v, want healthy first", orderOf(ranked))
			}
			if ranked[1].Usable {
				t.Fatalf("entry %+v should be unusable", ranked[1])
			}
			if ranked[1].Reason != testCase.wantReason {
				t.Fatalf("reason = %q, want %q", ranked[1].Reason, testCase.wantReason)
			}
		})
	}
}

func TestRankExhaustedPerModelWindowOnlyAffectsThatModel(t *testing.T) {
	t.Parallel()

	spentFable := Candidate{AuthID: "a", Provider: "claude", Reading: readingOf("claude", map[string]Window{
		WindowSevenDay: {UsedPercent: 10, ResetAt: rankBase.Add(5 * 24 * time.Hour)},
		WindowFable:    {UsedPercent: 100, ResetAt: rankBase.Add(5 * 24 * time.Hour)},
	})}

	onOpus := Rank([]Candidate{spentFable}, RankOptions{Model: "claude-opus-5", Now: rankBase})
	if !onOpus[0].Usable {
		t.Fatal("a spent Fable window must not block a non-Fable request")
	}
	onFable := Rank([]Candidate{spentFable}, RankOptions{Model: "claude-fable-5-1", Now: rankBase})
	if onFable[0].Usable {
		t.Fatal("a spent Fable window must block a Fable request")
	}
}

func TestRankUnknownsSortAfterKnownsInStableOrder(t *testing.T) {
	t.Parallel()

	candidates := []Candidate{
		{AuthID: "unknown-b", Provider: "gemini"},
		{AuthID: "known", Provider: "claude", Reading: longWindowOf(99, 7*24*time.Hour)},
		{AuthID: "unknown-a", Provider: "gemini"},
	}
	ranked := Rank(candidates, RankOptions{Now: rankBase})
	want := []string{"known", "unknown-a", "unknown-b"}
	got := orderOf(ranked)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	// Even a credential with almost nothing left outranks a credential with no
	// reading at all, and the unknowns report themselves as such.
	if ranked[0].Reason != ReasonRanked {
		t.Fatalf("known reason = %q, want %q", ranked[0].Reason, ReasonRanked)
	}
	for _, entry := range ranked[1:] {
		if entry.Known || entry.Reason != ReasonNoData {
			t.Fatalf("entry %+v should be no-data", entry)
		}
		if !entry.Usable {
			t.Fatal("a credential with no reading stays usable")
		}
	}
}

func TestRankFallsBackToTheOnlyWindow(t *testing.T) {
	t.Parallel()

	// A provider that reports only a short window ranks on it: the five-hour
	// window gates usability only when a long window exists.
	candidate := Candidate{AuthID: "a", Provider: "codex", Reading: readingOf("codex", map[string]Window{
		WindowFiveHour: {UsedPercent: 50, ResetAt: rankBase.Add(time.Hour)},
	})}
	ranked := Rank([]Candidate{candidate}, RankOptions{Now: rankBase})
	if ranked[0].RankingWindow != WindowFiveHour {
		t.Fatalf("ranking window = %q, want %q", ranked[0].RankingWindow, WindowFiveHour)
	}
	if ranked[0].Urgency != 50 {
		t.Fatalf("urgency = %.3f, want 50", ranked[0].Urgency)
	}
}

func TestUrgencyFloorsTheDenominator(t *testing.T) {
	t.Parallel()

	// A window that resets in a second must not produce an unbounded score.
	window := Window{UsedPercent: 0, ResetAt: rankBase.Add(time.Second), Known: true}
	if got := Urgency(window, rankBase); got != 100/minHoursToReset {
		t.Fatalf("urgency = %.3f, want %.3f", got, 100/minHoursToReset)
	}
	// A passed reset is floored the same way; the store normalises it separately.
	passed := Window{UsedPercent: 50, ResetAt: rankBase.Add(-time.Hour), Known: true}
	if got := Urgency(passed, rankBase); got != 50/minHoursToReset {
		t.Fatalf("urgency = %.3f, want %.3f", got, 50/minHoursToReset)
	}
}

func TestPerModelWindow(t *testing.T) {
	t.Parallel()

	for model, want := range map[string]string{
		"claude-fable-5-1":     WindowFable,
		"CLAUDE-FABLE-5":       WindowFable,
		"claude-opus-5":        "",
		"gpt-6.1-sol":          "",
		"claude-fable-5[1m]":   WindowFable,
		"claude-sonnet-4-5":    "",
		"anthropic/fable-pro":  WindowFable,
		"claude-haiku-4-5-max": "",
	} {
		if got := PerModelWindow(model); got != want {
			t.Fatalf("PerModelWindow(%q) = %q, want %q", model, got, want)
		}
	}
}
