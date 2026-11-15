package helps

import (
	cryptorand "crypto/rand"
	"encoding/json"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	defaultClaudeRateLimitFuzzMinSeconds = 1
	defaultClaudeRateLimitFuzzMaxSeconds = 30



	maxClaudeRateLimitWindow = 7*24*time.Hour + time.Hour
)





func ClaudeHeadersIndicateUnifiedRateLimitRejection(headers http.Header) bool {
	if headers == nil {
		return false
	}
	unifiedStatus := strings.ToLower(strings.TrimSpace(getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-Status")))
	status5h := strings.ToLower(strings.TrimSpace(getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-5h-Status")))
	if status5h == "rejected" {
		return true
	}
	status7d := strings.ToLower(strings.TrimSpace(getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-7d-Status")))
	if status7d == "rejected" {
		return true
	}
	if unifiedStatus != "rejected" {
		return false
	}
	status7dOI := strings.ToLower(strings.TrimSpace(getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-7d_oi-Status")))
	return !isOverageOrFableOnlyRejection(headers, unifiedStatus, status5h, status7d, status7dOI)
}

func isClaudeWindowAllowed(status string) bool {
	return status == "allowed" || status == "allowed_warning"
}

func isOverageOrFableOnlyRejection(headers http.Header, unifiedStatus, status5h, status7d, status7dOI string) bool {
	if status5h == "rejected" || status7d == "rejected" {
		return false
	}

	overageStatus := strings.ToLower(strings.TrimSpace(getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-Overage-Status")))
	overageDisabledReason := strings.TrimSpace(getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-Overage-Disabled-Reason"))
	representativeClaim := strings.ToLower(strings.TrimSpace(getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-Representative-Claim")))

	isOverageRejected := status7dOI == "rejected" ||
		overageStatus == "rejected" ||
		overageDisabledReason != "" ||
		strings.Contains(representativeClaim, "overage")

	if !isOverageRejected {
		return false
	}

	shared5hAllowed := isClaudeWindowAllowed(status5h)
	shared7dAllowed := isClaudeWindowAllowed(status7d)

	if shared5hAllowed && shared7dAllowed {
		return true
	}






	if shared7dAllowed && status5h == "" {
		if isClaudeUtilizationHealthy(getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-5h-Utilization")) {
			return true
		}
		if unifiedStatus == "rejected" &&
			!hasHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-5h-Utilization") {
			return true
		}
	}
	if shared5hAllowed && status7d == "" {
		if isClaudeUtilizationHealthy(getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-7d-Utilization")) {
			return true
		}
	}





	if unifiedStatus == "rejected" && status5h == "" && status7d == "" && strings.Contains(representativeClaim, "overage") {
		util5h := getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-5h-Utilization")
		util7d := getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-7d-Utilization")
		util5hAbsentOrHealthy := strings.TrimSpace(util5h) == "" || isClaudeUtilizationHealthy(util5h)
		util7dAbsentOrHealthy := strings.TrimSpace(util7d) == "" || isClaudeUtilizationHealthy(util7d)
		if util5hAbsentOrHealthy && util7dAbsentOrHealthy {
			return true
		}
	}

	return false
}

func isClaudeUtilizationHealthy(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, errParse := strconv.ParseFloat(raw, 64)
	if errParse != nil || math.IsNaN(u) || math.IsInf(u, 0) {
		return false
	}
	return u >= 0 && u < 1.0
}




var claudeQuotaStatusHeaders = []string{
	"Anthropic-Ratelimit-Unified-Status",
	"Anthropic-Ratelimit-Unified-5h-Status",
	"Anthropic-Ratelimit-Unified-7d-Status",
	"Anthropic-Ratelimit-Unified-7d_oi-Status",
	"Anthropic-Ratelimit-Unified-Overage-Status",
}








func ParseClaudeRateLimitResetForError(headers http.Header, body []byte, now time.Time) *time.Duration {
	if headers == nil || !claudeBodyIsExplicitPerMinuteRateLimit(body) || claudeHeadersHaveQuotaStatus(headers) {
		return ParseClaudeRateLimitReset(headers, now)
	}
	rawRetryAfter, ok := singleHeaderValueCaseInsensitive(headers, "Retry-After")
	if !ok {
		return ParseClaudeRateLimitReset(headers, now)
	}
	retryAt, okRetry := parseRetryAfterHeader(rawRetryAfter, now)
	if !okRetry || !retryAt.After(now) || retryAt.Sub(now) > maxClaudeRateLimitWindow {
		return ParseClaudeRateLimitReset(headers, now)
	}


	rateHeaders := headers.Clone()
	for name := range rateHeaders {
		if strings.EqualFold(name, "Anthropic-Ratelimit-Unified-Reset") {
			delete(rateHeaders, name)
		}
	}
	return ParseClaudeRateLimitReset(rateHeaders, now)
}





func claudeBodyIsExplicitPerMinuteRateLimit(body []byte) bool {
	var envelope struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if errUnmarshal := json.Unmarshal(body, &envelope); errUnmarshal != nil || envelope.Error.Type != "rate_limit_error" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(envelope.Error.Message)) {
	case "this request would exceed the requests per minute rate limit. please try again later.",
		"this request would exceed the input tokens per minute rate limit. please try again later.",
		"this request would exceed the output tokens per minute rate limit. please try again later.":
		return true
	default:
		return false
	}
}

func claudeHeadersHaveQuotaStatus(headers http.Header) bool {
	for _, name := range claudeQuotaStatusHeaders {
		for key, values := range headers {
			if !strings.EqualFold(key, name) {
				continue
			}
			for _, value := range values {
				if strings.TrimSpace(value) != "" {
					return true
				}
			}
		}
	}
	return false
}




func singleHeaderValueCaseInsensitive(headers http.Header, target string) (string, bool) {
	value := ""
	found := false
	for key, values := range headers {
		if !strings.EqualFold(key, target) {
			continue
		}
		if found || len(values) != 1 {
			return "", false
		}
		found = true
		value = values[0]
	}
	return value, found
}





func ParseClaudeRateLimitReset(headers http.Header, now time.Time) *time.Duration {
	return parseClaudeRateLimitResetWithFuzz(headers, now, defaultClaudeRateLimitFuzzMinSeconds, defaultClaudeRateLimitFuzzMaxSeconds)
}

func parseClaudeRateLimitResetWithFuzz(headers http.Header, now time.Time, minFuzzSec, maxFuzzSec int) *time.Duration {
	if headers == nil {
		return nil
	}

	unifiedStatus := strings.ToLower(strings.TrimSpace(getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-Status")))
	status5h := strings.ToLower(strings.TrimSpace(getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-5h-Status")))
	status7d := strings.ToLower(strings.TrimSpace(getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-7d-Status")))
	status7dOI := strings.ToLower(strings.TrimSpace(getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-7d_oi-Status")))
	overageOnlyRejection := isOverageOrFableOnlyRejection(headers, unifiedStatus, status5h, status7d, status7dOI)

	var candidateDeadlines []time.Time
	var rejectedWindows []string

	if unifiedStatus == "rejected" {
		rejectedWindows = append(rejectedWindows, "unified")
	}
	if status5h == "rejected" {
		rejectedWindows = append(rejectedWindows, "5h")
	}
	if status7d == "rejected" {
		rejectedWindows = append(rejectedWindows, "7d")
	}
	if status7dOI == "rejected" {
		rejectedWindows = append(rejectedWindows, "7d_oi")
	}


	if !overageOnlyRejection {
		if rawRetryAfter := getHeaderCaseInsensitive(headers, "Retry-After"); rawRetryAfter != "" {
			if !containsString(rejectedWindows, "retry-after") {
				rejectedWindows = append(rejectedWindows, "retry-after")
			}
			if t, ok := parseRetryAfterHeader(rawRetryAfter, now); ok && t.After(now) {
				candidateDeadlines = append(candidateDeadlines, t)
			}
		}
	}


	if status5h == "rejected" {
		if raw := getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-5h-Reset"); raw != "" {
			if t, ok := parseUnixOrTimestamp(raw); ok && t.After(now) {
				candidateDeadlines = append(candidateDeadlines, t)
			}
		}
	}


	if status7d == "rejected" {
		if raw := getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-7d-Reset"); raw != "" {
			if t, ok := parseUnixOrTimestamp(raw); ok && t.After(now) {
				candidateDeadlines = append(candidateDeadlines, t)
			}
		}
	}


	if status7dOI == "rejected" && !overageOnlyRejection {
		if raw := getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-7d_oi-Reset"); raw != "" {
			if t, ok := parseUnixOrTimestamp(raw); ok && t.After(now) {
				candidateDeadlines = append(candidateDeadlines, t)
			}
		}
	}


	unifiedRejected := !overageOnlyRejection && (unifiedStatus == "rejected" || status5h == "rejected" || status7d == "rejected" || status7dOI == "rejected" ||
		(unifiedStatus == "" && !isClaudeWindowAllowed(status5h) && !isClaudeWindowAllowed(status7d)))

	if unifiedRejected {
		if raw := getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-Reset"); raw != "" {
			if !containsString(rejectedWindows, "unified") {
				rejectedWindows = append(rejectedWindows, "unified")
			}



			if !claudeUnifiedResetIsOverageBillingBoundary(headers, raw) {
				if resetAt, ok := parseUnixOrTimestamp(raw); ok && resetAt.After(now) {
					candidateDeadlines = append(candidateDeadlines, resetAt)
				}
			}
		}
	}

	candidateDeadlines = filterClaudeRateLimitDeadlines(candidateDeadlines, now)
	if len(candidateDeadlines) == 0 {
		if len(rejectedWindows) > 0 {
			log.WithFields(log.Fields{
				"rejected_windows": strings.Join(rejectedWindows, ","),
				"status":           "fallback_exponential_backoff",
			}).Info("Anthropic rate limit window rejected; falling back to generic exponential backoff")
		}
		return nil
	}


	var latestDeadline time.Time
	for _, deadline := range candidateDeadlines {
		if deadline.After(latestDeadline) {
			latestDeadline = deadline
		}
	}

	if latestDeadline.IsZero() || !latestDeadline.After(now) {
		if len(rejectedWindows) > 0 {
			log.WithFields(log.Fields{
				"rejected_windows": strings.Join(rejectedWindows, ","),
				"status":           "fallback_exponential_backoff",
			}).Info("Anthropic rate limit window rejected; falling back to generic exponential backoff")
		}
		return nil
	}

	baseDuration := latestDeadline.Sub(now)
	fuzz := randomClaudeFuzzDuration(minFuzzSec, maxFuzzSec)
	effectiveDuration := baseDuration + fuzz

	log.WithFields(log.Fields{
		"rejected_windows":   strings.Join(rejectedWindows, ","),
		"effective_cooldown": effectiveDuration.String(),
		"base_cooldown":      baseDuration.String(),
		"fuzz":               fuzz.String(),
		"deadline":           latestDeadline.Format(time.RFC3339),
	}).Info("parsed Anthropic rate limit reset headers")

	return &effectiveDuration
}

func claudeUnifiedResetIsOverageBillingBoundary(headers http.Header, unifiedReset string) bool {
	claim := strings.ToLower(strings.TrimSpace(getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-Representative-Claim")))
	if !strings.Contains(claim, "overage") {
		return false
	}
	overageReset := getHeaderCaseInsensitive(headers, "Anthropic-Ratelimit-Unified-Overage-Reset")
	unifiedAt, okUnified := parseUnixOrTimestamp(unifiedReset)
	overageAt, okOverage := parseUnixOrTimestamp(overageReset)
	if !okUnified || !okOverage {
		return false
	}
	return unifiedAt.Equal(overageAt)
}

func filterClaudeRateLimitDeadlines(deadlines []time.Time, now time.Time) []time.Time {
	if len(deadlines) == 0 {
		return deadlines
	}
	kept := deadlines[:0]
	for _, deadline := range deadlines {
		if !deadline.After(now) {
			continue
		}
		if deadline.Sub(now) > maxClaudeRateLimitWindow {
			continue
		}
		kept = append(kept, deadline)
	}
	return kept
}

func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

func getHeaderCaseInsensitive(h http.Header, target string) string {
	if h == nil {
		return ""
	}
	if val := h.Get(target); val != "" {
		return val
	}
	for k, v := range h {
		if strings.EqualFold(k, target) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

func hasHeaderCaseInsensitive(h http.Header, target string) bool {
	if h == nil {
		return false
	}
	for k, v := range h {
		if strings.EqualFold(k, target) && len(v) > 0 {
			return true
		}
	}
	return false
}

func parseUnixOrTimestamp(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if sec, err := strconv.ParseFloat(raw, 64); err == nil && sec > 0 {
		secInt := int64(sec)
		nsec := int64((sec - float64(secInt)) * 1e9)
		return time.Unix(secInt, nsec), true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	if t, err := http.ParseTime(raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func parseRetryAfterHeader(raw string, now time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if sec, err := strconv.ParseFloat(raw, 64); err == nil && sec > 0 {
		d := time.Duration(sec * float64(time.Second))
		return now.Add(d), true
	}
	if t, err := http.ParseTime(raw); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func randomClaudeFuzzDuration(minSec, maxSec int) time.Duration {
	if maxSec <= minSec {
		if minSec < 0 {
			return 0
		}
		return time.Duration(minSec) * time.Second
	}
	nBig, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(maxSec-minSec+1)))
	if err != nil {
		return time.Duration(minSec) * time.Second
	}
	return time.Duration(minSec+int(nBig.Int64())) * time.Second
}
