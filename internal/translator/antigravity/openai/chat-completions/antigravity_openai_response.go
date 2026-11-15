




package chat_completions

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	log "github.com/sirupsen/logrus"

	"github.com/MoeForge/moego/ojson"
	. "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/gemini/openai/chat-completions"
)


type convertCliResponseToOpenAIChatParams struct {
	UnixTimestamp        int64
	FunctionIndex        int
	SawResponse          bool
	SawToolCall          bool
	SawFinishReason      bool
	UpstreamFinishReason string
	ModelVersion         string
	ResponseID           string
	PendingUsageMetadata []byte
	SanitizedNameMap     map[string]string
}


var functionCallIDCounter uint64















func ConvertAntigravityResponseToOpenAI(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	params, ok := (*param).(*convertCliResponseToOpenAIChatParams)
	if !ok {
		params = &convertCliResponseToOpenAIChatParams{
			UnixTimestamp: 0,
			FunctionIndex: 0,
		}
		*param = params
	}
	if params.SanitizedNameMap == nil {
		params.SanitizedNameMap = util.DisambiguatedToolNameMap(originalRequestRawJSON)
	}

	if bytes.Equal(rawJSON, []byte("[DONE]")) {



		if params.SawResponse && !params.SawFinishReason {
			params.SawFinishReason = true
			finishReason, nativeFinishReason := resolveOpenAIFinishReason(params)
			templateDoc := ojson.NewDoc([]byte(`{"id":"","object":"chat.completion.chunk","created":0,"model":"model","choices":[{"index":0,"delta":{},"finish_reason":"stop","native_finish_reason":"stop"}]}`))
			_ = templateDoc.Set("created", params.UnixTimestamp)
			if params.ModelVersion != "" {
				_ = templateDoc.Set("model", params.ModelVersion)
			}
			if params.ResponseID != "" {
				_ = templateDoc.Set("id", params.ResponseID)
			}
			if len(params.PendingUsageMetadata) > 0 {
				setOpenAIUsageMetadataDoc(templateDoc, util.ParseGJSONBytesNoCopy(params.PendingUsageMetadata))
			}
			_ = templateDoc.Set("choices.0.finish_reason", finishReason)
			_ = templateDoc.Set("choices.0.native_finish_reason", nativeFinishReason)
			return [][]byte{templateDoc.Bytes()}
		}
		return [][]byte{}
	}

	if !params.SawResponse {
		params.SawResponse = hasAntigravityResponsePayload(rawJSON)
	}


	templateDoc := ojson.NewDoc([]byte(`{"id":"","object":"chat.completion.chunk","created":12345,"model":"model","choices":[{"index":0,"delta":{"role":null,"content":null,"reasoning_content":null},"finish_reason":null,"native_finish_reason":null}]}`))


	if modelVersionResult := ojson.GetBytesNoCopy(rawJSON, "response.modelVersion"); modelVersionResult.Exists() {
		params.ModelVersion = strings.Clone(modelVersionResult.String())
		_ = templateDoc.Set("model", params.ModelVersion)
	}


	if createTimeResult := ojson.GetBytesNoCopy(rawJSON, "response.createTime"); createTimeResult.Exists() {
		t, err := time.Parse(time.RFC3339Nano, createTimeResult.String())
		if err == nil {
			params.UnixTimestamp = t.Unix()
		}
		_ = templateDoc.Set("created", params.UnixTimestamp)
	} else {
		_ = templateDoc.Set("created", params.UnixTimestamp)
	}


	if responseIDResult := ojson.GetBytesNoCopy(rawJSON, "response.responseId"); responseIDResult.Exists() {
		params.ResponseID = strings.Clone(responseIDResult.String())
		_ = templateDoc.Set("id", params.ResponseID)
	}


	if finishReasonResult := ojson.GetBytesNoCopy(rawJSON, "response.candidates.0.finishReason"); finishReasonResult.Exists() {
		params.UpstreamFinishReason = strings.Clone(strings.ToUpper(finishReasonResult.String()))
	}




	usageResult := ojson.GetBytesNoCopy(rawJSON, "response.usageMetadata")
	if !usageResult.Exists() {
		if pendingUsage := ojson.GetBytesNoCopy(rawJSON, "response.cpaUsageMetadata"); pendingUsage.Exists() {
			params.PendingUsageMetadata = append(params.PendingUsageMetadata[:0], pendingUsage.Raw...)
		}
	} else {
		setOpenAIUsageMetadataDoc(templateDoc, usageResult)
	}


	partsResult := ojson.GetBytesNoCopy(rawJSON, "response.candidates.0.content.parts")
	if partsResult.IsArray() {
		partResults := partsResult.Array()
		hasToolCallsArray := false
		imageCount := 0
		var textContent strings.Builder
		var reasoningContent strings.Builder
		hasTextContent := false
		hasReasoningContent := false
		for i := 0; i < len(partResults); i++ {
			partResult := partResults[i]
			partTextResult := partResult.Get("text")
			functionCallResult := partResult.Get("functionCall")
			thoughtSignatureResult := partResult.Get("thoughtSignature")
			if !thoughtSignatureResult.Exists() {
				thoughtSignatureResult = partResult.Get("thought_signature")
			}
			inlineDataResult := partResult.Get("inlineData")
			if !inlineDataResult.Exists() {
				inlineDataResult = partResult.Get("inline_data")
			}

			hasThoughtSignature := thoughtSignatureResult.Exists() && thoughtSignatureResult.String() != ""
			hasTextPayload := partTextResult.Exists() && partTextResult.String() != ""
			hasContentPayload := hasTextPayload || functionCallResult.Exists() || inlineDataResult.Exists()


			if hasThoughtSignature && !hasContentPayload {
				continue
			}

			if partTextResult.Exists() {
				text := partTextResult.String()


				if partResult.Get("thought").Bool() {
					hasReasoningContent = true
					reasoningContent.WriteString(text)
				} else {
					hasTextContent = true
					textContent.WriteString(text)
				}
				_ = templateDoc.Set("choices.0.delta.role", "assistant")
			} else if functionCallResult.Exists() {

				(*param).(*convertCliResponseToOpenAIChatParams).SawToolCall = true
				functionCallIndex := (*param).(*convertCliResponseToOpenAIChatParams).FunctionIndex
				(*param).(*convertCliResponseToOpenAIChatParams).FunctionIndex++
				if !hasToolCallsArray {
					_ = templateDoc.SetRaw("choices.0.delta.tool_calls", `[]`)
					hasToolCallsArray = true
				}

				fcDoc := ojson.NewDoc([]byte(`{"id": "","index": 0,"type": "function","function": {"name": "","arguments": ""}}`))
				fcName := util.RestoreSanitizedToolName((*param).(*convertCliResponseToOpenAIChatParams).SanitizedNameMap, functionCallResult.Get("name").String())
				fcID := strings.TrimSpace(functionCallResult.Get("id").String())
				if fcID == "" {
					fcID = fmt.Sprintf("%s-%d-%d", fcName, time.Now().UnixNano(), atomic.AddUint64(&functionCallIDCounter, 1))
				}
				_ = fcDoc.Set("id", fcID)
				_ = fcDoc.Set("index", functionCallIndex)
				_ = fcDoc.Set("function.name", fcName)
				if fcArgsResult := functionCallResult.Get("args"); fcArgsResult.Exists() {
					_ = fcDoc.Set("function.arguments", fcArgsResult.Raw)
				}
				_ = templateDoc.Set("choices.0.delta.role", "assistant")
				_ = templateDoc.SetRawBytes("choices.0.delta.tool_calls.-1", fcDoc.Bytes())
			} else if inlineDataResult.Exists() {
				data := inlineDataResult.Get("data").String()
				if data == "" {
					continue
				}
				mimeType := inlineDataResult.Get("mimeType").String()
				if mimeType == "" {
					mimeType = inlineDataResult.Get("mime_type").String()
				}
				if mimeType == "" {
					mimeType = "image/png"
				}
				imageURL := fmt.Sprintf("data:%s;base64,%s", mimeType, data)
				if imageCount == 0 {
					_ = templateDoc.SetRaw("choices.0.delta.images", `[]`)
				}
				imageIndex := imageCount
				imageCount++
				imageDoc := ojson.NewDoc([]byte(`{"type":"image_url","image_url":{"url":""}}`))
				_ = imageDoc.Set("index", imageIndex)
				_ = imageDoc.Set("image_url.url", imageURL)
				_ = templateDoc.Set("choices.0.delta.role", "assistant")
				_ = templateDoc.SetRawBytes("choices.0.delta.images.-1", imageDoc.Bytes())
			}
		}
		if hasTextContent {
			_ = templateDoc.Set("choices.0.delta.content", textContent.String())
		}
		if hasReasoningContent {
			_ = templateDoc.Set("choices.0.delta.reasoning_content", reasoningContent.String())
		}
	}







	isFinalChunk := params.UpstreamFinishReason != "" && usageResult.Exists()

	if isFinalChunk {
		finishReason, nativeFinishReason := resolveOpenAIFinishReason(params)
		_ = templateDoc.Set("choices.0.finish_reason", finishReason)
		_ = templateDoc.Set("choices.0.native_finish_reason", nativeFinishReason)
		params.SawFinishReason = true
	}

	return [][]byte{templateDoc.Bytes()}
}






func hasAntigravityResponsePayload(rawJSON []byte) bool {
	for _, path := range []string{"response.candidates", "candidates"} {
		if candidates := ojson.GetBytesNoCopy(rawJSON, path); candidates.IsArray() && len(candidates.Array()) > 0 {
			return true
		}
	}


	for _, path := range []string{
		"response.usageMetadata", "response.cpaUsageMetadata",
		"usageMetadata", "cpaUsageMetadata",
	} {
		if usage := ojson.GetBytesNoCopy(rawJSON, path); usage.IsObject() && len(usage.Map()) > 0 {
			return true
		}
	}
	return false
}




func resolveOpenAIFinishReason(params *convertCliResponseToOpenAIChatParams) (finishReason, nativeFinishReason string) {
	switch {
	case params.SawToolCall:
		finishReason = "tool_calls"
	case params.UpstreamFinishReason == "MAX_TOKENS":
		finishReason = "max_tokens"
	case isAntigravityContentFilterFinishReason(params.UpstreamFinishReason):
		finishReason = "content_filter"
	default:
		finishReason = "stop"
	}
	nativeFinishReason = "stop"
	if params.UpstreamFinishReason != "" {
		nativeFinishReason = strings.ToLower(params.UpstreamFinishReason)
	}
	return finishReason, nativeFinishReason
}

func isAntigravityContentFilterFinishReason(reason string) bool {
	switch strings.ToUpper(strings.TrimSpace(reason)) {
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY", "MALFORMED_RESPONSE", "MALFORMED_FUNCTION_CALL":
		return true
	default:
		return false
	}
}

func setOpenAIUsageMetadataDoc(templateDoc *ojson.Doc, usageResult ojson.Result) {
	cachedTokenCount := usageResult.Get("cachedContentTokenCount").Int()
	_ = templateDoc.Set("usage.completion_tokens", usageResult.Get("candidatesTokenCount").Int()+usageResult.Get("thoughtsTokenCount").Int())
	if totalTokenCountResult := usageResult.Get("totalTokenCount"); totalTokenCountResult.Exists() {
		_ = templateDoc.Set("usage.total_tokens", totalTokenCountResult.Int())
	}
	promptTokenCount := usageResult.Get("promptTokenCount").Int()
	thoughtsTokenCount := usageResult.Get("thoughtsTokenCount").Int()
	_ = templateDoc.Set("usage.prompt_tokens", promptTokenCount)
	if thoughtsTokenCount > 0 {
		_ = templateDoc.Set("usage.completion_tokens_details.reasoning_tokens", thoughtsTokenCount)
	}
	if cachedTokenCount > 0 {
		if errSet := templateDoc.Set("usage.prompt_tokens_details.cached_tokens", cachedTokenCount); errSet != nil {
			log.Warnf("antigravity openai response: failed to set cached_tokens: %v", errSet)
		}
	}
}














func ConvertAntigravityResponseToOpenAINonStream(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []byte {
	responseResult := ojson.GetBytesNoCopy(rawJSON, "response")
	if responseResult.Exists() {
		responseJSON := restoreAntigravityOpenAIFunctionNames([]byte(responseResult.Raw), originalRequestRawJSON)
		return ConvertGeminiResponseToOpenAINonStream(ctx, modelName, originalRequestRawJSON, requestRawJSON, responseJSON, param)
	}
	return []byte{}
}

func restoreAntigravityOpenAIFunctionNames(rawJSON, originalRequestRawJSON []byte) []byte {
	nameMap := util.DisambiguatedToolNameMap(originalRequestRawJSON)
	if len(nameMap) == 0 {
		return rawJSON
	}
	var rawDoc *ojson.Doc
	candidates := ojson.GetBytesNoCopy(rawJSON, "candidates")
	for candidateIndex, candidate := range candidates.Array() {
		for partIndex, part := range candidate.Get("content.parts").Array() {
			for _, field := range []string{"functionCall", "functionResponse"} {
				nameResult := part.Get(field + ".name")
				name := nameResult.String()
				if name == "" {
					continue
				}
				restoredName := util.RestoreSanitizedToolName(nameMap, name)
				if nameResult.Type == ojson.String && restoredName == name {
					continue
				}
				if rawDoc == nil {
					rawDoc = ojson.NewDoc(rawJSON)
				}
				path := fmt.Sprintf("candidates.%d.content.parts.%d.%s.name", candidateIndex, partIndex, field)
				_ = rawDoc.Set(path, restoredName)
			}
		}
	}
	if rawDoc != nil {
		return rawDoc.Bytes()
	}
	return rawJSON
}
