package quota

import (
	"encoding/json"
	"strings"
	"time"
)

// UsageReading is the parsed result of a provider usage endpoint response.
type UsageReading struct {
	// Plan is the provider's plan slug when the response carries one.
	Plan string
	// Windows holds the canonical window readings found in the response.
	Windows map[string]Window
}

// ParseUsage turns a provider usage endpoint body into canonical windows.
//
// Claude answers GET api.anthropic.com/api/oauth/usage, Codex answers
// GET chatgpt.com/backend-api/wham/usage. Both shapes are read defensively: an
// unrecognised body yields no windows rather than an error, so a provider
// changing its response degrades the credential to "no data" instead of
// breaking routing.
func ParseUsage(provider string, body []byte, learnedAt time.Time) (UsageReading, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return UsageReading{}, err
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude", "anthropic":
		return parseClaudeUsage(raw, learnedAt), nil
	case "codex":
		return parseCodexUsage(raw, learnedAt), nil
	default:
		return UsageReading{}, nil
	}
}

// parseClaudeUsage reads the limits array, then the flat per-window fallback.
//
// In the array form each entry carries kind ("session" is the 5-hour window),
// group ("weekly" is the 7-day window), percent and resets_at. A Fable-scoped
// weekly entry becomes the per-model fable window. In the flat form the keys
// are five_hour, seven_day and iguana_necktie, which is the Fable window.
func parseClaudeUsage(raw map[string]any, learnedAt time.Time) UsageReading {
	out := UsageReading{Plan: stringField(raw, "plan_type"), Windows: make(map[string]Window)}
	for _, item := range arrayField(raw, "limits") {
		entry := objectOf(item)
		if entry == nil {
			continue
		}
		kind := strings.ToLower(stringField(entry, "kind"))
		group := strings.ToLower(stringField(entry, "group"))
		canonical := ""
		switch {
		case kind == "session":
			canonical = WindowFiveHour
		case group == "weekly" || kind == "seven_day" || kind == "weekly":
			canonical = WindowSevenDay
		}
		if canonical == "" {
			continue
		}
		// A weekly window scoped to a single model is a per-model window. Fable is
		// the only one the routing rule consults.
		if canonical == WindowSevenDay && scopeMentionsFable(entry) {
			canonical = WindowFable
		}
		percent, hasPercent := floatField(entry, "percent")
		resetAt, hasReset := timeField(entry, "resets_at")
		if !hasPercent && !hasReset {
			continue
		}
		window := Window{LearnedAt: learnedAt, Known: hasPercent}
		if hasPercent {
			window.UsedPercent = clampPercent(percent)
		}
		if hasReset {
			window.ResetAt = resetAt
		}
		if strings.EqualFold(stringField(entry, "severity"), "limit_reached") {
			window.HardLimited = true
		}
		mergeWindow(out.Windows, canonical, window)
	}
	if len(out.Windows) > 0 {
		return out
	}
	for key, value := range raw {
		canonical := ""
		switch {
		case key == "iguana_necktie":
			canonical = WindowFable
		case strings.HasPrefix(key, "five_hour"):
			canonical = WindowFiveHour
		case strings.HasPrefix(key, "seven_day") && key != "seven_day_breakdown":
			canonical = WindowSevenDay
		default:
			continue
		}
		entry := objectOf(value)
		if entry == nil {
			continue
		}
		percent, hasPercent := floatField(entry, "utilization")
		resetAt, hasReset := timeField(entry, "resets_at")
		if !hasPercent && !hasReset {
			continue
		}
		window := Window{LearnedAt: learnedAt, Known: hasPercent}
		if hasPercent {
			window.UsedPercent = clampPercent(percent)
		}
		if hasReset {
			window.ResetAt = resetAt
		}
		mergeWindow(out.Windows, canonical, window)
	}
	return out
}

// parseCodexUsage reads rate_limit.primary_window and secondary_window. The
// reported limit_window_seconds decides the canonical id: 18000 is the 5-hour
// window and 604800 the weekly one.
func parseCodexUsage(raw map[string]any, learnedAt time.Time) UsageReading {
	out := UsageReading{Plan: stringField(raw, "plan_type"), Windows: make(map[string]Window)}
	limits := objectOf(raw["rate_limit"])
	if limits == nil {
		limits = objectOf(raw["rate_limits"])
	}
	if limits == nil {
		return out
	}
	for slot, key := range map[string]string{"primary": "primary_window", "secondary": "secondary_window"} {
		entry := objectOf(limits[key])
		if entry == nil {
			continue
		}
		minutes := 0
		if seconds, ok := floatField(entry, "limit_window_seconds"); ok && seconds > 0 {
			minutes = int(seconds / 60)
		}
		canonical := canonicalCodexWindow(slot, minutes)
		if canonical == "" {
			continue
		}
		percent, hasPercent := floatField(entry, "used_percent")
		resetAt, hasReset := timeField(entry, "reset_at")
		if !hasReset {
			if seconds, ok := floatField(entry, "resets_in_seconds"); ok && seconds >= 0 {
				resetAt, hasReset = learnedAt.Add(time.Duration(seconds)*time.Second), true
			}
		}
		if !hasPercent && !hasReset {
			continue
		}
		window := Window{LearnedAt: learnedAt, WindowMinutes: minutes, Known: hasPercent}
		if hasPercent {
			window.UsedPercent = clampPercent(percent)
		}
		if hasReset {
			window.ResetAt = resetAt
		}
		mergeWindow(out.Windows, canonical, window)
	}
	return out
}

// mergeWindow keeps the reading that reports more usage when a response carries
// the same canonical window twice, so a pooled surface cannot hide a limit.
func mergeWindow(windows map[string]Window, canonical string, window Window) {
	previous, exists := windows[canonical]
	if !exists {
		windows[canonical] = window
		return
	}
	if window.UsedPercent > previous.UsedPercent || (!previous.Known && window.Known) {
		if previous.HardLimited {
			window.HardLimited = true
		}
		windows[canonical] = window
	}
}

// scopeMentionsFable reports whether a Claude limit entry is scoped to Fable.
func scopeMentionsFable(entry map[string]any) bool {
	scope := objectOf(entry["scope"])
	if scope == nil {
		return false
	}
	model := objectOf(scope["model"])
	for _, candidate := range []string{
		stringField(model, "id"),
		stringField(model, "display_name"),
		stringField(scope, "model"),
	} {
		if strings.Contains(strings.ToLower(candidate), "fable") {
			return true
		}
	}
	return false
}

func objectOf(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func arrayField(raw map[string]any, key string) []any {
	if raw == nil {
		return nil
	}
	if typed, ok := raw[key].([]any); ok {
		return typed
	}
	return nil
}

func stringField(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	if typed, ok := raw[key].(string); ok {
		return strings.TrimSpace(typed)
	}
	return ""
}

func floatField(raw map[string]any, key string) (float64, bool) {
	if raw == nil {
		return 0, false
	}
	switch typed := raw[key].(type) {
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		return parseFloat(typed)
	default:
		return 0, false
	}
}

func timeField(raw map[string]any, key string) (time.Time, bool) {
	if raw == nil {
		return time.Time{}, false
	}
	// Usage bodies always carry an absolute reset, so a relative offset is not
	// accepted here: it would otherwise land near the zero time.
	switch typed := raw[key].(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return time.Time{}, false
		}
		if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
			return parsed.UTC(), true
		}
		seconds, ok := parseFloat(trimmed)
		if !ok || seconds < unixEpochFloor {
			return time.Time{}, false
		}
		return time.Unix(int64(seconds), 0).UTC(), true
	case float64:
		if typed < unixEpochFloor {
			return time.Time{}, false
		}
		return time.Unix(int64(typed), 0).UTC(), true
	default:
		return time.Time{}, false
	}
}
