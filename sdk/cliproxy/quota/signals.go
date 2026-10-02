package quota

import (
	"strconv"
	"strings"
	"time"
)

// unixEpochFloor separates a unix second count from a relative second offset:
// every plausible absolute reset is far above this bound.
const unixEpochFloor = 1_000_000_000

// SignalWindows turns the passive header snapshot upstream already keeps in
// Auth.Quota.Signals into canonical window readings.
//
// The snapshot is a flat map of header name to value, lower-cased by the
// collector. Upstream only allowlists prefixes (anthropic-ratelimit-unified-*,
// x-codex-*), never an exhaustive header list, so this parser matches the same
// way: it groups keys by the window segment and reads the suffix. A spelling
// this parser does not recognise is ignored rather than guessed at.
func SignalWindows(provider string, signals map[string]string, observedAt time.Time) map[string]Window {
	if len(signals) == 0 {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude", "anthropic":
		return claudeSignalWindows(signals, observedAt)
	case "codex":
		return codexSignalWindows(signals, observedAt)
	default:
		return nil
	}
}

// SignalPlan reads the plan slug out of a header snapshot when one is present.
func SignalPlan(provider string, signals map[string]string) string {
	if len(signals) == 0 {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(provider), "codex") {
		for name, value := range signals {
			if strings.EqualFold(strings.TrimSpace(name), "x-codex-plan-type") {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

// claudeSignalWindows reads anthropic-ratelimit-unified-<window>-<field>.
//
// Observed field spellings are "-remaining" plus "-limit" (a pair that yields a
// used percentage), "-utilization" (a 0..1 fraction), "-used-percent" / "-percent" (a direct
// percentage), "-reset" / "-reset-at" (a unix second count or an RFC3339 time)
// and "-status" (whose "rate_limited" value marks a hard limit).
func claudeSignalWindows(signals map[string]string, observedAt time.Time) map[string]Window {
	const prefix = "anthropic-ratelimit-unified-"
	type accumulator struct {
		used      float64
		hasUsed   bool
		remaining float64
		hasRem    bool
		limit     float64
		hasLimit  bool
		resetAt   time.Time
		limited   bool
	}
	grouped := make(map[string]*accumulator)
	for rawName, rawValue := range signals {
		name := strings.ToLower(strings.TrimSpace(rawName))
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := strings.TrimPrefix(name, prefix)
		window, field := splitWindowField(rest)
		canonical := canonicalClaudeWindow(window)
		if canonical == "" {
			continue
		}
		entry := grouped[canonical]
		if entry == nil {
			entry = &accumulator{}
			grouped[canonical] = entry
		}
		value := strings.TrimSpace(rawValue)
		switch field {
		case "utilization":
			// Anthropic reports utilization as a fraction: "0.74" means 74% used.
			if parsed, ok := parseFloat(value); ok {
				entry.used, entry.hasUsed = parsed*100, true
			}
		case "used-percent", "usedpercent", "percent":
			if parsed, ok := parseFloat(value); ok {
				entry.used, entry.hasUsed = parsed, true
			}
		case "remaining":
			if parsed, ok := parseFloat(value); ok {
				entry.remaining, entry.hasRem = parsed, true
			}
		case "limit":
			if parsed, ok := parseFloat(value); ok {
				entry.limit, entry.hasLimit = parsed, true
			}
		case "reset", "reset-at", "resets-at":
			if parsed, ok := parseTimestamp(value, observedAt); ok {
				entry.resetAt = parsed
			}
		case "reset-after-seconds", "resets-in-seconds":
			if parsed, ok := parseFloat(value); ok {
				entry.resetAt = observedAt.Add(time.Duration(parsed) * time.Second)
			}
		case "status":
			if isLimitedStatus(value) {
				entry.limited = true
			}
		}
	}
	if len(grouped) == 0 {
		return nil
	}
	out := make(map[string]Window, len(grouped))
	for canonical, entry := range grouped {
		window := Window{ResetAt: entry.resetAt, LearnedAt: observedAt, HardLimited: entry.limited}
		switch {
		case entry.hasUsed:
			window.UsedPercent, window.Known = clampPercent(entry.used), true
		case entry.hasRem && entry.hasLimit && entry.limit > 0:
			window.UsedPercent = clampPercent((entry.limit - entry.remaining) / entry.limit * 100)
			window.Known = true
		case entry.limited:
			// A status header with no numbers still tells the selector to skip
			// this credential, so keep the reading as a hard limit marker.
			window.UsedPercent, window.Known = 100, true
		default:
			continue
		}
		out[canonical] = window
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// codexSignalWindows reads x-codex-<slot>-<field>. The primary slot is the
// 5-hour window and the secondary slot the weekly one, which the reported
// window-minutes confirms when present.
func codexSignalWindows(signals map[string]string, observedAt time.Time) map[string]Window {
	type accumulator struct {
		used     float64
		hasUsed  bool
		minutes  int
		resetAt  time.Time
		limited  bool
		hasReset bool
	}
	grouped := make(map[string]*accumulator)
	for rawName, rawValue := range signals {
		name := strings.ToLower(strings.TrimSpace(rawName))
		slot := ""
		switch {
		case strings.HasPrefix(name, "x-codex-primary-"):
			slot, name = "primary", strings.TrimPrefix(name, "x-codex-primary-")
		case strings.HasPrefix(name, "x-codex-secondary-"):
			slot, name = "secondary", strings.TrimPrefix(name, "x-codex-secondary-")
		default:
			continue
		}
		entry := grouped[slot]
		if entry == nil {
			entry = &accumulator{}
			grouped[slot] = entry
		}
		value := strings.TrimSpace(rawValue)
		switch name {
		case "used-percent", "usedpercent":
			if parsed, ok := parseFloat(value); ok {
				entry.used, entry.hasUsed = parsed, true
			}
		case "window-minutes":
			if parsed, errParse := strconv.Atoi(value); errParse == nil && parsed > 0 {
				entry.minutes = parsed
			}
		case "reset-at":
			if parsed, ok := parseTimestamp(value, observedAt); ok {
				entry.resetAt, entry.hasReset = parsed, true
			}
		case "reset-after-seconds":
			if parsed, ok := parseFloat(value); ok && !entry.hasReset {
				entry.resetAt, entry.hasReset = observedAt.Add(time.Duration(parsed)*time.Second), true
			}
		case "limit-reached":
			if isLimitedStatus(value) {
				entry.limited = true
			}
		}
	}
	if len(grouped) == 0 {
		return nil
	}
	out := make(map[string]Window, len(grouped))
	for slot, entry := range grouped {
		canonical := canonicalCodexWindow(slot, entry.minutes)
		if canonical == "" {
			continue
		}
		window := Window{
			ResetAt:       entry.resetAt,
			WindowMinutes: entry.minutes,
			LearnedAt:     observedAt,
			HardLimited:   entry.limited,
		}
		switch {
		case entry.hasUsed:
			window.UsedPercent, window.Known = clampPercent(entry.used), true
		case entry.limited:
			window.UsedPercent, window.Known = 100, true
		default:
			continue
		}
		out[canonical] = window
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// splitWindowField splits "7d-remaining" into "7d" and "remaining". A single
// segment is treated as a field on the account-wide window.
func splitWindowField(rest string) (window, field string) {
	rest = strings.Trim(rest, "-")
	if rest == "" {
		return "", ""
	}
	index := strings.Index(rest, "-")
	if index <= 0 {
		return "", rest
	}
	return rest[:index], rest[index+1:]
}

// canonicalClaudeWindow maps an observed window segment onto a canonical id.
func canonicalClaudeWindow(window string) string {
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "5h", "5hour", "5-hour", "five-hour", "five_hour", "session":
		return WindowFiveHour
	case "7d", "7day", "7-day", "seven-day", "seven_day", "week", "weekly":
		return WindowSevenDay
	case "fable", "opus", "iguana", "iguana_necktie":
		return WindowFable
	default:
		return ""
	}
}

// canonicalCodexWindow maps a Codex slot onto a canonical id. The reported
// window length wins when it is present, because Codex reuses the slot names.
func canonicalCodexWindow(slot string, minutes int) string {
	if minutes > 0 {
		if minutes <= 360 {
			return WindowFiveHour
		}
		return WindowWeekly
	}
	switch slot {
	case "primary":
		return WindowFiveHour
	case "secondary":
		return WindowWeekly
	default:
		return ""
	}
}

func parseFloat(value string) (float64, bool) {
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "%"))
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// parseTimestamp accepts an RFC3339 time or a unix second count. A value small
// enough to be a relative second offset is treated as one, which is how the
// Anthropic reset header behaves on some responses.
func parseTimestamp(value string, observedAt time.Time) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), true
	}
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}, false
	}
	if seconds < unixEpochFloor {
		return observedAt.Add(time.Duration(seconds) * time.Second), true
	}
	return time.Unix(int64(seconds), 0).UTC(), true
}

func isLimitedStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "rate_limited", "rate-limited", "limited", "exceeded", "allowed_warning_rate_limited":
		return true
	default:
		return false
	}
}
