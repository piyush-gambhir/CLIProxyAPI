package console

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
)

type ManagementCall func(method, path string, body any) (Object, error)

func LoopbackURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	h := u.Hostname()
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}
func list(v any) []any { a, _ := v.([]any); return a }
func number(v any) (float64, bool) {
	n, ok := v.(float64)
	return n, ok && !math.IsNaN(n) && !math.IsInf(n, 0)
}
func remaining(v any) any {
	if n, ok := number(v); ok {
		return math.Max(0, math.Min(100, 100-n))
	}
	return nil
}
func date(v any) any {
	if n, ok := number(v); ok && n > 0 && n < 253402300800 {
		return time.Unix(int64(n), 0).UTC().Format(time.RFC3339)
	}
	if t, err := time.Parse(time.RFC3339Nano, Text(v)); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	return nil
}
func bounded(v any) string {
	s := Text(v)
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}
func first(values ...any) string {
	for _, v := range values {
		if s := bounded(v); s != "" {
			return s
		}
	}
	return ""
}
func NormalizeUsage(provider string, raw Object) Object {
	windows := []Object{}
	out := Object{"checkedAt": time.Now().UTC().Format(time.RFC3339Nano), "plan": nil}
	if plan := bounded(raw["plan_type"]); plan != "" {
		out["plan"] = plan
	}
	if provider == "codex" {
		limits := Map(raw["rate_limit"])
		for _, key := range []string{"primary_window", "secondary_window"} {
			w := Map(limits[key])
			if len(w) == 0 {
				continue
			}
			label := "Primary limit"
			if key == "secondary_window" {
				label = "Secondary limit"
			}
			if n, ok := number(w["limit_window_seconds"]); ok {
				if n == 18000 {
					label = "5-hour limit"
				} else if n == 604800 {
					label = "Weekly limit"
				} else {
					label = fmt.Sprintf("%g-hour limit", n/3600)
				}
			}
			windows = append(windows, Object{"label": label, "remainingPercent": remaining(w["used_percent"]), "resetsAt": date(w["reset_at"])})
		}
	} else if provider == "claude" || provider == "anthropic" {
		for _, v := range list(raw["limits"]) {
			w := Map(v)
			kind := bounded(w["kind"])
			_, hasNumber := number(w["percent"])
			if kind == "" || !hasNumber && date(w["resets_at"]) == nil {
				continue
			}
			scope := Map(w["scope"])
			model, surface := Map(scope["model"]), Map(scope["surface"])
			group := strings.ReplaceAll(kind, "_", " ")
			if kind == "session" {
				group = "5-hour limit"
			} else if w["group"] == "weekly" {
				group = "Weekly limit"
			}
			parts := []string{group}
			for _, p := range []string{first(model["display_name"], model["id"]), first(surface["display_name"], surface["id"], scope["surface"])} {
				if p != "" {
					parts = append(parts, p)
				}
			}
			windows = append(windows, Object{"label": strings.Join(parts, " · "), "remainingPercent": remaining(w["percent"]), "resetsAt": date(w["resets_at"]), "active": w["is_active"] == true, "severity": bounded(w["severity"])})
		}
		if len(windows) == 0 {
			for key, v := range raw {
				if !strings.HasPrefix(key, "five_hour") && !strings.HasPrefix(key, "seven_day") && key != "iguana_necktie" {
					continue
				}
				w := Map(v)
				if w["utilization"] == nil && w["resets_at"] == nil {
					continue
				}
				label := strings.NewReplacer("five_hour", "5-hour", "seven_day", "Weekly", "_", " ").Replace(key)
				if key == "iguana_necktie" {
					label = "Weekly · Fable"
				}
				windows = append(windows, Object{"label": label, "remainingPercent": remaining(w["utilization"]), "resetsAt": date(w["resets_at"])})
			}
		}
		out["extraUsageEnabled"] = nil
		extra, spend := Map(raw["extra_usage"]), Map(raw["spend"])
		if b, ok := spend["enabled"].(bool); ok {
			out["extraUsageEnabled"] = b
		} else if b, ok = extra["is_enabled"].(bool); ok {
			out["extraUsageEnabled"] = b
		}
		breakdown := Map(raw["seven_day_breakdown"])
		out["breakdownCheckedAt"] = date(breakdown["as_of"])
		rows := []Object{}
		for _, v := range list(breakdown["rows"]) {
			r := Map(v)
			if n, ok := number(r["percent"]); ok && bounded(r["display_name"]) != "" {
				rows = append(rows, Object{"label": bounded(r["display_name"]), "percent": n})
			}
		}
		out["usageBreakdown"] = rows
	} else {
		sub := Map(raw["subscription"])
		out["plan"] = first(sub["plan"], sub["tierName"])
		for _, g := range list(raw["groups"]) {
			group := Map(g)
			for _, b := range list(group["buckets"]) {
				w := Map(b)
				var pct any
				if n, ok := number(w["remainingFraction"]); ok {
					pct = math.Max(0, math.Min(100, n*100))
				}
				windows = append(windows, Object{"label": strings.Trim(bounded(group["displayName"])+" · "+bounded(w["window"]), " ·"), "remainingPercent": pct, "resetsAt": date(w["resetTime"])})
			}
		}
	}
	out["windows"] = windows
	return out
}
func FetchUsage(call ManagementCall, authFile string) (Object, error) {
	files, err := call("GET", "auth-files", nil)
	if err != nil {
		return nil, err
	}
	var file Object
	for _, v := range list(files["files"]) {
		f := Map(v)
		if f["name"] == authFile {
			file = f
			break
		}
	}
	if Text(file["auth_index"]) == "" {
		return nil, Fail(409, "Subscription account is missing on the proxy")
	}
	provider := Text(file["provider"])
	if provider != "claude" && provider != "anthropic" && provider != "codex" {
		if file["supports_quota"] != true {
			return nil, Fail(422, "Live usage is unavailable for this provider")
		}
		result, err := call("POST", "quota/fetch", Object{"auth_index": file["auth_index"]})
		if err != nil {
			return nil, err
		}
		return NormalizeUsage("plugin", result), nil
	}
	headers := Object{"Authorization": "Bearer $TOKEN$", "Content-Type": "application/json"}
	endpoint := "https://api.anthropic.com/api/oauth/usage"
	if provider == "codex" {
		endpoint = "https://chatgpt.com/backend-api/wham/usage"
		headers["User-Agent"] = "codex-tui/0.149.1"
		if id := Text(Map(file["id_token"])["chatgpt_account_id"]); id != "" {
			headers["Chatgpt-Account-Id"] = id
		}
	} else {
		headers["anthropic-beta"] = "oauth-2025-04-20"
	}
	probe := func(url string) (Object, error) {
		result, err := call("POST", "api-call", Object{"auth_index": file["auth_index"], "method": "GET", "header": headers, "url": url})
		if err != nil {
			return nil, err
		}
		status, _ := number(result["status_code"])
		if status < 200 || status >= 300 {
			return nil, Fail(502, fmt.Sprintf("Provider usage returned %g. Saved usage is unchanged.", status))
		}
		var raw Object
		if json.Unmarshal([]byte(Text(result["body"])), &raw) != nil || raw == nil {
			return nil, Fail(502, "Provider returned unreadable usage")
		}
		return raw, nil
	}
	raw, err := probe(endpoint)
	if err != nil {
		return nil, err
	}
	usage := NormalizeUsage(provider, raw)
	if provider != "codex" {
		profile, err := probe("https://api.anthropic.com/api/oauth/profile")
		if err != nil {
			usage["planCheckUnavailable"] = true
		} else {
			account, org := Map(profile["account"]), Map(profile["organization"])
			plan := bounded(org["organization_type"])
			if account["has_claude_max"] == true {
				plan = "Claude Max"
			} else if account["has_claude_pro"] == true {
				plan = "Claude Pro"
			}
			tier := bounded(org["rate_limit_tier"])
			if tier == "default_claude_max_20x" {
				tier = "20×"
			}
			if tier == "default_claude_max_5x" {
				tier = "5×"
			}
			if plan == "Claude Max" && (tier == "20×" || tier == "5×") {
				plan += " (" + tier + ")"
			}
			usage["plan"] = plan
			usage["planTier"] = tier
			usage["subscriptionStatus"] = bounded(org["subscription_status"])
		}
	}
	return usage, nil
}
