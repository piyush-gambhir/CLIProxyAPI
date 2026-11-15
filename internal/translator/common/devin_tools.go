package common

import (
	"regexp"
	"strings"
)

func IsDevinCodexAppAutomationUpdate(namespace, toolName string) bool {
	cleanNamespace := strings.TrimSpace(namespace)
	cleanTool := strings.TrimSpace(toolName)
	if strings.EqualFold(cleanNamespace, "mcp__codex_app") && strings.EqualFold(cleanTool, "automation_update") {
		return true
	}
	if strings.EqualFold(cleanTool, "mcp__codex_app__automation_update") {
		return true
	}
	return false
}

const (
	execCommandTargetPhrase     = "returning output or a session ID for ongoing interaction"
	execCommandObfuscatedPhrase = "returning output or an session ID for ongoing interaction"
	writeStdinTargetPhrase      = "Writes characters to an existing unified exec session and returns recent output."
	writeStdinObfuscatedPhrase  = "Writes characters to a existing unified exec session and returns recent output."
)

var (
	execCommandRegex = regexp.MustCompile(`(?i)returning output or a session ID for ongoing interaction`)
	writeStdinRegex  = regexp.MustCompile(`(?i)Writes characters to an existing unified exec session and returns recent output(\.?)`)
)

func ObfuscateExecCommandDescription(desc string) string {
	if strings.Contains(desc, execCommandTargetPhrase) {
		desc = strings.ReplaceAll(desc, execCommandTargetPhrase, execCommandObfuscatedPhrase)
	}
	return execCommandRegex.ReplaceAllString(desc, execCommandObfuscatedPhrase)
}

func ObfuscateWriteStdinDescription(desc string) string {
	if strings.Contains(desc, writeStdinTargetPhrase) {
		desc = strings.ReplaceAll(desc, writeStdinTargetPhrase, writeStdinObfuscatedPhrase)
	}
	return writeStdinRegex.ReplaceAllString(desc, "Writes characters to a existing unified exec session and returns recent output$1")
}

func SanitizeDevinToolDescription(_ string, desc string) string {
	if desc == "" {
		return desc
	}
	desc = ObfuscateExecCommandDescription(desc)
	desc = ObfuscateWriteStdinDescription(desc)
	return desc
}
