



package thinking

import (
	"strconv"
	"strings"
)












func ParseSuffix(model string) SuffixResult {

	lastOpen := strings.LastIndex(model, "(")
	if lastOpen == -1 {
		return SuffixResult{ModelName: model, HasSuffix: false}
	}


	if !strings.HasSuffix(model, ")") {
		return SuffixResult{ModelName: model, HasSuffix: false}
	}


	modelName := model[:lastOpen]
	rawSuffix := model[lastOpen+1 : len(model)-1]

	return SuffixResult{
		ModelName: modelName,
		HasSuffix: true,
		RawSuffix: rawSuffix,
	}
}


func HasThinkingSuffix(model string) bool {
	result := ParseSuffix(model)
	return result.HasSuffix && hasThinkingConfig(parseSuffixToConfig(result.RawSuffix, "", result.ModelName))
}





















func ParseNumericSuffix(rawSuffix string) (budget int, ok bool) {
	if rawSuffix == "" {
		return 0, false
	}

	value, err := strconv.Atoi(rawSuffix)
	if err != nil {
		return 0, false
	}



	if value < 0 {
		return 0, false
	}

	return value, true
}









func ParseSpecialSuffix(rawSuffix string) (mode ThinkingMode, ok bool) {
	if rawSuffix == "" {
		return ModeBudget, false
	}


	switch strings.ToLower(rawSuffix) {
	case "none":
		return ModeNone, true
	case "auto", "-1":
		return ModeAuto, true
	default:
		return ModeBudget, false
	}
}


















func ParseLevelSuffix(rawSuffix string) (level ThinkingLevel, ok bool) {
	if rawSuffix == "" {
		return "", false
	}


	switch strings.ToLower(rawSuffix) {
	case "minimal":
		return LevelMinimal, true
	case "low":
		return LevelLow, true
	case "medium":
		return LevelMedium, true
	case "high":
		return LevelHigh, true
	case "xhigh":
		return LevelXHigh, true
	case "max":
		return LevelMax, true
	default:
		return "", false
	}
}
