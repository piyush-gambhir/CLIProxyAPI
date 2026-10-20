package quota

import (
	"testing"
	"time"
)

var signalBase = time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)

func TestSignalWindowsClaude(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		signals map[string]string
		want    map[string]Window
	}{
		{
			name: "direct utilization and an absolute reset",
			signals: map[string]string{
				"anthropic-ratelimit-unified-5h-utilization": "0.42",
				"anthropic-ratelimit-unified-5h-reset":       "2026-10-03T14:00:00Z",
				"anthropic-ratelimit-unified-7d-utilization": "0.615",
				"anthropic-ratelimit-unified-7d-reset":       "2026-10-08T10:00:00Z",
			},
			want: map[string]Window{
				WindowFiveHour: {UsedPercent: 42, ResetAt: time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC), Known: true},
				WindowSevenDay: {UsedPercent: 61.5, ResetAt: time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC), Known: true},
			},
		},
		{
			name: "remaining over limit becomes a used percentage",
			signals: map[string]string{
				"anthropic-ratelimit-unified-7d-remaining": "25",
				"anthropic-ratelimit-unified-7d-limit":     "100",
			},
			want: map[string]Window{
				WindowSevenDay: {UsedPercent: 75, Known: true},
			},
		},
		{
			name: "a relative reset is resolved against the observation time",
			signals: map[string]string{
				"anthropic-ratelimit-unified-5h-utilization": "0.1",
				"anthropic-ratelimit-unified-5h-reset":       "3600",
			},
			want: map[string]Window{
				WindowFiveHour: {UsedPercent: 10, ResetAt: signalBase.Add(time.Hour), Known: true},
			},
		},
		{
			name: "a rate limited status with no numbers is still a reading",
			signals: map[string]string{
				"anthropic-ratelimit-unified-7d-status": "rate_limited",
			},
			want: map[string]Window{
				WindowSevenDay: {UsedPercent: 100, Known: true, HardLimited: true},
			},
		},
		{
			name: "the fable window is recognised",
			signals: map[string]string{
				"anthropic-ratelimit-unified-fable-utilization": "0.88",
			},
			want: map[string]Window{
				WindowFable: {UsedPercent: 88, Known: true},
			},
		},
		{
			name: "an unrelated header yields nothing",
			signals: map[string]string{
				"retry-after":  "60",
				"content-type": "application/json",
			},
			want: nil,
		},
		{
			name: "an unrecognised window segment is ignored rather than guessed",
			signals: map[string]string{
				"anthropic-ratelimit-unified-42q-utilization": "0.5",
			},
			want: nil,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := SignalWindows("claude", testCase.signals, signalBase)
			assertWindows(t, got, testCase.want)
		})
	}
}

func TestSignalWindowsCodex(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		signals map[string]string
		want    map[string]Window
	}{
		{
			name: "primary and secondary slots keyed by window length",
			signals: map[string]string{
				"x-codex-primary-used-percent":          "12",
				"x-codex-primary-window-minutes":        "300",
				"x-codex-primary-reset-at":              "2026-10-03T13:00:00Z",
				"x-codex-secondary-used-percent":        "34",
				"x-codex-secondary-window-minutes":      "10080",
				"x-codex-secondary-reset-after-seconds": "86400",
			},
			want: map[string]Window{
				WindowFiveHour: {UsedPercent: 12, WindowMinutes: 300, ResetAt: time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC), Known: true},
				WindowWeekly:   {UsedPercent: 34, WindowMinutes: 10080, ResetAt: signalBase.Add(24 * time.Hour), Known: true},
			},
		},
		{
			name: "slot names alone are enough when no window length is reported",
			signals: map[string]string{
				"x-codex-primary-used-percent":   "5",
				"x-codex-secondary-used-percent": "6",
			},
			want: map[string]Window{
				WindowFiveHour: {UsedPercent: 5, Known: true},
				WindowWeekly:   {UsedPercent: 6, Known: true},
			},
		},
		{
			name: "limit reached marks a hard limit",
			signals: map[string]string{
				"x-codex-primary-used-percent":  "100",
				"x-codex-primary-limit-reached": "true",
			},
			want: map[string]Window{
				WindowFiveHour: {UsedPercent: 100, Known: true, HardLimited: true},
			},
		},
		{
			name: "a reported length overrides the slot name",
			signals: map[string]string{
				"x-codex-primary-used-percent":   "7",
				"x-codex-primary-window-minutes": "10080",
			},
			want: map[string]Window{
				WindowWeekly: {UsedPercent: 7, WindowMinutes: 10080, Known: true},
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := SignalWindows("codex", testCase.signals, signalBase)
			assertWindows(t, got, testCase.want)
		})
	}
}

func TestSignalWindowsIgnoresUnpolledProviders(t *testing.T) {
	t.Parallel()

	signals := map[string]string{"anthropic-ratelimit-unified-7d-utilization": "0.1"}
	if got := SignalWindows("gemini", signals, signalBase); got != nil {
		t.Fatalf("SignalWindows(gemini) = %v, want nil", got)
	}
	if got := SignalWindows("claude", nil, signalBase); got != nil {
		t.Fatalf("SignalWindows with no signals = %v, want nil", got)
	}
	// Anthropic is an alias for Claude so both spellings parse.
	if got := SignalWindows("anthropic", signals, signalBase); len(got) != 1 {
		t.Fatalf("SignalWindows(anthropic) = %v, want one window", got)
	}
}

func TestSignalPlan(t *testing.T) {
	t.Parallel()

	if got := SignalPlan("codex", map[string]string{"x-codex-plan-type": "pro"}); got != "pro" {
		t.Fatalf("SignalPlan() = %q, want pro", got)
	}
	if got := SignalPlan("claude", map[string]string{"x-codex-plan-type": "pro"}); got != "" {
		t.Fatalf("SignalPlan(claude) = %q, want empty", got)
	}
}

func assertWindows(t *testing.T, got, want map[string]Window) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("windows = %+v, want %+v", got, want)
	}
	for id, wantWindow := range want {
		gotWindow, ok := got[id]
		if !ok {
			t.Fatalf("window %q missing from %+v", id, got)
		}
		if gotWindow.UsedPercent != wantWindow.UsedPercent {
			t.Fatalf("window %q used percent = %.2f, want %.2f", id, gotWindow.UsedPercent, wantWindow.UsedPercent)
		}
		if !gotWindow.ResetAt.Equal(wantWindow.ResetAt) {
			t.Fatalf("window %q reset = %s, want %s", id, gotWindow.ResetAt, wantWindow.ResetAt)
		}
		if gotWindow.WindowMinutes != wantWindow.WindowMinutes {
			t.Fatalf("window %q minutes = %d, want %d", id, gotWindow.WindowMinutes, wantWindow.WindowMinutes)
		}
		if gotWindow.Known != wantWindow.Known {
			t.Fatalf("window %q known = %t, want %t", id, gotWindow.Known, wantWindow.Known)
		}
		if gotWindow.HardLimited != wantWindow.HardLimited {
			t.Fatalf("window %q hard limited = %t, want %t", id, gotWindow.HardLimited, wantWindow.HardLimited)
		}
	}
}

func TestParseUsageClaudeLimitsArray(t *testing.T) {
	t.Parallel()

	body := []byte(`{
      "limits": [
        {"kind": "session", "percent": 30, "resets_at": "2026-10-03T14:00:00Z"},
        {"kind": "usage", "group": "weekly", "percent": 55, "resets_at": "2026-10-08T10:00:00Z"},
        {"kind": "usage", "group": "weekly", "percent": 91, "resets_at": "2026-10-08T10:00:00Z",
         "scope": {"model": {"id": "claude-fable-5-1", "display_name": "Fable"}}}
      ]
    }`)
	reading, err := ParseUsage("claude", body, signalBase)
	if err != nil {
		t.Fatalf("ParseUsage() error = %v", err)
	}
	assertWindows(t, reading.Windows, map[string]Window{
		WindowFiveHour: {UsedPercent: 30, ResetAt: time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC), Known: true},
		WindowSevenDay: {UsedPercent: 55, ResetAt: time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC), Known: true},
		WindowFable:    {UsedPercent: 91, ResetAt: time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC), Known: true},
	})
}

func TestParseUsageClaudeFlatFallback(t *testing.T) {
	t.Parallel()

	// The flat shape names the Fable window "iguana_necktie".
	body := []byte(`{
      "plan_type": "max",
      "five_hour": {"utilization": 10, "resets_at": "2026-10-03T14:00:00Z"},
      "seven_day": {"utilization": 20, "resets_at": "2026-10-08T10:00:00Z"},
      "iguana_necktie": {"utilization": 77, "resets_at": "2026-10-08T10:00:00Z"},
      "seven_day_breakdown": {"as_of": "2026-10-03T09:00:00Z", "rows": []}
    }`)
	reading, err := ParseUsage("anthropic", body, signalBase)
	if err != nil {
		t.Fatalf("ParseUsage() error = %v", err)
	}
	if reading.Plan != "max" {
		t.Fatalf("plan = %q, want max", reading.Plan)
	}
	assertWindows(t, reading.Windows, map[string]Window{
		WindowFiveHour: {UsedPercent: 10, ResetAt: time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC), Known: true},
		WindowSevenDay: {UsedPercent: 20, ResetAt: time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC), Known: true},
		WindowFable:    {UsedPercent: 77, ResetAt: time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC), Known: true},
	})
}

func TestParseUsageCodex(t *testing.T) {
	t.Parallel()

	reading, err := ParseUsage("codex", []byte(codexUsageBody), signalBase)
	if err != nil {
		t.Fatalf("ParseUsage() error = %v", err)
	}
	if reading.Plan != "pro" {
		t.Fatalf("plan = %q, want pro", reading.Plan)
	}
	assertWindows(t, reading.Windows, map[string]Window{
		WindowFiveHour: {UsedPercent: 12, WindowMinutes: 300, ResetAt: time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC), Known: true},
		WindowWeekly:   {UsedPercent: 34, WindowMinutes: 10080, ResetAt: time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC), Known: true},
	})
}

func TestParseUsageCodexRelativeReset(t *testing.T) {
	t.Parallel()

	body := []byte(`{"rate_limit":{"primary_window":{"used_percent":5,"limit_window_seconds":18000,"resets_in_seconds":1800}}}`)
	reading, err := ParseUsage("codex", body, signalBase)
	if err != nil {
		t.Fatalf("ParseUsage() error = %v", err)
	}
	window, ok := reading.Windows[WindowFiveHour]
	if !ok {
		t.Fatalf("windows = %+v, want a five hour window", reading.Windows)
	}
	if !window.ResetAt.Equal(signalBase.Add(30 * time.Minute)) {
		t.Fatalf("reset = %s, want %s", window.ResetAt, signalBase.Add(30*time.Minute))
	}
}

func TestParseUsageRejectsBadInput(t *testing.T) {
	t.Parallel()

	if _, err := ParseUsage("claude", []byte("not json"), signalBase); err == nil {
		t.Fatal("ParseUsage() on invalid JSON should error")
	}
	reading, err := ParseUsage("gemini", []byte(`{"anything":1}`), signalBase)
	if err != nil {
		t.Fatalf("ParseUsage() error = %v", err)
	}
	if len(reading.Windows) != 0 {
		t.Fatalf("windows = %+v, want none for an unpolled provider", reading.Windows)
	}
}

// TestSignalWindowsClaudeLiveHeaders pins the header set a real Claude Max
// response carried on 2026-10-03: utilization is a 0..1 fraction, resets are unix
// seconds, and the extra unified headers must not create phantom windows.
func TestSignalWindowsClaudeLiveHeaders(t *testing.T) {
	t.Parallel()

	signals := map[string]string{
		"Anthropic-Ratelimit-Unified-5h-Reset":                "1790983800",
		"Anthropic-Ratelimit-Unified-5h-Status":               "allowed",
		"Anthropic-Ratelimit-Unified-5h-Utilization":          "0.3",
		"Anthropic-Ratelimit-Unified-7d-Reset":                "1791223200",
		"Anthropic-Ratelimit-Unified-7d-Status":               "allowed",
		"Anthropic-Ratelimit-Unified-7d-Utilization":          "0.74",
		"Anthropic-Ratelimit-Unified-Fallback-Percentage":     "0.5",
		"Anthropic-Ratelimit-Unified-Overage-Disabled-Reason": "org_level_disabled",
		"Anthropic-Ratelimit-Unified-Overage-Status":          "rejected",
		"Anthropic-Ratelimit-Unified-Representative-Claim":    "five_hour",
		"Anthropic-Ratelimit-Unified-Reset":                   "1790983800",
		"Anthropic-Ratelimit-Unified-Status":                  "allowed",
	}
	got := SignalWindows("claude", signals, signalBase)
	if len(got) != 2 {
		t.Fatalf("windows = %v, want exactly five_hour and seven_day", got)
	}
	five, ok := got[WindowFiveHour]
	if !ok || !five.Known || five.UsedPercent < 29.99 || five.UsedPercent > 30.01 {
		t.Fatalf("five_hour = %+v, want 30%% used", five)
	}
	if want := time.Unix(1790983800, 0); !five.ResetAt.Equal(want) {
		t.Fatalf("five_hour reset = %v, want %v", five.ResetAt, want)
	}
	week, ok := got[WindowSevenDay]
	if !ok || !week.Known || week.UsedPercent < 73.99 || week.UsedPercent > 74.01 {
		t.Fatalf("seven_day = %+v, want 74%% used", week)
	}
	if week.HardLimited || five.HardLimited {
		t.Fatalf("allowed status must not mark a hard limit: %+v %+v", five, week)
	}
}
