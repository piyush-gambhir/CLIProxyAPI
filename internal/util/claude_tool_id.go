package util

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

const geminiClaudeToolUseIDPrefix = "cpa_gemini_"

var (
	claudeToolUseIDSanitizer    = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	claudeFunctionNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	claudeToolUseIDCounter      uint64
)

func SanitizeClaudeToolID(id string) string {
	s := claudeToolUseIDSanitizer.ReplaceAllString(id, "_")
	if s == "" {
		s = fmt.Sprintf("toolu_%d_%d", time.Now().UnixNano(), atomic.AddUint64(&claudeToolUseIDCounter, 1))
	}
	return s
}

func SanitizeClaudeFunctionName(name string) string {
	if name == "" {
		return ""
	}
	s := claudeFunctionNameSanitizer.ReplaceAllString(name, "_")
	if len(s) > 64 {
		s = s[:64]
	}
	if s == "" {
		s = "_"
	}
	return s
}

func GeminiClaudeToolUseID(callID, name, argsRaw string) string {
	callID = strings.TrimSpace(callID)
	name = strings.TrimSpace(name)
	if callID == "" || name == "" {
		return ""
	}
	if strings.TrimSpace(argsRaw) != "" {
		var value any
		if json.Unmarshal([]byte(argsRaw), &value) == nil {
			if canonical, errMarshal := json.Marshal(value); errMarshal == nil {
				argsRaw = string(canonical)
			}
		} else {
			argsRaw = strings.TrimSpace(argsRaw)
		}
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{callID, name, argsRaw}, "\x00")))
	return geminiClaudeToolUseIDPrefix + hex.EncodeToString(sum[:16])
}

func IsGeminiClaudeToolUseID(id string) bool {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, geminiClaudeToolUseIDPrefix) {
		return false
	}
	digest := strings.TrimPrefix(id, geminiClaudeToolUseIDPrefix)
	if len(digest) != 32 {
		return false
	}
	_, errDecode := hex.DecodeString(digest)
	return errDecode == nil
}
