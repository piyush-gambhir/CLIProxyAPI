package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/modelconfig"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)



func ComputeOpenAICompatModelsHash(models []config.OpenAICompatibilityModel) string {
	return modelconfig.ComputeOpenAICompatModelsHash(models)
}


func ComputeVertexCompatModelsHash(models []config.VertexCompatModel) string {
	return modelconfig.ComputeVertexCompatModelsHash(models)
}


func ComputeClaudeModelsHash(models []config.ClaudeModel) string {
	return modelconfig.ComputeClaudeModelsHash(models)
}


func ComputeCodexModelsHash(models []config.CodexModel) string {
	return modelconfig.ComputeCodexModelsHash(models)
}


func ComputeGeminiModelsHash(models []config.GeminiModel) string {
	return modelconfig.ComputeGeminiModelsHash(models)
}


func ComputeExcludedModelsHash(excluded []string) string {
	if len(excluded) == 0 {
		return ""
	}
	normalized := make([]string, 0, len(excluded))
	for _, entry := range excluded {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			normalized = append(normalized, strings.ToLower(trimmed))
		}
	}
	if len(normalized) == 0 {
		return ""
	}
	sort.Strings(normalized)
	data, _ := json.Marshal(normalized)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func thinkingHashSuffix(support *registry.ThinkingSupport) string {
	data, _ := json.Marshal(support)
	return "|thinking=" + string(data)
}

func normalizeModelPairs(collect func(out func(key string))) []string {
	seen := make(map[string]struct{})
	keys := make([]string, 0)
	collect(func(key string) {
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	})
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	return keys
}

func hashJoined(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return hex.EncodeToString(sum[:])
}
