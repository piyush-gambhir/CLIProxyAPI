package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/google/uuid"
	claudeauth "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/claude"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/buildinfo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/misc"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/session"
	log "github.com/sirupsen/logrus"

	"github.com/gin-gonic/gin"
)

const (
	claudeTokenCountingBeta          = "token-counting-2024-11-01"
	claudeFastModeBeta               = "fast-mode-2026-02-01"
	claudeOAuthBeta                  = "oauth-2025-04-20"
	claudeCodeBeta                   = "claude-code-20250219"
	claudeContext1MBeta              = "context-1m-2025-08-07"
	claudeMidConvSystemBeta          = "mid-conversation-system-2026-04-07"
	claudePerTurnControlBeta         = "per-turn-control-2026-07-01"
	claudePerTurnTimingBeta          = "timing-2026-09-09"
	claudeMidConvToolChangesBeta     = "mid-conversation-tool-changes-2026-07-01"
	claudeInlineToolsBeta            = "inline-tools-2026-09-15"
	claudeMidConvSystemClearAtBeta   = "mid-conversation-system-clear-at-2026-08-21"
	claudeDangerousToolUseBeta       = "dangerous-tool-use-2026-09-03"
	claudeAdvisorToolBeta            = "advisor-tool-2026-03-01"
	claudeAdvancedToolUseBeta        = "advanced-tool-use-2025-11-20"
	claudeEffortBeta                 = "effort-2025-11-24"
	claudeServerSideFallbackBeta     = "server-side-fallback-2026-06-01"
	claudeFallbackCreditBeta         = "fallback-credit-2026-06-01"
	claudeStructuredOutputsBeta      = "structured-outputs-2025-12-15"
	claudeThinkingDisplayUpdatesBeta = "thinking-display-updates-2026-08-18"
	claudeThinkingBindingBeta        = "thinking-binding-controls-2026-08-01"
	claudeThinkingResumptionBeta     = "thinking-resumption-2026-07-17"
	claudeExtendedCacheTTLBeta       = "extended-cache-ttl-2025-04-11"
	claudePromptCachingEvictBeta     = "prompt-caching-evict-2026-05-12"
	claudeCacheDiagnosisBeta         = "cache-diagnosis-2026-04-07"
	claudeRedactThinkingBeta         = "redact-thinking-2026-02-12"
	claudeAFKModeBeta                = "afk-mode-2026-01-31"
	claudeThinkingTokenCountBeta     = "thinking-token-count-2026-05-13"
	claudeMessageThreadsBeta         = "message-threads-2026-08-12"
)

var claudeCodeCLIConstantBetas = []string{
	"interleaved-thinking-2025-05-14",
	claudeRedactThinkingBeta,
	"context-management-2025-06-27",
	"prompt-caching-scope-2026-01-05",
}

var claudeCodeTrailingBetas = []string{
	claudeServerSideFallbackBeta,
	claudeFallbackCreditBeta,
	claudeStructuredOutputsBeta,
}

var claudeManagedBetaSet = func() map[string]bool {
	managed := []string{
		claudeTokenCountingBeta,
		claudeFastModeBeta,
		claudeOAuthBeta,
		claudeCodeBeta,
		claudeContext1MBeta,
		claudeMidConvSystemBeta,
		claudePerTurnControlBeta,
		claudePerTurnTimingBeta,
		claudeMidConvToolChangesBeta,
		claudeInlineToolsBeta,
		claudeMidConvSystemClearAtBeta,
		claudeDangerousToolUseBeta,
		claudeAdvisorToolBeta,
		claudeAdvancedToolUseBeta,
		claudeEffortBeta,
		claudeServerSideFallbackBeta,
		claudeFallbackCreditBeta,
		claudeStructuredOutputsBeta,
		claudeThinkingDisplayUpdatesBeta,
		claudeThinkingBindingBeta,
		claudeThinkingResumptionBeta,
		claudeExtendedCacheTTLBeta,
		claudePromptCachingEvictBeta,
		claudeCacheDiagnosisBeta,
		claudeRedactThinkingBeta,
		claudeAFKModeBeta,
		claudeThinkingTokenCountBeta,
		claudeMessageThreadsBeta,
	}
	managed = append(managed, claudeCodeCLIConstantBetas...)
	managed = append(managed, claudeCodeTrailingBetas...)
	set := make(map[string]bool, len(managed))
	for _, beta := range managed {
		set[beta] = true
	}
	return set
}()

func isManagedClaudeBeta(beta string) bool {
	return claudeManagedBetaSet[strings.TrimSpace(beta)]
}

func claudeCodeCLIBetas(body []byte, requested map[string]bool, oauthToken bool) string {
	betas := make([]string, 0, len(claudeCodeCLIConstantBetas)+len(claudeCodeTrailingBetas)+12)
	isProbeOrHelper := helps.IsClaudeProbeOrHelperRequest(body)
	isQuotaProbe := helps.IsClaudeQuotaProbeRequest(body)
	model := ojson.GetBytesNoCopy(body, "model").String()
	claudeCodeAfterScope := isClaudeHaiku55Model(model)
	if !isQuotaProbe && !claudeCodeAfterScope {
		betas = append(betas, claudeCodeBeta)
	}
	if oauthToken {
		betas = append(betas, claudeOAuthBeta)
	}
	if requested[claudeContext1MBeta] {
		betas = append(betas, claudeContext1MBeta)
	}
	redactThinking := !claudeThinkingDisplaySet(body)
	for _, beta := range claudeCodeCLIConstantBetas {
		if beta == claudeRedactThinkingBeta && !redactThinking {
			continue
		}
		betas = append(betas, beta)
	}
	if !isQuotaProbe && claudeCodeAfterScope {
		betas = append(betas, claudeCodeBeta)
	}
	if !claudeUsesLegacySystemReminder(body) {
		if requested[claudeMidConvSystemBeta] {
			betas = append(betas, claudeMidConvSystemBeta)
		}
		if claudeIncludePerTurnControl(body, requested) {
			betas = append(betas, claudePerTurnControlBeta)
		}
		if claudeIncludePerTurnTiming(body, requested) {
			betas = append(betas, claudePerTurnTimingBeta)
		}
		if !isClaudeSonnet5Model(model) {
			betas = append(betas, claudeMidConvToolChangesBeta)
		}
		if claudeIncludeInlineTools(body, requested) {
			betas = append(betas, claudeInlineToolsBeta)
		}
	} else {

		if claudeIncludePerTurnControl(body, requested) {
			betas = append(betas, claudePerTurnControlBeta)
		}
		if claudeIncludePerTurnTiming(body, requested) {
			betas = append(betas, claudePerTurnTimingBeta)
		}
	}
	if requested[claudeAdvisorToolBeta] || claudeBodyHasAdvisorTool(body) {
		betas = append(betas, claudeAdvisorToolBeta)
	}
	if !isQuotaProbe || requested[claudeThinkingTokenCountBeta] {
		betas = append(betas, claudeThinkingTokenCountBeta)
	}
	if requested[claudeAdvancedToolUseBeta] || claudeBodyUsesAdvancedToolUse(body) {
		betas = append(betas, claudeAdvancedToolUseBeta)
	}
	if !claudeUsesLegacySystemReminder(body) && claudeIncludeMidConvClearAt(body, requested) {
		betas = append(betas, claudeMidConvSystemClearAtBeta)
	}
	if claudeRequestSupportsEffort(body, requested) {
		betas = append(betas, claudeEffortBeta)
	}
	if requested[claudeDangerousToolUseBeta] || ojson.GetBytesNoCopy(body, "safeguards").Exists() {
		betas = append(betas, claudeDangerousToolUseBeta)
	}
	if !isProbeOrHelper && (requested[claudeServerSideFallbackBeta] || ojson.GetBytesNoCopy(body, "fallbacks").Exists()) {
		betas = append(betas, claudeServerSideFallbackBeta)
	}
	shouldIncludeFallbackCredit := requested[claudeFallbackCreditBeta] ||
		ojson.GetBytesNoCopy(body, "fallback_credit_token").Exists() ||
		(oauthToken && ojson.GetBytesNoCopy(body, "fallbacks").Exists())
	if shouldIncludeFallbackCredit {
		betas = append(betas, claudeFallbackCreditBeta)
	}
	for _, beta := range claudeCodeTrailingBetas {
		if beta == claudeServerSideFallbackBeta || beta == claudeFallbackCreditBeta {
			continue
		}
		if requested[beta] {
			betas = append(betas, beta)
		}
	}
	thinkingType := ojson.GetBytesNoCopy(body, "thinking.type").String()
	if requested[claudeThinkingBindingBeta] || ojson.GetBytesNoCopy(body, "thinking.block_binding").Exists() ||
		(claudeModelUsesProgressDisplay(ojson.GetBytesNoCopy(body, "model").String()) && thinkingType == "adaptive") {
		betas = append(betas, claudeThinkingBindingBeta)
	}
	if !isProbeOrHelper && thinkingType != "disabled" && (requested[claudeThinkingDisplayUpdatesBeta] || claudeThinkingDisplayUpdates(body)) {
		betas = append(betas, claudeThinkingDisplayUpdatesBeta)
	}
	if requested[claudeThinkingResumptionBeta] {
		betas = append(betas, claudeThinkingResumptionBeta)
	}
	if claudeRequestUsesFastMode(body, requested) {
		betas = append(betas, claudeFastModeBeta)
	}
	if requested[claudeAFKModeBeta] {
		betas = append(betas, claudeAFKModeBeta)
	}
	if !isProbeOrHelper {
		includeExtended := (oauthToken && !helps.IsClaudeSubagentRequest(nil, body)) ||
			requested[claudeExtendedCacheTTLBeta] ||
			helps.ClaudePayloadHas1hTTL(body)
		if includeExtended {
			betas = append(betas, claudeExtendedCacheTTLBeta)
		}
	}
	if requested[claudePromptCachingEvictBeta] || bytes.Contains(body, []byte(`"evict_on_complete"`)) {
		betas = append(betas, claudePromptCachingEvictBeta)
	}
	if diagnostics := ojson.GetBytesNoCopy(body, "diagnostics"); diagnostics.IsObject() {
		betas = append(betas, claudeCacheDiagnosisBeta)
	}
	if requested[claudeMessageThreadsBeta] || ojson.GetBytesNoCopy(body, "thread").IsObject() {
		betas = append(betas, claudeMessageThreadsBeta)
	}
	return strings.Join(betas, ",")
}

func isClaudeHaikuModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "haiku")
}

func claudeCanonicalModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if slash := strings.LastIndexByte(model, '/'); slash >= 0 {
		model = model[slash+1:]
	}
	return model
}

func isClaudeOpus55Model(model string) bool {
	model = claudeCanonicalModel(model)
	return model == "claude-opus-5-5" || strings.HasPrefix(model, "claude-opus-5-5[")
}

func isClaudeSonnet55Model(model string) bool {
	model = claudeCanonicalModel(model)
	return model == "claude-sonnet-5-5" || strings.HasPrefix(model, "claude-sonnet-5-5-") || strings.HasPrefix(model, "claude-sonnet-5-5[")
}

func isClaudeSonnet5Model(model string) bool {
	model = claudeCanonicalModel(model)
	if isClaudeSonnet55Model(model) {
		return false
	}
	return model == "claude-sonnet-5" || strings.HasPrefix(model, "claude-sonnet-5-") || strings.HasPrefix(model, "claude-sonnet-5[")
}

func isClaudeHaiku55Model(model string) bool {
	model = claudeCanonicalModel(model)
	return model == "claude-haiku-5-5" || strings.HasPrefix(model, "claude-haiku-5-5-") || strings.HasPrefix(model, "claude-haiku-5-5[") || model == "claude-haiku-5.5" || strings.HasPrefix(model, "claude-haiku-5.5-") || strings.HasPrefix(model, "claude-haiku-5.5[")
}

func isClaudeFable51OnlyModel(model string) bool {
	m := claudeCanonicalModel(model)
	for _, target := range []string{"fable-5-1", "fable-5.1"} {
		idx := strings.Index(m, target)
		if idx != -1 {
			nextIdx := idx + len(target)
			if nextIdx >= len(m) || m[nextIdx] < '0' || m[nextIdx] > '9' {
				return true
			}
		}
	}
	return false
}

func isClaudePreservedThinkingModel(model string) bool {
	return isClaudeOpus55Model(model) || isClaudeSonnet55Model(model) || isClaudeHaiku55Model(model) || isClaudeFable51OnlyModel(model) || isClaudeFable52Model(model)
}

func claudeThinkingTypeAcceptsBlockBinding(model, thinkingType string) bool {
	switch strings.ToLower(strings.TrimSpace(thinkingType)) {
	case "adaptive":
		return true
	case "enabled":
		return !isClaudeHaiku55Model(model) && !isClaudeSonnet55Model(model)
	default:
		return false
	}
}

func claudeBodyHasSignedThinkingBlock(body []byte) bool {
	return claudeMessagesHaveSignedThinkingBlock(ojson.GetBytesNoCopy(body, "messages"))
}

func claudeDocHasSignedThinkingBlock(doc *ojson.Doc) bool {
	if doc == nil {
		return false
	}
	return claudeMessagesHaveSignedThinkingBlock(doc.Get("messages"))
}

func claudeMessagesHaveSignedThinkingBlock(messages ojson.Result) bool {
	found := false
	messages.ForEach(func(_, msg ojson.Result) bool {
		msg.Get("content").ForEach(func(_, block ojson.Result) bool {
			switch strings.TrimSpace(block.Get("type").String()) {
			case "thinking":
				found = strings.TrimSpace(block.Get("signature").String()) != ""
			case "redacted_thinking":
				found = strings.TrimSpace(block.Get("data").String()) != ""
			}
			return !found
		})
		return !found
	})
	return found
}

func applyClaudePreservedThinkingBinding(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	doc := ojson.NewDoc(body)
	if !applyClaudePreservedThinkingBindingDoc(doc) {
		return body
	}
	return doc.Bytes()
}

func applyClaudePreservedThinkingBindingDoc(doc *ojson.Doc) bool {
	if doc == nil || doc.Len() == 0 {
		return false
	}
	model := doc.Get("model").String()
	if !isClaudePreservedThinkingModel(model) || claudeToolChoiceForcesToolUseDoc(doc) {
		return false
	}
	thinking := doc.Get("thinking")
	if thinking.Exists() {
		if !claudeThinkingTypeAcceptsBlockBinding(model, thinking.Get("type").String()) || thinking.Get("block_binding.prefix_mismatch_behavior").Exists() {
			return false
		}
	}
	if !claudeDocHasSignedThinkingBlock(doc) {
		return false
	}
	if !thinking.Exists() {
		_ = doc.Set("thinking.type", "adaptive")
	}
	return doc.Set("thinking.block_binding.prefix_mismatch_behavior", "drop_block") == nil
}

func claudeModelUsesProgressDisplay(model string) bool {
	return isClaudeOpus55Model(model) || isClaudeFable51Model(model) || isClaudeFable52Model(model) ||
		isClaudeSonnet5Model(model) || isClaudeSonnet55Model(model) || isClaudeHaiku55Model(model)
}

func isClaudeFable52Model(model string) bool {
	model = claudeCanonicalModel(model)
	for _, prefix := range []string{"claude-fable-5-2", "claude-fable-5.2"} {
		if model == prefix || strings.HasPrefix(model, prefix+"-") || strings.HasPrefix(model, prefix+"[") {
			return true
		}
	}
	return false
}

func applyClaudeCloakThinkingDisplay(body []byte, callerOwned bool) []byte {
	if callerOwned || len(body) == 0 {
		return body
	}
	doc := ojson.NewDoc(body)
	if !applyClaudeCloakThinkingDisplayDoc(doc, callerOwned) {
		return body
	}
	return doc.Bytes()
}

func applyClaudeCloakThinkingDisplayDoc(doc *ojson.Doc, callerOwned bool) bool {
	if callerOwned || doc == nil || doc.Len() == 0 || doc.Get("thinking.display").Exists() {
		return false
	}
	if !claudeModelUsesProgressDisplay(doc.Get("model").String()) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(doc.Get("thinking.type").String())) {
	case "adaptive", "enabled":
	default:
		return false
	}
	return doc.Set("thinking.display", "updates") == nil
}

func claudeModelHasPerTurnEffort(model string) bool {
	return isClaudeOpus55Model(model) ||
		strings.HasPrefix(claudeCanonicalModel(model), "claude-fable-5-1") ||
		isClaudeFable52Model(model) ||
		isClaudeSonnet55Model(model) ||
		isClaudeHaiku55Model(model)
}

func claudeModelHasPerTurnTiming(model string) bool {
	model = claudeCanonicalModel(model)
	if isClaudeHaiku55Model(model) {
		return false
	}
	return claudeModelHasPerTurnEffort(model) ||
		strings.HasPrefix(model, "claude-mythos-5-1") ||
		strings.HasPrefix(model, "claude-mythos-5-2") ||
		strings.HasPrefix(model, "claude-mythos-5.2")
}

func claudeIncludePerTurnControl(body []byte, requested map[string]bool) bool {
	if requested[claudePerTurnControlBeta] {
		return true
	}
	return claudeModelHasPerTurnEffort(ojson.GetBytesNoCopy(body, "model").String())
}

func claudeIncludePerTurnTiming(body []byte, requested map[string]bool) bool {
	if requested[claudePerTurnTimingBeta] {
		return true
	}
	if !claudeModelHasPerTurnTiming(ojson.GetBytesNoCopy(body, "model").String()) {
		return false
	}
	if ojson.GetBytesNoCopy(body, "output_config.timing").Exists() {
		return true
	}
	found := false
	ojson.GetBytesNoCopy(body, "messages").ForEach(func(_, msg ojson.Result) bool {
		if msg.Get("output_config.timing").Exists() {
			found = true
			return false
		}
		return true
	})
	return found
}

func claudeIncludeInlineTools(body []byte, requested map[string]bool) bool {
	if requested[claudeInlineToolsBeta] {
		return true
	}
	found := false
	ojson.GetBytesNoCopy(body, "messages").ForEach(func(_, msg ojson.Result) bool {
		msg.Get("content").ForEach(func(_, block ojson.Result) bool {
			if strings.EqualFold(strings.TrimSpace(block.Get("type").String()), "tool_addition") && block.Get("tool.definition").Exists() {
				found = true
				return false
			}
			return true
		})
		return !found
	})
	return found
}

func claudeIncludeMidConvClearAt(body []byte, requested map[string]bool) bool {
	if requested[claudeMidConvSystemClearAtBeta] || claudeModelUsesProgressDisplay(ojson.GetBytesNoCopy(body, "model").String()) {
		return true
	}
	found := false
	ojson.GetBytesNoCopy(body, "messages").ForEach(func(_, msg ojson.Result) bool {
		if msg.Get("clear_at").Exists() {
			found = true
			return false
		}
		return true
	})
	return found
}

func claudeRequestSupportsEffort(body []byte, requested map[string]bool) bool {
	if len(body) > 0 {
		if helps.IsClaudeProbeOrHelperRequest(body) {
			return false
		}
		model := strings.ToLower(strings.TrimSpace(ojson.GetBytesNoCopy(body, "model").String()))

		if isClaudeHaikuModel(model) && !isClaudeHaiku55Model(model) {
			return false
		}
		thinkingType := strings.ToLower(strings.TrimSpace(ojson.GetBytesNoCopy(body, "thinking.type").String()))
		if thinkingType == "disabled" {
			return false
		}
	}
	if requested[claudeEffortBeta] {
		return true
	}
	return true
}

func claudeThinkingDisplayUpdates(body []byte) bool {
	display := ojson.GetBytesNoCopy(body, "thinking.display")
	return display.Type == ojson.String && strings.EqualFold(strings.TrimSpace(display.String()), "updates")
}

func claudeBodyUsesAdvancedToolUse(body []byte) bool {
	tools := ojson.GetBytesNoCopy(body, "tools")
	if !tools.IsArray() {
		return false
	}
	for _, tool := range tools.Array() {
		toolType := strings.ToLower(strings.TrimSpace(tool.Get("type").String()))
		if strings.HasPrefix(toolType, "tool_search_tool_") {
			return true
		}
		if tool.Get("defer_loading").Bool() || tool.Get("input_examples").Exists() || tool.Get("allowed_callers").Exists() {
			return true
		}
	}
	return false
}

func claudeBodyHasAdvisorTool(body []byte) bool {
	tools := ojson.GetBytesNoCopy(body, "tools")
	if !tools.IsArray() {
		return false
	}
	for _, tool := range tools.Array() {
		toolType := strings.ToLower(strings.TrimSpace(tool.Get("type").String()))
		if strings.HasPrefix(toolType, "advisor_") {
			return true
		}
	}
	return false
}

func claudeThinkingDisplaySet(body []byte) bool {
	display := ojson.GetBytesNoCopy(body, "thinking.display")
	return display.Type == ojson.String && strings.TrimSpace(display.String()) != ""
}

func claudeRequestUsesFastMode(body []byte, requested map[string]bool) bool {
	if requested[claudeFastModeBeta] {
		return true
	}
	speed := ojson.GetBytesNoCopy(body, "speed")
	return speed.Type == ojson.String && strings.EqualFold(strings.TrimSpace(speed.String()), "fast")
}

var claudeCountTokensBetas = []string{
	claudeCodeBeta,
	"interleaved-thinking-2025-05-14",
	"context-management-2025-06-27",
	claudeTokenCountingBeta,
}

func claudeCountTokensBetasForCredential(oauthToken bool) string {
	betas := make([]string, 0, len(claudeCountTokensBetas)+1)
	betas = append(betas, claudeCodeBeta)
	if oauthToken {
		betas = append(betas, claudeOAuthBeta)
	}
	betas = append(betas, claudeCountTokensBetas[1:]...)
	return strings.Join(betas, ",")
}

func withClaudeCountTokensOAuthBeta(betas string) string {
	parts := make([]string, 0, len(claudeCountTokensBetas)+1)
	seen := make(map[string]bool)
	for _, beta := range strings.Split(betas, ",") {
		if beta = strings.TrimSpace(beta); beta != "" && !seen[beta] {
			parts = append(parts, beta)
			seen[beta] = true
		}
	}
	if seen[claudeOAuthBeta] {
		return strings.Join(parts, ",")
	}
	insertAt := 0
	if len(parts) > 0 && parts[0] == claudeCodeBeta {
		insertAt = 1
	}
	parts = append(parts, "")
	copy(parts[insertAt+1:], parts[insertAt:])
	parts[insertAt] = claudeOAuthBeta
	return strings.Join(parts, ",")
}

func withClaudeOAuthCredentialBetas(betas string, includeExtendedCacheTTL bool) string {
	parts := make([]string, 0, 16)
	seen := make(map[string]bool)
	for _, beta := range strings.Split(betas, ",") {
		if beta = strings.TrimSpace(beta); beta != "" && !seen[beta] {
			parts = append(parts, beta)
			seen[beta] = true
		}
	}
	if !seen[claudeOAuthBeta] {

		insertAt := 0
		if len(parts) > 0 && parts[0] == claudeCodeBeta {
			insertAt = 1
		}
		parts = append(parts, "")
		copy(parts[insertAt+1:], parts[insertAt:])
		parts[insertAt] = claudeOAuthBeta
	}
	if includeExtendedCacheTTL && !seen[claudeExtendedCacheTTLBeta] {
		parts = append(parts, claudeExtendedCacheTTLBeta)
	}
	return strings.Join(parts, ",")
}

func withoutClaudeBeta(betas, removeBeta string) string {
	parts := strings.Split(betas, ",")
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && p != removeBeta {
			res = append(res, p)
		}
	}
	return strings.Join(res, ",")
}

func withClaudeBetaAppended(betas, beta string) string {
	beta = strings.TrimSpace(beta)
	if beta == "" {
		return betas
	}
	for _, existing := range strings.Split(betas, ",") {
		if strings.TrimSpace(existing) == beta {
			return betas
		}
	}
	if strings.TrimSpace(betas) == "" {
		return beta
	}
	return betas + "," + beta
}

func withClaudeExtendedCacheTTLBeta(betas string) string {
	parts := make([]string, 0, 16)
	seen := make(map[string]bool)
	for _, beta := range strings.Split(betas, ",") {
		if beta = strings.TrimSpace(beta); beta != "" && !seen[beta] {
			parts = append(parts, beta)
			seen[beta] = true
		}
	}
	if !seen[claudeExtendedCacheTTLBeta] {
		parts = append(parts, claudeExtendedCacheTTLBeta)
	}
	return strings.Join(parts, ",")
}

func withClaudeAdvisorToolBeta(betas string) string {
	if strings.TrimSpace(betas) == "" {
		return claudeAdvisorToolBeta
	}
	parts := make([]string, 0, 16)
	seen := make(map[string]bool)
	for _, beta := range strings.Split(betas, ",") {
		if beta = strings.TrimSpace(beta); beta != "" && beta != claudeAdvisorToolBeta && !seen[beta] {
			parts = append(parts, beta)
			seen[beta] = true
		}
	}
	insertAt := len(parts)
	for index, beta := range parts {
		if beta == claudeAdvancedToolUseBeta ||
			beta == claudeEffortBeta ||
			beta == claudeServerSideFallbackBeta ||
			beta == claudeFallbackCreditBeta ||
			beta == claudeStructuredOutputsBeta ||
			beta == claudeFastModeBeta ||
			beta == claudeAFKModeBeta ||
			beta == claudeExtendedCacheTTLBeta ||
			beta == claudeCacheDiagnosisBeta {
			insertAt = index
			break
		}
	}
	parts = append(parts, "")
	copy(parts[insertAt+1:], parts[insertAt:])
	parts[insertAt] = claudeAdvisorToolBeta
	return strings.Join(parts, ",")
}

type claudeEntitlementError struct {
	statusErr
}

func (claudeEntitlementError) IsRequestScoped() bool {
	return true
}

func (claudeEntitlementError) IsCredentialScoped() bool {
	return false
}

type claudeRateLimitError struct {
	statusErr
	credentialScoped bool
}

func (e claudeRateLimitError) IsCredentialScoped() bool {
	return e.credentialScoped
}

func (e claudeRateLimitError) IsRequestScoped() bool {
	return false
}

func classifyClaudeUpstreamError(statusCode int, headers http.Header, body []byte) error {
	return classifyClaudeUpstreamErrorWithCooling(statusCode, headers, body, false)
}

func classifyClaudeUpstreamErrorWithCooling(statusCode int, headers http.Header, body []byte, modelLevelCooling bool) error {
	var retryAfter *time.Duration
	if statusCode == http.StatusTooManyRequests || (statusCode >= 400 && statusCode < 600) {
		retryAfter = helps.ParseClaudeRateLimitResetForError(headers, body, time.Now())
	}
	err := statusErr{code: statusCode, msg: string(body), retryAfter: retryAfter}
	if statusCode == http.StatusTooManyRequests {
		if !modelLevelCooling && helps.ClaudeHeadersIndicateUnifiedRateLimitRejection(headers) {
			return claudeRateLimitError{statusErr: err, credentialScoped: true}
		}
		if claudeBodyIndicatesFastModeCredits(body) {
			return claudeEntitlementError{err}
		}

		return claudeRateLimitError{statusErr: err, credentialScoped: false}
	}
	return err
}

func claudeBodyIndicatesFastModeCredits(body []byte) bool {
	message := strings.ToLower(ojson.GetBytesNoCopy(body, "error.message").String())
	if message == "" {
		message = strings.ToLower(string(body))
	}
	return strings.Contains(message, "fast request rejected") ||
		(strings.Contains(message, "fast") &&
			(strings.Contains(message, "usage credits") || strings.Contains(message, "credits are required")))
}

func claudeRequestedBetas(incomingBetas string, extraBetas []string) map[string]bool {
	requested := make(map[string]bool)
	for _, beta := range strings.Split(incomingBetas, ",") {
		if beta = strings.TrimSpace(beta); beta != "" {
			requested[beta] = true
		}
	}
	for _, beta := range extraBetas {
		if beta = strings.TrimSpace(beta); beta != "" {
			requested[beta] = true
		}
	}
	return requested
}

func isAnthropicUpstreamURL(u *url.URL) bool {
	return helps.IsAnthropicUpstreamURL(u)
}

func isAnthropicUpstreamBase(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	return isAnthropicUpstreamURL(parsed)
}

func extractAndRemoveBetas(body []byte) ([]string, []byte) {
	doc := ojson.NewDoc(body)
	betas, removed := extractAndRemoveBetasDoc(doc)
	if !removed {
		return betas, body
	}
	return betas, doc.Bytes()
}

func extractAndRemoveBetasDoc(doc *ojson.Doc) ([]string, bool) {
	if doc == nil {
		return nil, false
	}
	betasResult := doc.Get("betas")
	if !betasResult.Exists() {
		return nil, false
	}
	var betas []string
	if betasResult.IsArray() {
		for _, item := range betasResult.Array() {
			if s := strings.TrimSpace(item.String()); s != "" {
				betas = append(betas, strings.Clone(s))
			}
		}
	} else if s := strings.TrimSpace(betasResult.String()); s != "" {
		betas = append(betas, strings.Clone(s))
	}
	return betas, doc.Delete("betas") == nil
}

func claudeToolChoiceForcesToolUse(body []byte) bool {
	toolChoiceType := ojson.GetBytesNoCopy(body, "tool_choice.type").String()
	return toolChoiceType == "any" || toolChoiceType == "tool"
}

func claudeToolChoiceForcesToolUseDoc(doc *ojson.Doc) bool {
	if doc == nil {
		return false
	}
	toolChoiceType := doc.Get("tool_choice.type").String()
	return toolChoiceType == "any" || toolChoiceType == "tool"
}

func disableThinkingIfToolChoiceForced(body []byte) []byte {
	doc := ojson.NewDoc(body)
	if !disableThinkingIfToolChoiceForcedDoc(doc) {
		return body
	}
	return doc.Bytes()
}

func disableThinkingIfToolChoiceForcedDoc(doc *ojson.Doc) bool {

	if claudeToolChoiceForcesToolUseDoc(doc) {
		changed := false

		if doc.Delete("thinking") == nil {
			changed = true
		}

		if doc.Delete("output_config.effort") == nil {
			changed = true
		}
		if oc := doc.Get("output_config"); oc.Exists() && oc.IsObject() && len(oc.Map()) == 0 {
			if doc.Delete("output_config") == nil {
				changed = true
			}
		}
		return changed
	}
	return false
}

func normalizeClaudeSamplingForUpstream(body []byte, nativeOwned bool) []byte {
	doc := ojson.NewDoc(body)
	if !normalizeClaudeSamplingForUpstreamDoc(doc, nativeOwned) {
		return body
	}
	return doc.Bytes()
}

func normalizeClaudeSamplingForUpstreamDoc(doc *ojson.Doc, nativeOwned bool) bool {
	if doc == nil {
		return false
	}
	thinkingActive := false
	switch strings.ToLower(strings.TrimSpace(doc.Get("thinking.type").String())) {
	case "enabled", "adaptive", "auto":
		thinkingActive = true
	}

	changed := false
	if !nativeOwned {
		if doc.Delete("temperature") == nil {
			changed = true
		}
		if doc.Delete("top_p") == nil {
			changed = true
		}
		if thinkingActive {
			if doc.Delete("top_k") == nil {
				changed = true
			}
		}
		return changed
	}

	if thinkingActive {
		if temperature := doc.Get("temperature"); temperature.Exists() && temperature.Num != 1 {
			if doc.Delete("temperature") == nil {
				changed = true
			}
		}
		if topP := doc.Get("top_p"); topP.Exists() && topP.Num < 0.95 {
			if doc.Delete("top_p") == nil {
				changed = true
			}
		}
		if doc.Delete("top_k") == nil {
			changed = true
		}
		return changed
	}

	if doc.Get("temperature").Exists() && doc.Get("top_p").Exists() {
		if doc.Delete("top_p") == nil {
			changed = true
		}
	}
	return changed
}

func claudeResponseContentEncoding(header http.Header) string {
	return strings.Join(header.Values("Content-Encoding"), ",")
}

func decodeResponseBody(body io.ReadCloser, contentEncoding string) (io.ReadCloser, error) {
	return helps.DecodeResponseBody(body, http.Header{"Content-Encoding": {contentEncoding}})
}

func claudeCredentialUsesOAuth(auth *cliproxyauth.Auth, apiKey string) bool {
	if isClaudeOAuthToken(apiKey) {
		return true
	}
	if auth != nil && auth.AuthKind() == cliproxyauth.AuthKindAPIKey {
		return false
	}
	hasAPIKeyAttr := auth != nil && auth.Attributes != nil && strings.TrimSpace(auth.Attributes["api_key"]) != ""
	return !hasAPIKeyAttr
}

func copyClaudeCallerFingerprintHeaders(dst, src http.Header, confirmedClaudeCode, uncloak bool) {
	if dst == nil || src == nil {
		return
	}
	for name, values := range src {
		lowerName := strings.ToLower(strings.TrimSpace(name))
		if lowerName != "accept" && lowerName != "accept-encoding" && lowerName != "user-agent" &&
			lowerName != "x-app" && lowerName != "x-client-request-id" &&
			!strings.HasPrefix(lowerName, "anthropic-") &&
			!strings.HasPrefix(lowerName, "x-stainless-") &&
			!strings.HasPrefix(lowerName, "x-claude-code-") &&
			!strings.HasPrefix(lowerName, "x-claude-remote-") &&
			lowerName != "x-client-app" &&
			lowerName != "x-anthropic-additional-protection" {
			continue
		}
		if !confirmedClaudeCode && (strings.HasPrefix(lowerName, "x-claude-code-") || strings.HasPrefix(lowerName, "x-claude-remote-")) {
			continue
		}
		if uncloak {
			if lowerName == "x-app" ||
				lowerName == "x-client-request-id" ||
				lowerName == "x-client-app" ||
				lowerName == "x-anthropic-additional-protection" ||
				lowerName == "anthropic-dangerous-direct-browser-access" ||
				strings.HasPrefix(lowerName, "x-stainless-") ||
				strings.HasPrefix(lowerName, "x-claude-code-") ||
				strings.HasPrefix(lowerName, "x-claude-remote-") {
				continue
			}
			if lowerName == "user-agent" && len(values) > 0 && helps.IsClaudeCodeUserAgent(values[0]) {
				continue
			}
		}
		dst.Del(name)
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

func applyClaudeHeaders(r *http.Request, auth *cliproxyauth.Auth, apiKey string, stream bool, extraBetas []string, body []byte, cfg *config.Config, incomingHeaders http.Header, confirmedClaudeCode bool, sessionIDs ...string) error {
	return applyClaudeHeadersWithNativeProfile(
		r,
		auth,
		apiKey,
		stream,
		extraBetas,
		body,
		cfg,
		incomingHeaders,
		confirmedClaudeCode,
		false,
		sessionIDs...,
	)
}

func applyClaudeHeadersWithNativeProfile(
	r *http.Request,
	auth *cliproxyauth.Auth,
	apiKey string,
	stream bool,
	extraBetas []string,
	body []byte,
	cfg *config.Config,
	incomingHeaders http.Header,
	confirmedClaudeCode bool,
	helperProfile bool,
	sessionIDs ...string,
) error {
	if r == nil {
		return nil
	}
	hdrDefault := func(cfgVal, fallback string) string {
		if cfgVal != "" {
			return cfgVal
		}
		return fallback
	}

	var hd config.ClaudeHeaderDefaults
	if cfg != nil {
		hd = cfg.ClaudeHeaderDefaults
	}

	credentialUsesBearer := claudeCredentialUsesOAuth(auth, apiKey)
	useAPIKey := !credentialUsesBearer
	fp := resolveClaudeFingerprintPolicy(cfg, auth, apiKey)
	wirePolicy, _ := resolveClaudeWirePolicy(cfg, auth, apiKey, confirmedClaudeCode)
	if wirePolicy.Uncloak {
		confirmedClaudeCode = false
		helperProfile = false
		sessionIDs = nil
	}
	applyCLIFingerprint := fp.ProfileClaudeCodeCLI || wirePolicy.Cloak
	preserveCallerFingerprint := !applyCLIFingerprint && !confirmedClaudeCode
	useOAuthBetas := fp.UseOAuthBetas
	isAnthropicBase := isAnthropicUpstreamURL(r.URL)
	if strings.TrimSpace(apiKey) != "" {
		if isAnthropicBase && useAPIKey {
			r.Header.Del("Authorization")
			r.Header.Set("x-api-key", apiKey)
		} else {
			r.Header.Del("x-api-key")
			r.Header.Set("Authorization", "Bearer "+apiKey)
		}
	} else {
		r.Header.Del("Authorization")
		r.Header.Del("x-api-key")
	}
	r.Header.Set("Content-Type", "application/json")

	if incomingHeaders == nil {
		if ginCtx, ok := r.Context().Value("gin").(*gin.Context); ok && ginCtx != nil && ginCtx.Request != nil {
			incomingHeaders = ginCtx.Request.Header
		}
	}
	stabilizeDeviceProfile := helps.ClaudeDeviceProfileStabilizationEnabled(cfg)
	var deviceProfile helps.ClaudeDeviceProfile

	incomingBetas := strings.TrimSpace(strings.Join(helps.HeaderValuesCaseInsensitive(incomingHeaders, "Anthropic-Beta"), ","))
	countTokens := r.URL != nil && strings.HasSuffix(r.URL.Path, "/count_tokens")
	if !confirmedClaudeCode && !wirePolicy.Cloak && wirePolicy.ExplicitNever && !wirePolicy.Uncloak {
		det := helps.DetectClaudeCodeRequest(incomingHeaders, body, countTokens, cfg)
		if det.ConfirmedShape {
			confirmedClaudeCode = true
			helperProfile = helperProfile || (!det.BetasPresent && !countTokens)
		}
	}
	requestedMap := claudeRequestedBetas(incomingBetas, extraBetas)
	advisorNeeded := requestedMap[claudeAdvisorToolBeta] || claudeBodyHasAdvisorTool(body)

	baseBetas := incomingBetas
	if !preserveCallerFingerprint {
		baseBetas = claudeCodeCLIBetas(body, requestedMap, useOAuthBetas)
		if countTokens {
			baseBetas = claudeCountTokensBetasForCredential(useOAuthBetas)
			if advisorNeeded {
				baseBetas = withClaudeAdvisorToolBeta(baseBetas)
			}
		}
	}
	if confirmedClaudeCode && incomingBetas != "" {
		baseBetas = incomingBetas
		if advisorNeeded {
			baseBetas = withClaudeAdvisorToolBeta(baseBetas)
		}

		if useOAuthBetas {
			if countTokens {
				baseBetas = withClaudeCountTokensOAuthBeta(baseBetas)
			} else {
				isSubagent := helps.IsClaudeSubagentRequest(incomingHeaders, body)
				isProbe := helps.IsClaudeProbeOrHelperRequest(body)
				subagent1h := isSubagent && helps.ClaudeSubagentRequests1h(incomingHeaders, body)
				includeExtendedCacheTTL := !helperProfile && (!isSubagent || subagent1h) && !isProbe
				baseBetas = withClaudeOAuthCredentialBetas(baseBetas, includeExtendedCacheTTL)
			}
		}
	} else if preserveCallerFingerprint && useOAuthBetas {
		if countTokens {
			baseBetas = withClaudeCountTokensOAuthBeta(baseBetas)
		} else {
			baseBetas = withClaudeOAuthCredentialBetas(baseBetas, false)
		}
	}
	if preserveCallerFingerprint && advisorNeeded {
		baseBetas = withClaudeAdvisorToolBeta(baseBetas)
	}
	if !claudeRequestSupportsEffort(body, nil) {
		baseBetas = withoutClaudeBeta(baseBetas, claudeEffortBeta)
	}
	existingSet := make(map[string]bool)
	for _, beta := range strings.Split(baseBetas, ",") {
		if beta = strings.TrimSpace(beta); beta != "" {
			existingSet[beta] = true
		}
	}
	appendBeta := func(beta string) {
		beta = strings.TrimSpace(beta)
		if beta == "" || existingSet[beta] {
			return
		}
		if strings.TrimSpace(baseBetas) == "" {
			baseBetas = beta
		} else {
			baseBetas += "," + beta
		}
		existingSet[beta] = true
	}
	if preserveCallerFingerprint {

		if strings.EqualFold(strings.TrimSpace(ojson.GetBytesNoCopy(body, "speed").String()), "fast") {
			appendBeta(claudeFastModeBeta)
		}
		for _, beta := range extraBetas {
			appendBeta(beta)
		}
	} else {

		if !confirmedClaudeCode && incomingBetas != "" {
			for _, beta := range strings.Split(incomingBetas, ",") {
				beta = strings.TrimSpace(beta)
				if beta == "" {
					continue
				}
				if isManagedClaudeBeta(beta) && isAnthropicBase {
					continue
				}
				appendBeta(beta)
			}
		}
		if !isAnthropicBase {
			for _, beta := range extraBetas {
				appendBeta(beta)
			}
		}
	}
	applyBetaHeader := func() {
		if wirePolicy.Uncloak {
			baseBetas = withoutClaudeBeta(baseBetas, claudeCodeBeta)
			baseBetas = withoutClaudeBeta(baseBetas, claudeOAuthBeta)
			baseBetas = withoutClaudeBeta(baseBetas, claudeRedactThinkingBeta)
			baseBetas = withoutClaudeBeta(baseBetas, claudeAFKModeBeta)
			if !ojson.GetBytesNoCopy(body, "diagnostics").Exists() {
				baseBetas = withoutClaudeBeta(baseBetas, claudeCacheDiagnosisBeta)
			}
			if !ojson.GetBytesNoCopy(body, "fallback_credit_token").Exists() {
				baseBetas = withoutClaudeBeta(baseBetas, claudeFallbackCreditBeta)
			}
			if !helps.ClaudePayloadHas1hTTL(body) {
				baseBetas = withoutClaudeBeta(baseBetas, claudeExtendedCacheTTLBeta)
			}
		}

		if !claudeRequestSupportsEffort(body, nil) {
			baseBetas = withoutClaudeBeta(baseBetas, claudeEffortBeta)
		}
		reqProbeOrHelper := helps.IsClaudeProbeOrHelperRequest(body)
		if reqProbeOrHelper && !helperProfile {
			baseBetas = withoutClaudeBeta(baseBetas, claudeServerSideFallbackBeta)
			baseBetas = withoutClaudeBeta(baseBetas, claudeThinkingDisplayUpdatesBeta)
			baseBetas = withoutClaudeBeta(baseBetas, claudeExtendedCacheTTLBeta)
		}
		reqThinkingType := ojson.GetBytesNoCopy(body, "thinking.type").String()
		if reqThinkingType == "disabled" {
			baseBetas = withoutClaudeBeta(baseBetas, claudeThinkingDisplayUpdatesBeta)
		}
		if helps.IsClaudeSubagentRequest(incomingHeaders, body) && !helps.ClaudeSubagentRequests1h(incomingHeaders, body) {
			baseBetas = withoutClaudeBeta(baseBetas, claudeExtendedCacheTTLBeta)
		}
		if !reqProbeOrHelper && !countTokens && helps.ClaudePayloadHas1hTTL(body) {
			baseBetas = withClaudeExtendedCacheTTLBeta(baseBetas)
		}
		reqModel := strings.ToLower(strings.TrimSpace(ojson.GetBytesNoCopy(body, "model").String()))
		if isClaudeHaikuModel(reqModel) && !ojson.GetBytesNoCopy(body, "fallbacks").Exists() && !helperProfile {
			baseBetas = withoutClaudeBeta(baseBetas, claudeServerSideFallbackBeta)
		}

		if ojson.GetBytesNoCopy(body, "thinking.block_binding").Exists() {
			baseBetas = withClaudeBetaAppended(baseBetas, claudeThinkingBindingBeta)
		}

		emittedBetas := baseBetas
		for _, beta := range hd.AnthropicBetaStrip {
			emittedBetas = withoutClaudeBeta(emittedBetas, beta)
		}
		for _, beta := range hd.AnthropicBetaAppend {
			emittedBetas = withClaudeBetaAppended(emittedBetas, beta)
		}

		if strings.TrimSpace(emittedBetas) == "" {
			r.Header.Del("Anthropic-Beta")
			return
		}
		r.Header.Set("Anthropic-Beta", emittedBetas)
	}
	applyBetaHeader()

	if preserveCallerFingerprint {
		defaultAccept := "application/json"
		defaultAcceptEncoding := "gzip, deflate, br, zstd"
		if stream && !isAnthropicBase {
			defaultAccept = "text/event-stream"
			defaultAcceptEncoding = "identity"
		}
		copyClaudeCallerFingerprintHeaders(r.Header, incomingHeaders, confirmedClaudeCode, wirePolicy.Uncloak)
		if hd.ClientRequestID == config.ClaudeClientRequestIDOff {
			r.Header.Del("X-Client-Request-Id")
		}
		misc.EnsureHeader(r.Header, incomingHeaders, "Anthropic-Version", "2023-06-01")
		misc.EnsureHeader(r.Header, incomingHeaders, "Accept", defaultAccept)
		misc.EnsureHeader(r.Header, incomingHeaders, "Accept-Encoding", defaultAcceptEncoding)

		uaSourceHeaders := incomingHeaders
		if wirePolicy.Uncloak && helps.IsClaudeCodeUserAgent(helps.HeaderValueCaseInsensitive(incomingHeaders, "User-Agent")) {
			uaSourceHeaders = nil
		}
		misc.EnsureHeader(r.Header, uaSourceHeaders, "User-Agent", "CLIProxyAPI/"+buildinfo.Version)
		applyBetaHeader()
		sessionID := ""
		for _, candidate := range sessionIDs {
			if candidate = strings.TrimSpace(candidate); candidate != "" {
				sessionID = candidate
				break
			}
		}
		if sessionID != "" {
			r.Header.Set("X-Claude-Code-Session-Id", sessionID)
		}
		var attrs map[string]string
		if auth != nil {
			attrs = auth.Attributes
		}
		util.ApplyCustomHeadersFromAttrs(r, attrs, incomingHeaders)

		restoreCallerTransport := func() {
			resetHeader := func(name, fallback string) {
				if value := strings.TrimSpace(incomingHeaders.Get(name)); value != "" {
					r.Header.Set(name, value)
					return
				}
				r.Header.Set(name, fallback)
			}
			resetHeader("Accept", defaultAccept)
			resetHeader("Accept-Encoding", defaultAcceptEncoding)
		}
		if isAnthropicBase {
			applyBetaHeader()
			restoreCallerTransport()
		} else if stream {
			restoreCallerTransport()
		}
		return nil
	}

	identityHeader := func(name, fallback string) {
		if confirmedClaudeCode {
			misc.EnsureHeader(r.Header, incomingHeaders, name, fallback)
			return
		}
		r.Header.Set(name, fallback)
	}
	identityHeader("Anthropic-Version", "2023-06-01")
	identityHeader("Anthropic-Dangerous-Direct-Browser-Access", "true")
	identityHeader("X-App", "cli")

	identityHeader("X-Stainless-Retry-Count", "0")
	identityHeader("X-Stainless-Runtime", "node")
	identityHeader("X-Stainless-Lang", "js")

	if confirmedClaudeCode && helps.HeaderValueCaseInsensitive(incomingHeaders, "X-Stainless-Async") == "async" {
		r.Header.Set("X-Stainless-Async", "async")
	}

	if !countTokens {
		identityHeader("X-Stainless-Timeout", hdrDefault(hd.Timeout, "600"))
	} else if confirmedClaudeCode {
		if incomingTimeout := helps.HeaderValueCaseInsensitive(incomingHeaders, "X-Stainless-Timeout"); incomingTimeout != "" {
			r.Header.Set("X-Stainless-Timeout", incomingTimeout)
		}
	}

	sessionID := ""
	for _, candidate := range sessionIDs {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			sessionID = candidate
			break
		}
	}
	if sessionID != "" {
		r.Header.Set("X-Claude-Code-Session-Id", sessionID)
	} else {
		var errSessionID error
		sessionID, errSessionID = helps.CachedSessionIDRequired(r.Context(), apiKey)
		if errSessionID != nil {
			return errSessionID
		}
		identityHeader("X-Claude-Code-Session-Id", sessionID)
	}

	for _, hdr := range []string{
		"X-Claude-Code-Agent-Id",
		"X-Claude-Code-Parent-Agent-Id",
		"X-Claude-Remote-Container-Id",
		"X-Claude-Remote-Session-Id",
		"X-Client-App",
		"X-Anthropic-Additional-Protection",
	} {
		if val := helps.HeaderValueCaseInsensitive(incomingHeaders, hdr); val != "" {
			r.Header.Set(hdr, val)
		}
	}

	if confirmedClaudeCode {
		for _, hdr := range []string{
			"X-Claude-Code-Request-Class",
			"X-Claude-Code-Agent-Type",
			"X-Claude-Code-Prev-Tool-Durations",
			"X-Claude-Code-Compaction",
			"X-Claude-Code-Context-Compacted",
		} {
			if val := helps.HeaderValueCaseInsensitive(incomingHeaders, hdr); val != "" {
				r.Header.Set(hdr, val)
			}
		}
	}

	incomingClientRequestID := helps.HeaderValueCaseInsensitive(incomingHeaders, "x-client-request-id")
	if hd.ClientRequestID != config.ClaudeClientRequestIDOff && (isAnthropicBase || (helperProfile && incomingClientRequestID != "")) {
		if hd.ClientRequestID != config.ClaudeClientRequestIDPassthrough {
			identityHeader("x-client-request-id", uuid.New().String())
		} else if incomingClientRequestID != "" {
			r.Header.Set("x-client-request-id", incomingClientRequestID)
		}
	}
	r.Header.Set("Connection", "keep-alive")

	applyTransportNegotiation := func() {
		if helperProfile {
			identityHeader("Accept", "application/json")
			identityHeader("Accept-Encoding", "gzip, deflate, br, zstd")
			return
		}
		if stream && !isAnthropicBase {

			r.Header.Set("Accept", "text/event-stream")
			r.Header.Set("Accept-Encoding", "identity")
			return
		}
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	}
	applyTransportNegotiation()

	if wirePolicy.ExplicitNever && confirmedClaudeCode {
		helps.ApplyClaudeLegacyDeviceHeaders(r, incomingHeaders, cfg, true, true)
	} else if stabilizeDeviceProfile {
		if confirmedClaudeCode {
			var errDeviceProfile error
			deviceProfile, errDeviceProfile = helps.ResolveClaudeDeviceProfileRequired(r.Context(), auth, apiKey, incomingHeaders, cfg)
			if errDeviceProfile != nil {
				return errDeviceProfile
			}
			helps.ApplyClaudeDeviceProfileHeaders(r, deviceProfile, helps.HeaderValueCaseInsensitive(incomingHeaders, "User-Agent"))
		} else {
			helps.ApplyClaudeDefaultDeviceProfileHeaders(r, cfg)
		}
	} else {
		helps.ApplyClaudeLegacyDeviceHeaders(r, incomingHeaders, cfg, confirmedClaudeCode)
	}
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(r, attrs, incomingHeaders)

	if isAnthropicBase {
		applyBetaHeader()
		applyTransportNegotiation()
	} else if stream {

		applyTransportNegotiation()
	}
	return nil
}

func doClaudeUpstreamRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	applyClaudeWireHeaderCasing(req)
	cliproxyexecutor.MarkUpstreamAttempt(req.Context())
	return client.Do(req)
}

var claudeWireHeaderCasing = map[string]string{
	"X-Stainless-Os":      "X-Stainless-OS",
	"Anthropic-Beta":      "anthropic-beta",
	"Anthropic-Version":   "anthropic-version",
	"X-App":               "x-app",
	"X-Client-Request-Id": "x-client-request-id",

	"Anthropic-Dangerous-Direct-Browser-Access": "anthropic-dangerous-direct-browser-access",
}

func applyClaudeWireHeaderCasing(r *http.Request) {
	if r == nil || r.Header == nil || !isAnthropicUpstreamURL(r.URL) {
		return
	}
	for canonical, wire := range claudeWireHeaderCasing {
		values, ok := r.Header[canonical]
		if !ok {
			continue
		}
		delete(r.Header, canonical)
		r.Header[wire] = values
	}
}

func claudeCreds(a *cliproxyauth.Auth) (apiKey, baseURL string) {
	if a == nil {
		return "", ""
	}
	if a.Attributes != nil {
		apiKey = a.Attributes["api_key"]
		baseURL = a.Attributes["base_url"]
	}
	if apiKey == "" {
		apiKey = claudeauth.ReadMetadataString(&a.Metadata, "access_token")
	}
	return
}

func claudePayloadHasMidSystemMessage(payload []byte) bool {
	return claudeMessagesHaveMidSystemMessage(ojson.GetBytesNoCopy(payload, "messages"))
}

func claudeDocHasMidSystemMessage(doc *ojson.Doc) bool {
	if doc == nil {
		return false
	}
	return claudeMessagesHaveMidSystemMessage(doc.Get("messages"))
}

func claudeMessagesHaveMidSystemMessage(messages ojson.Result) bool {
	if !messages.IsArray() {
		return false
	}
	found := false
	messages.ForEach(func(_, message ojson.Result) bool {
		if strings.EqualFold(strings.TrimSpace(message.Get("role").String()), "system") {
			found = true
			return false
		}
		return true
	})
	return found
}

func rebuildMidSystemMessagesToTopLevel(payload []byte) []byte {
	doc := ojson.NewDoc(payload)
	if !rebuildMidSystemMessagesToTopLevelDoc(doc) {
		return payload
	}
	return doc.Bytes()
}

func rebuildMidSystemMessagesToTopLevelDoc(doc *ojson.Doc) bool {
	if doc == nil {
		return false
	}
	messages := doc.Get("messages")
	if !messages.IsArray() {
		return false
	}

	var movedSystemParts []string
	keptMessages := make([]string, 0, int(messages.Get("#").Int()))
	messages.ForEach(func(_, message ojson.Result) bool {
		if strings.EqualFold(strings.TrimSpace(message.Get("role").String()), "system") {
			movedSystemParts = append(movedSystemParts, claudeSystemTextParts(message.Get("content"))...)
			return true
		}
		keptMessages = append(keptMessages, message.Raw)
		return true
	})
	if len(movedSystemParts) == 0 {
		return false
	}

	changed := false
	systemParts := claudeSystemTextParts(doc.Get("system"))
	systemParts = append(systemParts, movedSystemParts...)
	if len(systemParts) > 0 {
		if errSetSystem := doc.SetRawBytes("system", rawJSONArray(systemParts)); errSetSystem == nil {
			changed = true
		}
	}
	if errSetMessages := doc.SetRawBytes("messages", rawJSONArray(keptMessages)); errSetMessages == nil {
		changed = true
	}
	return changed
}

func claudeSystemTextParts(content ojson.Result) []string {
	if !content.Exists() {
		return nil
	}
	if content.Type == ojson.String {
		text := content.String()
		if strings.TrimSpace(text) == "" {
			return nil
		}
		block := []byte(`{"type":"text","text":""}`)
		block, _ = ojson.SetBytes(block, "text", text)
		return []string{string(block)}
	}
	if !content.IsArray() {
		return nil
	}

	var parts []string
	content.ForEach(func(_, item ojson.Result) bool {
		if item.Type == ojson.String {
			text := item.String()
			if strings.TrimSpace(text) != "" {
				block := []byte(`{"type":"text","text":""}`)
				block, _ = ojson.SetBytes(block, "text", text)
				parts = append(parts, string(block))
			}
			return true
		}
		if item.IsObject() && item.Get("type").String() == "text" && strings.TrimSpace(item.Get("text").String()) != "" {
			parts = append(parts, item.Raw)
		}
		return true
	})
	return parts
}

func rawJSONArray(items []string) []byte {
	if len(items) == 0 {
		return []byte("[]")
	}
	var builder strings.Builder
	builder.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(item)
	}
	builder.WriteByte(']')
	return []byte(builder.String())
}

func isClaudeOAuthToken(apiKey string) bool {
	return strings.Contains(apiKey, "sk-ant-oat")
}

type claudeMCPAliasOptions struct {
	secret string
}

func resolveClaudeMCPAliasOptions(ctx context.Context, metadataSets ...map[string]any) claudeMCPAliasOptions {

	secret := callerScopeFromMetadataSets(metadataSets...)
	if secret == "" {
		secret = session.CallerScope(helps.APIKeyFromContext(ctx))
	}
	if secret == "" {
		secret = "cpa-claude-mcp-default-caller"
	}
	return claudeMCPAliasOptions{secret: secret}
}

func callerScopeFromMetadataSets(metadataSets ...map[string]any) string {
	for _, metadata := range metadataSets {
		if scope := metadataString(metadata, cliproxyexecutor.CallerScopeMetadataKey); scope != "" {
			return scope
		}
	}
	return ""
}

func prepareClaudeOAuthToolNamesForUpstream(body []byte, mcpAliases claudeMCPAliasOptions) ([]byte, map[string]string) {
	return remapOAuthToolNamesWithOptions(body, mcpAliases)
}

func prepareClaudeOAuthToolNamesForUpstreamDoc(doc *ojson.Doc, mcpAliases claudeMCPAliasOptions) (map[string]string, bool) {
	return remapOAuthToolNamesWithOptionsDoc(doc, mcpAliases)
}

func restoreClaudeOAuthToolNamesFromResponse(body []byte, reverseMap map[string]string) ([]byte, error) {
	return reverseRemapOAuthToolNames(body, reverseMap)
}

func restoreClaudeOAuthToolNamesFromStreamLine(line []byte, reverseMap map[string]string) ([]byte, error) {
	return reverseRemapOAuthToolNamesFromStreamLine(line, reverseMap)
}

func remapOAuthToolNames(body []byte) ([]byte, map[string]string) {
	return remapOAuthToolNamesWithOptions(body, claudeMCPAliasOptions{secret: "cpa-claude-mcp-default-caller"})
}

type claudeRawJSONEdit struct {
	start       int
	end         int
	replacement string
}

func remapOAuthToolNamesWithOptions(body []byte, mcpAliases claudeMCPAliasOptions) ([]byte, map[string]string) {
	remapped, reverseMap, ok := remapOAuthToolNamesWithBatchedEdits(body, mcpAliases)
	if ok {
		return remapped, reverseMap
	}
	return remapOAuthToolNamesWithOptionsLegacy(body, mcpAliases)
}

func remapOAuthToolNamesWithBatchedEdits(body []byte, mcpAliases claudeMCPAliasOptions) ([]byte, map[string]string, bool) {
	if !ojson.ValidBytes(body) {
		return nil, nil, false
	}

	reverseMap := make(map[string]string)
	recordRename := func(original, renamed string) {

		if _, exists := reverseMap[renamed]; !exists {
			reverseMap[renamed] = original
		}
	}

	tools := ojson.GetBytesNoCopy(body, "tools")
	forwardMap := make(map[string]string)
	protectedNames := make(map[string]bool)
	reservedNames := helps.AugmentClaudeBuiltinToolRegistry(body, nil)
	if tools.Exists() && tools.IsArray() {
		tools.ForEach(func(_, tool ojson.Result) bool {
			name := tool.Get("name").String()
			if name != "" {
				reservedNames[strings.Clone(name)] = true
			}
			if helps.IsClaudeServerToolType(tool.Get("type").String()) {
				protectedNames[strings.Clone(name)] = true
			}
			return true
		})
		passthroughMCPTools := make([]string, 0, 4)
		tools.ForEach(func(_, tool ojson.Result) bool {
			if helps.IsClaudeServerToolType(tool.Get("type").String()) {
				return true
			}
			name := strings.Clone(tool.Get("name").String())
			if name == "" {
				return true
			}
			if helps.IsClaudeMCPToolName(name) {
				passthroughMCPTools = append(passthroughMCPTools, name)
				return true
			}
			if _, exists := forwardMap[name]; exists {
				return true
			}
			alias, allocated := helps.AllocateClaudeMCPToolAlias(mcpAliases.secret, name, reservedNames)
			if !allocated {
				log.Warnf("claude oauth mcp alias: no free alias left for tool %q, forwarding the original name", name)
				return true
			}
			forwardMap[name] = alias
			reservedNames[alias] = true
			return true
		})
		recordPassthroughMCPTools(recordRename, forwardMap, passthroughMCPTools)
	}

	rewriteName := func(name string) (string, bool) {
		if name == "" || protectedNames[name] || helps.IsClaudeMCPToolName(name) {
			return name, false
		}
		if newName, ok := forwardMap[name]; ok && newName != name {
			return newName, true
		}
		return name, false
	}

	edits := make([]claudeRawJSONEdit, 0, len(forwardMap)+1)
	appendRawEdit := func(result ojson.Result, replacement string) bool {
		start := result.Index
		end := start + len(result.Raw)
		if result.Raw == "" || start < 0 || end < start || end > len(body) || !bytes.Equal(body[start:end], []byte(result.Raw)) {
			return false
		}
		edits = append(edits, claudeRawJSONEdit{start: start, end: end, replacement: replacement})
		return true
	}
	appendStringEdit := func(result ojson.Result, replacement string) bool {

		return appendRawEdit(result, `"`+replacement+`"`)
	}

	toolsNeedRewrite := false
	if tools.Exists() && tools.IsArray() {
		tools.ForEach(func(_, tool ojson.Result) bool {
			toolType := tool.Get("type").String()
			if helps.IsClaudeServerToolType(toolType) {
				return true
			}
			if strings.TrimSpace(toolType) != "" {
				toolsNeedRewrite = true
				return false
			}
			name := tool.Get("name").String()
			_, toolsNeedRewrite = rewriteName(name)
			return !toolsNeedRewrite
		})
	}
	if toolsNeedRewrite {
		var toolsJSON strings.Builder
		toolsJSON.WriteByte('[')
		toolCount := 0
		tools.ForEach(func(_, tool ojson.Result) bool {
			if helps.IsClaudeServerToolType(tool.Get("type").String()) {
				if toolCount > 0 {
					toolsJSON.WriteByte(',')
				}
				toolsJSON.WriteString(tool.Raw)
				toolCount++
				return true
			}

			name := strings.Clone(tool.Get("name").String())
			hasType := strings.TrimSpace(tool.Get("type").String()) != ""
			newName, renamed := rewriteName(name)
			toolJSON := tool.Raw
			if hasType || renamed {
				toolDoc := ojson.NewDoc([]byte(tool.Raw))
				if hasType {
					_ = toolDoc.Delete("type")
				}
				if renamed {
					if errSet := toolDoc.Set("name", newName); errSet == nil {
						recordRename(name, newName)
					}
				}
				toolJSON = toolDoc.String()
			}

			if toolCount > 0 {
				toolsJSON.WriteByte(',')
			}
			toolsJSON.WriteString(toolJSON)
			toolCount++
			return true
		})
		toolsJSON.WriteByte(']')
		if !appendRawEdit(tools, toolsJSON.String()) {
			return nil, nil, false
		}
	}

	toolChoice := ojson.GetBytesNoCopy(body, "tool_choice")
	if toolChoice.Get("type").String() == "tool" {
		nameResult := toolChoice.Get("name")
		tcName := strings.Clone(nameResult.String())
		if newName, renamed := rewriteName(tcName); renamed {
			if !appendStringEdit(nameResult, newName) {
				return nil, nil, false
			}
			recordRename(tcName, newName)
		}
	}

	messages := ojson.GetBytesNoCopy(body, "messages")
	validOffsets := true
	if messages.Exists() && messages.IsArray() {
		messages.ForEach(func(_, msg ojson.Result) bool {
			content := msg.Get("content")
			if !content.Exists() || !content.IsArray() {
				return true
			}
			content.ForEach(func(_, part ojson.Result) bool {
				switch part.Get("type").String() {
				case "tool_use":
					nameResult := part.Get("name")
					name := strings.Clone(nameResult.String())
					if newName, renamed := rewriteName(name); renamed {
						if !appendStringEdit(nameResult, newName) {
							validOffsets = false
							return false
						}
						recordRename(name, newName)
					}
				case "tool_reference":
					nameResult := part.Get("tool_name")
					toolName := strings.Clone(nameResult.String())
					if newName, renamed := rewriteName(toolName); renamed {
						if !appendStringEdit(nameResult, newName) {
							validOffsets = false
							return false
						}
						recordRename(toolName, newName)
					}
				case "tool_result":
					nestedContent := part.Get("content")
					if nestedContent.Exists() && nestedContent.IsArray() {
						nestedContent.ForEach(func(_, nestedPart ojson.Result) bool {
							if nestedPart.Get("type").String() != "tool_reference" {
								return true
							}
							nameResult := nestedPart.Get("tool_name")
							nestedToolName := strings.Clone(nameResult.String())
							if newName, renamed := rewriteName(nestedToolName); renamed {
								if !appendStringEdit(nameResult, newName) {
									validOffsets = false
									return false
								}
								recordRename(nestedToolName, newName)
							}
							return true
						})
					}
				case "tool_search_tool_result":
					toolRefs := part.Get("content.tool_references")
					if toolRefs.Exists() && toolRefs.IsArray() {
						toolRefs.ForEach(func(_, refPart ojson.Result) bool {
							if refPart.Get("type").String() != "tool_reference" {
								return true
							}
							nameResult := refPart.Get("tool_name")
							refToolName := strings.Clone(nameResult.String())
							if newName, renamed := rewriteName(refToolName); renamed {
								if !appendStringEdit(nameResult, newName) {
									validOffsets = false
									return false
								}
								recordRename(refToolName, newName)
							}
							return true
						})
					}
				case "tool_addition", "tool_removal":
					if namePath := claudeToolChangeNamePath(part); namePath != "" {
						nameResult := part.Get(namePath)
						changeToolName := strings.Clone(nameResult.String())
						if newName, renamed := rewriteName(changeToolName); renamed {
							if !appendStringEdit(nameResult, newName) {
								validOffsets = false
								return false
							}
							recordRename(changeToolName, newName)
						}
					}
				}
				return validOffsets
			})
			return validOffsets
		})
	}
	if !validOffsets {
		return nil, nil, false
	}

	remapped, ok := applyClaudeRawJSONEdits(body, edits)
	if !ok {
		return nil, nil, false
	}
	return remapped, reverseMap, true
}

func applyClaudeRawJSONEdits(body []byte, edits []claudeRawJSONEdit) ([]byte, bool) {
	if len(edits) == 0 {
		return body, true
	}
	sort.Slice(edits, func(i, j int) bool {
		return edits[i].start < edits[j].start
	})

	finalSize := len(body)
	cursor := 0
	for _, edit := range edits {
		if edit.start < cursor || edit.start < 0 || edit.end < edit.start || edit.end > len(body) {
			return nil, false
		}
		finalSize += len(edit.replacement) - (edit.end - edit.start)
		if finalSize < 0 {
			return nil, false
		}
		cursor = edit.end
	}

	out := make([]byte, 0, finalSize)
	cursor = 0
	for _, edit := range edits {
		out = append(out, body[cursor:edit.start]...)
		out = append(out, edit.replacement...)
		cursor = edit.end
	}
	out = append(out, body[cursor:]...)
	return out, true
}

func remapOAuthToolNamesWithOptionsDoc(doc *ojson.Doc, mcpAliases claudeMCPAliasOptions) (map[string]string, bool) {
	if doc == nil || doc.Len() == 0 {
		return nil, false
	}
	reverseMap := make(map[string]string)
	recordRename := func(original, renamed string) {
		if _, exists := reverseMap[renamed]; !exists {
			reverseMap[renamed] = original
		}
	}

	tools := doc.Get("tools")
	forwardMap := make(map[string]string)
	protectedNames := make(map[string]bool)
	reservedNames := helps.AugmentClaudeBuiltinToolRegistry(nil, nil)
	if tools.Exists() && tools.IsArray() {
		tools.ForEach(func(_, tool ojson.Result) bool {
			name := strings.Clone(tool.Get("name").String())
			if name != "" {
				reservedNames[name] = true
			}
			if helps.IsClaudeServerToolType(tool.Get("type").String()) {
				protectedNames[name] = true
			}
			return true
		})
		passthroughMCPTools := make([]string, 0, 4)
		tools.ForEach(func(_, tool ojson.Result) bool {
			if helps.IsClaudeServerToolType(tool.Get("type").String()) {
				return true
			}
			name := strings.Clone(tool.Get("name").String())
			if name == "" {
				return true
			}
			if helps.IsClaudeMCPToolName(name) {
				passthroughMCPTools = append(passthroughMCPTools, name)
				return true
			}
			if _, exists := forwardMap[name]; exists {
				return true
			}
			alias, allocated := helps.AllocateClaudeMCPToolAlias(mcpAliases.secret, name, reservedNames)
			if !allocated {
				log.Warnf("claude oauth mcp alias: no free alias left for tool %q, forwarding the original name", name)
				return true
			}
			forwardMap[name] = alias
			reservedNames[alias] = true
			return true
		})
		recordPassthroughMCPTools(recordRename, forwardMap, passthroughMCPTools)
	}

	rewriteName := func(name string) (string, bool) {
		if name == "" || protectedNames[name] || helps.IsClaudeMCPToolName(name) {
			return name, false
		}
		if newName, ok := forwardMap[name]; ok && newName != name {
			return newName, true
		}
		return name, false
	}

	changed := false
	toolsNeedRewrite := false
	if tools.Exists() && tools.IsArray() {
		tools.ForEach(func(_, tool ojson.Result) bool {
			toolType := tool.Get("type").String()
			if helps.IsClaudeServerToolType(toolType) {
				return true
			}
			if strings.TrimSpace(toolType) != "" {
				toolsNeedRewrite = true
				return false
			}
			name := tool.Get("name").String()
			_, toolsNeedRewrite = rewriteName(name)
			return !toolsNeedRewrite
		})
	}
	if toolsNeedRewrite {
		var toolsJSON strings.Builder
		toolsJSON.WriteByte('[')
		toolCount := 0
		tools.ForEach(func(_, tool ojson.Result) bool {
			if helps.IsClaudeServerToolType(tool.Get("type").String()) {
				if toolCount > 0 {
					toolsJSON.WriteByte(',')
				}
				toolsJSON.WriteString(tool.Raw)
				toolCount++
				return true
			}

			name := strings.Clone(tool.Get("name").String())
			hasType := strings.TrimSpace(tool.Get("type").String()) != ""
			newName, renamed := rewriteName(name)
			toolJSON := tool.Raw
			if hasType || renamed {
				toolDoc := ojson.NewDoc([]byte(tool.Raw))
				if hasType {
					_ = toolDoc.Delete("type")
				}
				if renamed {
					if errSet := toolDoc.Set("name", newName); errSet == nil {
						recordRename(name, newName)
					}
				}
				toolJSON = toolDoc.String()
			}

			if toolCount > 0 {
				toolsJSON.WriteByte(',')
			}
			toolsJSON.WriteString(toolJSON)
			toolCount++
			return true
		})
		toolsJSON.WriteByte(']')
		if doc.SetRawBytes("tools", []byte(toolsJSON.String())) == nil {
			changed = true
		}
	}

	toolChoiceType := doc.Get("tool_choice.type").String()
	if toolChoiceType == "tool" {
		tcName := strings.Clone(doc.Get("tool_choice.name").String())
		if newName, renamed := rewriteName(tcName); renamed {
			if doc.Set("tool_choice.name", newName) == nil {
				changed = true
				recordRename(tcName, newName)
			}
		}
	}

	messages := doc.Get("messages")
	if messages.Exists() && messages.IsArray() {
		messages.ForEach(func(msgIndex, msg ojson.Result) bool {
			content := msg.Get("content")
			if !content.Exists() || !content.IsArray() {
				return true
			}
			content.ForEach(func(contentIndex, part ojson.Result) bool {
				partType := part.Get("type").String()
				switch partType {
				case "tool_use":
					name := strings.Clone(part.Get("name").String())
					if newName, renamed := rewriteName(name); renamed {
						path := fmt.Sprintf("messages.%d.content.%d.name", msgIndex.Int(), contentIndex.Int())
						if doc.Set(path, newName) == nil {
							changed = true
							recordRename(name, newName)
						}
					}
				case "tool_reference":
					toolName := strings.Clone(part.Get("tool_name").String())
					if newName, renamed := rewriteName(toolName); renamed {
						path := fmt.Sprintf("messages.%d.content.%d.tool_name", msgIndex.Int(), contentIndex.Int())
						if doc.Set(path, newName) == nil {
							changed = true
							recordRename(toolName, newName)
						}
					}
				case "tool_result":
					nestedContent := part.Get("content")
					if nestedContent.Exists() && nestedContent.IsArray() {
						nestedContent.ForEach(func(nestedIndex, nestedPart ojson.Result) bool {
							if nestedPart.Get("type").String() == "tool_reference" {
								nestedToolName := strings.Clone(nestedPart.Get("tool_name").String())
								if newName, renamed := rewriteName(nestedToolName); renamed {
									nestedPath := fmt.Sprintf("messages.%d.content.%d.content.%d.tool_name", msgIndex.Int(), contentIndex.Int(), nestedIndex.Int())
									if doc.Set(nestedPath, newName) == nil {
										changed = true
										recordRename(nestedToolName, newName)
									}
								}
							}
							return true
						})
					}
				case "tool_search_tool_result":
					toolRefs := part.Get("content.tool_references")
					if toolRefs.Exists() && toolRefs.IsArray() {
						toolRefs.ForEach(func(refIndex, refPart ojson.Result) bool {
							if refPart.Get("type").String() == "tool_reference" {
								refToolName := strings.Clone(refPart.Get("tool_name").String())
								if newName, renamed := rewriteName(refToolName); renamed {
									refPath := fmt.Sprintf("messages.%d.content.%d.content.tool_references.%d.tool_name", msgIndex.Int(), contentIndex.Int(), refIndex.Int())
									if doc.Set(refPath, newName) == nil {
										changed = true
										recordRename(refToolName, newName)
									}
								}
							}
							return true
						})
					}
				case "tool_addition", "tool_removal":
					if namePath := claudeToolChangeNamePath(part); namePath != "" {
						changeToolName := strings.Clone(part.Get(namePath).String())
						if newName, renamed := rewriteName(changeToolName); renamed {
							changePath := fmt.Sprintf("messages.%d.content.%d.%s", msgIndex.Int(), contentIndex.Int(), namePath)
							if doc.Set(changePath, newName) == nil {
								changed = true
								recordRename(changeToolName, newName)
							}
						}
					}
				}
				return true
			})
			return true
		})
	}

	return reverseMap, changed
}

func remapOAuthToolNamesWithOptionsLegacy(body []byte, mcpAliases claudeMCPAliasOptions) ([]byte, map[string]string) {
	doc := ojson.NewDoc(body)
	reverseMap, changed := remapOAuthToolNamesWithOptionsDoc(doc, mcpAliases)
	if !changed {
		return body, reverseMap
	}
	return doc.Bytes(), reverseMap
}

func claudeToolChangeNamePath(part ojson.Result) string {
	switch part.Get("tool.type").String() {
	case "tool_reference":
		return "tool.name"
	case "tool_definition":
		if part.Get("type").String() != "tool_addition" || helps.IsClaudeServerToolType(part.Get("tool.definition.type").String()) {
			return ""
		}
		return "tool.definition.name"
	}
	return ""
}

type claudeMCPAliasParts struct {
	server   string
	toolID   string
	semantic string
}

type claudeMCPAliasEntry struct {
	alias    string
	original string
	parts    claudeMCPAliasParts
}

type claudeMCPAliasResolver struct {
	exact        map[string]string
	aliases      []claudeMCPAliasEntry
	servers      map[string]struct{}
	passthroughs []string
}

type claudeMCPAliasRestoreError struct {
	error
}

func (e claudeMCPAliasRestoreError) Unwrap() error {
	return e.error
}

func (claudeMCPAliasRestoreError) IsRequestScoped() bool {
	return true
}

func newClaudeMCPAliasResolver(reverseMap map[string]string) claudeMCPAliasResolver {
	resolver := claudeMCPAliasResolver{
		exact:        reverseMap,
		aliases:      make([]claudeMCPAliasEntry, 0, len(reverseMap)),
		servers:      make(map[string]struct{}),
		passthroughs: make([]string, 0),
	}
	for alias, original := range reverseMap {
		if alias == original {

			resolver.passthroughs = append(resolver.passthroughs, original)
			continue
		}
		parts, ok := parseClaudeMCPAlias(alias)
		if !ok {
			continue
		}
		resolver.aliases = append(resolver.aliases, claudeMCPAliasEntry{
			alias:    alias,
			original: original,
			parts:    parts,
		})
		resolver.servers[parts.server] = struct{}{}
	}
	return resolver
}

func (resolver claudeMCPAliasResolver) restorePassthroughWrapped(tail string) (string, bool) {
	if tail == "" {
		return "", false
	}
	for _, original := range resolver.passthroughs {
		if original == tail {
			return original, true
		}
	}
	return "", false
}

func parseClaudeMCPAlias(name string) (claudeMCPAliasParts, bool) {
	if !helps.IsClaudeMCPToolName(name) {
		return claudeMCPAliasParts{}, false
	}
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok {
		return claudeMCPAliasParts{}, false
	}
	server, tool, ok := strings.Cut(rest, "__")
	if !ok || server == "" {
		return claudeMCPAliasParts{}, false
	}
	toolID, semantic, ok := strings.Cut(tool, "_")
	if !ok || toolID == "" || semantic == "" {
		return claudeMCPAliasParts{}, false
	}
	return claudeMCPAliasParts{server: server, toolID: toolID, semantic: semantic}, true
}

func claudeMCPAliasServer(name string) string {
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok {
		return ""
	}
	server, _, ok := strings.Cut(rest, "__")
	if !ok {
		return ""
	}
	return server
}

func recordPassthroughMCPTools(recordRename func(original, renamed string), forwardMap map[string]string, passthrough []string) {
	if len(forwardMap) == 0 {
		return
	}
	for _, name := range passthrough {
		recordRename(name, name)
	}
}

func (resolver claudeMCPAliasResolver) resolve(name string) (string, bool, error) {
	if original, ok := resolver.exact[name]; ok {
		if original == name {

			return "", false, nil
		}
		return original, true, nil
	}

	server := claudeMCPAliasServer(name)
	if _, known := resolver.servers[server]; !known {
		return "", false, nil
	}

	canonicalServerPrefix := "mcp__" + server + "__"
	normalizedName := name
	suffix := strings.TrimPrefix(name, canonicalServerPrefix)
	for {
		strippedSuffix, repeatedServer := strings.CutPrefix(suffix, server+"__")
		if !repeatedServer {
			break
		}
		suffix = strippedSuffix
		normalizedName = canonicalServerPrefix + suffix
		if original, exact := resolver.exact[normalizedName]; exact {
			return original, true, nil
		}
	}

	matchedOriginal := ""
	matchCount := 0
	for _, entry := range resolver.aliases {
		if entry.parts.server == server && strings.HasSuffix(name, entry.alias) {
			matchedOriginal = entry.original
			matchCount++
		}
	}
	if matchCount == 1 {
		return matchedOriginal, true, nil
	}
	if matchCount > 1 {
		return "", false, claudeMCPAliasRestoreError{fmt.Errorf("cannot restore Claude OAuth MCP tool alias %q: matched multiple declared aliases", name)}
	}

	parts, validAlias := parseClaudeMCPAlias(normalizedName)
	if validAlias {
		for _, entry := range resolver.aliases {
			if entry.parts.server == parts.server && entry.parts.semantic == parts.semantic {
				matchedOriginal = entry.original
				matchCount++
			}
		}
	}

	if matchCount == 0 {
		var suffixMatches []claudeMCPAliasEntry
		for _, entry := range resolver.aliases {
			if entry.parts.server == server && strings.HasSuffix(normalizedName, "_"+entry.parts.semantic) {
				suffixMatches = append(suffixMatches, entry)
			}
		}
		if len(suffixMatches) == 1 {
			matchedOriginal = suffixMatches[0].original
			matchCount = 1
		} else if len(suffixMatches) > 1 {

			longest := suffixMatches[0]
			tie := false
			for _, candidate := range suffixMatches[1:] {
				if len(candidate.parts.semantic) > len(longest.parts.semantic) {
					longest = candidate
					tie = false
				} else if len(candidate.parts.semantic) == len(longest.parts.semantic) {
					tie = true
				}
			}
			if !tie {
				matchedOriginal = longest.original
				matchCount = 1
			} else {
				matchCount = len(suffixMatches)
			}
		}
		if matchCount == 1 {

			log.Debugf("claude oauth mcp alias: recovered drifted tool name %q as %q via semantic suffix", name, matchedOriginal)
		}
	}
	if matchCount == 1 {
		return matchedOriginal, true, nil
	}
	if matchCount > 1 {
		return "", false, claudeMCPAliasRestoreError{fmt.Errorf("cannot restore Claude OAuth MCP tool alias %q: semantic suffix matches multiple declared tools", name)}
	}

	if len(resolver.passthroughs) > 0 {

		if original, ok := resolver.restorePassthroughWrapped(suffix); ok {
			log.Debugf("claude oauth mcp alias: recovered %q as caller-owned tool %q", name, original)
			return original, true, nil
		}

		reprefixed := "mcp__" + suffix
		if original, exact := resolver.exact[reprefixed]; exact && original == reprefixed {
			log.Debugf("claude oauth mcp alias: recovered hybrid passthrough tool name %q as %q via exact prefix", name, original)
			return original, true, nil
		}

		matchedPassthrough := ""
		passthroughMatches := 0
		for _, pt := range resolver.passthroughs {
			toolPart := pt
			if rest, ok := strings.CutPrefix(pt, "mcp__"); ok {
				if _, tool, ok := strings.Cut(rest, "__"); ok {
					toolPart = tool
				}
			}
			if toolPart == suffix {
				matchedPassthrough = pt
				passthroughMatches++
			}
		}
		if passthroughMatches == 1 {
			log.Debugf("claude oauth mcp alias: recovered hybrid passthrough tool name %q as %q via unique tool suffix", name, matchedPassthrough)
			return matchedPassthrough, true, nil
		}
		if passthroughMatches > 1 {
			return "", false, claudeMCPAliasRestoreError{fmt.Errorf("cannot restore Claude OAuth MCP tool alias %q: passthrough tool suffix matches multiple declared tools", name)}
		}
	}

	log.Warnf("claude oauth mcp alias: cannot restore tool name %q: no unique request-local match; forwarding it unchanged", name)
	return "", false, nil
}

func reverseRemapOAuthToolNames(body []byte, reverseMap map[string]string) ([]byte, error) {
	if len(reverseMap) == 0 {
		return body, nil
	}
	content := ojson.GetBytesNoCopy(body, "content")
	if !content.Exists() || !content.IsArray() {
		return body, nil
	}
	resolver := newClaudeMCPAliasResolver(reverseMap)
	var doc *ojson.Doc
	var resolveErr error
	changed := false
	setField := func(path, val string) {
		if doc == nil {
			doc = ojson.NewDoc(body)
		}
		if doc.Set(path, val) == nil {
			changed = true
		}
	}
	content.ForEach(func(index, part ojson.Result) bool {
		partType := part.Get("type").String()
		switch partType {
		case "tool_use":
			name := part.Get("name").String()
			origName, matched, errResolve := resolver.resolve(name)
			if errResolve != nil {
				resolveErr = errResolve
				return false
			}
			if matched {
				path := fmt.Sprintf("content.%d.name", index.Int())
				setField(path, origName)
			}
		case "tool_reference":
			toolName := part.Get("tool_name").String()
			origName, matched, errResolve := resolver.resolve(toolName)
			if errResolve != nil {
				resolveErr = errResolve
				return false
			}
			if matched {
				path := fmt.Sprintf("content.%d.tool_name", index.Int())
				setField(path, origName)
			}
		case "tool_result":
			nestedContent := part.Get("content")
			if nestedContent.Exists() && nestedContent.IsArray() {
				nestedContent.ForEach(func(nestedIndex, nestedPart ojson.Result) bool {
					if nestedPart.Get("type").String() != "tool_reference" {
						return true
					}
					toolName := nestedPart.Get("tool_name").String()
					origName, matched, errResolve := resolver.resolve(toolName)
					if errResolve != nil {
						resolveErr = errResolve
						return false
					}
					if matched {
						path := fmt.Sprintf("content.%d.content.%d.tool_name", index.Int(), nestedIndex.Int())
						setField(path, origName)
					}
					return true
				})
			}
		case "tool_search_tool_result":
			toolRefs := part.Get("content.tool_references")
			if toolRefs.Exists() && toolRefs.IsArray() {
				toolRefs.ForEach(func(refIndex, refPart ojson.Result) bool {
					if refPart.Get("type").String() != "tool_reference" {
						return true
					}
					toolName := refPart.Get("tool_name").String()
					origName, matched, errResolve := resolver.resolve(toolName)
					if errResolve != nil {
						resolveErr = errResolve
						return false
					}
					if matched {
						path := fmt.Sprintf("content.%d.content.tool_references.%d.tool_name", index.Int(), refIndex.Int())
						setField(path, origName)
					}
					return true
				})
			}
		}
		return resolveErr == nil
	})
	if resolveErr != nil {
		return body, resolveErr
	}
	if !changed || doc == nil {
		return body, nil
	}
	return doc.Bytes(), nil
}

func reverseRemapOAuthToolNamesFromStreamLine(line []byte, reverseMap map[string]string) ([]byte, error) {
	if len(reverseMap) == 0 {
		return line, nil
	}
	payload := helps.JSONPayload(line)
	if len(payload) == 0 || !ojson.ValidBytes(payload) {
		return line, nil
	}

	contentBlock := ojson.GetBytesNoCopy(payload, "content_block")
	if !contentBlock.Exists() {
		return line, nil
	}

	resolver := newClaudeMCPAliasResolver(reverseMap)
	blockType := contentBlock.Get("type").String()
	var updated []byte
	var err error

	switch blockType {
	case "tool_use":
		name := contentBlock.Get("name").String()
		origName, matched, errResolve := resolver.resolve(name)
		if errResolve != nil {
			return line, errResolve
		}
		if !matched {
			return line, nil
		}
		updated, err = ojson.SetBytes(payload, "content_block.name", origName)
	case "tool_reference":
		toolName := contentBlock.Get("tool_name").String()
		origName, matched, errResolve := resolver.resolve(toolName)
		if errResolve != nil {
			return line, errResolve
		}
		if !matched {
			return line, nil
		}
		updated, err = ojson.SetBytes(payload, "content_block.tool_name", origName)
	case "tool_search_tool_result":
		toolRefs := contentBlock.Get("content.tool_references")
		if !toolRefs.Exists() || !toolRefs.IsArray() {
			return line, nil
		}
		var doc *ojson.Doc
		var resolveErr error
		hasChange := false
		toolRefs.ForEach(func(refIndex, refPart ojson.Result) bool {
			if refPart.Get("type").String() != "tool_reference" {
				return true
			}
			toolName := refPart.Get("tool_name").String()
			origName, matched, errResolve := resolver.resolve(toolName)
			if errResolve != nil {
				resolveErr = errResolve
				return false
			}
			if matched {
				if doc == nil {
					doc = ojson.NewDoc(payload)
				}
				path := fmt.Sprintf("content_block.content.tool_references.%d.tool_name", refIndex.Int())
				err = doc.Set(path, origName)
				if err != nil {
					return false
				}
				hasChange = true
			}
			return true
		})
		if resolveErr != nil {
			return line, resolveErr
		}
		if err != nil {
			return line, fmt.Errorf("rewrite Claude OAuth MCP tool alias: %w", err)
		}
		if !hasChange || doc == nil {
			return line, nil
		}
		updated = doc.Bytes()
	default:
		return line, nil
	}
	if err != nil {
		return line, fmt.Errorf("rewrite Claude OAuth MCP tool alias: %w", err)
	}

	trimmed := bytes.TrimSpace(line)
	if bytes.HasPrefix(trimmed, []byte("data:")) {
		return append([]byte("data: "), updated...), nil
	}
	return updated, nil
}

func applyClaudeToolPrefix(body []byte, prefix string) []byte {
	if prefix == "" {
		return body
	}
	doc := ojson.NewDoc(body)
	if !applyClaudeToolPrefixDoc(doc, prefix) {
		return body
	}
	return doc.Bytes()
}

func applyClaudeToolPrefixDoc(doc *ojson.Doc, prefix string) bool {
	if prefix == "" || doc == nil || doc.Len() == 0 {
		return false
	}
	changed := false

	builtinTools := helps.AugmentClaudeBuiltinToolRegistry(nil, nil)

	if tools := doc.Get("tools"); tools.Exists() && tools.IsArray() {
		tools.ForEach(func(index, tool ojson.Result) bool {

			if tool.Get("type").Exists() && tool.Get("type").String() != "" {
				if n := strings.Clone(tool.Get("name").String()); n != "" {
					builtinTools[n] = true
				}
				return true
			}
			name := tool.Get("name").String()
			if name == "" || strings.HasPrefix(name, prefix) || helps.IsClaudeMCPToolName(name) {
				return true
			}
			path := fmt.Sprintf("tools.%d.name", index.Int())
			if doc.Set(path, prefix+name) == nil {
				changed = true
			}
			return true
		})
	}

	if doc.Get("tool_choice.type").String() == "tool" {
		name := doc.Get("tool_choice.name").String()
		if name != "" && !strings.HasPrefix(name, prefix) && !builtinTools[name] && !helps.IsClaudeMCPToolName(name) {
			if doc.Set("tool_choice.name", prefix+name) == nil {
				changed = true
			}
		}
	}

	if messages := doc.Get("messages"); messages.Exists() && messages.IsArray() {
		messages.ForEach(func(msgIndex, msg ojson.Result) bool {
			content := msg.Get("content")
			if !content.Exists() || !content.IsArray() {
				return true
			}
			content.ForEach(func(contentIndex, part ojson.Result) bool {
				partType := part.Get("type").String()
				switch partType {
				case "tool_use":
					name := part.Get("name").String()
					if name == "" || strings.HasPrefix(name, prefix) || builtinTools[name] || helps.IsClaudeMCPToolName(name) {
						return true
					}
					path := fmt.Sprintf("messages.%d.content.%d.name", msgIndex.Int(), contentIndex.Int())
					if doc.Set(path, prefix+name) == nil {
						changed = true
					}
				case "tool_reference":
					toolName := part.Get("tool_name").String()
					if toolName == "" || strings.HasPrefix(toolName, prefix) || builtinTools[toolName] || helps.IsClaudeMCPToolName(toolName) {
						return true
					}
					path := fmt.Sprintf("messages.%d.content.%d.tool_name", msgIndex.Int(), contentIndex.Int())
					if doc.Set(path, prefix+toolName) == nil {
						changed = true
					}
				case "tool_result":

					nestedContent := part.Get("content")
					if nestedContent.Exists() && nestedContent.IsArray() {
						nestedContent.ForEach(func(nestedIndex, nestedPart ojson.Result) bool {
							if nestedPart.Get("type").String() == "tool_reference" {
								nestedToolName := nestedPart.Get("tool_name").String()
								if nestedToolName != "" && !strings.HasPrefix(nestedToolName, prefix) && !builtinTools[nestedToolName] && !helps.IsClaudeMCPToolName(nestedToolName) {
									nestedPath := fmt.Sprintf("messages.%d.content.%d.content.%d.tool_name", msgIndex.Int(), contentIndex.Int(), nestedIndex.Int())
									if doc.Set(nestedPath, prefix+nestedToolName) == nil {
										changed = true
									}
								}
							}
							return true
						})
					}
				}
				return true
			})
			return true
		})
	}

	return changed
}

func stripClaudeToolPrefixFromResponse(body []byte, prefix string) []byte {
	if prefix == "" {
		return body
	}
	content := ojson.GetBytesNoCopy(body, "content")
	if !content.Exists() || !content.IsArray() {
		return body
	}
	var doc *ojson.Doc
	changed := false
	setField := func(path, val string) {
		if doc == nil {
			doc = ojson.NewDoc(body)
		}
		if doc.Set(path, val) == nil {
			changed = true
		}
	}
	content.ForEach(func(index, part ojson.Result) bool {
		partType := part.Get("type").String()
		switch partType {
		case "tool_use":
			name := part.Get("name").String()
			if !strings.HasPrefix(name, prefix) {
				return true
			}
			path := fmt.Sprintf("content.%d.name", index.Int())
			setField(path, strings.TrimPrefix(name, prefix))
		case "tool_reference":
			toolName := part.Get("tool_name").String()
			if !strings.HasPrefix(toolName, prefix) {
				return true
			}
			path := fmt.Sprintf("content.%d.tool_name", index.Int())
			setField(path, strings.TrimPrefix(toolName, prefix))
		case "tool_result":

			nestedContent := part.Get("content")
			if nestedContent.Exists() && nestedContent.IsArray() {
				nestedContent.ForEach(func(nestedIndex, nestedPart ojson.Result) bool {
					if nestedPart.Get("type").String() == "tool_reference" {
						nestedToolName := nestedPart.Get("tool_name").String()
						if strings.HasPrefix(nestedToolName, prefix) {
							nestedPath := fmt.Sprintf("content.%d.content.%d.tool_name", index.Int(), nestedIndex.Int())
							setField(nestedPath, strings.TrimPrefix(nestedToolName, prefix))
						}
					}
					return true
				})
			}
		}
		return true
	})
	if !changed || doc == nil {
		return body
	}
	return doc.Bytes()
}

func stripClaudeToolPrefixFromStreamLine(line []byte, prefix string) []byte {
	if prefix == "" {
		return line
	}
	payload := helps.JSONPayload(line)
	if len(payload) == 0 || !ojson.ValidBytes(payload) {
		return line
	}
	contentBlock := ojson.GetBytesNoCopy(payload, "content_block")
	if !contentBlock.Exists() {
		return line
	}

	blockType := contentBlock.Get("type").String()
	var updated []byte
	var err error

	switch blockType {
	case "tool_use":
		name := contentBlock.Get("name").String()
		if !strings.HasPrefix(name, prefix) {
			return line
		}
		updated, err = ojson.SetBytes(payload, "content_block.name", strings.TrimPrefix(name, prefix))
		if err != nil {
			return line
		}
	case "tool_reference":
		toolName := contentBlock.Get("tool_name").String()
		if !strings.HasPrefix(toolName, prefix) {
			return line
		}
		updated, err = ojson.SetBytes(payload, "content_block.tool_name", strings.TrimPrefix(toolName, prefix))
		if err != nil {
			return line
		}
	default:
		return line
	}

	trimmed := bytes.TrimSpace(line)
	if bytes.HasPrefix(trimmed, []byte("data:")) {
		return append([]byte("data: "), updated...)
	}
	return updated
}
