package grokbuild

import "strings"

type ModelInfo struct {
	ID              string
	DisplayName     string
	ContextLength   int
	ReasoningLevels []string
}

type ReasoningEffort struct {
	Value string `json:"value"`
}

type ModelEntry struct {
	ID               string            `json:"id"`
	Model            string            `json:"model"`
	Name             string            `json:"name"`
	ContextWindow    int               `json:"context_window,omitempty"`
	APIBackend       string            `json:"api_backend"`
	SupportedInAPI   bool              `json:"supported_in_api"`
	ReasoningEfforts []ReasoningEffort `json:"reasoning_efforts,omitempty"`
}

type Response struct {
	Object string       `json:"object"`
	Data   []ModelEntry `json:"data"`
}

func IsGrokShellUserAgent(userAgent string) bool {
	return strings.Contains(strings.ToLower(userAgent), "grok-shell")
}

func BuildResponse(models []ModelInfo) Response {
	entries := make([]ModelEntry, 0, len(models))

	for _, m := range models {
		name := m.DisplayName
		if name == "" {
			name = m.ID
		}

		var efforts []ReasoningEffort
		for _, level := range m.ReasoningLevels {
			trimmed := strings.TrimSpace(level)
			if trimmed != "" {
				efforts = append(efforts, ReasoningEffort{Value: trimmed})
			}
		}

		entry := ModelEntry{
			ID:               m.ID,
			Model:            m.ID,
			Name:             name,
			APIBackend:       "responses",
			SupportedInAPI:   true,
			ReasoningEfforts: efforts,
		}

		if m.ContextLength > 0 {
			entry.ContextWindow = m.ContextLength
		}

		entries = append(entries, entry)
	}

	return Response{
		Object: "list",
		Data:   entries,
	}
}
