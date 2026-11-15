package helps

import (
	"strings"
	"time"
)

type DevinQuotaPeriod int

const (
	DevinQuotaPeriodNone DevinQuotaPeriod = iota

	DevinQuotaPeriodDaily

	DevinQuotaPeriodWeekly
)

const devinQuotaResetMaxDelay = 8 * 24 * time.Hour

func DevinUsageQuotaExhaustionPeriod(message string) DevinQuotaPeriod {
	lower := strings.ToLower(message)
	if !strings.Contains(lower, "quota") || !strings.Contains(lower, "exhausted") {
		return DevinQuotaPeriodNone
	}
	if strings.Contains(lower, "weekly") {
		return DevinQuotaPeriodWeekly
	}
	if strings.Contains(lower, "daily") {
		return DevinQuotaPeriodDaily
	}
	return DevinQuotaPeriodNone
}

func DevinQuotaRetryAfter(resetAtSignal string, now time.Time) *time.Duration {
	resetAt, errParse := time.Parse(time.RFC3339, strings.TrimSpace(resetAtSignal))
	if errParse != nil {
		return nil
	}
	delay := resetAt.Sub(now)
	if delay <= 0 || delay > devinQuotaResetMaxDelay {
		return nil
	}
	return &delay
}
