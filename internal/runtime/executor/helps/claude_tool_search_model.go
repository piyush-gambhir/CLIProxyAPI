package helps

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
)

var claudeToolSearchLegacyPrefixes = []string{"claude-3", "claude-2", "claude-instant"}

var claudeToolSearchLegacyModels = map[string]struct{}{
	"claude-opus-4":            {},
	"claude-opus-4-0":          {},
	"claude-opus-4-20250514":   {},
	"claude-opus-4-1":          {},
	"claude-opus-4-1-20250805": {},
	"claude-sonnet-4":          {},
	"claude-sonnet-4-0":        {},
	"claude-sonnet-4-20250514": {},
}

func ClaudeModelLacksToolSearch(model string) bool {
	model = strings.ToLower(strings.TrimSpace(thinking.ParseSuffix(model).ModelName))
	if slash := strings.LastIndexByte(model, '/'); slash >= 0 {
		model = model[slash+1:]
	}
	if bracket := strings.IndexByte(model, '['); bracket >= 0 {
		model = model[:bracket]
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	if _, legacy := claudeToolSearchLegacyModels[model]; legacy {
		return true
	}
	for _, prefix := range claudeToolSearchLegacyPrefixes {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}
