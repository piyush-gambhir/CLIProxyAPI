package quota

import (
	"sort"
	"strings"
	"time"
)

// DefaultFiveHourGuardPercent is the five-hour guard from the design: a
// credential at or above this much of its short window ranks after every
// credential below it. It is Theo's "unless you keep hitting 5-hour limits"
// exception, not a hard block.
const DefaultFiveHourGuardPercent = 90.0

// minHoursToReset floors the urgency denominator so a window about to reset
// cannot produce an unbounded score.
const minHoursToReset = 0.25

// Reasons recorded on a ranked candidate. They are surfaced by the management
// status endpoint and by the per-pick debug log.
const (
	// ReasonRanked means the candidate has a usable ranking window.
	ReasonRanked = "ranked"
	// ReasonGuarded means the candidate is past the five-hour guard.
	ReasonGuarded = "five_hour_guard"
	// ReasonNoData means no window reading is known for this credential.
	ReasonNoData = "no_data"
	// ReasonShortWindowExhausted means the five-hour window is spent.
	ReasonShortWindowExhausted = "five_hour_exhausted"
	// ReasonShortWindowHardLimited means the provider refused on the short window.
	ReasonShortWindowHardLimited = "five_hour_hard_limited"
	// ReasonModelWindowExhausted means the requested model's own window is spent.
	ReasonModelWindowExhausted = "model_window_exhausted"
	// ReasonModelWindowHardLimited means the provider refused on the model window.
	ReasonModelWindowHardLimited = "model_window_hard_limited"
	// ReasonLongWindowHardLimited means the provider refused on the ranking window.
	ReasonLongWindowHardLimited = "long_window_hard_limited"
)

// Candidate is one credential offered to the ranker.
type Candidate struct {
	// AuthID is the credential's auth manager ID.
	AuthID string
	// Provider is the upstream provider key.
	Provider string
	// Reading is the credential's normalized quota reading. A zero Reading with
	// no windows means no data.
	Reading Reading
}

// RankOptions configures one ranking pass.
type RankOptions struct {
	// Model is the requested model, used to resolve a per-model window.
	Model string
	// FiveHourGuardPercent is the guard threshold. Zero or less uses the default.
	FiveHourGuardPercent float64
	// Now is the reference time for the urgency denominator.
	Now time.Time
}

// Ranked is one ranking result.
type Ranked struct {
	// AuthID is the credential's auth manager ID.
	AuthID string
	// Provider is the upstream provider key.
	Provider string
	// Urgency is percent left divided by hours to reset, 0 when unknown.
	Urgency float64
	// Usable reports whether the candidate may serve the request at all.
	Usable bool
	// Reason explains the placement.
	Reason string
	// RankingWindow names the window that set the urgency, empty when unknown.
	RankingWindow string
	// Known reports whether a ranking window reading exists.
	Known bool
	// Guarded reports whether the five-hour guard demoted this candidate.
	Guarded bool
	// ResetAt is the ranking window's reset, used as a tie break.
	ResetAt time.Time
}

// Rank orders candidates by urgency per the design's reset-soonest rule.
//
// Unusable candidates keep their place in the returned slice so the status
// endpoint can explain them, but they are reported with Usable false and sort
// after every usable candidate. Callers that select a credential must filter on
// Usable.
//
// The order is: usable before unusable, then below the guard before guarded,
// then known before unknown, then higher urgency, then an earlier reset, then
// the auth ID. Credentials with no reading therefore land together at the end of
// the usable block in stable ID order, which lets the caller round-robin them.
func Rank(candidates []Candidate, opts RankOptions) []Ranked {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	guard := opts.FiveHourGuardPercent
	if guard <= 0 {
		guard = DefaultFiveHourGuardPercent
	}
	modelWindow := PerModelWindow(opts.Model)

	out := make([]Ranked, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, rankOne(candidate, modelWindow, guard, now))
	}
	sort.SliceStable(out, func(i, j int) bool { return lessRanked(out[i], out[j]) })
	return out
}

// rankOne scores a single candidate.
func rankOne(candidate Candidate, modelWindow string, guard float64, now time.Time) Ranked {
	result := Ranked{
		AuthID:   candidate.AuthID,
		Provider: candidate.Provider,
		Usable:   true,
		Reason:   ReasonNoData,
	}

	// Step 2 of the design: an exhausted or hard-limited short window, or an
	// exhausted per-model window for the requested model, drops the credential.
	if short, ok := candidate.Reading.Window(WindowFiveHour); ok {
		switch {
		case short.HardLimited:
			result.Usable, result.Reason = false, ReasonShortWindowHardLimited
		case short.Known && short.UsedPercent >= 100:
			result.Usable, result.Reason = false, ReasonShortWindowExhausted
		}
	}
	if result.Usable && modelWindow != "" {
		if perModel, ok := candidate.Reading.Window(modelWindow); ok {
			switch {
			case perModel.HardLimited:
				result.Usable, result.Reason = false, ReasonModelWindowHardLimited
			case perModel.Known && perModel.UsedPercent >= 100:
				result.Usable, result.Reason = false, ReasonModelWindowExhausted
			}
		}
	}

	windowID, window, ok := rankingWindow(candidate.Reading, modelWindow)
	if !ok {
		// Credit balances and unreadable providers never rank: they stay here as
		// no-data and round-robin behind every known credential.
		return result
	}
	result.Known = true
	result.RankingWindow = windowID
	result.ResetAt = window.ResetAt
	result.Urgency = Urgency(window, now)
	// A reason set in step 2 explains why the credential is unusable and must
	// survive: the urgency is still reported so the status surface can show it.
	if result.Usable {
		result.Reason = ReasonRanked
		if window.HardLimited {
			result.Usable = false
			result.Reason = ReasonLongWindowHardLimited
			if windowID == WindowFiveHour {
				result.Reason = ReasonShortWindowHardLimited
			}
		}
	}
	if result.Usable {
		if short, hasShort := candidate.Reading.Window(WindowFiveHour); hasShort && short.Known && short.UsedPercent >= guard {
			result.Guarded, result.Reason = true, ReasonGuarded
		}
	}
	return result
}

// Urgency is the percentage of a window still left divided by the hours until
// it resets. A window with no reset time is treated as resetting a full window
// length from now, which gives it the low urgency the design asks for.
func Urgency(window Window, now time.Time) float64 {
	left := 100 - clampPercent(window.UsedPercent)
	if left < 0 {
		left = 0
	}
	hours := minHoursToReset
	if !window.ResetAt.IsZero() {
		if delta := window.ResetAt.Sub(now).Hours(); delta > hours {
			hours = delta
		}
	} else if fallback := window.length("").Hours(); fallback > hours {
		hours = fallback
	}
	return left / hours
}

// rankingWindow picks the window that sets a credential's urgency.
//
// The ranking window is the longest one: Claude's seven_day, Codex's weekly, or
// the only window present. When the request's model has a per-model window, the
// design takes whichever of that window and the main long window has LESS left.
// That is a deliberate departure from bandoyer #8, which takes the more urgent
// of the two: Theo only cares about the Fable 7-day once it is the binding one.
func rankingWindow(reading Reading, modelWindow string) (string, Window, bool) {
	mainID, main, hasMain := longWindow(reading)
	if modelWindow == "" {
		return mainID, main, hasMain
	}
	perModel, hasPerModel := reading.Window(modelWindow)
	if !hasPerModel || !perModel.Known {
		return mainID, main, hasMain
	}
	if !hasMain || !main.Known {
		return modelWindow, perModel, true
	}
	// Less left means a higher used percentage.
	if perModel.UsedPercent > main.UsedPercent {
		return modelWindow, perModel, true
	}
	return mainID, main, true
}

// longWindow returns the longest non-per-model window with a reading.
func longWindow(reading Reading) (string, Window, bool) {
	for _, id := range []string{WindowSevenDay, WindowWeekly} {
		if window, ok := reading.Window(id); ok && window.Known {
			return id, window, true
		}
	}
	// A provider with a single window ranks on it. The five-hour window only
	// gates usability when a long window exists, so it is the fallback here.
	if window, ok := reading.Window(WindowFiveHour); ok && window.Known {
		return WindowFiveHour, window, true
	}
	return "", Window{}, false
}

// PerModelWindow maps a requested model onto its per-model quota window. Only
// Claude's Fable 7-day window is recognised today.
func PerModelWindow(model string) string {
	if strings.Contains(strings.ToLower(strings.TrimSpace(model)), "fable") {
		return WindowFable
	}
	return ""
}

// lessRanked implements the documented ordering.
func lessRanked(left, right Ranked) bool {
	if left.Usable != right.Usable {
		return left.Usable
	}
	if left.Guarded != right.Guarded {
		return !left.Guarded
	}
	if left.Known != right.Known {
		return left.Known
	}
	if left.Known && left.Urgency != right.Urgency {
		return left.Urgency > right.Urgency
	}
	if left.Known && !left.ResetAt.Equal(right.ResetAt) {
		if left.ResetAt.IsZero() {
			return false
		}
		if right.ResetAt.IsZero() {
			return true
		}
		return left.ResetAt.Before(right.ResetAt)
	}
	return left.AuthID < right.AuthID
}
