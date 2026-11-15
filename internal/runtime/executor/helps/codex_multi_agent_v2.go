package helps

import (
	"context"
	"net/http"

	"github.com/MoeForge/moego/ojson"
	multiagentv2 "github.com/router-for-me/CLIProxyAPI/v8/internal/client/codex/optimize-multi-agent-v2"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	openaichatclaude "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/claude/openai/chat-completions"
	responsesclaude "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/claude/openai/responses"
	codexclaude "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/codex/claude"
	geminiclaude "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/gemini/claude"
	interactionsclaude "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/interactions/claude"
	openaiclaude "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/openai/claude"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func RewriteCodexSpawnAgentDescription(ctx context.Context, headers http.Header, payload []byte, cfg *config.Config) []byte {
	return multiagentv2.RewriteCodexSpawnAgentDescription(ctx, headers, payload, cfg)
}

func RewriteCodexMultiAgentV2Input(ctx context.Context, headers http.Header, payload []byte, cfg *config.Config) []byte {
	return multiagentv2.RewriteCodexMultiAgentV2Input(ctx, headers, payload, cfg)
}

func RewriteCodexMultiAgentV2InputDoc(doc *ojson.Doc, ctx context.Context, headers http.Header, cfg *config.Config) bool {
	return multiagentv2.RewriteCodexMultiAgentV2InputDoc(doc, ctx, headers, cfg)
}

func RewriteCodexOrphanDelegationInput(ctx context.Context, headers http.Header, payload []byte, cfg *config.Config) []byte {
	return multiagentv2.RewriteCodexOrphanDelegationInputForConfig(ctx, headers, payload, cfg)
}

func RewriteCodexOrphanDelegationInputDoc(doc *ojson.Doc, ctx context.Context, headers http.Header, cfg *config.Config) bool {
	return multiagentv2.RewriteCodexOrphanDelegationInputForConfigDoc(doc, ctx, headers, cfg)
}

func TranslateRequestWithCodexMultiAgentV2(ctx context.Context, headers http.Header, cfg *config.Config, from, to sdktranslator.Format, model string, payload []byte, stream bool) []byte {
	return TranslateRequestWithCodexMultiAgentV2ForExecutor(ctx, headers, cfg, "", from, to, model, payload, stream)
}

func TranslateRequestWithCodexMultiAgentV2ForExecutor(ctx context.Context, headers http.Header, cfg *config.Config, targetExecutor string, from, to sdktranslator.Format, model string, payload []byte, stream bool) []byte {
	if IsCodexUserAgent(headers) && !isCodexTargetExecutor(targetExecutor) {
		payload = NormalizeCodexToolIntegerTypes(payload, headers)
	}
	return multiagentv2.TranslateRequestWithCodexMultiAgentV2(ctx, headers, cfg, from, to, model, payload, stream)
}

func TranslateRequestEnvelopeWithCodexMultiAgentV2(ctx context.Context, headers http.Header, cfg *config.Config, from, to sdktranslator.Format, req sdktranslator.RequestEnvelope) sdktranslator.RequestEnvelope {
	return multiagentv2.TranslateRequestEnvelopeWithCodexMultiAgentV2(ctx, headers, cfg, from, to, req)
}

func TranslateRequestPairWithCodexMultiAgentV2(ctx context.Context, headers http.Header, cfg *config.Config, from, to sdktranslator.Format, model string, originalPayload, requestPayload []byte, stream bool) (original, working []byte) {
	req := sdktranslator.RequestEnvelope{Format: from, Model: model, Stream: stream}
	original, working, _ = TranslateRequestEnvelopePairWithCodexMultiAgentV2(ctx, headers, cfg, from, to, req, originalPayload, requestPayload)
	return original, working
}

func TranslateRequestEnvelopePairWithCodexMultiAgentV2(ctx context.Context, headers http.Header, cfg *config.Config, from, to sdktranslator.Format, req sdktranslator.RequestEnvelope, originalPayload, requestPayload []byte) (original, working []byte, err error) {
	originalReq := req
	originalReq.Body = originalPayload
	originalTranslated := TranslateRequestEnvelopeWithCodexMultiAgentV2(ctx, headers, cfg, from, to, originalReq)
	original = originalTranslated.Body
	if sameByteSlice(originalPayload, requestPayload) {

		return original, append([]byte(nil), original...), originalTranslated.Err
	}
	workingReq := req
	workingReq.Body = requestPayload
	workingTranslated := TranslateRequestEnvelopeWithCodexMultiAgentV2(ctx, headers, cfg, from, to, workingReq)
	return original, workingTranslated.Body, workingTranslated.Err
}

func sameByteSlice(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	return &a[0] == &b[0]
}

func TranslateRequestPairWithAPIKeyModelCompatibility(ctx context.Context, headers http.Header, cfg *config.Config, from, to sdktranslator.Format, model string, originalPayload, requestPayload []byte, stream, isCompat bool) (original, working []byte) {
	original, working, _ = translateRequestPairWithAPIKeyModelCompatibility(ctx, headers, cfg, from, to, model, originalPayload, requestPayload, stream, isCompat)
	return original, working
}

func TranslateRequestPairReturningError(ctx context.Context, headers http.Header, cfg *config.Config, from, to sdktranslator.Format, model string, originalPayload, requestPayload []byte, stream, isCompat bool) ([]byte, []byte, error) {
	return translateRequestPairWithAPIKeyModelCompatibility(ctx, headers, cfg, from, to, model, originalPayload, requestPayload, stream, isCompat)
}

func translateRequestPairWithAPIKeyModelCompatibility(ctx context.Context, headers http.Header, cfg *config.Config, from, to sdktranslator.Format, model string, originalPayload, requestPayload []byte, stream, isCompat bool) (original, working []byte, err error) {
	original, working, _, err = TranslateRequestPairWithAPIKeyModelCompatibilityAndUpdateIntent(ctx, headers, cfg, from, to, model, originalPayload, requestPayload, stream, isCompat)
	return original, working, err
}

func TranslateRequestPairWithAPIKeyModelCompatibilityAndUpdateIntent(ctx context.Context, headers http.Header, cfg *config.Config, from, to sdktranslator.Format, model string, originalPayload, requestPayload []byte, stream, isCompat bool) (original, working []byte, updatesChanged bool, err error) {
	original, updatesChanged, err = TranslateRequestWithAPIKeyModelCompatibilityAndUpdateIntent(ctx, headers, cfg, from, to, model, originalPayload, stream, isCompat)
	if sameByteSlice(originalPayload, requestPayload) {

		return original, append([]byte(nil), original...), updatesChanged, err
	}

	working, updatesChanged, err = TranslateRequestWithAPIKeyModelCompatibilityAndUpdateIntent(ctx, headers, cfg, from, to, model, requestPayload, stream, isCompat)
	return original, working, updatesChanged, err
}

func TranslateRequestWithAPIKeyModelCompatibilityAndUpdateIntent(ctx context.Context, headers http.Header, cfg *config.Config, from, to sdktranslator.Format, model string, payload []byte, stream, isCompat bool) ([]byte, bool, error) {
	return TranslateRequestWithAPIKeyModelCompatibilityAndUpdateIntentForExecutor(ctx, headers, cfg, "", from, to, model, payload, stream, isCompat)
}

func TranslateRequestWithAPIKeyModelCompatibilityAndUpdateIntentForExecutor(ctx context.Context, headers http.Header, cfg *config.Config, targetExecutor string, from, to sdktranslator.Format, model string, payload []byte, stream, isCompat bool) ([]byte, bool, error) {
	if !isCompat || (to == sdktranslator.FormatCodex && from != sdktranslator.FormatClaude) {
		translated := sdktranslator.RequestEnvelope{Format: from, Model: model, Stream: stream, Body: payload}
		if IsCodexUserAgent(headers) && !isCodexTargetExecutor(targetExecutor) {
			translated.Body = NormalizeCodexToolIntegerTypes(translated.Body, headers)
		}
		translated = multiagentv2.TranslateRequestEnvelopeWithCodexMultiAgentV2(ctx, headers, cfg, from, to, translated)
		return translated.Body, translated.ConfigurationUpdatesChanged, translated.Err
	}
	body, err := translateRequestWithAPIKeyModelCompatibilityForExecutor(ctx, headers, cfg, targetExecutor, from, to, model, payload, stream, isCompat)
	return body, false, err
}

func TranslateRequestWithAPIKeyModelCompatibility(ctx context.Context, headers http.Header, cfg *config.Config, from, to sdktranslator.Format, model string, payload []byte, stream, isCompat bool) []byte {
	return TranslateRequestWithAPIKeyModelCompatibilityForExecutor(ctx, headers, cfg, "", from, to, model, payload, stream, isCompat)
}

func TranslateRequestWithAPIKeyModelCompatibilityForExecutor(ctx context.Context, headers http.Header, cfg *config.Config, targetExecutor string, from, to sdktranslator.Format, model string, payload []byte, stream, isCompat bool) []byte {
	body, _ := translateRequestWithAPIKeyModelCompatibilityForExecutor(ctx, headers, cfg, targetExecutor, from, to, model, payload, stream, isCompat)
	return body
}

func TranslateRequestReturningError(ctx context.Context, headers http.Header, cfg *config.Config, from, to sdktranslator.Format, model string, payload []byte, stream, isCompat bool) ([]byte, error) {
	return translateRequestWithAPIKeyModelCompatibilityForExecutor(ctx, headers, cfg, "", from, to, model, payload, stream, isCompat)
}

func translateRequestWithAPIKeyModelCompatibilityForExecutor(ctx context.Context, headers http.Header, cfg *config.Config, targetExecutor string, from, to sdktranslator.Format, model string, payload []byte, stream, isCompat bool) ([]byte, error) {
	needsIntNorm := IsCodexUserAgent(headers) && !isCodexTargetExecutor(targetExecutor)
	if !isCompat {
		if needsIntNorm {
			payload = NormalizeCodexToolIntegerTypes(payload, headers)
		}
		return translateRequestWithCodexMultiAgentV2Checked(ctx, headers, cfg, from, to, model, payload, stream)
	}
	if from == sdktranslator.FormatOpenAIResponse {
		if len(payload) > 0 {
			doc := ojson.NewDoc(payload)
			changed := false
			if needsIntNorm && NormalizeCodexToolIntegerTypesDoc(doc, headers) {
				changed = true
			}
			if RewriteCodexOrphanDelegationInputDoc(doc, ctx, headers, cfg) {
				changed = true
			}
			if to != sdktranslator.FormatCodex && to != sdktranslator.FormatOpenAIResponse {
				if multiagentv2.RewriteCodexMultiAgentV2InputDoc(doc, ctx, headers, cfg, isCompat) {
					changed = true
				}
			}
			if changed {
				payload = doc.Bytes()
			}
		}
	} else if needsIntNorm {
		payload = NormalizeCodexToolIntegerTypes(payload, headers)
	}

	var translated []byte
	var convertErr error
	switch {
	case from == sdktranslator.FormatClaude && to == sdktranslator.FormatCodex:
		translated, convertErr = codexclaude.ConvertClaudeRequestToCodexWithCompatContext(ctx, model, payload, stream)
	case from == sdktranslator.FormatClaude && to == sdktranslator.FormatGemini:
		translated, convertErr = geminiclaude.ConvertClaudeRequestToGeminiWithCompat(model, payload, stream)
	case from == sdktranslator.FormatClaude && to == sdktranslator.FormatInteractions:
		translated, convertErr = interactionsclaude.ConvertClaudeRequestToInteractionsWithCompat(model, payload, stream)
	case from == sdktranslator.FormatClaude && to == sdktranslator.FormatOpenAI:
		translated, convertErr = openaiclaude.ConvertClaudeRequestToOpenAIWithCompat(model, payload, stream)
	case from == sdktranslator.FormatOpenAI && to == sdktranslator.FormatClaude:
		translated, convertErr = openaichatclaude.ConvertOpenAIRequestToClaudeWithCompat(model, payload, stream)
	case from == sdktranslator.FormatOpenAIResponse && to == sdktranslator.FormatClaude:
		translated, convertErr = responsesclaude.ConvertOpenAIResponsesRequestToClaudeWithCompatContext(ctx, model, payload, stream)
	default:
		return translateRequestWithCodexMultiAgentV2Checked(ctx, headers, cfg, from, to, model, payload, stream)
	}

	summaryConfig := thinking.ExtractTranslatedSummaryConfig(payload, from.String(), to.String())
	translated = thinking.ApplySummaryConfigForModel(translated, to.String(), model, summaryConfig)
	return sdktranslator.NormalizeRequest(ctx, from, to, model, translated, stream), convertErr
}

func HasCodexMultiAgentV2NamespaceConflict(payload []byte) bool {
	return multiagentv2.HasCodexMultiAgentV2NamespaceConflict(payload)
}

func HasCodexMultiAgentV2NamespaceConflictDoc(doc *ojson.Doc) bool {
	return multiagentv2.HasCodexMultiAgentV2NamespaceConflictDoc(doc)
}

func OptimizeCodexMultiAgentV2Request(ctx context.Context, headers http.Header, payload []byte, cfg *config.Config) ([]byte, bool) {
	return multiagentv2.OptimizeCodexMultiAgentV2Request(ctx, headers, payload, cfg)
}

func OptimizeCodexMultiAgentV2RequestDoc(doc *ojson.Doc, ctx context.Context, headers http.Header, cfg *config.Config) bool {
	return multiagentv2.OptimizeCodexMultiAgentV2RequestDoc(doc, ctx, headers, cfg)
}

func OptimizeCodexMultiAgentV2RequestForAuth(ctx context.Context, headers http.Header, payload []byte, cfg *config.Config, auth *cliproxyauth.Auth, isCompat bool) ([]byte, bool) {
	if len(payload) == 0 {
		return payload, false
	}
	doc := ojson.NewDoc(payload)
	optimized := OptimizeCodexMultiAgentV2RequestForAuthDoc(doc, ctx, headers, cfg, auth, isCompat)
	return doc.Bytes(), optimized
}

func OptimizeCodexMultiAgentV2RequestForAuthDoc(doc *ojson.Doc, ctx context.Context, headers http.Header, cfg *config.Config, auth *cliproxyauth.Auth, isCompat bool) bool {
	if doc == nil || doc.Len() == 0 {
		return false
	}
	if auth != nil && auth.AuthKind() == cliproxyauth.AuthKindAPIKey {
		cfg = cfg.ForAPIKey()
	}
	multiagentv2.RewriteCodexOrphanDelegationInputForConfigDoc(doc, ctx, headers, cfg)
	optimized := multiagentv2.OptimizeCodexMultiAgentV2RequestDoc(doc, ctx, headers, cfg)
	if isCompat {
		multiagentv2.RewriteCodexMultiAgentV2InputDoc(doc, ctx, headers, cfg, true)
	}
	return optimized
}

func RestoreCodexMultiAgentV2Response(payload []byte, optimized bool) []byte {
	return multiagentv2.RestoreCodexMultiAgentV2Response(payload, optimized)
}

func translateRequestWithCodexMultiAgentV2Checked(ctx context.Context, headers http.Header, cfg *config.Config, from, to sdktranslator.Format, model string, payload []byte, stream bool) ([]byte, error) {
	translated := multiagentv2.TranslateRequestEnvelopeWithCodexMultiAgentV2(ctx, headers, cfg, from, to, sdktranslator.RequestEnvelope{Format: from, Model: model, Stream: stream, Body: payload})
	return translated.Body, translated.Err
}
