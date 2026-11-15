package claude

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

const (
	ClaudeDeviceIDsMetadataKey = "claude_device_ids"
	ClaudeDevicePoolSize       = 1
	claudeDeviceIDByteSize     = 32
)

var claudeDevicePoolMu sync.Mutex

func GenerateDeviceIDPool() ([]string, error) {
	deviceIDs := make([]string, 0, ClaudeDevicePoolSize)
	seen := make(map[string]struct{}, ClaudeDevicePoolSize)
	for len(deviceIDs) < ClaudeDevicePoolSize {
		deviceID, errDeviceID := generateDeviceID()
		if errDeviceID != nil {
			return nil, errDeviceID
		}
		if _, exists := seen[deviceID]; exists {
			continue
		}
		seen[deviceID] = struct{}{}
		deviceIDs = append(deviceIDs, deviceID)
	}
	return deviceIDs, nil
}

func generateDeviceID() (string, error) {
	data := make([]byte, claudeDeviceIDByteSize)
	if _, errRead := rand.Read(data); errRead != nil {
		return "", fmt.Errorf("generate Claude device ID: %w", errRead)
	}
	return hex.EncodeToString(data), nil
}

func NormalizeDeviceIDPool(raw any) []string {
	var values []string
	switch typed := raw.(type) {
	case []string:
		values = typed
	case []any:
		values = make([]string, 0, len(typed))
		for _, value := range typed {
			if text, ok := value.(string); ok {
				values = append(values, text)
			}
		}
	default:
		return nil
	}

	deviceIDs := make([]string, 0, min(len(values), ClaudeDevicePoolSize))
	seen := make(map[string]struct{}, ClaudeDevicePoolSize)
	for _, value := range values {
		deviceID := strings.ToLower(strings.TrimSpace(value))
		if !ValidDeviceID(deviceID) {
			continue
		}
		if _, exists := seen[deviceID]; exists {
			continue
		}
		seen[deviceID] = struct{}{}
		deviceIDs = append(deviceIDs, deviceID)
		if len(deviceIDs) == ClaudeDevicePoolSize {
			break
		}
	}
	return deviceIDs
}

func HasCanonicalDeviceIDPool(raw any) bool {
	var values []string
	switch typed := raw.(type) {
	case []string:
		values = typed
	case []any:
		values = make([]string, 0, len(typed))
		for _, value := range typed {
			text, ok := value.(string)
			if !ok {
				return false
			}
			values = append(values, text)
		}
	default:
		return false
	}
	normalized := NormalizeDeviceIDPool(values)
	return len(values) == ClaudeDevicePoolSize && len(normalized) == ClaudeDevicePoolSize && values[0] == normalized[0]
}

func EnsureDeviceIDPool(metadata map[string]any) ([]string, bool, error) {
	claudeDevicePoolMu.Lock()
	defer claudeDevicePoolMu.Unlock()

	return ensureDeviceIDPoolLocked(metadata)
}

func EnsureDeviceIDPoolFor(metadata *map[string]any) ([]string, bool, error) {
	if metadata == nil {
		return nil, false, fmt.Errorf("ensure Claude device pool: metadata pointer is nil")
	}
	claudeDevicePoolMu.Lock()
	defer claudeDevicePoolMu.Unlock()

	if *metadata == nil {
		*metadata = make(map[string]any)
	}
	return ensureDeviceIDPoolLocked(*metadata)
}

func ReadDeviceIDPool(metadata *map[string]any) any {
	if metadata == nil {
		return nil
	}
	claudeDevicePoolMu.Lock()
	defer claudeDevicePoolMu.Unlock()

	if *metadata == nil {
		*metadata = make(map[string]any)
		return nil
	}
	switch stored := (*metadata)[ClaudeDeviceIDsMetadataKey].(type) {
	case []string:
		return append([]string(nil), stored...)
	case []any:
		return append([]any(nil), stored...)
	default:
		return stored
	}
}

func StoreDeviceIDPool(metadata *map[string]any, deviceIDs []string) {
	if metadata == nil {
		return
	}
	claudeDevicePoolMu.Lock()
	defer claudeDevicePoolMu.Unlock()

	if *metadata == nil {
		*metadata = make(map[string]any)
	}
	(*metadata)[ClaudeDeviceIDsMetadataKey] = append([]string(nil), deviceIDs...)
}

func ReadMetadataString(metadata *map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	claudeDevicePoolMu.Lock()
	defer claudeDevicePoolMu.Unlock()

	if *metadata == nil {
		return ""
	}
	value, _ := (*metadata)[key].(string)
	return value
}

func ReadMetadataStringMap(metadata *map[string]any, key string) map[string]string {
	if metadata == nil {
		return nil
	}
	claudeDevicePoolMu.Lock()
	defer claudeDevicePoolMu.Unlock()

	if *metadata == nil {
		return nil
	}
	switch typed := (*metadata)[key].(type) {
	case map[string]string:
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for k, v := range typed {
			out[k] = v
		}
		return out
	case map[string]any:
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for k, rawVal := range typed {
			if strVal, ok := rawVal.(string); ok {
				out[k] = strVal
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func copyStringSliceFromAny(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		if len(typed) == 0 {
			return nil
		}
		out := make([]string, 0, len(typed))
		for _, s := range typed {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		if len(typed) == 0 {
			return nil
		}
		out := make([]string, 0, len(typed))
		for _, elem := range typed {
			if s, ok := elem.(string); ok {
				if trimmed := strings.TrimSpace(s); trimmed != "" {
					out = append(out, trimmed)
				}
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func copyStringMapFromAny(raw any) map[string]string {
	switch typed := raw.(type) {
	case map[string]string:
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for k, v := range typed {
			out[k] = v
		}
		return out
	case map[string]any:
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for k, rawVal := range typed {
			if strVal, ok := rawVal.(string); ok {
				out[k] = strVal
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func ReadMetadataStringSlice(metadata *map[string]any, key string) []string {
	if metadata == nil {
		return nil
	}
	claudeDevicePoolMu.Lock()
	defer claudeDevicePoolMu.Unlock()

	if *metadata == nil {
		return nil
	}
	return copyStringSliceFromAny((*metadata)[key])
}

func ReadMetadataHomeCloak(metadata *map[string]any) (
	mode string,
	strictMode bool,
	sensitiveWords []string,
	sensitiveWordsReplace map[string]string,
	cacheUserID *bool,
	present bool,
) {
	if metadata == nil {
		return "", false, nil, nil, nil, false
	}
	claudeDevicePoolMu.Lock()
	defer claudeDevicePoolMu.Unlock()

	if *metadata == nil {
		return "", false, nil, nil, nil, false
	}
	opts, ok := (*metadata)["credential_options"].(map[string]any)
	if !ok || opts == nil {
		return "", false, nil, nil, nil, false
	}
	rawCloak, exists := opts["cloak"]
	if !exists || rawCloak == nil {
		return "", false, nil, nil, nil, false
	}
	cloakMap, ok := rawCloak.(map[string]any)
	if !ok {
		return "", false, nil, nil, nil, false
	}
	mode, _ = cloakMap["mode"].(string)
	strictMode, _ = cloakMap["strict-mode"].(bool)
	sensitiveWords = copyStringSliceFromAny(cloakMap["sensitive-words"])
	sensitiveWordsReplace = copyStringMapFromAny(cloakMap["sensitive-words-replace"])
	if rawCache, hasCache := cloakMap["cache-user-id"]; hasCache {
		if b, okBool := rawCache.(bool); okBool {
			cacheCopy := b
			cacheUserID = &cacheCopy
		}
	}
	return mode, strictMode, sensitiveWords, sensitiveWordsReplace, cacheUserID, true
}

func StoreMetadataString(metadata *map[string]any, key, value string) {
	if metadata == nil || strings.TrimSpace(value) == "" {
		return
	}
	claudeDevicePoolMu.Lock()
	defer claudeDevicePoolMu.Unlock()

	if *metadata == nil {
		*metadata = make(map[string]any)
	}
	(*metadata)[key] = value
}

func StoreMetadataValue(metadata *map[string]any, key string, value any) {
	if metadata == nil {
		return
	}
	claudeDevicePoolMu.Lock()
	defer claudeDevicePoolMu.Unlock()

	if *metadata == nil {
		*metadata = make(map[string]any)
	}
	(*metadata)[key] = value
}

func EnsureMetadataMap(metadata *map[string]any) {
	if metadata == nil {
		return
	}
	claudeDevicePoolMu.Lock()
	defer claudeDevicePoolMu.Unlock()

	if *metadata == nil {
		*metadata = make(map[string]any)
	}
}

func ensureDeviceIDPoolLocked(metadata map[string]any) ([]string, bool, error) {
	if metadata == nil {
		return nil, false, fmt.Errorf("ensure Claude device pool: metadata is nil")
	}
	rawDeviceIDs := metadata[ClaudeDeviceIDsMetadataKey]
	deviceIDs := NormalizeDeviceIDPool(rawDeviceIDs)
	changed := !HasCanonicalDeviceIDPool(rawDeviceIDs)
	seen := make(map[string]struct{}, ClaudeDevicePoolSize)
	for _, deviceID := range deviceIDs {
		seen[deviceID] = struct{}{}
	}
	for len(deviceIDs) < ClaudeDevicePoolSize {
		deviceID, errDeviceID := generateDeviceID()
		if errDeviceID != nil {
			return nil, false, errDeviceID
		}
		if _, exists := seen[deviceID]; exists {
			continue
		}
		seen[deviceID] = struct{}{}
		deviceIDs = append(deviceIDs, deviceID)
	}

	if changed {
		metadata[ClaudeDeviceIDsMetadataKey] = append([]string(nil), deviceIDs...)
	}
	return append([]string(nil), deviceIDs...), changed, nil
}

func SelectDeviceID(deviceIDs []string, sessionID string) (string, error) {
	deviceIDs = NormalizeDeviceIDPool(deviceIDs)
	if len(deviceIDs) != ClaudeDevicePoolSize {
		return "", fmt.Errorf("select Claude device ID: device pool has %d entries, want %d", len(deviceIDs), ClaudeDevicePoolSize)
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", fmt.Errorf("select Claude device ID: session ID is empty")
	}
	return deviceIDs[0], nil
}

func ValidDeviceID(value string) bool {
	if len(value) != claudeDeviceIDByteSize*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, errDecode := hex.DecodeString(value)
	return errDecode == nil && len(decoded) == claudeDeviceIDByteSize
}
