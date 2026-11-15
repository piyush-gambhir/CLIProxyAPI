package quota

import (
	"sort"
	"strings"
	"time"
)

const DefaultFiveHourGuardPercent = 90.0

const minHoursToReset = 0.25

const (
	ReasonRanked = "ranked"

	ReasonGuarded = "five_hour_guard"

	ReasonNoData = "no_data"

	ReasonShortWindowExhausted = "five_hour_exhausted"

	ReasonShortWindowHardLimited = "five_hour_hard_limited"

	ReasonModelWindowExhausted = "model_window_exhausted"

	ReasonModelWindowHardLimited = "model_window_hard_limited"

	ReasonLongWindowHardLimited = "long_window_hard_limited"
)

type Candidate struct {
	AuthID string

	Provider string

	Reading Reading
}

type RankOptions struct {
	Model string

	FiveHourGuardPercent float64

	Now time.Time
}

type Ranked struct {
	AuthID string

	Provider string

	Urgency float64

	Usable bool

	Reason string

	RankingWindow string

	Known bool

	Guarded bool

	ResetAt time.Time
}

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

func rankOne(candidate Candidate, modelWindow string, guard float64, now time.Time) Ranked {
	result := Ranked{
		AuthID:   candidate.AuthID,
		Provider: candidate.Provider,
		Usable:   true,
		Reason:   ReasonNoData,
	}

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

		return result
	}
	result.Known = true
	result.RankingWindow = windowID
	result.ResetAt = window.ResetAt
	result.Urgency = Urgency(window, now)

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

	if perModel.UsedPercent > main.UsedPercent {
		return modelWindow, perModel, true
	}
	return mainID, main, true
}

func longWindow(reading Reading) (string, Window, bool) {
	for _, id := range []string{WindowSevenDay, WindowWeekly} {
		if window, ok := reading.Window(id); ok && window.Known {
			return id, window, true
		}
	}

	if window, ok := reading.Window(WindowFiveHour); ok && window.Known {
		return WindowFiveHour, window, true
	}
	return "", Window{}, false
}

func PerModelWindow(model string) string {
	if strings.Contains(strings.ToLower(strings.TrimSpace(model)), "fable") {
		return WindowFable
	}
	return ""
}

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
