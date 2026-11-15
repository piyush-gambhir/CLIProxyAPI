package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	responsesclaude "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/claude/openai/responses"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	log "github.com/sirupsen/logrus"
)

func (e *ClaudeExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	apiKey, baseURL := claudeCreds(auth)
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	if shouldUseClaudeUpstreamTokenCount(apiKey, baseURL) {
		return e.countTokensUpstream(ctx, auth, req, opts)
	}

	baseModel := thinking.ParseSuffix(req.Model).ModelName
	upstreamModel := e.upstreamModel(baseModel)
	from := opts.SourceFormat
	ctx, req, opts = e.applyNativeToolSearchFallback(ctx, auth, from, upstreamModel, req, opts)
	responseFormat := cliproxyexecutor.ResponseFormatOrSource(opts)
	to := sdktranslator.FromString("claude")

	if from == sdktranslator.FormatOpenAIResponse {
		ctx = responsesclaude.WithTopLevelSystemItems(ctx, true)
	}

	originalPayload := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayload = opts.OriginalRequest
	}
	webSearchSessionKey, webSearchTTL, _ := e.claudeWebSearchResultCacheScope(ctx, auth, from, req, opts, originalPayload)
	ctx = helps.WithClaudeWebSearchResultRestore(ctx, webSearchSessionKey, webSearchTTL)

	stream := from != to
	body, err := helps.TranslateRequestReturningError(ctx, opts.Headers, e.cfg, from, to, baseModel, req.Payload, stream, helps.APIKeyModelIsCompat(req))
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}
	originalTranslatedForPayload := append([]byte(nil), body...)
	if len(opts.OriginalRequest) > 0 {
		originalTranslatedForPayload = helps.TranslateRequestWithAPIKeyModelCompatibility(ctx, opts.Headers, e.cfg, from, to, baseModel, opts.OriginalRequest, stream, helps.APIKeyModelIsCompat(req))
	}
	doc := ojson.NewDoc(body)
	helps.SetStringIfDifferentDoc(doc, "model", upstreamModel)
	helps.NormalizeClaudeToolChoiceDoc(e.upstreamRequestLogProvider(), upstreamModel, doc)
	if errThinking := helps.ApplyRequestThinkingDoc(doc, req, opts, from.String(), to.String(), e.Identifier()); errThinking != nil {
		return cliproxyexecutor.Response{}, errThinking
	}
	helps.RestoreClaudeWebSearchResultsContextDoc(ctx, doc, webSearchSessionKey, webSearchTTL)
	if rebuildMidSystemMessageEnabled(e.cfg, auth) {
		rebuildMidSystemMessagesToTopLevelDoc(doc)
	}
	helps.NormalizeClaudeToolPropertyNamesDoc(doc)
	sanitizeClaudeMessagesForClaudeUpstreamWithDebugDoc(ctx, doc, baseModel, helps.APIKeyModelIsCompat(req))
	applyClaudePreservedThinkingBindingDoc(doc)
	finalize := helps.NewPayloadFinalizer(e.cfg, e.Identifier(), baseModel, to.String(), "", originalTranslatedForPayload, req, opts)
	ctx = helps.WithPayloadFinalizer(ctx, finalize)
	helps.FinalizePayloadDoc(ctx, doc)
	stripPromptCacheOptionsDoc(doc)
	body = doc.Bytes()
	if errValidate := validateClaudeTokenCountRequest(body); errValidate != nil {
		return cliproxyexecutor.Response{}, errValidate
	}

	count, err := helps.CountClaudeInputTokens(body)
	if err != nil {
		return cliproxyexecutor.Response{}, fmt.Errorf("claude executor: token counting failed: %w", err)
	}

	usageJSON := []byte(fmt.Sprintf(`{"input_tokens":%d}`, count))
	out := sdktranslator.TranslateTokenCount(ctx, to, responseFormat, count, usageJSON)
	return cliproxyexecutor.Response{Payload: out}, nil
}

type claudeTokenCountValidationError struct {
	statusErr
}

func (claudeTokenCountValidationError) IsRequestScoped() bool {
	return true
}

func newClaudeTokenCountValidationError(message string) error {
	return claudeTokenCountValidationError{statusErr{code: http.StatusBadRequest, msg: message}}
}

func validateClaudeTokenCountRequest(body []byte) error {
	if !ojson.ValidBytes(body) {
		return newClaudeTokenCountValidationError("invalid Claude token count request JSON")
	}
	root := util.ParseGJSONBytesNoCopy(body)
	if !root.IsObject() {
		return newClaudeTokenCountValidationError("Claude token count request must be a JSON object")
	}
	messages := root.Get("messages")
	if !messages.IsArray() || len(messages.Array()) == 0 {
		return newClaudeTokenCountValidationError("Claude token count request messages must be a non-empty array")
	}
	for _, message := range messages.Array() {
		if !message.IsObject() {
			return newClaudeTokenCountValidationError("Claude token count request messages must contain objects")
		}
		role := message.Get("role").String()
		if role != "user" && role != "assistant" {
			return newClaudeTokenCountValidationError("Claude token count request message role must be user or assistant")
		}
		content := message.Get("content")
		if content.Type == ojson.String {
			continue
		}
		if !content.IsArray() {
			return newClaudeTokenCountValidationError("Claude token count request message content must be a string or array")
		}
		for _, block := range content.Array() {
			if !block.IsObject() || block.Get("type").Type != ojson.String || block.Get("type").String() == "" {
				return newClaudeTokenCountValidationError("Claude token count request content blocks must be typed objects")
			}
		}
	}
	return nil
}

func shouldUseClaudeUpstreamTokenCount(apiKey, baseURL string) bool {
	return strings.TrimSpace(apiKey) != "" && isAnthropicUpstreamBase(baseURL)
}

func (e *ClaudeExecutor) countTokensUpstream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	upstreamModel := e.upstreamModel(baseModel)

	apiKey, baseURL := claudeCreds(auth)
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	url := fmt.Sprintf("%s/v1/messages/count_tokens?beta=true", baseURL)
	fp := resolveClaudeFingerprintPolicy(e.cfg, auth, apiKey)

	from := opts.SourceFormat
	ctx, req, opts = e.applyNativeToolSearchFallback(ctx, auth, from, upstreamModel, req, opts)
	responseFormat := cliproxyexecutor.ResponseFormatOrSource(opts)
	to := sdktranslator.FromString("claude")
	originalPayload := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayload = opts.OriginalRequest
	}
	incomingHeaders, claudeCodeDetection := detectIncomingClaudeCodeRequest(ctx, opts.Headers, originalPayload, true, e.cfg)
	initialPolicy, initialSettings := resolveClaudeWirePolicy(e.cfg, auth, apiKey, claudeCodeDetection.Confirmed)
	confirmedClaudeCode := isEffectiveConfirmedClaudeCode(claudeCodeDetection, initialPolicy)
	ctx = e.pinResponsesSystemItemPlacement(ctx, auth, from, upstreamModel, initialPolicy, initialSettings)
	claudeSessionID := ""
	if fp.ProfileClaudeCodeCLI {
		claudeSessionID = helps.ClaudeAgentSessionUUIDForRequest(incomingHeaders, originalPayload, req.Payload, confirmedClaudeCode, opts.Metadata, req.Metadata)
	}
	webSearchSessionKey, webSearchTTL, _ := e.claudeWebSearchResultCacheScope(ctx, auth, from, req, opts, originalPayload)
	ctx = helps.WithClaudeWebSearchResultRestore(ctx, webSearchSessionKey, webSearchTTL)

	stream := from != to
	body, err := helps.TranslateRequestReturningError(ctx, opts.Headers, e.cfg, from, to, baseModel, req.Payload, stream, helps.APIKeyModelIsCompat(req))
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}
	originalTranslatedForPayload := append([]byte(nil), body...)
	if len(opts.OriginalRequest) > 0 {
		originalTranslatedForPayload = helps.TranslateRequestWithAPIKeyModelCompatibility(ctx, opts.Headers, e.cfg, from, to, baseModel, opts.OriginalRequest, stream, helps.APIKeyModelIsCompat(req))
	}
	doc := ojson.NewDoc(body)
	helps.SetStringIfDifferentDoc(doc, "model", upstreamModel)
	helps.NormalizeClaudeToolChoiceDoc(e.upstreamRequestLogProvider(), upstreamModel, doc)
	if errThinking := helps.ApplyRequestThinkingDoc(doc, req, opts, from.String(), to.String(), e.Identifier()); errThinking != nil {
		return cliproxyexecutor.Response{}, errThinking
	}
	helps.RestoreClaudeWebSearchResultsContextDoc(ctx, doc, webSearchSessionKey, webSearchTTL)
	if rebuildMidSystemMessageEnabled(e.cfg, auth) {
		rebuildMidSystemMessagesToTopLevelDoc(doc)
	}

	directAnthropic := isAnthropicUpstreamBase(baseURL)

	policy, settings := resolveClaudeWirePolicy(e.cfg, auth, apiKey, confirmedClaudeCode)
	cloaked := policy.Cloak
	explicitCacheMode := isExplicitPromptCacheModeDoc(doc, originalPayload, req.Payload)
	if cloaked {
		if !settings.strictMode {
			if errSystem := validateClaudeCallerSystemBlocks(doc.Get("system")); errSystem != nil {
				return cliproxyexecutor.Response{}, errSystem
			}
		}
		relocateClaudeSystemPromptForCountTokensWithPolicyDoc(doc, settings.strictMode, explicitCacheMode, !policy.OAuth)
		if matcher := settings.buildSensitiveWordMatcher(); matcher != nil {
			helps.ObfuscateSensitiveWordsDoc(doc, matcher)
		}
	} else if policy.Uncloak {
		uncloakClaudeCodePayloadDoc(doc, confirmedClaudeCode)
	}

	enforceCacheControlLimitDoc(doc, 4)
	if !explicitCacheMode {
		normalizeCacheControlTTLDoc(doc)
	}

	extraBetas, _ := extractAndRemoveBetasDoc(doc)

	extraBetas = append(extraBetas, claudeTokenCountingBeta)
	if fp.MCPAlias && cloaked {
		mcpAliases := resolveClaudeMCPAliasOptions(ctx, opts.Metadata, req.Metadata)
		prepareClaudeOAuthToolNamesForUpstreamDoc(doc, mcpAliases)
	}
	helps.NormalizeClaudeToolPropertyNamesDoc(doc)
	sanitizeClaudeMessagesForClaudeUpstreamWithDebugDoc(ctx, doc, baseModel, helps.APIKeyModelIsCompat(req))
	applyClaudePreservedThinkingBindingDoc(doc)
	stripPromptCacheOptionsDoc(doc)

	alignCLICountTokensShape := fp.ProfileClaudeCodeCLI
	if directAnthropic || alignCLICountTokensShape {
		_ = doc.Delete("metadata")
		_ = doc.Delete("context_management")
		_ = doc.Delete("diagnostics")
	}
	if alignCLICountTokensShape {
		stripClaudeCodeAttributionSystemDoc(doc)
	}

	finalize := helps.NewPayloadFinalizer(e.cfg, e.Identifier(), baseModel, to.String(), "", originalTranslatedForPayload, req, opts)
	ctx = helps.WithPayloadFinalizer(ctx, finalize)
	helps.FinalizePayloadDoc(ctx, doc)
	stripPromptCacheOptionsDoc(doc)
	body = doc.Bytes()
	if errMidSystem := validateClaudeMidSystemMessageModel(body, confirmedClaudeCode, directAnthropic); errMidSystem != nil {
		return cliproxyexecutor.Response{}, errMidSystem
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}
	if errHeaders := applyClaudeHeaders(httpReq, auth, apiKey, false, extraBetas, body, e.cfg, incomingHeaders, confirmedClaudeCode && !cloaked, claudeSessionID); errHeaders != nil {
		return cliproxyexecutor.Response{}, errHeaders
	}
	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	helps.RecordAPIRequest(ctx, e.cfg, helps.UpstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      body,
		Provider:  e.upstreamRequestLogProvider(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	httpClient := helps.NewUtlsHTTPClient(ctx, e.cfg, auth, 0)
	resp, err := doClaudeUpstreamRequest(httpClient, httpReq)
	if err != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, err)
		return cliproxyexecutor.Response{}, err
	}
	helps.RecordAPIResponseMetadata(ctx, e.cfg, resp.StatusCode, resp.Header.Clone())
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {

		errBody, decErr := decodeResponseBody(resp.Body, claudeResponseContentEncoding(resp.Header))
		if decErr != nil {
			helps.RecordAPIResponseError(ctx, e.cfg, decErr)
			msg := fmt.Sprintf("failed to decode error response body: %v", decErr)
			helps.LogWithRequestID(ctx).Warn(msg)
			return cliproxyexecutor.Response{}, classifyClaudeUpstreamErrorWithCooling(resp.StatusCode, resp.Header, []byte(msg), e.modelLevelCooling())
		}
		b, readErr := io.ReadAll(errBody)
		helps.AppendAPIResponseChunk(ctx, e.cfg, b)
		if readErr != nil {
			helps.RecordAPIResponseError(ctx, e.cfg, readErr)
			msg := fmt.Sprintf("failed to read error response body: %v", readErr)
			helps.LogWithRequestID(ctx).Warn(msg)
			if len(b) == 0 {
				b = []byte(msg)
			}
		}
		if errClose := errBody.Close(); errClose != nil {
			log.Errorf("response body close error: %v", errClose)
		}
		return cliproxyexecutor.Response{}, classifyClaudeUpstreamErrorWithCooling(resp.StatusCode, resp.Header, b, e.modelLevelCooling())
	}
	decodedBody, err := decodeResponseBody(resp.Body, claudeResponseContentEncoding(resp.Header))
	if err != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, err)
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("response body close error: %v", errClose)
		}
		return cliproxyexecutor.Response{}, err
	}
	defer func() {
		if errClose := decodedBody.Close(); errClose != nil {
			log.Errorf("response body close error: %v", errClose)
		}
	}()
	data, err := io.ReadAll(decodedBody)
	helps.AppendAPIResponseChunk(ctx, e.cfg, data)
	if err != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, err)
		return cliproxyexecutor.Response{}, err
	}
	count := ojson.GetBytesNoCopy(data, "input_tokens").Int()
	out := sdktranslator.TranslateTokenCount(ctx, to, responseFormat, count, data)
	return cliproxyexecutor.Response{Payload: out, Headers: resp.Header.Clone()}, nil
}
