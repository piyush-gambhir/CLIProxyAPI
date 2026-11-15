package helps

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
)

var knownDevinSuffixes = []string{
	"-none",
	"-low",
	"-medium",
	"-high",
	"-xhigh",
	"-max",
	"-fast",
	"-slow",
	"-priority",
	"-low-priority",
	"-medium-priority",
	"-high-priority",
	"-xhigh-priority",
	"-max-priority",
	"-low-fast",
	"-medium-fast",
	"-high-fast",
	"-xhigh-fast",
	"-max-fast",
	"-none-fast",
	"-thinking-1m",
	"-thinking",
	"-max-1m",
	"-none-1m",
	"-minimal-lightning",
	"-low-lightning",
	"-medium-lightning",
	"-high-lightning",
	"-xhigh-lightning",
	"-max-lightning",
	"-none-lightning",
	"_none",
	"_minimal",
	"_low",
	"_medium",
	"_high",
	"_xhigh",
	"_max",
	"_thinking",
}

var specialDevinAliases = map[string]string{
	"claude-haiku-4-5": "MODEL_PRIVATE_11",
	"gpt-4-1":          "MODEL_CHAT_GPT_4_1_2025_04_14",
}

func HasDevinEffortSuffix(model string) bool {
	lower := strings.ToLower(strings.TrimSpace(model))
	for _, s := range knownDevinSuffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	return false
}

func NormalizeThinkingLevel(level string, budgetTokens int) string {
	normalized := strings.ToLower(strings.TrimSpace(level))
	switch normalized {
	case "minimal", "low", "medium", "high", "xhigh", "max", "fast":
		return normalized
	case "none", "off", "disabled":
		return "none"
	case "auto", "adaptive":
		return "high"
	}

	if budgetTokens > 0 {
		switch {
		case budgetTokens <= 4096:
			return "low"
		case budgetTokens <= 16384:
			return "medium"
		case budgetTokens <= 32768:
			return "high"
		default:
			return "max"
		}
	}

	return ""
}

func ResolveDevinChatModelUID(rawModel string, thinkingLevel string, budgetTokens int) string {
	model := strings.TrimSpace(rawModel)
	if model == "" {
		return "swe-2-high"
	}

	cleanModel := model
	if strings.HasPrefix(strings.ToLower(cleanModel), "devin/") {
		cleanModel = cleanModel[6:]
	}

	if HasDevinEffortSuffix(cleanModel) {
		return cleanModel
	}

	parsedSuffix := thinking.ParseSuffix(cleanModel)
	baseModel := strings.TrimSpace(parsedSuffix.ModelName)
	if parsedSuffix.HasSuffix {
		thinkingLevel = parsedSuffix.RawSuffix
	} else if colonIdx := strings.LastIndex(cleanModel, ":"); colonIdx != -1 {
		baseModel = strings.TrimSpace(cleanModel[:colonIdx])
		thinkingLevel = strings.TrimSpace(cleanModel[colonIdx+1:])
	}

	rawLevel := strings.ToLower(strings.TrimSpace(thinkingLevel))
	effort := NormalizeThinkingLevel(thinkingLevel, budgetTokens)
	lowerBase := strings.ToLower(baseModel)
	canonicalBase := strings.ReplaceAll(lowerBase, ".", "-")

	if alias, exists := specialDevinAliases[canonicalBase]; exists {
		return alias
	}
	if canonicalBase == "claude-sonnet-4-5" || strings.Contains(canonicalBase, "sonnet-4-5") {
		if effort != "" && effort != "none" {
			return "MODEL_PRIVATE_3"
		}
		return "MODEL_PRIVATE_2"
	}
	if canonicalBase == "gemini-3-flash" {
		canonicalBase = "gemini-3-8-flash"
	}

	normalizedUnder := strings.ReplaceAll(canonicalBase, "-", "_")
	switch normalizedUnder {
	case "model_gpt_5_2":
		gptLevels := []string{"none", "low", "medium", "high", "xhigh"}
		eff := clampEffort(effort, gptLevels, "low")
		return "MODEL_GPT_5_2_" + strings.ToUpper(eff)
	case "model_google_gemini_3_0_flash":
		gemLevels := []string{"minimal", "low", "medium", "high"}
		eff := clampEffort(effort, gemLevels, "high")
		return "MODEL_GOOGLE_GEMINI_3_0_FLASH_" + strings.ToUpper(eff)
	case "model_claude_4_5_opus":
		if effort != "" && effort != "none" {
			return "MODEL_CLAUDE_4_5_OPUS_THINKING"
		}
		return "MODEL_CLAUDE_4_5_OPUS"
	}

	modelInfo := registry.LookupDevinModel(canonicalBase)
	if modelInfo == nil && canonicalBase != lowerBase {
		modelInfo = registry.LookupDevinModel(lowerBase)
	}

	var allowedLevels []string
	if modelInfo != nil && modelInfo.Thinking != nil && len(modelInfo.Thinking.Levels) > 0 {
		allowedLevels = modelInfo.Thinking.Levels
	}

	switch canonicalBase {
	case "swe-1-7":
		if effort == "medium" {
			return "swe-1-7-medium"
		}
		return "swe-1-7"
	case "swe-1-6":
		if effort == "fast" {
			return "swe-1-6-fast"
		}
		return "swe-1-6"
	case "glm-5-2":
		if effort == "none" {
			return "glm-5-2-none"
		}
		if effort == "max" {
			return "glm-5-2-max"
		}
		return "glm-5-2"
	case "glm-5-2-1m":
		if effort == "none" {
			return "glm-5-2-none-1m"
		}
		if effort == "max" {
			return "glm-5-2-max-1m"
		}
		return "glm-5-2-1m"
	case "claude-opus-4-6", "claude-sonnet-4-6":
		if effort != "" && effort != "none" {
			return canonicalBase + "-thinking"
		}
		return canonicalBase
	case "claude-opus-4-6-1m":
		if effort != "" && effort != "none" {
			return "claude-opus-4-6-thinking-1m"
		}
		return canonicalBase
	case "claude-sonnet-4-6-1m":
		if effort != "" && effort != "none" {
			return "claude-sonnet-4-6-thinking-1m"
		}
		return canonicalBase
	}

	if strings.HasSuffix(canonicalBase, "-lightning") {
		stem := strings.TrimSuffix(canonicalBase, "-lightning")
		if HasDevinEffortSuffix(stem) {

			return canonicalBase
		}
		if len(allowedLevels) == 0 {
			return canonicalBase
		}
		defaultEffort := selectDefaultDevinEffort(stem, allowedLevels)
		clamped := clampEffort(effort, allowedLevels, defaultEffort)
		return stem + "-" + clamped + "-lightning"
	}

	if len(allowedLevels) == 0 {
		return canonicalBase
	}

	if base, isFast := strings.CutSuffix(rawLevel, "-fast"); isFast && devinLevelIndex(base) != -1 {
		defaultEffort := selectDefaultDevinEffort(canonicalBase, allowedLevels)
		clamped := clampEffort(base, allowedLevels, defaultEffort)
		return canonicalBase + "-" + clamped + "-fast"
	}

	defaultEffort := selectDefaultDevinEffort(canonicalBase, allowedLevels)
	clamped := clampEffort(effort, allowedLevels, defaultEffort)
	if effort == "fast" {
		return canonicalBase + "-" + clamped + "-fast"
	}
	return canonicalBase + "-" + clamped
}

func selectDefaultDevinEffort(baseModel string, levels []string) string {
	if strings.Contains(baseModel, "swe-2") {
		return "high"
	}
	hasNone := false
	hasLow := false
	hasMedium := false
	hasHigh := false
	for _, l := range levels {
		switch l {
		case "none":
			hasNone = true
		case "low":
			hasLow = true
		case "medium":
			hasMedium = true
		case "high":
			hasHigh = true
		}
	}

	if hasNone && hasLow && strings.HasPrefix(baseModel, "gpt-5") {
		return "low"
	}

	if hasHigh && (strings.Contains(baseModel, "gemini") ||
		strings.Contains(baseModel, "grok") ||
		strings.Contains(baseModel, "glm") ||
		strings.Contains(baseModel, "deepseek") ||
		strings.Contains(baseModel, "kimi") ||
		strings.Contains(baseModel, "nemotron")) {
		return "high"
	}
	if hasMedium {
		return "medium"
	}
	if hasHigh {
		return "high"
	}
	if hasLow {
		return "low"
	}
	return levels[0]
}

var devinStandardLevelOrder = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

func devinLevelIndex(level string) int {
	lower := strings.ToLower(strings.TrimSpace(level))
	for i, l := range devinStandardLevelOrder {
		if l == lower {
			return i
		}
	}
	return -1
}

func clampEffort(requested string, allowed []string, defaultEffort string) string {
	if requested == "" {
		return defaultEffort
	}
	reqLower := strings.ToLower(strings.TrimSpace(requested))
	for _, a := range allowed {
		if reqLower == strings.ToLower(strings.TrimSpace(a)) {
			return a
		}
	}
	if reqLower == "none" {
		return defaultEffort
	}

	reqIdx := devinLevelIndex(reqLower)
	if reqIdx == -1 {
		return defaultEffort
	}

	bestMatch := defaultEffort
	bestDist := 999
	bestIdx := -1
	for _, a := range allowed {
		aIdx := devinLevelIndex(a)
		if aIdx == -1 {
			continue
		}
		dist := reqIdx - aIdx
		if dist < 0 {
			dist = -dist
		}
		if dist < bestDist {
			bestDist = dist
			bestMatch = a
			bestIdx = aIdx
		} else if dist == bestDist && aIdx > bestIdx {

			bestMatch = a
			bestIdx = aIdx
		}
	}

	return bestMatch
}
