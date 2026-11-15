package executor

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/signature"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func openaiResponsesReasoningSummaryIsEmpty(summary ojson.Result) bool {
	if !summary.Exists() || summary.Type == ojson.Null {
		return true
	}
	return summary.IsArray() && len(summary.Array()) == 0
}

func promoteOpenAIResponsesReasoningTextToSummary(itemRaw string, content ojson.Result) (string, error) {
	var b strings.Builder
	b.WriteByte('[')
	n := 0
	for _, part := range content.Array() {
		if strings.TrimSpace(part.Get("type").String()) != "reasoning_text" {
			continue
		}
		text := part.Get("text").String()
		if text == "" {
			continue
		}
		partJSON, err := ojson.Set(`{"type":"summary_text"}`, "text", text)
		if err != nil {
			return itemRaw, err
		}
		if n > 0 {
			b.WriteByte(',')
		}
		b.WriteString(partJSON)
		n++
	}
	b.WriteByte(']')
	if n == 0 {
		return itemRaw, nil
	}
	return ojson.SetRaw(itemRaw, "summary", b.String())
}

func promoteOpenAIResponsesReasoningTextToSummaryDoc(itemDoc *ojson.Doc, content ojson.Result) error {
	var b strings.Builder
	b.WriteByte('[')
	n := 0
	for _, part := range content.Array() {
		if strings.TrimSpace(part.Get("type").String()) != "reasoning_text" {
			continue
		}
		text := part.Get("text").String()
		if text == "" {
			continue
		}
		partJSON, err := ojson.Set(`{"type":"summary_text"}`, "text", text)
		if err != nil {
			return err
		}
		if n > 0 {
			b.WriteByte(',')
		}
		b.WriteString(partJSON)
		n++
	}
	b.WriteByte(']')
	if n == 0 {
		return nil
	}
	return itemDoc.SetRawBytes("summary", []byte(b.String()))
}

type metaSignaturePolicy int

const (
	metaSignatureForeign metaSignaturePolicy = iota

	metaSignatureNativeOnly

	metaSignatureAllowRouted
)

func sanitizeOpenAIResponsesReasoningEncryptedContent(ctx context.Context, provider string, body []byte) []byte {
	return sanitizeOpenAIResponsesReasoningEncryptedContentWithOptions(ctx, provider, body, false, false, metaSignatureForeign, signature.SignatureProviderUnknown, false)
}

func sanitizeOpenAIResponsesReasoningEncryptedContentDoc(ctx context.Context, provider string, doc *ojson.Doc) bool {
	return sanitizeOpenAIResponsesReasoningEncryptedContentWithOptionsDoc(ctx, provider, doc, false, false, metaSignatureForeign, signature.SignatureProviderUnknown, false)
}

func sanitizeOpenAIResponsesReasoningEncryptedContentWithCompat(ctx context.Context, provider string, body []byte, isCompat bool) []byte {
	return sanitizeOpenAIResponsesReasoningEncryptedContentWithCompatModel(ctx, provider, body, isCompat, "")
}

func sanitizeOpenAIResponsesReasoningEncryptedContentWithCompatDoc(ctx context.Context, provider string, doc *ojson.Doc, isCompat bool) bool {
	return sanitizeOpenAIResponsesReasoningEncryptedContentWithCompatModelDoc(ctx, provider, doc, isCompat, "")
}

func resolveResponsesSignatureTargetModel(baseModel string, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) string {
	if signature.SignatureProviderFromModelName(baseModel) != signature.SignatureProviderUnknown {
		return baseModel
	}
	requested := thinking.ParseSuffix(helps.PayloadRequestedModel(opts, req.Model)).ModelName
	switch signature.SignatureProviderFromModelName(requested) {
	case signature.SignatureProviderMeta, signature.SignatureProviderGrok, signature.SignatureProviderGPT:
		return requested
	}
	return baseModel
}

func resolveMetaSignatureTargetModel(baseModel string, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) string {
	targetModel := resolveResponsesSignatureTargetModel(baseModel, req, opts)
	if signature.SignatureProviderFromModelName(targetModel) == signature.SignatureProviderMeta {
		return targetModel
	}
	return "muse-" + baseModel
}

func sanitizeOpenAIResponsesReasoningEncryptedContentWithCompatModel(ctx context.Context, provider string, body []byte, isCompat bool, targetModel string) []byte {
	return sanitizeOpenAIResponsesReasoningEncryptedContentWithCompatModelAndBaseURL(ctx, provider, body, isCompat, targetModel, "")
}

func sanitizeOpenAIResponsesReasoningEncryptedContentWithCompatModelDoc(ctx context.Context, provider string, doc *ojson.Doc, isCompat bool, targetModel string) bool {
	return sanitizeOpenAIResponsesReasoningEncryptedContentWithCompatModelAndBaseURLDoc(ctx, provider, doc, isCompat, targetModel, "")
}

func sanitizeOpenAIResponsesReasoningEncryptedContentWithCompatModelAndBaseURL(ctx context.Context, provider string, body []byte, isCompat bool, targetModel string, baseURL string) []byte {
	doc := ojson.NewDoc(body)
	if !sanitizeOpenAIResponsesReasoningEncryptedContentWithCompatModelAndBaseURLDoc(ctx, provider, doc, isCompat, targetModel, baseURL) {
		return body
	}
	return doc.Bytes()
}

func sanitizeOpenAIResponsesReasoningEncryptedContentWithCompatModelAndBaseURLDoc(ctx context.Context, provider string, doc *ojson.Doc, isCompat bool, targetModel string, baseURL string) bool {
	targetProvider := signature.SignatureProviderFromModelName(targetModel)
	metaPolicy := metaSignatureForeign
	if targetProvider == signature.SignatureProviderMeta {
		if strings.TrimSpace(baseURL) != "" && isNativeMetaUpstreamURL(baseURL) {
			metaPolicy = metaSignatureNativeOnly
		} else {
			metaPolicy = metaSignatureAllowRouted
		}
	}
	return sanitizeOpenAIResponsesReasoningEncryptedContentWithOptionsDoc(ctx, provider, doc, isCompat, isCompat, metaPolicy, targetProvider, isOpenRouterUpstreamURL(baseURL))
}

func sanitizeOpenAIResponsesReasoningEncryptedContentKeepForeign(ctx context.Context, provider string, body []byte) []byte {
	return sanitizeOpenAIResponsesReasoningEncryptedContentWithOptions(ctx, provider, body, false, true, metaSignatureForeign, signature.SignatureProviderUnknown, false)
}

func sanitizeOpenAIResponsesReasoningEncryptedContentKeepForeignDoc(ctx context.Context, provider string, doc *ojson.Doc) bool {
	return sanitizeOpenAIResponsesReasoningEncryptedContentWithOptionsDoc(ctx, provider, doc, false, true, metaSignatureForeign, signature.SignatureProviderUnknown, false)
}

func sanitizeOpenAIResponsesReasoningEncryptedContentForMeta(ctx context.Context, provider string, body []byte) []byte {
	return sanitizeOpenAIResponsesReasoningEncryptedContentWithOptions(ctx, provider, body, false, true, metaSignatureNativeOnly, signature.SignatureProviderMeta, false)
}

func sanitizeOpenAIResponsesReasoningEncryptedContentForMetaDoc(ctx context.Context, provider string, doc *ojson.Doc) bool {
	return sanitizeOpenAIResponsesReasoningEncryptedContentWithOptionsDoc(ctx, provider, doc, false, true, metaSignatureNativeOnly, signature.SignatureProviderMeta, false)
}

func sanitizeOpenAIResponsesReasoningEncryptedContentForMetaBaseURL(ctx context.Context, provider string, body []byte, baseURL string) []byte {
	doc := ojson.NewDoc(body)
	if !sanitizeOpenAIResponsesReasoningEncryptedContentForMetaBaseURLDoc(ctx, provider, doc, baseURL) {
		return body
	}
	return doc.Bytes()
}

func sanitizeOpenAIResponsesReasoningEncryptedContentForMetaBaseURLDoc(ctx context.Context, provider string, doc *ojson.Doc, baseURL string) bool {
	if isNativeMetaUpstreamURL(baseURL) {
		return sanitizeOpenAIResponsesReasoningEncryptedContentForMetaDoc(ctx, provider, doc)
	}
	return sanitizeOpenAIResponsesReasoningEncryptedContentWithOptionsDoc(ctx, provider, doc, false, true, metaSignatureAllowRouted, signature.SignatureProviderMeta, isOpenRouterUpstreamURL(baseURL))
}

func isNativeMetaUpstreamURL(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return true
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	return host == "" || host == "api.meta.ai" || strings.HasSuffix(host, ".meta.ai")
}

func isOpenRouterUpstreamURL(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	return host == "openrouter.ai" || strings.HasSuffix(host, ".openrouter.ai")
}

func stripRouterSuffix(rawSignature, suffix string) string {
	if suffix == "" || len(rawSignature) <= len(suffix)+1 {
		return rawSignature
	}
	return rawSignature[:len(rawSignature)-len(suffix)-1]
}

func sanitizeOpenAIResponsesReasoningEncryptedContentWithOptions(ctx context.Context, provider string, body []byte, isCompat bool, keepForeign bool, metaPolicy metaSignaturePolicy, targetProvider signature.SignatureProvider, allowRouterSuffix bool) []byte {
	doc := ojson.NewDoc(body)
	if !sanitizeOpenAIResponsesReasoningEncryptedContentWithOptionsDoc(ctx, provider, doc, isCompat, keepForeign, metaPolicy, targetProvider, allowRouterSuffix) {
		return body
	}
	return doc.Bytes()
}

func sanitizeOpenAIResponsesReasoningEncryptedContentWithOptionsDoc(ctx context.Context, provider string, doc *ojson.Doc, isCompat bool, keepForeign bool, metaPolicy metaSignaturePolicy, targetProvider signature.SignatureProvider, allowRouterSuffix bool) bool {
	if doc == nil || doc.Len() == 0 {
		return false
	}
	inputResult := doc.Get("input")
	if !inputResult.Exists() || !inputResult.IsArray() {
		return false
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = "openai responses upstream"
	}

	stripOrphanReasoningIDs := !doc.Get("store").Bool()

	items := inputResult.Array()

	var rebuilt []byte
	itemsWritten := 0
	keep := func(raw string) {
		if rebuilt == nil {
			return
		}
		if itemsWritten > 0 {
			rebuilt = append(rebuilt, ',')
		}
		rebuilt = append(rebuilt, raw...)
		itemsWritten++
	}
	startRebuild := func(index int) {
		if rebuilt != nil {
			return
		}

		rebuilt = make([]byte, 0, len(inputResult.Raw))
		rebuilt = append(rebuilt, '[')
		for i := range index {
			keep(items[i].Raw)
		}
	}

	for index, item := range items {
		if strings.TrimSpace(item.Get("type").String()) != "reasoning" {
			keep(item.Raw)
			continue
		}

		encryptedContent := item.Get("encrypted_content")
		itemID := strings.TrimSpace(item.Get("id").String())
		if itemID == "" {
			itemID = fmt.Sprintf("input[%d]", index)
		}

		var itemDoc *ojson.Doc
		ensureItemDoc := func() *ojson.Doc {
			if itemDoc == nil {
				itemDoc = ojson.NewDoc([]byte(item.Raw))
			}
			return itemDoc
		}
		changed := false

		content := item.Get("content")
		if !isCompat && content.IsArray() && len(content.Array()) > 0 {
			if openaiResponsesReasoningSummaryIsEmpty(item.Get("summary")) {
				if errPromote := promoteOpenAIResponsesReasoningTextToSummaryDoc(ensureItemDoc(), content); errPromote != nil {
					helps.LogWithRequestID(ctx).Debugf("%s: failed to promote reasoning_text into summary at input[%d]: %v", provider, index, errPromote)
				} else {
					changed = true
				}
			}
			if errClear := ensureItemDoc().SetRawBytes("content", []byte("[]")); errClear != nil {
				helps.LogWithRequestID(ctx).Debugf("%s: failed to clear reasoning content at input[%d]: %v", provider, index, errClear)
			} else {
				changed = true
				helps.LogWithRequestID(ctx).Debugf("%s: cleared reasoning content at input[%d] item_id=%q", provider, index, itemID)
			}
		}

		if !encryptedContent.Exists() {
			if !isCompat && stripOrphanReasoningIDs && item.Get("id").Exists() {
				if errDel := ensureItemDoc().Delete("id"); errDel != nil {
					helps.LogWithRequestID(ctx).Debugf("%s: failed to drop orphan reasoning id at input[%d]: %v", provider, index, errDel)
				} else {
					changed = true
					helps.LogWithRequestID(ctx).Debugf("%s: dropped orphan reasoning id at input[%d] item_id=%q reason=missing encrypted_content with store disabled", provider, index, itemID)
				}
			}
			if !changed || itemDoc == nil {
				keep(item.Raw)
				continue
			}
			startRebuild(index)
			keep(itemDoc.String())
			continue
		}

		reason := ""
		rewrittenSignature := ""
		switch encryptedContent.Type {
		case ojson.String:
			rawSignature := encryptedContent.String()
			if rawSignature != strings.TrimSpace(rawSignature) {
				reason = "encrypted_content has leading or trailing whitespace"
			} else if metaPolicy != metaSignatureForeign {
				if info, err := signature.InspectMetaReasoningSignature(rawSignature); err != nil {
					reason = err.Error()
				} else if info.RouterSuffix != "" {
					if metaPolicy == metaSignatureNativeOnly {
						reason = "router-suffixed Meta reasoning signature is not accepted by the native Meta upstream"
					} else if !allowRouterSuffix {
						rewrittenSignature = stripRouterSuffix(rawSignature, info.RouterSuffix)
					}
				}
			} else if targetProvider == signature.SignatureProviderUnknown || targetProvider == signature.SignatureProviderGPT {
				candidate := rawSignature
				hasGPTPrefix := false
				if prefixedProvider, unprefixed, ok := signature.SplitSignatureProviderPrefix(rawSignature); ok && prefixedProvider == signature.SignatureProviderGPT {
					candidate = unprefixed
					hasGPTPrefix = true
				}
				if gptInfo, err := signature.InspectGPTReasoningSignature(candidate); err == nil {
					if gptInfo.RouterSuffix != "" && !allowRouterSuffix {
						rewrittenSignature = stripRouterSuffix(candidate, gptInfo.RouterSuffix)
					} else if hasGPTPrefix {
						rewrittenSignature = candidate
					}
				} else if !(keepForeign && rawSignature != "" && signature.DetectSignatureProvider(rawSignature) == signature.SignatureProviderUnknown) {
					reason = err.Error()
				}
			} else {
				keepSig := false
				if rawSignature != "" {
					switch targetProvider {
					case signature.SignatureProviderGrok:
						if grokInfo, errGrok := signature.InspectGrokEncryptedContent(rawSignature); errGrok == nil {
							keepSig = true
							if grokInfo.RouterSuffix != "" && !allowRouterSuffix {
								rewrittenSignature = stripRouterSuffix(rawSignature, grokInfo.RouterSuffix)
							}
						}
					case signature.SignatureProviderGemini, signature.SignatureProviderClaude, signature.SignatureProviderKimi, signature.SignatureProviderSWE:
						var normalized string
						normalized, keepSig = signature.CompatibleSignatureForProvider(targetProvider, rawSignature)
						if keepSig && normalized != "" && normalized != rawSignature {
							rewrittenSignature = normalized
						}
					}
					if !keepSig && keepForeign {
						keepSig = signature.DetectSignatureProvider(rawSignature) == signature.SignatureProviderUnknown
					}
				}
				if !keepSig {
					reason = fmt.Sprintf("reasoning encrypted_content is not compatible with target provider %s", targetProvider)
				}
			}
		case ojson.Null:
			reason = "encrypted_content is null"
		default:
			reason = fmt.Sprintf("encrypted_content must be a string, got %s", encryptedContent.Type.String())
		}
		if reason == "" {
			if rewrittenSignature != "" {
				if errSet := ensureItemDoc().Set("encrypted_content", rewrittenSignature); errSet == nil {
					changed = true
					helps.LogWithRequestID(ctx).Debugf("%s: stripped router suffix from reasoning encrypted_content at input[%d] item_id=%q", provider, index, itemID)
				}
			}
			if !changed || itemDoc == nil {
				keep(item.Raw)
				continue
			}
			startRebuild(index)
			keep(itemDoc.String())
			continue
		}

		if errDel := ensureItemDoc().Delete("encrypted_content"); errDel != nil {
			helps.LogWithRequestID(ctx).Debugf("%s: failed to drop invalid reasoning encrypted_content at input[%d]: %v", provider, index, errDel)
			if !changed || itemDoc == nil {
				keep(item.Raw)
				continue
			}
			startRebuild(index)
			keep(itemDoc.String())
			continue
		}
		changed = true
		if !isCompat && stripOrphanReasoningIDs && item.Get("id").Exists() {
			if errID := ensureItemDoc().Delete("id"); errID != nil {
				helps.LogWithRequestID(ctx).Debugf("%s: failed to drop reasoning id after invalid encrypted_content at input[%d]: %v", provider, index, errID)
			}
		}

		startRebuild(index)
		keep(itemDoc.String())

		helps.LogWithRequestID(ctx).Debugf("%s: dropped invalid reasoning encrypted_content at input[%d] item_id=%q reason=%s", provider, index, itemID, reason)
	}

	if rebuilt == nil {
		return false
	}
	rebuilt = append(rebuilt, ']')

	if errSet := doc.SetRawBytes("input", rebuilt); errSet != nil {
		helps.LogWithRequestID(ctx).Debugf("%s: failed to rebuild input array while sanitizing reasoning encrypted_content: %v", provider, errSet)
		return false
	}
	return true
}
