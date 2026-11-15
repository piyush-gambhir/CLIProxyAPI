package main

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

const (
	defaultCodexWebSearchModel = "gpt-5.4-mini"

	defaultXAIWebSearchModel = "grok-4.3"
)

func resolveAntigravityWebSearchTargetModel(configured, requested string) string {
	if m := strings.TrimSpace(configured); m != "" {
		return m
	}
	if m := registry.AntigravityWebSearchModelFor(strings.TrimSpace(requested)); m != "" {
		return m
	}
	for _, model := range registry.GetGlobalRegistry().GetAvailableModelsByProvider("antigravity") {
		if model == nil || !model.SupportsWebSearch {
			continue
		}
		if id := strings.TrimSpace(model.ID); id != "" {
			return id
		}
	}
	return ""
}

func resolveCodexWebSearchTargetModel(configured string) string {
	if m := strings.TrimSpace(configured); m != "" {
		return m
	}
	return defaultCodexWebSearchModel
}

func resolveXAIWebSearchTargetModel(configured string) string {
	if m := strings.TrimSpace(configured); m != "" {
		return m
	}
	return defaultXAIWebSearchModel
}
