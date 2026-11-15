package claude

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/MoeForge/moego/ojson"
	translatorcommon "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/common"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
)

var (
	dataTag = []byte("data:")
)

type ConvertOpenAIResponseToAnthropicParams struct {
	MessageID   string
	Model       string
	CreatedAt   int64
	ToolNameMap map[string]string

	SawToolCall bool

	ContentAccumulator strings.Builder

	ToolCallsAccumulator map[int]*ToolCallAccumulator

	TextContentBlockStarted bool

	ThinkingContentBlockStarted bool

	FinishReason string

	ContentBlocksStopped bool

	MessageDeltaSent bool

	MessageStarted bool

	MessageStopSent bool

	ToolCallBlockIndexes map[int]int

	OmittedToolCallIndexes map[int]int

	TextContentBlockIndex int

	ThinkingContentBlockIndex int

	NextContentBlockIndex int

	OpenToolCallIndex int

	InterleavedContentChunks []InterleavedContentChunk

	UsageInputTokens      int64
	UsageOutputTokens     int64
	UsageCachedTokens     int64
	UsageCacheWriteTokens int64

	ToolNameMapResolved bool

	StreamResolved  bool
	StreamRequested bool
}

type InterleavedContentChunk struct {
	Type string
	Text string
}

type ToolCallAccumulator struct {
	ID        string
	Name      string
	Arguments strings.Builder

	StartEmitted bool
}

func ConvertOpenAIResponseToClaude(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	if *param == nil {
		*param = &ConvertOpenAIResponseToAnthropicParams{
			MessageID:                   "",
			Model:                       "",
			CreatedAt:                   0,
			ToolNameMap:                 nil,
			SawToolCall:                 false,
			ContentAccumulator:          strings.Builder{},
			ToolCallsAccumulator:        nil,
			TextContentBlockStarted:     false,
			ThinkingContentBlockStarted: false,
			FinishReason:                "",
			ContentBlocksStopped:        false,
			MessageDeltaSent:            false,
			ToolCallBlockIndexes:        make(map[int]int),
			TextContentBlockIndex:       -1,
			ThinkingContentBlockIndex:   -1,
			NextContentBlockIndex:       0,
			OpenToolCallIndex:           -1,
			InterleavedContentChunks:    nil,
			UsageInputTokens:            0,
			UsageOutputTokens:           0,
			UsageCachedTokens:           0,
			UsageCacheWriteTokens:       0,
		}
	}

	if !bytes.HasPrefix(rawJSON, dataTag) {
		return [][]byte{}
	}
	rawJSON = bytes.TrimSpace(rawJSON[5:])

	state := (*param).(*ConvertOpenAIResponseToAnthropicParams)
	if !state.ToolNameMapResolved {
		if state.ToolNameMap == nil {
			state.ToolNameMap = util.ToolNameMapFromClaudeRequest(originalRequestRawJSON)
		}
		state.ToolNameMapResolved = true
	}

	if bytes.Equal(bytes.TrimSpace(rawJSON), []byte("[DONE]")) {
		return convertOpenAIDoneToAnthropic(state)
	}

	if !state.StreamResolved {
		streamResult := ojson.GetBytesNoCopy(originalRequestRawJSON, "stream")
		state.StreamRequested = streamResult.Exists() && streamResult.Type != ojson.False
		state.StreamResolved = true
	}
	if !state.StreamRequested {
		return convertOpenAINonStreamingToAnthropic(rawJSON)
	}
	return convertOpenAIStreamingChunkToAnthropic(rawJSON, state)
}

func hasValidToolCallArguments(param *ConvertOpenAIResponseToAnthropicParams) bool {
	if param == nil || len(param.ToolCallsAccumulator) == 0 {
		return true
	}
	for _, acc := range param.ToolCallsAccumulator {
		if acc == nil {
			continue
		}
		if !acc.StartEmitted && acc.Name == "" && acc.ID == "" && acc.Arguments.Len() == 0 {
			continue
		}
		if acc.Arguments.Len() == 0 {
			continue
		}
		argsStr := strings.TrimSpace(acc.Arguments.String())
		if argsStr == "" {
			return false
		}
		if argsStr == "{}" {
			continue
		}
		fixed := util.FixJSON(argsStr)
		if !ojson.Valid(fixed) || !ojson.Parse(fixed).IsObject() {
			return false
		}
	}
	return true
}

func effectiveOpenAIFinishReason(param *ConvertOpenAIResponseToAnthropicParams) string {
	if param == nil {
		return ""
	}
	if param.FinishReason == "length" || param.FinishReason == "content_filter" {
		return param.FinishReason
	}
	if param.SawToolCall {
		if hasValidToolCallArguments(param) {
			return "tool_calls"
		}
		return "length"
	}
	return param.FinishReason
}

func terminalOpenAIFinishReason(param *ConvertOpenAIResponseToAnthropicParams) string {
	if reason := effectiveOpenAIFinishReason(param); reason != "" {
		return reason
	}
	return "stop"
}

func convertOpenAIStreamingChunkToAnthropic(rawJSON []byte, param *ConvertOpenAIResponseToAnthropicParams) [][]byte {
	root := util.ParseGJSONBytesNoCopy(rawJSON)
	var results [][]byte

	if param.MessageID == "" {
		param.MessageID = strings.Clone(root.Get("id").String())
	}
	if param.Model == "" {
		param.Model = strings.Clone(root.Get("model").String())
	}
	if param.CreatedAt == 0 {
		param.CreatedAt = root.Get("created").Int()
	}

	if delta := root.Get("choices.0.delta"); delta.Exists() {
		if !param.MessageStarted {

			messageStartDoc := ojson.NewDocString(`{"type":"message_start","message":{"id":"","type":"message","role":"assistant","model":"","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}`)
			_ = messageStartDoc.Set("message.id", param.MessageID)
			_ = messageStartDoc.Set("message.model", param.Model)
			results = append(results, translatorcommon.AppendSSEEventBytes(nil, "message_start", messageStartDoc.Bytes(), 2))
			param.MessageStarted = true

		}

		for _, reasoningText := range collectOpenAIObjectReasoningTexts(delta) {
			emitStreamingThinkingDelta(param, reasoningText, &results)
		}

		if content := delta.Get("content"); content.Exists() {
			if content.IsArray() {
				content.ForEach(func(_, part ojson.Result) bool {
					if part.Type == ojson.String {
						emitStreamingTextDelta(param, part.String(), &results)
						return true
					}
					switch part.Get("type").String() {
					case "thinking", "reasoning":
						emitStreamingThinkingDelta(param, extractOpenAIContentPartThinkingText(part), &results)
					case "text":
						emitStreamingTextDelta(param, part.Get("text").String(), &results)
					default:
						emitStreamingTextDelta(param, part.String(), &results)
					}
					return true
				})
			} else if content.Type == ojson.String && content.String() != "" {
				emitStreamingTextDelta(param, content.String(), &results)
			}
		}

		if toolCalls := delta.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
			if param.ToolCallsAccumulator == nil {
				param.ToolCallsAccumulator = make(map[int]*ToolCallAccumulator)
			}

			toolCalls.ForEach(func(arrayIndex, toolCall ojson.Result) bool {
				var incomingID string
				if id := toolCall.Get("id"); id.Exists() && id.Type == ojson.String {
					incomingID = id.String()
				}

				index := int(toolCall.Get("index").Int())
				if !toolCall.Get("index").Exists() {
					pos := int(arrayIndex.Int())
					index = pos
					if param.OmittedToolCallIndexes != nil {
						if mapped, ok := param.OmittedToolCallIndexes[pos]; ok {
							index = mapped
						}
					}
					if existing, exists := param.ToolCallsAccumulator[index]; exists && existing != nil && incomingID != "" && existing.ID != "" && existing.ID != incomingID {
						nextIndex := index + 1
						for k := range param.ToolCallsAccumulator {
							if k >= nextIndex {
								nextIndex = k + 1
							}
						}
						index = nextIndex
						if param.OmittedToolCallIndexes == nil {
							param.OmittedToolCallIndexes = make(map[int]int)
						}
						param.OmittedToolCallIndexes[pos] = index
					}
				}

				if _, exists := param.ToolCallsAccumulator[index]; !exists {
					param.ToolCallsAccumulator[index] = &ToolCallAccumulator{}
				}

				accumulator := param.ToolCallsAccumulator[index]

				if incomingID != "" && !accumulator.StartEmitted {
					accumulator.ID = strings.Clone(incomingID)
				}

				if function := toolCall.Get("function"); function.Exists() {

					if !accumulator.StartEmitted {
						if name := function.Get("name"); name.Exists() && name.Type == ojson.String && name.String() != "" {
							accumulator.Name = strings.Clone(util.MapToolName(param.ToolNameMap, name.String()))
						}
					}

					if args := function.Get("arguments"); args.Exists() {
						argsText := args.String()
						if argsText != "" {
							accumulator.Arguments.WriteString(argsText)
						}
					}
				}

				if !accumulator.StartEmitted && accumulator.Name != "" && accumulator.ID != "" && !param.ContentBlocksStopped {
					if param.OpenToolCallIndex == -1 {
						emitToolUseStart(param, index, accumulator, &results)
					}
				}

				return true
			})
		}
	}

	if finishReason := root.Get("choices.0.finish_reason"); finishReason.Exists() && finishReason.String() != "" {
		reason := finishReason.String()
		switch {
		case reason == "length":
			param.FinishReason = "length"
		case reason == "content_filter":
			param.FinishReason = "content_filter"
		case param.SawToolCall:
			if hasValidToolCallArguments(param) {
				param.FinishReason = "tool_calls"
			} else {
				param.FinishReason = "length"
			}
		case reason == "tool_calls":
			param.FinishReason = "stop"
		default:
			param.FinishReason = strings.Clone(reason)
		}

		finalizeOpenAIAnthropicContentBlocks(param, &results)

	}

	usage := root.Get("usage")
	hasUsage := usage.Exists() && usage.Type != ojson.Null
	if hasUsage {
		param.UsageInputTokens, param.UsageOutputTokens, param.UsageCachedTokens, param.UsageCacheWriteTokens = extractOpenAIUsage(usage)
	}

	isTrailingUsageChunk := hasUsage && !root.Get("choices.0").Exists() &&
		(param.FinishReason != "" || param.SawToolCall || param.TextContentBlockStarted || param.ThinkingContentBlockStarted || param.ContentAccumulator.Len() > 0 || len(param.InterleavedContentChunks) > 0)

	if !param.MessageDeltaSent && isTrailingUsageChunk {
		finalizeOpenAIAnthropicContentBlocks(param, &results)
		emitAnthropicMessageDelta(param, &results, param.UsageInputTokens, param.UsageOutputTokens, param.UsageCachedTokens, param.UsageCacheWriteTokens)
		emitMessageStopIfNeeded(param, &results)
	}

	return results
}

func convertOpenAIDoneToAnthropic(param *ConvertOpenAIResponseToAnthropicParams) [][]byte {
	var results [][]byte

	finalizeOpenAIAnthropicContentBlocks(param, &results)

	if !param.MessageDeltaSent {
		emitAnthropicMessageDelta(param, &results, param.UsageInputTokens, param.UsageOutputTokens, param.UsageCachedTokens, param.UsageCacheWriteTokens)
	}

	emitMessageStopIfNeeded(param, &results)

	return results
}

func convertOpenAINonStreamingToAnthropic(rawJSON []byte) [][]byte {
	root := util.ParseGJSONBytesNoCopy(rawJSON)

	outDoc := ojson.NewDocString(`{"id":"","type":"message","role":"assistant","model":"","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}`)
	_ = outDoc.Set("id", root.Get("id").String())
	_ = outDoc.Set("model", root.Get("model").String())

	if choices := root.Get("choices"); choices.Exists() && choices.IsArray() && len(choices.Array()) > 0 {
		choice := choices.Array()[0]
		var contentBlocks [][]byte

		for _, reasoningText := range collectOpenAIObjectReasoningTexts(choice.Get("message")) {
			if strings.TrimSpace(reasoningText) == "" {
				continue
			}
			blockDoc := ojson.NewDocString(`{"type":"thinking","thinking":""}`)
			_ = blockDoc.Set("thinking", reasoningText)
			contentBlocks = append(contentBlocks, blockDoc.Bytes())
		}

		hasToolCall := false
		appendOpenAINonStreamingMessageContent(choice.Get("message.content"), nil, &contentBlocks, &hasToolCall)

		if toolCalls := choice.Get("message.tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
			toolCalls.ForEach(func(_, toolCall ojson.Result) bool {
				hasToolCall = true
				toolUseDoc := ojson.NewDocString(`{"type":"tool_use","id":"","name":"","input":{}}`)
				_ = toolUseDoc.Set("id", util.SanitizeClaudeToolID(toolCall.Get("id").String()))
				_ = toolUseDoc.Set("name", toolCall.Get("function.name").String())

				argsStr := util.FixJSON(toolCall.Get("function.arguments").String())
				if argsStr != "" && ojson.Valid(argsStr) {
					argsJSON := ojson.Parse(argsStr)
					if argsJSON.IsObject() {
						_ = toolUseDoc.SetRaw("input", argsJSON.Raw)
					} else {
						_ = toolUseDoc.SetRaw("input", `{}`)
					}
				} else {
					_ = toolUseDoc.SetRaw("input", `{}`)
				}

				contentBlocks = append(contentBlocks, toolUseDoc.Bytes())
				return true
			})
		}

		if len(contentBlocks) > 0 {
			translatorcommon.SetRawArrayItemsDoc(outDoc, "content", contentBlocks)
		}

		if finishReason := choice.Get("finish_reason"); finishReason.Exists() || hasToolCall {
			_ = outDoc.Set("stop_reason", mapOpenAINonStreamFinishReasonToAnthropic(finishReason.String(), hasToolCall))
		}
	}

	if usage := root.Get("usage"); usage.Exists() {
		inputTokens, outputTokens, cachedTokens, cacheWriteTokens := extractOpenAIUsage(usage)
		_ = outDoc.Set("usage.input_tokens", inputTokens)
		_ = outDoc.Set("usage.output_tokens", outputTokens)
		if cachedTokens > 0 {
			_ = outDoc.Set("usage.cache_read_input_tokens", cachedTokens)
		}
		if cacheWriteTokens > 0 {
			_ = outDoc.Set("usage.cache_creation_input_tokens", cacheWriteTokens)
		}
	}

	return [][]byte{outDoc.Bytes()}
}

func mapOpenAIFinishReasonToAnthropic(openAIReason string) string {
	switch openAIReason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "content_filter":
		return "end_turn"
	case "function_call":
		return "tool_use"
	default:
		return "end_turn"
	}
}

func mapOpenAINonStreamFinishReasonToAnthropic(openAIReason string, hasToolCall bool) string {
	if hasToolCall && openAIReason != "length" && openAIReason != "content_filter" {
		return "tool_use"
	}
	return mapOpenAIFinishReasonToAnthropic(openAIReason)
}

func (p *ConvertOpenAIResponseToAnthropicParams) toolContentBlockIndex(openAIToolIndex int) int {
	if idx, ok := p.ToolCallBlockIndexes[openAIToolIndex]; ok {
		return idx
	}
	idx := p.NextContentBlockIndex
	p.NextContentBlockIndex++
	p.ToolCallBlockIndexes[openAIToolIndex] = idx
	return idx
}

func collectOpenAIObjectReasoningTexts(obj ojson.Result) []string {
	if !obj.Exists() {
		return nil
	}
	for _, path := range []string{"reasoning_content", "reasoning", "reasoning_details"} {
		texts := collectOpenAIReasoningTexts(obj.Get(path))
		if len(texts) > 0 {
			return texts
		}
	}
	return nil
}

func extractOpenAIContentPartThinkingText(part ojson.Result) string {
	for _, field := range []string{"thinking", "reasoning", "reasoning_content", "text"} {
		if texts := collectOpenAIReasoningTexts(part.Get(field)); len(texts) > 0 {
			return strings.Join(texts, "")
		}
	}
	return ""
}

func emitStreamingThinkingDelta(param *ConvertOpenAIResponseToAnthropicParams, reasoningText string, results *[][]byte) {
	if reasoningText == "" {
		return
	}

	if param.OpenToolCallIndex != -1 {
		if n := len(param.InterleavedContentChunks); n > 0 && param.InterleavedContentChunks[n-1].Type == "thinking" {
			param.InterleavedContentChunks[n-1].Text += reasoningText
		} else if strings.TrimSpace(reasoningText) != "" {
			param.InterleavedContentChunks = append(param.InterleavedContentChunks, InterleavedContentChunk{
				Type: "thinking",
				Text: strings.Clone(reasoningText),
			})
		}
		return
	}
	if !param.ThinkingContentBlockStarted && strings.TrimSpace(reasoningText) == "" {
		return
	}
	stopTextContentBlock(param, results)
	if !param.ThinkingContentBlockStarted {
		if param.ThinkingContentBlockIndex == -1 {
			param.ThinkingContentBlockIndex = param.NextContentBlockIndex
			param.NextContentBlockIndex++
		}
		contentBlockStartDoc := ojson.NewDocString(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)
		_ = contentBlockStartDoc.Set("index", param.ThinkingContentBlockIndex)
		*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "content_block_start", contentBlockStartDoc.Bytes(), 2))
		param.ThinkingContentBlockStarted = true
	}

	thinkingDeltaDoc := ojson.NewDocString(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":""}}`)
	_ = thinkingDeltaDoc.Set("index", param.ThinkingContentBlockIndex)
	_ = thinkingDeltaDoc.Set("delta.thinking", reasoningText)
	*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "content_block_delta", thinkingDeltaDoc.Bytes(), 2))
}

func emitStreamingTextDelta(param *ConvertOpenAIResponseToAnthropicParams, text string, results *[][]byte) {
	if text == "" {
		return
	}
	if param.OpenToolCallIndex != -1 {

		if n := len(param.InterleavedContentChunks); n > 0 && param.InterleavedContentChunks[n-1].Type == "text" {
			param.InterleavedContentChunks[n-1].Text += text
		} else {
			param.InterleavedContentChunks = append(param.InterleavedContentChunks, InterleavedContentChunk{
				Type: "text",
				Text: strings.Clone(text),
			})
		}
		param.ContentAccumulator.WriteString(text)
		return
	}

	if !param.TextContentBlockStarted {
		stopThinkingContentBlock(param, results)
		if param.TextContentBlockIndex == -1 {
			param.TextContentBlockIndex = param.NextContentBlockIndex
			param.NextContentBlockIndex++
		}
		contentBlockStartDoc := ojson.NewDocString(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		_ = contentBlockStartDoc.Set("index", param.TextContentBlockIndex)
		*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "content_block_start", contentBlockStartDoc.Bytes(), 2))
		param.TextContentBlockStarted = true
	}

	contentDeltaDoc := ojson.NewDocString(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":""}}`)
	_ = contentDeltaDoc.Set("index", param.TextContentBlockIndex)
	_ = contentDeltaDoc.Set("delta.text", text)
	*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "content_block_delta", contentDeltaDoc.Bytes(), 2))

	param.ContentAccumulator.WriteString(text)
}

func appendOpenAINonStreamingMessageContent(contentResult ojson.Result, toolNameMap map[string]string, blocks *[][]byte, hasToolCall *bool) {
	if !contentResult.Exists() {
		return
	}
	if contentResult.IsArray() {
		var textBuilder strings.Builder
		var thinkingBuilder strings.Builder

		flushText := func() {
			if textBuilder.Len() == 0 {
				return
			}
			blockDoc := ojson.NewDocString(`{"type":"text","text":""}`)
			_ = blockDoc.Set("text", textBuilder.String())
			*blocks = append(*blocks, blockDoc.Bytes())
			textBuilder.Reset()
		}

		flushThinking := func() {
			if thinkingBuilder.Len() == 0 {
				return
			}
			blockDoc := ojson.NewDocString(`{"type":"thinking","thinking":""}`)
			_ = blockDoc.Set("thinking", thinkingBuilder.String())
			*blocks = append(*blocks, blockDoc.Bytes())
			thinkingBuilder.Reset()
		}

		for _, item := range contentResult.Array() {
			switch item.Get("type").String() {
			case "text":
				if text := item.Get("text").String(); text != "" {
					flushThinking()
					textBuilder.WriteString(text)
				}
			case "tool_calls":
				flushThinking()
				flushText()
				toolCalls := item.Get("tool_calls")
				if toolCalls.IsArray() {
					toolCalls.ForEach(func(_, tc ojson.Result) bool {
						*hasToolCall = true
						toolUseDoc := ojson.NewDocString(`{"type":"tool_use","id":"","name":"","input":{}}`)
						_ = toolUseDoc.Set("id", util.SanitizeClaudeToolID(tc.Get("id").String()))
						_ = toolUseDoc.Set("name", util.MapToolName(toolNameMap, tc.Get("function.name").String()))

						argsStr := util.FixJSON(tc.Get("function.arguments").String())
						if argsStr != "" && ojson.Valid(argsStr) {
							argsJSON := ojson.Parse(argsStr)
							if argsJSON.IsObject() {
								_ = toolUseDoc.SetRaw("input", argsJSON.Raw)
							} else {
								_ = toolUseDoc.SetRaw("input", `{}`)
							}
						} else {
							_ = toolUseDoc.SetRaw("input", `{}`)
						}

						*blocks = append(*blocks, toolUseDoc.Bytes())
						return true
					})
				}
			case "reasoning", "thinking":
				if thinkingText := extractOpenAIContentPartThinkingText(item); thinkingBuilder.Len() > 0 || strings.TrimSpace(thinkingText) != "" {
					flushText()
					thinkingBuilder.WriteString(thinkingText)
				}
			default:
				flushThinking()
				flushText()
			}
		}

		flushThinking()
		flushText()
	} else if contentResult.Type == ojson.String {
		textContent := contentResult.String()
		if textContent != "" {
			blockDoc := ojson.NewDocString(`{"type":"text","text":""}`)
			_ = blockDoc.Set("text", textContent)
			*blocks = append(*blocks, blockDoc.Bytes())
		}
	}
}

func collectOpenAIReasoningTexts(node ojson.Result) []string {
	var texts []string
	if !node.Exists() {
		return texts
	}

	if node.IsArray() {
		node.ForEach(func(_, value ojson.Result) bool {
			texts = append(texts, collectOpenAIReasoningTexts(value)...)
			return true
		})
		return texts
	}

	switch node.Type {
	case ojson.String:
		if text := node.String(); text != "" {
			texts = append(texts, text)
		}
	case ojson.JSON:
		if text := node.Get("text"); text.Exists() {
			if textStr := text.String(); textStr != "" {
				texts = append(texts, textStr)
			}
		} else if raw := node.Raw; raw != "" && !strings.HasPrefix(raw, "{") && !strings.HasPrefix(raw, "[") {
			texts = append(texts, raw)
		}
	}

	return texts
}

func stopThinkingContentBlock(param *ConvertOpenAIResponseToAnthropicParams, results *[][]byte) {
	if !param.ThinkingContentBlockStarted {
		return
	}
	contentBlockStopJSON := []byte(`{"type":"content_block_stop","index":0}`)
	contentBlockStopJSON, _ = ojson.SetBytes(contentBlockStopJSON, "index", param.ThinkingContentBlockIndex)
	*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "content_block_stop", contentBlockStopJSON, 2))
	param.ThinkingContentBlockStarted = false
	param.ThinkingContentBlockIndex = -1
}

func emitMessageStopIfNeeded(param *ConvertOpenAIResponseToAnthropicParams, results *[][]byte) {
	if param.MessageStopSent {
		return
	}
	*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "message_stop", []byte(`{"type":"message_stop"}`), 2))
	param.MessageStopSent = true
}

func stopTextContentBlock(param *ConvertOpenAIResponseToAnthropicParams, results *[][]byte) {
	if !param.TextContentBlockStarted {
		return
	}
	contentBlockStopJSON := []byte(`{"type":"content_block_stop","index":0}`)
	contentBlockStopJSON, _ = ojson.SetBytes(contentBlockStopJSON, "index", param.TextContentBlockIndex)
	*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "content_block_stop", contentBlockStopJSON, 2))
	param.TextContentBlockStarted = false
	param.TextContentBlockIndex = -1
}

func emitToolUseStart(param *ConvertOpenAIResponseToAnthropicParams, openAIToolIndex int, accumulator *ToolCallAccumulator, results *[][]byte) {
	stopThinkingContentBlock(param, results)
	stopTextContentBlock(param, results)

	blockIndex := param.toolContentBlockIndex(openAIToolIndex)
	contentBlockStartDoc := ojson.NewDocString(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"","name":"","input":{}}}`)
	_ = contentBlockStartDoc.Set("index", blockIndex)
	_ = contentBlockStartDoc.Set("content_block.id", util.SanitizeClaudeToolID(accumulator.ID))
	_ = contentBlockStartDoc.Set("content_block.name", accumulator.Name)
	*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "content_block_start", contentBlockStartDoc.Bytes(), 2))
	accumulator.StartEmitted = true
	param.SawToolCall = true
	param.OpenToolCallIndex = openAIToolIndex
}

func emitBelatedToolUseStart(param *ConvertOpenAIResponseToAnthropicParams, openAIToolIndex int, accumulator *ToolCallAccumulator, results *[][]byte) bool {
	if accumulator == nil {
		return false
	}
	if accumulator.StartEmitted {
		return true
	}
	if accumulator.Name == "" && accumulator.ID == "" && accumulator.Arguments.Len() == 0 {
		return false
	}
	if accumulator.Name == "" {
		accumulator.Name = fmt.Sprintf("tool_%d", openAIToolIndex)
	}
	emitToolUseStart(param, openAIToolIndex, accumulator, results)
	return true
}

func finalizeSingleToolCall(param *ConvertOpenAIResponseToAnthropicParams, openAIToolIndex int, results *[][]byte) {
	accumulator := param.ToolCallsAccumulator[openAIToolIndex]
	if accumulator == nil {
		return
	}
	if !accumulator.StartEmitted {
		if !emitBelatedToolUseStart(param, openAIToolIndex, accumulator, results) {
			return
		}
	}
	blockIndex := param.toolContentBlockIndex(openAIToolIndex)

	if accumulator.Arguments.Len() > 0 {
		inputDeltaDoc := ojson.NewDocString(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`)
		_ = inputDeltaDoc.Set("index", blockIndex)
		_ = inputDeltaDoc.Set("delta.partial_json", util.FixJSON(accumulator.Arguments.String()))
		*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "content_block_delta", inputDeltaDoc.Bytes(), 2))
	}

	contentBlockStopJSON := []byte(`{"type":"content_block_stop","index":0}`)
	contentBlockStopJSON, _ = ojson.SetBytes(contentBlockStopJSON, "index", blockIndex)
	*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "content_block_stop", contentBlockStopJSON, 2))
	delete(param.ToolCallBlockIndexes, openAIToolIndex)
	param.OpenToolCallIndex = -1
}

func emitBufferedInterleavedContent(param *ConvertOpenAIResponseToAnthropicParams, results *[][]byte) {
	if param == nil || len(param.InterleavedContentChunks) == 0 {
		return
	}
	for _, chunk := range param.InterleavedContentChunks {
		if chunk.Text == "" {
			continue
		}
		idx := param.NextContentBlockIndex
		param.NextContentBlockIndex++

		switch chunk.Type {
		case "thinking":
			startJSON := []byte(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)
			startJSON, _ = ojson.SetBytes(startJSON, "index", idx)
			*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "content_block_start", startJSON, 2))

			deltaDoc := ojson.NewDocString(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":""}}`)
			_ = deltaDoc.Set("index", idx)
			_ = deltaDoc.Set("delta.thinking", chunk.Text)
			*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "content_block_delta", deltaDoc.Bytes(), 2))

			stopJSON := []byte(`{"type":"content_block_stop","index":0}`)
			stopJSON, _ = ojson.SetBytes(stopJSON, "index", idx)
			*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "content_block_stop", stopJSON, 2))
		case "text":
			startJSON := []byte(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
			startJSON, _ = ojson.SetBytes(startJSON, "index", idx)
			*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "content_block_start", startJSON, 2))

			deltaDoc := ojson.NewDocString(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":""}}`)
			_ = deltaDoc.Set("index", idx)
			_ = deltaDoc.Set("delta.text", chunk.Text)
			*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "content_block_delta", deltaDoc.Bytes(), 2))

			stopJSON := []byte(`{"type":"content_block_stop","index":0}`)
			stopJSON, _ = ojson.SetBytes(stopJSON, "index", idx)
			*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "content_block_stop", stopJSON, 2))
		}
	}
	param.InterleavedContentChunks = nil
}

func finalizeOpenAIAnthropicContentBlocks(param *ConvertOpenAIResponseToAnthropicParams, results *[][]byte) {
	if param == nil {
		return
	}
	stopThinkingContentBlock(param, results)
	stopTextContentBlock(param, results)

	if !param.ContentBlocksStopped {
		if param.OpenToolCallIndex != -1 {
			finalizeSingleToolCall(param, param.OpenToolCallIndex, results)
		}

		for _, index := range toolCallAccumulatorIndexes(param.ToolCallsAccumulator) {
			accumulator := param.ToolCallsAccumulator[index]
			if accumulator == nil || accumulator.StartEmitted {
				continue
			}
			finalizeSingleToolCall(param, index, results)
		}
		param.ContentBlocksStopped = true

		emitBufferedInterleavedContent(param, results)
	}
}

func emitAnthropicMessageDelta(param *ConvertOpenAIResponseToAnthropicParams, results *[][]byte, inputTokens, outputTokens, cachedTokens, cacheWriteTokens int64) {
	if param == nil || param.MessageDeltaSent {
		return
	}
	messageDeltaDoc := ojson.NewDocString(`{"type":"message_delta","delta":{"stop_reason":"","stop_sequence":null},"usage":{"input_tokens":0,"output_tokens":0}}`)
	_ = messageDeltaDoc.Set("delta.stop_reason", mapOpenAIFinishReasonToAnthropic(terminalOpenAIFinishReason(param)))
	_ = messageDeltaDoc.Set("usage.input_tokens", inputTokens)
	_ = messageDeltaDoc.Set("usage.output_tokens", outputTokens)
	if cachedTokens > 0 {
		_ = messageDeltaDoc.Set("usage.cache_read_input_tokens", cachedTokens)
	}
	if cacheWriteTokens > 0 {
		_ = messageDeltaDoc.Set("usage.cache_creation_input_tokens", cacheWriteTokens)
	}
	*results = append(*results, translatorcommon.AppendSSEEventBytes(nil, "message_delta", messageDeltaDoc.Bytes(), 2))
	param.MessageDeltaSent = true
}

func toolCallAccumulatorIndexes(accumulators map[int]*ToolCallAccumulator) []int {
	indexes := make([]int, 0, len(accumulators))
	for index := range accumulators {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	return indexes
}

func ConvertOpenAIResponseToClaudeNonStream(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	_ = requestRawJSON

	root := util.ParseGJSONBytesNoCopy(rawJSON)
	toolNameMap := util.ToolNameMapFromClaudeRequest(originalRequestRawJSON)
	outDoc := ojson.NewDocString(`{"id":"","type":"message","role":"assistant","model":"","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}`)
	_ = outDoc.Set("id", root.Get("id").String())
	_ = outDoc.Set("model", root.Get("model").String())

	hasToolCall := false
	finishReason := ""
	var blocks [][]byte

	if choices := root.Get("choices"); choices.Exists() && choices.IsArray() && len(choices.Array()) > 0 {
		choice := choices.Array()[0]
		finishReason = choice.Get("finish_reason").String()

		if message := choice.Get("message"); message.Exists() {
			for _, reasoningText := range collectOpenAIObjectReasoningTexts(message) {
				if strings.TrimSpace(reasoningText) == "" {
					continue
				}
				blockDoc := ojson.NewDocString(`{"type":"thinking","thinking":""}`)
				_ = blockDoc.Set("thinking", reasoningText)
				blocks = append(blocks, blockDoc.Bytes())
			}

			appendOpenAINonStreamingMessageContent(message.Get("content"), toolNameMap, &blocks, &hasToolCall)

			if toolCalls := message.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				toolCalls.ForEach(func(_, toolCall ojson.Result) bool {
					hasToolCall = true
					toolUseDoc := ojson.NewDocString(`{"type":"tool_use","id":"","name":"","input":{}}`)
					_ = toolUseDoc.Set("id", util.SanitizeClaudeToolID(toolCall.Get("id").String()))
					_ = toolUseDoc.Set("name", util.MapToolName(toolNameMap, toolCall.Get("function.name").String()))

					argsStr := util.FixJSON(toolCall.Get("function.arguments").String())
					if argsStr != "" && ojson.Valid(argsStr) {
						argsJSON := ojson.Parse(argsStr)
						if argsJSON.IsObject() {
							_ = toolUseDoc.SetRaw("input", argsJSON.Raw)
						} else {
							_ = toolUseDoc.SetRaw("input", `{}`)
						}
					} else {
						_ = toolUseDoc.SetRaw("input", `{}`)
					}

					blocks = append(blocks, toolUseDoc.Bytes())
					return true
				})
			}
		}
	}

	if len(blocks) > 0 {
		_ = outDoc.SetRawBytes("content", translatorcommon.JoinRawArray(blocks))
	}

	if respUsage := root.Get("usage"); respUsage.Exists() {
		inputTokens, outputTokens, cachedTokens, cacheWriteTokens := extractOpenAIUsage(respUsage)
		_ = outDoc.Set("usage.input_tokens", inputTokens)
		_ = outDoc.Set("usage.output_tokens", outputTokens)
		if cachedTokens > 0 {
			_ = outDoc.Set("usage.cache_read_input_tokens", cachedTokens)
		}
		if cacheWriteTokens > 0 {
			_ = outDoc.Set("usage.cache_creation_input_tokens", cacheWriteTokens)
		}
	}

	_ = outDoc.Set("stop_reason", mapOpenAINonStreamFinishReasonToAnthropic(finishReason, hasToolCall))

	return outDoc.Bytes()
}

func ClaudeTokenCount(ctx context.Context, count int64) []byte {
	return translatorcommon.ClaudeInputTokensJSON(count)
}

func extractOpenAIUsage(usage ojson.Result) (int64, int64, int64, int64) {
	if !usage.Exists() || usage.Type == ojson.Null {
		return 0, 0, 0, 0
	}

	inputTokens := usage.Get("prompt_tokens").Int()
	outputTokens := usage.Get("completion_tokens").Int()
	cachedTokens := usage.Get("prompt_tokens_details.cached_tokens").Int()
	cacheWriteTokens := usage.Get("prompt_tokens_details.cache_write_tokens").Int()
	if cacheWriteTokens <= 0 {
		cacheWriteTokens = usage.Get("prompt_tokens_details.cache_creation_tokens").Int()
	}

	deductTokens := int64(0)
	if cachedTokens > 0 {
		deductTokens += cachedTokens
	}
	if cacheWriteTokens > 0 {
		if math.MaxInt64-deductTokens < cacheWriteTokens {
			deductTokens = math.MaxInt64
		} else {
			deductTokens += cacheWriteTokens
		}
	}

	if deductTokens > 0 {
		if inputTokens >= deductTokens {
			inputTokens -= deductTokens
		} else {
			inputTokens = 0
		}
	}
	if inputTokens < 0 {
		inputTokens = 0
	}

	return inputTokens, outputTokens, cachedTokens, cacheWriteTokens
}
