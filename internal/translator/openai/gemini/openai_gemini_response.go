




package gemini

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
	translatorcommon "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/common"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
)


type ConvertOpenAIResponseToGeminiParams struct {

	ToolCallsAccumulator map[int]*ToolCallAccumulator

	ContentAccumulator strings.Builder

	IsFirstChunk bool
}


type ToolCallAccumulator struct {
	ID               string
	Name             string
	Arguments        strings.Builder
	ThoughtSignature string
}













func ConvertOpenAIResponseToGemini(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	if *param == nil {
		*param = &ConvertOpenAIResponseToGeminiParams{
			ToolCallsAccumulator: nil,
			ContentAccumulator:   strings.Builder{},
			IsFirstChunk:         false,
		}
	}


	if bytes.Equal(bytes.TrimSpace(rawJSON), []byte("[DONE]")) {
		return [][]byte{}
	}

	if bytes.HasPrefix(rawJSON, []byte("data:")) {
		rawJSON = bytes.TrimSpace(rawJSON[5:])
	}

	root := util.ParseGJSONBytesNoCopy(rawJSON)


	if (*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator == nil {
		(*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator = make(map[int]*ToolCallAccumulator)
	}


	if choices := root.Get("choices"); choices.Exists() && choices.IsArray() {

		if len(choices.Array()) == 0 {

			if usage := root.Get("usage"); usage.Exists() {
				templateDoc := ojson.NewDocString(`{"candidates":[],"usageMetadata":{}}`)


				if model := root.Get("model"); model.Exists() {
					_ = templateDoc.Set("model", model.String())
				}

				setGeminiUsageMetadataFromOpenAIUsageDoc(templateDoc, usage)
				return [][]byte{templateDoc.Bytes()}
			}
			return [][]byte{}
		}

		var results [][]byte

		choices.ForEach(func(choiceIndex, choice ojson.Result) bool {

			template := []byte(`{"candidates":[{"content":{"parts":[],"role":"model"},"index":0}]}`)


			if model := root.Get("model"); model.Exists() {
				template, _ = ojson.SetBytes(template, "model", model.String())
			}

			_ = int(choice.Get("index").Int())
			delta := choice.Get("delta")
			baseTemplate := append([]byte(nil), template...)


			if role := delta.Get("role"); role.Exists() && (*param).(*ConvertOpenAIResponseToGeminiParams).IsFirstChunk {

				if role.String() == "assistant" {
					template, _ = ojson.SetBytes(template, "candidates.0.content.role", "model")
				}
				(*param).(*ConvertOpenAIResponseToGeminiParams).IsFirstChunk = false
				results = append(results, template)
				return true
			}

			var chunkOutputs [][]byte


			if reasoning := delta.Get("reasoning_content"); reasoning.Exists() {
				for _, reasoningText := range extractReasoningTexts(reasoning) {
					if reasoningText == "" {
						continue
					}
					reasoningDoc := ojson.NewDoc(baseTemplate)
					_ = reasoningDoc.Set("candidates.0.content.parts.0.thought", true)
					_ = reasoningDoc.Set("candidates.0.content.parts.0.text", reasoningText)
					chunkOutputs = append(chunkOutputs, reasoningDoc.Bytes())
				}
			}


			if content := delta.Get("content"); content.Exists() && content.String() != "" {
				contentText := content.String()
				(*param).(*ConvertOpenAIResponseToGeminiParams).ContentAccumulator.WriteString(contentText)


				contentTemplate := append([]byte(nil), baseTemplate...)
				contentTemplate, _ = ojson.SetBytes(contentTemplate, "candidates.0.content.parts.0.text", contentText)
				chunkOutputs = append(chunkOutputs, contentTemplate)
			}

			if len(chunkOutputs) > 0 {
				results = append(results, chunkOutputs...)
				finishReason := choice.Get("finish_reason")
				if !delta.Get("tool_calls").Exists() && (finishReason.Type != ojson.String || finishReason.String() == "") {
					return true
				}
			}


			if toolCalls := delta.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				toolCalls.ForEach(func(_, toolCall ojson.Result) bool {
					toolIndex := int(toolCall.Get("index").Int())
					toolID := toolCall.Get("id").String()
					toolType := toolCall.Get("type").String()
					function := toolCall.Get("function")
					thoughtSig := openAIToolCallThoughtSignature(toolCall)


					if toolType != "" && toolType != "function" {
						return true
					}


					if !function.Exists() && thoughtSig == "" {
						return true
					}

					functionName := function.Get("name").String()
					functionArgs := function.Get("arguments").String()


					if _, exists := (*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator[toolIndex]; !exists {
						(*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator[toolIndex] = &ToolCallAccumulator{
							ID:   strings.Clone(toolID),
							Name: strings.Clone(functionName),
						}
					}

					acc := (*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator[toolIndex]


					if toolID != "" {
						acc.ID = strings.Clone(toolID)
					}


					if functionName != "" {
						acc.Name = strings.Clone(functionName)
					}


					if functionArgs != "" {
						acc.Arguments.WriteString(functionArgs)
					}

					if thoughtSig != "" {
						acc.ThoughtSignature = strings.Clone(thoughtSig)
					}

					return true
				})

				if finishReason := choice.Get("finish_reason"); finishReason.Type != ojson.String || finishReason.String() == "" {

					return true
				}
			}


			if finishReason := choice.Get("finish_reason"); finishReason.Type == ojson.String && finishReason.String() != "" {
				geminiFinishReason := mapOpenAIFinishReasonToGemini(finishReason.String())
				templateDoc := ojson.NewDoc(template)
				_ = templateDoc.Set("candidates.0.finishReason", geminiFinishReason)


				if len((*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator) > 0 {
					toolIndices := make([]int, 0, len((*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator))
					for toolIndex := range (*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator {
						toolIndices = append(toolIndices, toolIndex)
					}
					sort.Ints(toolIndices)

					partIndex := 0
					for _, toolIndex := range toolIndices {
						accumulator := (*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator[toolIndex]
						idPath := fmt.Sprintf("candidates.0.content.parts.%d.functionCall.id", partIndex)
						namePath := fmt.Sprintf("candidates.0.content.parts.%d.functionCall.name", partIndex)
						argsPath := fmt.Sprintf("candidates.0.content.parts.%d.functionCall.args", partIndex)
						if accumulator.ID != "" {
							_ = templateDoc.Set(idPath, accumulator.ID)
						}
						_ = templateDoc.Set(namePath, accumulator.Name)
						_ = templateDoc.SetRaw(argsPath, parseArgsToObjectRaw(accumulator.Arguments.String()))
						if accumulator.ThoughtSignature != "" {
							sigPath := fmt.Sprintf("candidates.0.content.parts.%d.thoughtSignature", partIndex)
							_ = templateDoc.Set(sigPath, accumulator.ThoughtSignature)
						}
						partIndex++
					}


					(*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator = make(map[int]*ToolCallAccumulator)
				}

				results = append(results, templateDoc.Bytes())
				return true
			}


			if usage := root.Get("usage"); usage.Exists() {
				templateDoc := ojson.NewDoc(template)
				setGeminiUsageMetadataFromOpenAIUsageDoc(templateDoc, usage)
				results = append(results, templateDoc.Bytes())
				return true
			}

			return true
		})
		return results
	}
	return [][]byte{}
}

func openAIToolCallThoughtSignature(toolCall ojson.Result) string {
	for _, path := range []string{
		"extra_content.google.thought_signature",
		"function.extra_content.google.thought_signature",
		"thoughtSignature",
		"thought_signature",
	} {
		if sig := toolCall.Get(path); sig.Exists() && sig.String() != "" {
			return sig.String()
		}
	}
	return ""
}


func mapOpenAIFinishReasonToGemini(openAIReason string) string {
	switch openAIReason {
	case "stop":
		return "STOP"
	case "length":
		return "MAX_TOKENS"
	case "tool_calls":
		return "STOP"
	case "content_filter":
		return "SAFETY"
	default:
		return "STOP"
	}
}



func parseArgsToObjectRaw(argsStr string) string {
	trimmed := strings.TrimSpace(argsStr)
	if trimmed == "" || trimmed == "{}" {
		return "{}"
	}


	if ojson.Valid(trimmed) {
		strict := ojson.Parse(trimmed)
		if strict.IsObject() {
			return strict.Raw
		}
	}


	tolerant := tolerantParseJSONObjectRaw(trimmed)
	if tolerant != "{}" {
		return tolerant
	}


	return "{}"
}

func escapeSjsonPathKey(key string) string {
	key = strings.ReplaceAll(key, `\`, `\\`)
	key = strings.ReplaceAll(key, `.`, `\.`)
	return key
}




func tolerantParseJSONObjectRaw(s string) string {

	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || start >= end {
		return "{}"
	}
	content := s[start+1 : end]

	runes := []rune(content)
	n := len(runes)
	i := 0
	resultDoc := ojson.NewDocString(`{}`)

	for i < n {

		for i < n && (runes[i] == ' ' || runes[i] == '\n' || runes[i] == '\r' || runes[i] == '\t' || runes[i] == ',') {
			i++
		}
		if i >= n {
			break
		}


		if runes[i] != '"' {

			for i < n && runes[i] != ',' {
				i++
			}
			continue
		}


		keyToken, nextIdx := parseJSONStringRunes(runes, i)
		if nextIdx == -1 {
			break
		}
		keyName := jsonStringTokenToRawString(keyToken)
		sjsonKey := escapeSjsonPathKey(keyName)
		i = nextIdx


		for i < n && (runes[i] == ' ' || runes[i] == '\n' || runes[i] == '\r' || runes[i] == '\t') {
			i++
		}
		if i >= n || runes[i] != ':' {
			break
		}
		i++

		for i < n && (runes[i] == ' ' || runes[i] == '\n' || runes[i] == '\r' || runes[i] == '\t') {
			i++
		}
		if i >= n {
			break
		}


		switch runes[i] {
		case '"':

			valToken, ni := parseJSONStringRunes(runes, i)
			if ni == -1 {

				_ = resultDoc.Set(sjsonKey, "")
				i = n
			} else {
				_ = resultDoc.Set(sjsonKey, jsonStringTokenToRawString(valToken))
				i = ni
			}
		case '{', '[':

			seg, ni := captureBracketed(runes, i)
			if ni == -1 {
				i = n
			} else {
				if ojson.Valid(seg) {
					_ = resultDoc.SetRaw(sjsonKey, seg)
				} else {
					_ = resultDoc.Set(sjsonKey, seg)
				}
				i = ni
			}
		default:

			j := i
			for j < n && runes[j] != ',' {
				j++
			}
			token := strings.TrimSpace(string(runes[i:j]))

			if token == "true" {
				_ = resultDoc.Set(sjsonKey, true)
			} else if token == "false" {
				_ = resultDoc.Set(sjsonKey, false)
			} else if token == "null" {
				_ = resultDoc.Set(sjsonKey, nil)
			} else if numVal, ok := tryParseNumber(token); ok {
				_ = resultDoc.Set(sjsonKey, numVal)
			} else {
				_ = resultDoc.Set(sjsonKey, token)
			}
			i = j
		}


		for i < n && (runes[i] == ' ' || runes[i] == '\n' || runes[i] == '\r' || runes[i] == '\t') {
			i++
		}
		if i < n && runes[i] == ',' {
			i++
		}
	}

	return resultDoc.String()
}


func parseJSONStringRunes(runes []rune, start int) (string, int) {
	if start >= len(runes) || runes[start] != '"' {
		return "", -1
	}
	i := start + 1
	escaped := false
	for i < len(runes) {
		r := runes[i]
		if r == '\\' && !escaped {
			escaped = true
			i++
			continue
		}
		if r == '"' && !escaped {
			return string(runes[start : i+1]), i + 1
		}
		escaped = false
		i++
	}
	return string(runes[start:]), -1
}


func jsonStringTokenToRawString(token string) string {
	r := ojson.Parse(token)
	if r.Type == ojson.String {
		return r.String()
	}

	if len(token) >= 2 && token[0] == '"' && token[len(token)-1] == '"' {
		return token[1 : len(token)-1]
	}
	return token
}



func captureBracketed(runes []rune, i int) (string, int) {
	if i >= len(runes) {
		return "", -1
	}
	startRune := runes[i]
	var endRune rune
	if startRune == '{' {
		endRune = '}'
	} else if startRune == '[' {
		endRune = ']'
	} else {
		return "", -1
	}
	depth := 0
	j := i
	inStr := false
	escaped := false
	for j < len(runes) {
		r := runes[j]
		if inStr {
			if r == '\\' && !escaped {
				escaped = true
				j++
				continue
			}
			if r == '"' && !escaped {
				inStr = false
			} else {
				escaped = false
			}
			j++
			continue
		}
		if r == '"' {
			inStr = true
			j++
			continue
		}
		if r == startRune {
			depth++
		} else if r == endRune {
			depth--
			if depth == 0 {
				return string(runes[i : j+1]), j + 1
			}
		}
		j++
	}
	return string(runes[i:]), -1
}


func tryParseNumber(s string) (interface{}, bool) {
	if s == "" {
		return nil, false
	}

	if i64, errParseInt := strconv.ParseInt(s, 10, 64); errParseInt == nil {
		return i64, true
	}
	if u64, errParseUInt := strconv.ParseUint(s, 10, 64); errParseUInt == nil {
		return u64, true
	}
	if f64, errParseFloat := strconv.ParseFloat(s, 64); errParseFloat == nil {
		return f64, true
	}
	return nil, false
}











func ConvertOpenAIResponseToGeminiNonStream(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	root := util.ParseGJSONBytesNoCopy(rawJSON)


	outDoc := ojson.NewDocString(`{"candidates":[{"content":{"parts":[],"role":"model"},"index":0}]}`)


	if model := root.Get("model"); model.Exists() {
		_ = outDoc.Set("model", model.String())
	}

	var allParts [][]byte


	if choices := root.Get("choices"); choices.Exists() && choices.IsArray() {
		choices.ForEach(func(choiceIndex, choice ojson.Result) bool {
			choiceIdx := int(choice.Get("index").Int())
			message := choice.Get("message")


			if role := message.Get("role"); role.Exists() {
				if role.String() == "assistant" {
					_ = outDoc.Set("candidates.0.content.role", "model")
				}
			}

			partIndex := 0
			ensurePartDoc := func(idx int) *ojson.Doc {
				for len(allParts) <= idx {
					allParts = append(allParts, []byte(`{}`))
				}
				return ojson.NewDoc(allParts[idx])
			}


			if reasoning := message.Get("reasoning_content"); reasoning.Exists() {
				for _, reasoningText := range extractReasoningTexts(reasoning) {
					if reasoningText == "" {
						continue
					}
					partDoc := ensurePartDoc(partIndex)
					_ = partDoc.Set("thought", true)
					_ = partDoc.Set("text", reasoningText)
					allParts[partIndex] = partDoc.Bytes()
					partIndex++
				}
			}


			if content := message.Get("content"); content.Exists() && content.String() != "" {
				partDoc := ensurePartDoc(partIndex)
				_ = partDoc.Set("text", content.String())
				allParts[partIndex] = partDoc.Bytes()
				partIndex++
			}


			if toolCalls := message.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				toolCalls.ForEach(func(_, toolCall ojson.Result) bool {
					if toolCall.Get("type").String() == "function" {
						function := toolCall.Get("function")
						functionName := function.Get("name").String()
						functionArgs := function.Get("arguments").String()
						functionID := toolCall.Get("id").String()

						partDoc := ensurePartDoc(partIndex)
						if functionID != "" {
							_ = partDoc.Set("functionCall.id", functionID)
						}
						_ = partDoc.Set("functionCall.name", functionName)
						_ = partDoc.SetRaw("functionCall.args", parseArgsToObjectRaw(functionArgs))
						if thoughtSig := openAIToolCallThoughtSignature(toolCall); thoughtSig != "" {
							_ = partDoc.Set("thoughtSignature", thoughtSig)
						}
						allParts[partIndex] = partDoc.Bytes()
						partIndex++
					}
					return true
				})
			}


			if finishReason := choice.Get("finish_reason"); finishReason.Type == ojson.String && finishReason.String() != "" {
				geminiFinishReason := mapOpenAIFinishReasonToGemini(finishReason.String())
				_ = outDoc.Set("candidates.0.finishReason", geminiFinishReason)
			}


			_ = outDoc.Set("candidates.0.index", choiceIdx)

			return true
		})

		if len(allParts) > 0 {
			_ = outDoc.SetRawBytes("candidates.0.content.parts", translatorcommon.JoinRawArray(allParts))
		}
	}


	if usage := root.Get("usage"); usage.Exists() {
		setGeminiUsageMetadataFromOpenAIUsageDoc(outDoc, usage)
	}

	return outDoc.Bytes()
}

func GeminiTokenCount(ctx context.Context, count int64) []byte {
	return translatorcommon.GeminiTokenCountJSON(count)
}

func reasoningTokensFromUsage(usage ojson.Result) int64 {
	if usage.Exists() {
		if v := usage.Get("completion_tokens_details.reasoning_tokens"); v.Exists() {
			return v.Int()
		}
		if v := usage.Get("output_tokens_details.reasoning_tokens"); v.Exists() {
			return v.Int()
		}
	}
	return 0
}

func setGeminiUsageMetadataFromOpenAIUsageDoc(outDoc *ojson.Doc, usage ojson.Result) {
	promptTokens, hasPromptTokens := tokenCountFromUsage(usage, "prompt_tokens", "input_tokens")
	completionTokens, hasCompletionTokens := tokenCountFromUsage(usage, "completion_tokens", "output_tokens")
	totalTokens, hasTotalTokens := tokenCountFromUsage(usage, "total_tokens")
	if hasPromptTokens {
		_ = outDoc.Set("usageMetadata.promptTokenCount", promptTokens)
	}
	if hasCompletionTokens {
		_ = outDoc.Set("usageMetadata.candidatesTokenCount", completionTokens)
	}
	if hasTotalTokens {
		_ = outDoc.Set("usageMetadata.totalTokenCount", totalTokens)
	} else if hasPromptTokens || hasCompletionTokens {
		_ = outDoc.Set("usageMetadata.totalTokenCount", promptTokens+completionTokens)
	}
	if reasoningTokens := reasoningTokensFromUsage(usage); reasoningTokens > 0 {
		_ = outDoc.Set("usageMetadata.thoughtsTokenCount", reasoningTokens)
	}
	if cachedTokens := cachedTokensFromUsage(usage); cachedTokens > 0 {
		_ = outDoc.Set("usageMetadata.cachedContentTokenCount", cachedTokens)
	}
}

func tokenCountFromUsage(usage ojson.Result, paths ...string) (int64, bool) {
	for _, path := range paths {
		if v := usage.Get(path); v.Exists() {
			return v.Int(), true
		}
	}
	return 0, false
}

func cachedTokensFromUsage(usage ojson.Result) int64 {
	if usage.Exists() {
		if v := usage.Get("prompt_tokens_details.cached_tokens"); v.Exists() {
			return v.Int()
		}
		if v := usage.Get("input_tokens_details.cached_tokens"); v.Exists() {
			return v.Int()
		}
	}
	return 0
}

func extractReasoningTexts(node ojson.Result) []string {
	var texts []string
	if !node.Exists() {
		return texts
	}

	if node.IsArray() {
		node.ForEach(func(_, value ojson.Result) bool {
			texts = append(texts, extractReasoningTexts(value)...)
			return true
		})
		return texts
	}

	switch node.Type {
	case ojson.String:
		texts = append(texts, node.String())
	case ojson.JSON:
		if text := node.Get("text"); text.Exists() {
			texts = append(texts, text.String())
		} else if raw := strings.TrimSpace(node.Raw); raw != "" && !strings.HasPrefix(raw, "{") && !strings.HasPrefix(raw, "[") {
			texts = append(texts, raw)
		}
	}

	return texts
}
