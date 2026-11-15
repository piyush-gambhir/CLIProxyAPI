
package thinking

import (
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	log "github.com/sirupsen/logrus"
)



























func ValidateConfig(config ThinkingConfig, modelInfo *registry.ModelInfo, fromFormat, toFormat string, fromSuffix bool) (*ThinkingConfig, error) {
	fromFormat, toFormat = strings.ToLower(strings.TrimSpace(fromFormat)), strings.ToLower(strings.TrimSpace(toFormat))
	model := "unknown"
	support := (*registry.ThinkingSupport)(nil)
	if modelInfo != nil {
		if modelInfo.ID != "" {
			model = modelInfo.ID
		}
		support = modelInfo.Thinking
	}

	if support == nil {
		if config.Mode != ModeNone {
			return nil, NewThinkingErrorWithModel(ErrThinkingNotSupported, "thinking not supported for this model", model)
		}
		return &config, nil
	}










	toCapability := detectModelCapability(modelInfo)
	toHasLevelSupport := toCapability == CapabilityLevelOnly || toCapability == CapabilityHybrid
	modelFamilyMismatch := false
	if modelInfo != nil {
		modelType := strings.ToLower(strings.TrimSpace(modelInfo.Type))
		if isKnownProviderFamily(modelType) {
			if (fromFormat != "" && !isSameProviderFamily(fromFormat, modelType)) ||
				(toFormat != "" && !isSameProviderFamily(toFormat, modelType)) {
				modelFamilyMismatch = true
			}
		}
	}
	allowClampUnsupported := toHasLevelSupport && (!isSameProviderFamily(fromFormat, toFormat) || modelFamilyMismatch)





	strictBudget := !fromSuffix && fromFormat != "" && isSameProviderFamily(fromFormat, toFormat) && !modelFamilyMismatch
	budgetDerivedFromLevel := false

	capability := detectModelCapability(modelInfo)
	switch capability {
	case CapabilityBudgetOnly:
		if config.Mode == ModeLevel {
			if config.Level == LevelAuto {
				break
			}
			budget, ok := ConvertLevelToBudget(string(config.Level))
			if !ok {
				return nil, NewThinkingError(ErrUnknownLevel, fmt.Sprintf("unknown level: %s", config.Level))
			}
			config.Mode = ModeBudget
			config.Budget = budget
			config.Level = ""
			budgetDerivedFromLevel = true
		}
	case CapabilityLevelOnly:
		if config.Mode == ModeBudget {
			level, ok := ConvertBudgetToLevel(config.Budget)
			if !ok {
				return nil, NewThinkingError(ErrUnknownLevel, fmt.Sprintf("budget %d cannot be converted to a valid level", config.Budget))
			}


			config.Mode = ModeLevel
			config.Level = clampLevel(ThinkingLevel(level), modelInfo, toFormat)
			config.Budget = 0
		}
	case CapabilityHybrid:
	}

	if config.Mode == ModeLevel && config.Level == LevelNone {
		config.Mode = ModeNone
		config.Budget = 0
		config.Level = ""
	}
	if config.Mode == ModeLevel && config.Level == LevelAuto {
		config.Mode = ModeAuto
		config.Budget = -1
		config.Level = ""
	}
	if config.Mode == ModeBudget && config.Budget == 0 {
		config.Mode = ModeNone
		config.Level = ""
	}

	if len(support.Levels) > 0 && config.Mode == ModeLevel {
		if !isLevelSupported(string(config.Level), support.Levels) {
			if allowClampUnsupported {
				config.Level = clampLevel(config.Level, modelInfo, toFormat)
			}
			if !isLevelSupported(string(config.Level), support.Levels) {


				validLevels := normalizeLevels(support.Levels)
				message := fmt.Sprintf("level %q not supported, valid levels: %s", strings.ToLower(string(config.Level)), strings.Join(validLevels, ", "))
				return nil, NewThinkingError(ErrLevelNotSupported, message)
			}
		}
	}

	if strictBudget && config.Mode == ModeBudget && !budgetDerivedFromLevel {
		min, max := support.Min, support.Max
		if min != 0 || max != 0 {
			if config.Budget < min || config.Budget > max || (config.Budget == 0 && !support.ZeroAllowed) {
				message := fmt.Sprintf("budget %d out of range [%d,%d]", config.Budget, min, max)
				return nil, NewThinkingError(ErrBudgetOutOfRange, message)
			}
		}
	}


	if config.Mode == ModeAuto && !support.DynamicAllowed {
		config = convertAutoToMidRange(config, support, toFormat, model)




		if config.Mode == ModeLevel && len(support.Levels) > 0 && !isLevelSupported(string(config.Level), support.Levels) {
			config.Level = clampLevel(config.Level, modelInfo, toFormat)
		}
	}

	if config.Mode == ModeNone && toFormat == "claude" {
		if !support.ZeroAllowed {
			if lowest, ok := lowestSupportedLevel(support.Levels); ok {



				config.Mode = ModeLevel
				config.Budget = 0
				config.Level = lowest
			} else {
				config.Budget = 0
				config.Level = ""
			}
		} else {


			config.Budget = 0
			config.Level = ""
		}
	} else {
		switch config.Mode {
		case ModeBudget, ModeAuto, ModeNone:
			config.Budget = clampBudget(config.Budget, modelInfo, toFormat)
		}





		cannotDisableLevelModel := !support.ZeroAllowed && !isLevelSupported(string(LevelNone), support.Levels)
		if config.Mode == ModeNone && len(support.Levels) > 0 && (config.Budget > 0 || cannotDisableLevelModel) {
			if lowest, ok := lowestSupportedLevel(support.Levels); ok {
				config.Level = lowest
			}
		}
	}

	return &config, nil
}











func convertAutoToMidRange(config ThinkingConfig, support *registry.ThinkingSupport, provider, model string) ThinkingConfig {

	if len(support.Levels) > 0 && support.Min == 0 && support.Max == 0 {
		config.Mode = ModeLevel
		config.Level = LevelMedium
		config.Budget = 0
		log.WithFields(log.Fields{
			"provider":      provider,
			"model":         model,
			"original_mode": "auto",
			"clamped_to":    string(LevelMedium),
		}).Debug("thinking: mode converted, dynamic not allowed, using medium level |")
		return config
	}


	mid := (support.Min + support.Max) / 2
	if mid <= 0 && support.ZeroAllowed {
		config.Mode = ModeNone
		config.Budget = 0
	} else if mid <= 0 {
		config.Mode = ModeBudget
		config.Budget = support.Min
	} else {
		config.Mode = ModeBudget
		config.Budget = mid
	}
	log.WithFields(log.Fields{
		"provider":      provider,
		"model":         model,
		"original_mode": "auto",
		"clamped_to":    config.Budget,
	}).Debug("thinking: mode converted, dynamic not allowed |")
	return config
}


var standardLevelOrder = []ThinkingLevel{LevelMinimal, LevelLow, LevelMedium, LevelHigh, LevelXHigh, LevelMax}



func lowestSupportedLevel(supported []string) (ThinkingLevel, bool) {
	bestIdx := -1
	for _, s := range supported {
		if idx := levelIndex(strings.TrimSpace(s)); idx != -1 {
			if bestIdx == -1 || idx < bestIdx {
				bestIdx = idx
			}
		}
	}
	if bestIdx >= 0 {
		return standardLevelOrder[bestIdx], true
	}
	return "", false
}



func clampLevel(level ThinkingLevel, modelInfo *registry.ModelInfo, provider string) ThinkingLevel {
	model := "unknown"
	var supported []string
	if modelInfo != nil {
		if modelInfo.ID != "" {
			model = modelInfo.ID
		}
		if modelInfo.Thinking != nil {
			supported = modelInfo.Thinking.Levels
		}
	}

	if len(supported) == 0 || isLevelSupported(string(level), supported) {
		return level
	}

	pos := levelIndex(string(level))
	if pos == -1 {
		return level
	}
	bestIdx, bestDist := -1, len(standardLevelOrder)+1

	for _, s := range supported {
		if idx := levelIndex(strings.TrimSpace(s)); idx != -1 {
			if dist := abs(pos - idx); dist < bestDist || (dist == bestDist && idx < bestIdx) {
				bestIdx, bestDist = idx, dist
			}
		}
	}

	if bestIdx >= 0 {
		clamped := standardLevelOrder[bestIdx]
		log.WithFields(log.Fields{
			"provider":       provider,
			"model":          model,
			"original_value": string(level),
			"clamped_to":     string(clamped),
		}).Debug("thinking: level clamped |")
		return clamped
	}
	return level
}


func clampBudget(value int, modelInfo *registry.ModelInfo, provider string) int {
	model := "unknown"
	support := (*registry.ThinkingSupport)(nil)
	if modelInfo != nil {
		if modelInfo.ID != "" {
			model = modelInfo.ID
		}
		support = modelInfo.Thinking
	}
	if support == nil {
		return value
	}


	if value == -1 {
		return value
	}

	min, max := support.Min, support.Max
	if value == 0 && !support.ZeroAllowed {
		log.WithFields(log.Fields{
			"provider":       provider,
			"model":          model,
			"original_value": value,
			"clamped_to":     min,
			"min":            min,
			"max":            max,
		}).Warn("thinking: budget zero not allowed |")
		return min
	}


	if min == 0 && max == 0 {
		return value
	}

	if value < min {
		if value == 0 && support.ZeroAllowed {
			return 0
		}
		logClamp(provider, model, value, min, min, max)
		return min
	}
	if value > max {
		logClamp(provider, model, value, max, min, max)
		return max
	}
	return value
}

func isLevelSupported(level string, supported []string) bool {
	for _, s := range supported {
		if strings.EqualFold(level, strings.TrimSpace(s)) {
			return true
		}
	}
	return false
}

func levelIndex(level string) int {
	for i, l := range standardLevelOrder {
		if strings.EqualFold(level, string(l)) {
			return i
		}
	}
	return -1
}

func normalizeLevels(levels []string) []string {
	out := make([]string, len(levels))
	for i, l := range levels {
		out[i] = strings.ToLower(strings.TrimSpace(l))
	}
	return out
}



func isBudgetCapableProvider(provider string) bool {
	switch provider {
	case "gemini", "antigravity", "claude":
		return true
	default:
		return false
	}
}

func isGeminiFamily(provider string) bool {
	switch provider {
	case "gemini", "antigravity":
		return true
	default:
		return false
	}
}

func isOpenAIFamily(provider string) bool {
	switch provider {
	case "openai", "openai-response", "codex":
		return true
	default:
		return false
	}
}

func isKnownProviderFamily(provider string) bool {
	switch provider {
	case "gemini", "antigravity", "claude", "openai", "openai-response", "codex", "kimi", "kimi-ai", "kimi.ai", "kimi.com", "xai", "interactions", "devin":
		return true
	default:
		return false
	}
}

func isSameProviderFamily(from, to string) bool {
	if from == to {
		return true
	}
	return (isGeminiFamily(from) && isGeminiFamily(to)) ||
		(isOpenAIFamily(from) && isOpenAIFamily(to))
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func logClamp(provider, model string, original, clampedTo, min, max int) {
	log.WithFields(log.Fields{
		"provider":       provider,
		"model":          model,
		"original_value": original,
		"min":            min,
		"max":            max,
		"clamped_to":     clampedTo,
	}).Debug("thinking: budget clamped |")
}
