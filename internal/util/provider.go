package util

import (
	"net/url"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	log "github.com/sirupsen/logrus"
)

const openAICompatibleProviderPrefix = "openai-compatible-"

func OpenAICompatibleProviderKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == "openai-compatibility" || strings.HasPrefix(name, openAICompatibleProviderPrefix) {
		if name == "" {
			return "openai-compatibility"
		}
		return name
	}
	return openAICompatibleProviderPrefix + name
}

func GetProviderName(modelName string) []string {
	if modelName == "" {
		return nil
	}

	providers := make([]string, 0, 4)
	seen := make(map[string]struct{})

	appendProvider := func(name string) {
		if name == "" {
			return
		}
		if _, exists := seen[name]; exists {
			return
		}
		seen[name] = struct{}{}
		providers = append(providers, name)
	}

	for _, provider := range registry.GetGlobalRegistry().GetModelProviders(modelName) {
		appendProvider(provider)
	}
	if len(providers) == 0 && strings.ToLower(modelName) != modelName {
		for _, provider := range registry.GetGlobalRegistry().GetModelProviders(strings.ToLower(modelName)) {
			appendProvider(provider)
		}
	}

	if len(providers) > 0 {
		return providers
	}

	return providers
}

func ResolveAutoModel(modelName string) string {
	if modelName != "auto" {
		return modelName
	}

	firstModel, err := registry.GetGlobalRegistry().GetFirstAvailableModel("")
	if err != nil {
		log.Warnf("Failed to resolve 'auto' model: %v, falling back to original model name", err)
		return modelName
	}

	log.Infof("Resolved 'auto' model to: %s", firstModel)
	return firstModel
}

func IsOpenAICompatibilityAlias(modelName string, cfg *config.Config) bool {
	if cfg == nil {
		return false
	}

	for _, compat := range cfg.OpenAICompatibility {
		if compat.Disabled {
			continue
		}
		for _, model := range compat.Models {
			if model.Alias == modelName {
				return true
			}
		}
	}
	return false
}

func GetOpenAICompatibilityConfig(alias string, cfg *config.Config) (*config.OpenAICompatibility, *config.OpenAICompatibilityModel) {
	if cfg == nil {
		return nil, nil
	}

	for _, compat := range cfg.OpenAICompatibility {
		if compat.Disabled {
			continue
		}
		for _, model := range compat.Models {
			if model.Alias == alias {
				return &compat, &model
			}
		}
	}
	return nil, nil
}

func InArray(hystack []string, needle string) bool {
	for _, item := range hystack {
		if needle == item {
			return true
		}
	}
	return false
}

func HideAPIKey(apiKey string) string {
	if len(apiKey) > 8 {
		return apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
	} else if len(apiKey) > 4 {
		return apiKey[:2] + "..." + apiKey[len(apiKey)-2:]
	} else if len(apiKey) > 2 {
		return apiKey[:1] + "..." + apiKey[len(apiKey)-1:]
	}
	if apiKey == "" {
		return ""
	}
	return "[REDACTED]"
}

func MaskAuthorizationHeader(value string) string {
	parts := strings.SplitN(strings.TrimSpace(value), " ", 2)
	if len(parts) < 2 {
		return HideAPIKey(value)
	}
	return parts[0] + " " + HideAPIKey(parts[1])
}

func MaskSensitiveHeaderValue(key, value string) string {
	lowerKey := strings.ToLower(strings.TrimSpace(key))
	switch {
	case lowerKey == "cookie" || lowerKey == "set-cookie" || lowerKey == "x-management-key" || strings.Contains(lowerKey, "password"):
		if value == "" {
			return ""
		}
		return "[REDACTED]"
	case strings.Contains(lowerKey, "authorization"):
		return MaskAuthorizationHeader(value)
	case strings.Contains(lowerKey, "api-key"),
		strings.Contains(lowerKey, "apikey"),
		strings.Contains(lowerKey, "token"),
		strings.Contains(lowerKey, "secret"),
		strings.Contains(lowerKey, "cookie"):
		return HideAPIKey(value)
	default:
		return value
	}
}

func MaskSensitiveQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	changed := false
	for i, part := range parts {
		if part == "" {
			continue
		}
		keyPart := part
		valuePart := ""
		if idx := strings.Index(part, "="); idx >= 0 {
			keyPart = part[:idx]
			valuePart = part[idx+1:]
		}
		decodedKey, err := url.QueryUnescape(keyPart)
		if err != nil {
			decodedKey = keyPart
		}
		if !shouldMaskQueryParam(decodedKey) {
			continue
		}
		decodedValue, err := url.QueryUnescape(valuePart)
		if err != nil {
			decodedValue = valuePart
		}
		masked := HideAPIKey(strings.TrimSpace(decodedValue))
		parts[i] = keyPart + "=" + url.QueryEscape(masked)
		changed = true
	}
	if !changed {
		return raw
	}
	return strings.Join(parts, "&")
}

func shouldMaskQueryParam(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	key = strings.TrimSuffix(key, "[]")

	if key == "key" || key == "value" || strings.Contains(key, "api-key") || strings.Contains(key, "apikey") || strings.Contains(key, "api_key") {
		return true
	}
	if strings.Contains(key, "token") || strings.Contains(key, "secret") {
		return true
	}
	return false
}
