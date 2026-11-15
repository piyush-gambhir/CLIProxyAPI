package thinking

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)



var levelToBudgetMap = map[string]int{
	"none":    0,
	"auto":    -1,
	"minimal": 512,
	"low":     1024,
	"medium":  8192,
	"high":    24576,
	"xhigh":   32768,


	"max": 128000,
}



















func ConvertLevelToBudget(level string) (int, bool) {
	budget, ok := levelToBudgetMap[strings.ToLower(level)]
	return budget, ok
}



const (

	ThresholdMinimal = 512

	ThresholdLow = 1024

	ThresholdMedium = 8192

	ThresholdHigh = 24576
)


















func ConvertBudgetToLevel(budget int) (string, bool) {
	switch {
	case budget < -1:

		return "", false
	case budget == -1:
		return string(LevelAuto), true
	case budget == 0:
		return string(LevelNone), true
	case budget <= ThresholdMinimal:
		return string(LevelMinimal), true
	case budget <= ThresholdLow:
		return string(LevelLow), true
	case budget <= ThresholdMedium:
		return string(LevelMedium), true
	case budget <= ThresholdHigh:
		return string(LevelHigh), true
	default:
		return string(LevelXHigh), true
	}
}



func HasLevel(levels []string, target string) bool {
	for _, level := range levels {
		if strings.EqualFold(strings.TrimSpace(level), target) {
			return true
		}
	}
	return false
}






func MapToClaudeEffort(level string, supportsMax bool) (string, bool) {
	level = strings.ToLower(strings.TrimSpace(level))
	switch level {
	case "":
		return "", false
	case "minimal":
		return "low", true
	case "low", "medium", "high":
		return level, true
	case "xhigh", "max":
		if supportsMax {
			return "max", true
		}
		return "high", true
	case "auto":
		return "high", true
	default:
		return "", false
	}
}


type ModelCapability int

const (

	CapabilityUnknown ModelCapability = iota - 1

	CapabilityNone

	CapabilityBudgetOnly

	CapabilityLevelOnly

	CapabilityHybrid
)











func detectModelCapability(modelInfo *registry.ModelInfo) ModelCapability {
	if modelInfo == nil {
		return CapabilityUnknown
	}
	if modelInfo.Thinking == nil {
		return CapabilityNone
	}
	support := modelInfo.Thinking
	hasBudget := support.Min > 0 || support.Max > 0
	hasLevels := len(support.Levels) > 0

	switch {
	case hasBudget && hasLevels:
		return CapabilityHybrid
	case hasBudget:
		return CapabilityBudgetOnly
	case hasLevels:
		return CapabilityLevelOnly
	default:
		return CapabilityNone
	}
}
