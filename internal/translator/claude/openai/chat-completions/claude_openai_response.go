package chat_completions

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
	translatorcommon "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/common"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
)

var (
	dataTag = []byte("data:")
)

type ConvertAnthropicResponseToOpenAIParams struct {
	CreatedAt         int64
	ResponseID        string
	Model             string
	Fallbacks         translatorcommon.ClaudeFallbackChain
	FinishReason      string
	Usage             claudeUsageTokens
	TrailingUsageSent bool

	ToolCallsAccumulator map[int]*ToolCallAccumulator
	NextToolCallIndex    int
}

type claudeUsageTokens struct {
	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64
	ThinkingTokens           int64
	HasThinkingTokens        bool
	HasUsage                 bool
}

type ToolCallAccumulator struct {
	ID        string
	Name      string
	Index     int
	Arguments strings.Builder
}

func (u *claudeUsageTokens) Merge(usage ojson.Result) {
	if !usage.Exists() {
		return
	}
	u.HasUsage = true
	if inputTokens := usage.Get("input_tokens"); inputTokens.Exists() {
		u.InputTokens = inputTokens.Int()
	}
	if outputTokens := usage.Get("output_tokens"); outputTokens.Exists() {
		u.OutputTokens = outputTokens.Int()
	}
	if cacheCreationInputTokens := usage.Get("cache_creation_input_tokens"); cacheCreationInputTokens.Exists() {
		u.CacheCreationInputTokens = cacheCreationInputTokens.Int()
	}
	if cacheReadInputTokens := usage.Get("cache_read_input_tokens"); cacheReadInputTokens.Exists() {
		u.CacheReadInputTokens = cacheReadInputTokens.Int()
	}
	if thinkingTokens := usage.Get("output_tokens_details.thinking_tokens"); thinkingTokens.Exists() {
		u.ThinkingTokens = thinkingTokens.Int()
		u.HasThinkingTokens = true
	}
}

func (u claudeUsageTokens) applyReasoningTokensDoc(outDoc *ojson.Doc) {
	if !u.HasThinkingTokens {
		return
	}
	_ = outDoc.Set("usage.completion_tokens_details.reasoning_tokens", u.ThinkingTokens)
}

func (u claudeUsageTokens) OpenAIUsage() (promptTokens, completionTokens, totalTokens, cachedTokens, cachedCreationTokens int64) {
	cachedTokens = u.CacheReadInputTokens
	cachedCreationTokens = u.CacheCreationInputTokens
	promptTokens = u.InputTokens + cachedCreationTokens + cachedTokens
	completionTokens = u.OutputTokens
	totalTokens = promptTokens + completionTokens
	return promptTokens, completionTokens, totalTokens, cachedTokens, cachedCreationTokens
}

func effectiveChatResponseModel(servedModel, requestedModel string) string {
	if servedModel != "" {
		return servedModel
	}
	return requestedModel
}

func ConvertClaudeResponseToOpenAI(_ context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	if *param == nil {
		*param = &ConvertAnthropicResponseToOpenAIParams{
			CreatedAt:    0,
			ResponseID:   "",
			FinishReason: "",
		}
	}

	if !bytes.HasPrefix(rawJSON, dataTag) {
		return [][]byte{}
	}
	rawJSON = bytes.TrimSpace(rawJSON[5:])

	root := util.ParseGJSONBytesNoCopy(rawJSON)
	eventType := root.Get("type").String()

	templateDoc := ojson.NewDoc([]byte(`{"id":"","object":"chat.completion.chunk","created":0,"model":"","choices":[{"index":0,"delta":{},"finish_reason":null}]}`))

	p := (*param).(*ConvertAnthropicResponseToOpenAIParams)

	if chunkModel := effectiveChatResponseModel(p.Model, modelName); chunkModel != "" {
		_ = templateDoc.Set("model", chunkModel)
	}

	if p.ResponseID != "" {
		_ = templateDoc.Set("id", p.ResponseID)
	}
	if p.CreatedAt > 0 {
		_ = templateDoc.Set("created", p.CreatedAt)
	}

	switch eventType {
	case "message_start":

		if message := root.Get("message"); message.Exists() {
			p.ResponseID = strings.Clone(message.Get("id").String())
			p.CreatedAt = time.Now().Unix()
			p.Fallbacks.Reset()
			upstreamModel := strings.TrimSpace(message.Get("model").String())
			if upstreamModel != "" {
				p.Model = strings.Clone(upstreamModel)
			}

			_ = templateDoc.Set("id", p.ResponseID)
			if chunkModel := effectiveChatResponseModel(p.Model, modelName); chunkModel != "" {
				_ = templateDoc.Set("model", chunkModel)
			}
			_ = templateDoc.Set("created", p.CreatedAt)

			_ = templateDoc.Set("choices.0.delta.role", "assistant")
			if fromModel, toModel, ok := translatorcommon.ClaudePreTokenFallbackModels(originalRequestRawJSON, requestRawJSON, modelName, upstreamModel); ok {
				if fbBytes := p.Fallbacks.RecordPreToken(fromModel, toModel, "", nil); len(fbBytes) > 0 {
					_ = templateDoc.SetRawBytes("choices.0.delta.fallback", fbBytes)
				}
			}

			if p.ToolCallsAccumulator == nil {
				p.ToolCallsAccumulator = make(map[int]*ToolCallAccumulator)
			}
			p.NextToolCallIndex = 0
			p.Usage.Merge(message.Get("usage"))
		}
		return [][]byte{templateDoc.Bytes()}

	case "content_block_start":

		if contentBlock := root.Get("content_block"); contentBlock.Exists() {
			blockType := contentBlock.Get("type").String()

			if blockType == "fallback" {
				toModel := strings.TrimSpace(contentBlock.Get("to.model").String())
				fromModel := strings.TrimSpace(contentBlock.Get("from.model").String())
				if toModel != "" {
					p.Model = strings.Clone(toModel)
					_ = templateDoc.Set("model", p.Model)
				}
				fbBytes := p.Fallbacks.RecordBlock(
					contentBlock,
					effectiveChatResponseModel(fromModel, modelName),
					effectiveChatResponseModel(toModel, p.Model),
					"",
					nil,
				)
				if len(fbBytes) > 0 {
					_ = templateDoc.SetRawBytes("choices.0.delta.fallback", fbBytes)
					if allBytes := p.Fallbacks.ArrayBytes(); len(allBytes) > 0 {
						_ = templateDoc.SetRawBytes("choices.0.delta.fallbacks", allBytes)
					}
					return [][]byte{templateDoc.Bytes()}
				}
				if toModel != "" {
					return [][]byte{templateDoc.Bytes()}
				}
				return [][]byte{}
			}

			if blockType == "tool_use" {

				toolCallID := strings.Clone(contentBlock.Get("id").String())
				toolName := strings.Clone(contentBlock.Get("name").String())
				index := int(root.Get("index").Int())

				if (*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator == nil {
					(*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator = make(map[int]*ToolCallAccumulator)
				}

				toolCallIndex := (*param).(*ConvertAnthropicResponseToOpenAIParams).NextToolCallIndex
				(*param).(*ConvertAnthropicResponseToOpenAIParams).NextToolCallIndex++

				(*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator[index] = &ToolCallAccumulator{
					ID:    toolCallID,
					Name:  toolName,
					Index: toolCallIndex,
				}

				return [][]byte{}
			}
		}
		return [][]byte{}

	case "content_block_delta":

		hasContent := false
		if delta := root.Get("delta"); delta.Exists() {
			deltaType := delta.Get("type").String()

			switch deltaType {
			case "text_delta":

				if text := delta.Get("text"); text.Exists() {
					_ = templateDoc.Set("choices.0.delta.content", text.String())
					hasContent = true
				}
			case "thinking_delta":

				if thinking := delta.Get("thinking"); thinking.Exists() {
					_ = templateDoc.Set("choices.0.delta.reasoning_content", thinking.String())
					hasContent = true
				}
			case "input_json_delta":

				if partialJSON := delta.Get("partial_json"); partialJSON.Exists() {
					index := int(root.Get("index").Int())
					if (*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator != nil {
						if accumulator, exists := (*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator[index]; exists {
							accumulator.Arguments.WriteString(partialJSON.String())
						}
					}
				}

				return [][]byte{}
			}
		}
		if hasContent {
			return [][]byte{templateDoc.Bytes()}
		} else {
			return [][]byte{}
		}

	case "content_block_stop":

		index := int(root.Get("index").Int())
		if (*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator != nil {
			if accumulator, exists := (*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator[index]; exists {

				arguments := accumulator.Arguments.String()
				if arguments == "" {
					arguments = "{}"
				}
				_ = templateDoc.Set("choices.0.delta.tool_calls.0.index", accumulator.Index)
				_ = templateDoc.Set("choices.0.delta.tool_calls.0.id", accumulator.ID)
				_ = templateDoc.Set("choices.0.delta.tool_calls.0.type", "function")
				_ = templateDoc.Set("choices.0.delta.tool_calls.0.function.name", accumulator.Name)
				_ = templateDoc.Set("choices.0.delta.tool_calls.0.function.arguments", arguments)

				delete((*param).(*ConvertAnthropicResponseToOpenAIParams).ToolCallsAccumulator, index)

				return [][]byte{templateDoc.Bytes()}
			}
		}
		return [][]byte{}

	case "message_delta":

		if delta := root.Get("delta"); delta.Exists() {
			if stopReason := delta.Get("stop_reason"); stopReason.Exists() {
				(*param).(*ConvertAnthropicResponseToOpenAIParams).FinishReason = mapAnthropicStopReasonToOpenAI(stopReason.String())
				_ = templateDoc.Set("choices.0.finish_reason", (*param).(*ConvertAnthropicResponseToOpenAIParams).FinishReason)
			}
		}

		if usage := root.Get("usage"); usage.Exists() {
			if iterModel, newHops := p.Fallbacks.RecordIterations(usage.Get("iterations"), modelName, "", nil); iterModel != "" {
				p.Model = iterModel
				_ = templateDoc.Set("model", p.Model)
				if len(newHops) > 0 {
					_ = templateDoc.SetRawBytes("choices.0.delta.fallback", p.Fallbacks.Latest())
					if allBytes := p.Fallbacks.ArrayBytes(); len(allBytes) > 0 {
						_ = templateDoc.SetRawBytes("choices.0.delta.fallbacks", allBytes)
					}
				}
			}
			p.Usage.Merge(usage)
			promptTokens, completionTokens, totalTokens, cachedTokens, cachedCreationTokens := p.Usage.OpenAIUsage()
			_ = templateDoc.Set("usage.prompt_tokens", promptTokens)
			_ = templateDoc.Set("usage.completion_tokens", completionTokens)
			_ = templateDoc.Set("usage.total_tokens", totalTokens)
			_ = templateDoc.Set("usage.prompt_tokens_details.cached_tokens", cachedTokens)
			_ = templateDoc.Set("usage.prompt_tokens_details.cached_creation_tokens", cachedCreationTokens)
			_ = templateDoc.Set("usage.prompt_tokens_details.cache_write_tokens", cachedCreationTokens)
			p.Usage.applyReasoningTokensDoc(templateDoc)
		}
		return [][]byte{templateDoc.Bytes()}

	case "message_stop":

		if p.Usage.HasUsage && !p.TrailingUsageSent {
			p.TrailingUsageSent = true
			usageDoc := ojson.NewDoc([]byte(`{"id":"","object":"chat.completion.chunk","created":0,"model":"","choices":[]}`))
			if p.ResponseID != "" {
				_ = usageDoc.Set("id", p.ResponseID)
			}
			if chunkModel := effectiveChatResponseModel(p.Model, modelName); chunkModel != "" {
				_ = usageDoc.Set("model", chunkModel)
			}
			if p.CreatedAt > 0 {
				_ = usageDoc.Set("created", p.CreatedAt)
			}
			promptTokens, completionTokens, totalTokens, cachedTokens, cachedCreationTokens := p.Usage.OpenAIUsage()
			_ = usageDoc.Set("usage.prompt_tokens", promptTokens)
			_ = usageDoc.Set("usage.completion_tokens", completionTokens)
			_ = usageDoc.Set("usage.total_tokens", totalTokens)
			_ = usageDoc.Set("usage.prompt_tokens_details.cached_tokens", cachedTokens)
			_ = usageDoc.Set("usage.prompt_tokens_details.cached_creation_tokens", cachedCreationTokens)
			_ = usageDoc.Set("usage.prompt_tokens_details.cache_write_tokens", cachedCreationTokens)
			p.Usage.applyReasoningTokensDoc(usageDoc)
			return [][]byte{usageDoc.Bytes()}
		}
		return [][]byte{}

	case "ping":

		return [][]byte{}

	case "error":

		if errorData := root.Get("error"); errorData.Exists() {
			errorDoc := ojson.NewDoc([]byte(`{"error":{"message":"","type":""}}`))
			_ = errorDoc.Set("error.message", errorData.Get("message").String())
			_ = errorDoc.Set("error.type", errorData.Get("type").String())
			return [][]byte{errorDoc.Bytes()}
		}
		return [][]byte{}

	default:

		return [][]byte{}
	}
}

func mapAnthropicStopReasonToOpenAI(anthropicReason string) string {
	switch anthropicReason {
	case "end_turn":
		return "stop"
	case "tool_use":
		return "tool_calls"
	case "max_tokens", "model_context_window_exceeded":
		return "length"
	case "stop_sequence":
		return "stop"
	case "refusal", "sensitive":
		return "content_filter"
	default:
		return "stop"
	}
}

func ConvertClaudeResponseToOpenAINonStream(_ context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	rawJSON, _ = translatorcommon.ClaudeMessagesJSONToSSE(rawJSON)
	chunks := make([][]byte, 0)

	lines := bytes.Split(rawJSON, []byte("\n"))
	for _, line := range lines {
		if !bytes.HasPrefix(line, dataTag) {
			continue
		}
		chunks = append(chunks, bytes.TrimSpace(line[5:]))
	}

	outDoc := ojson.NewDoc([]byte(`{"id":"","object":"chat.completion","created":0,"model":"","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`))

	var messageID string
	var model string
	var createdAt int64
	var stopReason string
	var contentParts []string
	var reasoningParts []string
	var fallbacks translatorcommon.ClaudeFallbackChain
	currentContentLen := 0
	usageTokens := claudeUsageTokens{}
	toolCallsAccumulator := make(map[int]*ToolCallAccumulator)

	for _, chunk := range chunks {
		root := util.ParseGJSONBytesNoCopy(chunk)
		eventType := root.Get("type").String()

		switch eventType {
		case "message_start":

			if message := root.Get("message"); message.Exists() {
				messageID = message.Get("id").String()
				if m := strings.TrimSpace(message.Get("model").String()); m != "" {
					model = m
				}
				if fromModel, toModel, ok := translatorcommon.ClaudePreTokenFallbackModels(originalRequestRawJSON, requestRawJSON, modelName, model); ok {
					offset := 0
					fallbacks.RecordPreToken(fromModel, toModel, "content_offset", &offset)
				}
				createdAt = time.Now().Unix()
				usageTokens.Merge(message.Get("usage"))
			}

		case "content_block_start":

			if contentBlock := root.Get("content_block"); contentBlock.Exists() {
				blockType := contentBlock.Get("type").String()
				if blockType == "thinking" {

					continue
				} else if blockType == "fallback" {
					toModel := strings.TrimSpace(contentBlock.Get("to.model").String())
					fromModel := strings.TrimSpace(contentBlock.Get("from.model").String())
					if toModel != "" {
						model = toModel
					}
					offset := currentContentLen
					fallbacks.RecordBlock(
						contentBlock,
						effectiveChatResponseModel(fromModel, modelName),
						effectiveChatResponseModel(toModel, model),
						"content_offset",
						&offset,
					)
				} else if blockType == "tool_use" {

					index := int(root.Get("index").Int())
					toolCallsAccumulator[index] = &ToolCallAccumulator{
						ID:   contentBlock.Get("id").String(),
						Name: contentBlock.Get("name").String(),
					}
				}
			}

		case "content_block_delta":

			if delta := root.Get("delta"); delta.Exists() {
				deltaType := delta.Get("type").String()
				switch deltaType {
				case "text_delta":

					if text := delta.Get("text"); text.Exists() {
						textStr := text.String()
						contentParts = append(contentParts, textStr)
						currentContentLen += len(textStr)
					}
				case "thinking_delta":

					if thinking := delta.Get("thinking"); thinking.Exists() {
						reasoningParts = append(reasoningParts, thinking.String())
					}
				case "input_json_delta":

					if partialJSON := delta.Get("partial_json"); partialJSON.Exists() {
						index := int(root.Get("index").Int())
						if accumulator, exists := toolCallsAccumulator[index]; exists {
							accumulator.Arguments.WriteString(partialJSON.String())
						}
					}
				}
			}

		case "content_block_stop":

			index := int(root.Get("index").Int())
			if accumulator, exists := toolCallsAccumulator[index]; exists {
				if accumulator.Arguments.Len() == 0 {
					accumulator.Arguments.WriteString("{}")
				}
			}

		case "message_delta":

			if delta := root.Get("delta"); delta.Exists() {
				if sr := delta.Get("stop_reason"); sr.Exists() {
					stopReason = sr.String()
				}
			}
			if usage := root.Get("usage"); usage.Exists() {
				offset := 0
				if iterModel, _ := fallbacks.RecordIterations(usage.Get("iterations"), modelName, "content_offset", &offset); iterModel != "" {
					model = iterModel
				}
				usageTokens.Merge(usage)
			}
		}
	}

	if usageTokens.HasUsage {
		promptTokens, completionTokens, totalTokens, cachedTokens, cachedCreationTokens := usageTokens.OpenAIUsage()
		_ = outDoc.Set("usage.prompt_tokens", promptTokens)
		_ = outDoc.Set("usage.completion_tokens", completionTokens)
		_ = outDoc.Set("usage.total_tokens", totalTokens)
		_ = outDoc.Set("usage.prompt_tokens_details.cached_tokens", cachedTokens)
		_ = outDoc.Set("usage.prompt_tokens_details.cached_creation_tokens", cachedCreationTokens)
		_ = outDoc.Set("usage.prompt_tokens_details.cache_write_tokens", cachedCreationTokens)
		usageTokens.applyReasoningTokensDoc(outDoc)
	}

	_ = outDoc.Set("id", messageID)
	_ = outDoc.Set("created", createdAt)
	_ = outDoc.Set("model", effectiveChatResponseModel(model, modelName))

	messageContent := strings.Join(contentParts, "")
	_ = outDoc.Set("choices.0.message.content", messageContent)
	if latestFB := fallbacks.Latest(); len(latestFB) > 0 {
		_ = outDoc.SetRawBytes("choices.0.message.fallback", latestFB)
		_ = outDoc.SetRawBytes("choices.0.message.fallbacks", fallbacks.ArrayBytes())
	}

	if len(reasoningParts) > 0 {
		reasoningContent := strings.Join(reasoningParts, "")

		_ = outDoc.Set("choices.0.message.reasoning_content", reasoningContent)
	}

	if len(toolCallsAccumulator) > 0 {
		toolCallsCount := 0
		maxIndex := -1
		for index := range toolCallsAccumulator {
			if index > maxIndex {
				maxIndex = index
			}
		}

		for i := 0; i <= maxIndex; i++ {
			accumulator, exists := toolCallsAccumulator[i]
			if !exists {
				continue
			}

			arguments := accumulator.Arguments.String()

			idPath := fmt.Sprintf("choices.0.message.tool_calls.%d.id", toolCallsCount)
			typePath := fmt.Sprintf("choices.0.message.tool_calls.%d.type", toolCallsCount)
			namePath := fmt.Sprintf("choices.0.message.tool_calls.%d.function.name", toolCallsCount)
			argumentsPath := fmt.Sprintf("choices.0.message.tool_calls.%d.function.arguments", toolCallsCount)

			_ = outDoc.Set(idPath, accumulator.ID)
			_ = outDoc.Set(typePath, "function")
			_ = outDoc.Set(namePath, accumulator.Name)
			_ = outDoc.Set(argumentsPath, arguments)
			toolCallsCount++
		}
		if finishReason := mapAnthropicStopReasonToOpenAI(stopReason); toolCallsCount > 0 && finishReason != "length" {
			_ = outDoc.Set("choices.0.finish_reason", "tool_calls")
		} else if finishReason != "stop" {
			_ = outDoc.Set("choices.0.finish_reason", finishReason)
		}
	} else if finishReason := mapAnthropicStopReasonToOpenAI(stopReason); finishReason != "stop" {
		_ = outDoc.Set("choices.0.finish_reason", finishReason)
	}

	return outDoc.Bytes()
}
