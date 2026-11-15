





package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	codexmodels "github.com/router-for-me/CLIProxyAPI/v8/internal/client/codex/models"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/clienterror"
	. "github.com/router-for-me/CLIProxyAPI/v8/internal/constant"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	responsesconverter "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/openai/openai/responses"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
)



type OpenAIAPIHandler struct {
	*handlers.BaseAPIHandler
}









func NewOpenAIAPIHandler(apiHandlers *handlers.BaseAPIHandler) *OpenAIAPIHandler {
	return &OpenAIAPIHandler{
		BaseAPIHandler: apiHandlers,
	}
}


func (h *OpenAIAPIHandler) HandlerType() string {
	return OpenAI
}


func (h *OpenAIAPIHandler) Models() []map[string]any {

	modelRegistry := registry.GetGlobalRegistry()
	return modelRegistry.GetAvailableModels("openai")
}




func (h *OpenAIAPIHandler) OpenAIModels(c *gin.Context) {
	if _, ok := c.Request.URL.Query()["client_version"]; ok {
		clientVersion := c.Query("client_version")
		body, errMarshal := codexmodels.MarshalCompact(h.codexClientModelsResponse(clientVersion))
		if errMarshal != nil {
			c.JSON(http.StatusInternalServerError, handlers.ErrorResponse{
				Error: handlers.ErrorDetail{
					Message: fmt.Sprintf("Failed to encode model list: %v", errMarshal),
					Type:    "server_error",
				},
			})
			return
		}
		h.WriteModelListResponse(c, h.HandlerType(), body)
		return
	}


	allModels := h.Models()


	filteredModels := make([]map[string]any, len(allModels))
	for i, model := range allModels {
		filteredModel := map[string]any{
			"id":     model["id"],
			"object": model["object"],
		}


		if created, exists := model["created"]; exists {
			filteredModel["created"] = created
		}


		if ownedBy, exists := model["owned_by"]; exists {
			filteredModel["owned_by"] = ownedBy
		}

		filteredModels[i] = filteredModel
	}

	h.WriteModelListResponse(c, h.HandlerType(), gin.H{
		"object": "list",
		"data":   filteredModels,
	})
}







func (h *OpenAIAPIHandler) ChatCompletions(c *gin.Context) {
	rawJSON, err := handlers.ReadRequestBody(c)

	if err != nil {
		c.JSON(clienterror.HTTPStatusFromErrorOr(err, http.StatusBadRequest), handlers.ErrorResponse{
			Error: handlers.ErrorDetail{
				Message: fmt.Sprintf("Invalid request: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}


	streamResult := ojson.GetBytesNoCopy(rawJSON, "stream")
	stream := streamResult.Type == ojson.True



	if shouldTreatAsResponsesFormat(rawJSON) {
		modelName := strings.Clone(ojson.GetBytesNoCopy(rawJSON, "model").String())
		rawJSON, _ = responsesconverter.ConvertOpenAIResponsesRequestToOpenAIChatCompletions(modelName, rawJSON, stream)
		stream = ojson.GetBytesNoCopy(rawJSON, "stream").Bool()
	}

	if stream {
		h.handleStreamingResponse(c, rawJSON)
	} else {
		h.handleNonStreamingResponse(c, rawJSON)
	}

}



func shouldTreatAsResponsesFormat(rawJSON []byte) bool {
	var fieldsBuf [3]ojson.Result
	fields := ojson.GetManyBytesNoCopy(fieldsBuf[:0], rawJSON, "messages", "input", "instructions")
	if fields[0].Exists() {
		return false
	}
	return fields[1].Exists() || fields[2].Exists()
}








func (h *OpenAIAPIHandler) Completions(c *gin.Context) {
	rawJSON, err := handlers.ReadRequestBody(c)

	if err != nil {
		c.JSON(clienterror.HTTPStatusFromErrorOr(err, http.StatusBadRequest), handlers.ErrorResponse{
			Error: handlers.ErrorDetail{
				Message: fmt.Sprintf("Invalid request: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}


	streamResult := ojson.GetBytesNoCopy(rawJSON, "stream")
	if streamResult.Type == ojson.True {
		h.handleCompletionsStreamingResponse(c, rawJSON)
	} else {
		h.handleCompletionsNonStreamingResponse(c, rawJSON)
	}

}









func convertCompletionsRequestToChatCompletions(rawJSON []byte) []byte {
	root := util.ParseGJSONBytesNoCopy(rawJSON)


	prompt := root.Get("prompt").String()
	if prompt == "" {
		prompt = "Complete this:"
	}


	outDoc := ojson.NewDoc([]byte(`{"model":"","messages":[{"role":"user","content":""}]}`))


	if model := root.Get("model"); model.Exists() {
		_ = outDoc.Set("model", model.String())
	}


	_ = outDoc.Set("messages.0.content", prompt)


	if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		_ = outDoc.Set("max_tokens", maxTokens.Int())
	}

	if temperature := root.Get("temperature"); temperature.Exists() {
		_ = outDoc.Set("temperature", temperature.Float())
	}

	if topP := root.Get("top_p"); topP.Exists() {
		_ = outDoc.Set("top_p", topP.Float())
	}

	if frequencyPenalty := root.Get("frequency_penalty"); frequencyPenalty.Exists() {
		_ = outDoc.Set("frequency_penalty", frequencyPenalty.Float())
	}

	if presencePenalty := root.Get("presence_penalty"); presencePenalty.Exists() {
		_ = outDoc.Set("presence_penalty", presencePenalty.Float())
	}

	if stop := root.Get("stop"); stop.Exists() {
		_ = outDoc.SetRawBytes("stop", []byte(stop.Raw))
	}

	if stream := root.Get("stream"); stream.Exists() {
		_ = outDoc.Set("stream", stream.Bool())
	}

	if logprobs := root.Get("logprobs"); logprobs.Exists() {
		_ = outDoc.Set("logprobs", logprobs.Bool())
	}

	if topLogprobs := root.Get("top_logprobs"); topLogprobs.Exists() {
		_ = outDoc.Set("top_logprobs", topLogprobs.Int())
	}

	if echo := root.Get("echo"); echo.Exists() {
		_ = outDoc.Set("echo", echo.Bool())
	}

	return outDoc.Bytes()
}









func convertChatCompletionsResponseToCompletions(rawJSON []byte) []byte {
	root := util.ParseGJSONBytesNoCopy(rawJSON)


	outDoc := ojson.NewDoc([]byte(`{"id":"","object":"text_completion","created":0,"model":"","choices":[]}`))


	if id := root.Get("id"); id.Exists() {
		_ = outDoc.Set("id", id.String())
	}

	if created := root.Get("created"); created.Exists() {
		_ = outDoc.Set("created", created.Int())
	}

	if model := root.Get("model"); model.Exists() {
		_ = outDoc.Set("model", model.String())
	}

	if usage := root.Get("usage"); usage.Exists() {
		_ = outDoc.SetRawBytes("usage", []byte(usage.Raw))
	}


	var choices []interface{}
	if chatChoices := root.Get("choices"); chatChoices.Exists() && chatChoices.IsArray() {
		chatChoices.ForEach(func(_, choice ojson.Result) bool {
			completionsChoice := map[string]interface{}{
				"index": choice.Get("index").Int(),
			}


			if message := choice.Get("message"); message.Exists() {
				if content := message.Get("content"); content.Exists() {
					completionsChoice["text"] = content.String()
				}
			} else if delta := choice.Get("delta"); delta.Exists() {

				if content := delta.Get("content"); content.Exists() {
					completionsChoice["text"] = content.String()
				}
			}


			if finishReason := choice.Get("finish_reason"); finishReason.Exists() {
				completionsChoice["finish_reason"] = finishReason.String()
			}


			if logprobs := choice.Get("logprobs"); logprobs.Exists() {
				completionsChoice["logprobs"] = logprobs.Value()
			}

			choices = append(choices, completionsChoice)
			return true
		})
	}

	if len(choices) > 0 {
		choicesJSON, _ := json.Marshal(choices)
		_ = outDoc.SetRawBytes("choices", choicesJSON)
	}

	return outDoc.Bytes()
}









func convertChatCompletionsStreamChunkToCompletions(chunkData []byte) []byte {
	root := util.ParseGJSONBytesNoCopy(chunkData)


	hasContent := false
	hasUsage := root.Get("usage").Exists()
	if chatChoices := root.Get("choices"); chatChoices.Exists() && chatChoices.IsArray() {
		chatChoices.ForEach(func(_, choice ojson.Result) bool {

			if delta := choice.Get("delta"); delta.Exists() {
				if content := delta.Get("content"); content.Exists() && content.String() != "" {
					hasContent = true
					return false
				}
			}

			if finishReason := choice.Get("finish_reason"); finishReason.Exists() && finishReason.String() != "" && finishReason.String() != "null" {
				hasContent = true
				return false
			}
			return true
		})
	}


	if !hasContent && !hasUsage {
		return nil
	}


	outDoc := ojson.NewDoc([]byte(`{"id":"","object":"text_completion","created":0,"model":"","choices":[]}`))


	if id := root.Get("id"); id.Exists() {
		_ = outDoc.Set("id", id.String())
	}

	if created := root.Get("created"); created.Exists() {
		_ = outDoc.Set("created", created.Int())
	}

	if model := root.Get("model"); model.Exists() {
		_ = outDoc.Set("model", model.String())
	}


	var choices []interface{}
	if chatChoices := root.Get("choices"); chatChoices.Exists() && chatChoices.IsArray() {
		chatChoices.ForEach(func(_, choice ojson.Result) bool {
			completionsChoice := map[string]interface{}{
				"index": choice.Get("index").Int(),
			}


			if delta := choice.Get("delta"); delta.Exists() {
				if content := delta.Get("content"); content.Exists() && content.String() != "" {
					completionsChoice["text"] = content.String()
				} else {
					completionsChoice["text"] = ""
				}
			} else {
				completionsChoice["text"] = ""
			}


			if finishReason := choice.Get("finish_reason"); finishReason.Exists() && finishReason.String() != "null" {
				completionsChoice["finish_reason"] = finishReason.String()
			}


			if logprobs := choice.Get("logprobs"); logprobs.Exists() {
				completionsChoice["logprobs"] = logprobs.Value()
			}

			choices = append(choices, completionsChoice)
			return true
		})
	}

	if len(choices) > 0 {
		choicesJSON, _ := json.Marshal(choices)
		_ = outDoc.SetRawBytes("choices", choicesJSON)
	}


	if usage := root.Get("usage"); usage.Exists() {
		_ = outDoc.SetRawBytes("usage", []byte(usage.Raw))
	}

	return outDoc.Bytes()
}








func (h *OpenAIAPIHandler) handleNonStreamingResponse(c *gin.Context, rawJSON []byte) {
	c.Header("Content-Type", "application/json")

	modelName := strings.Clone(ojson.GetBytesNoCopy(rawJSON, "model").String())
	cliCtx, cliCancel := h.GetContextWithCancel(h, c, context.Background())
	stopKeepAlive := h.StartNonStreamingKeepAlive(c, cliCtx)
	resp, upstreamHeaders, errMsg := h.ExecuteWithAuthManager(cliCtx, h.HandlerType(), modelName, rawJSON, h.GetAlt(c))
	stopKeepAlive()
	if errMsg != nil {
		h.WriteErrorResponse(c, errMsg)
		cliCancel(errMsg.Error)
		return
	}
	handlers.WriteUpstreamHeaders(c.Writer.Header(), upstreamHeaders)
	_, _ = c.Writer.Write(resp)
	cliCancel()
}








func (h *OpenAIAPIHandler) handleStreamingResponse(c *gin.Context, rawJSON []byte) {

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, handlers.ErrorResponse{
			Error: handlers.ErrorDetail{
				Message: "Streaming not supported",
				Type:    "server_error",
			},
		})
		return
	}

	modelName := strings.Clone(ojson.GetBytesNoCopy(rawJSON, "model").String())
	cliCtx, cliCancel := h.GetContextWithCancel(h, c, context.Background())
	setSSEHeaders := func() {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Access-Control-Allow-Origin", "*")
	}
	var upstreamHeaders http.Header
	committed := false
	commitSSE := func() {
		if committed {
			return
		}
		setSSEHeaders()
		handlers.WriteUpstreamHeaders(c.Writer.Header(), upstreamHeaders)
		committed = true
	}
	writeKeepAlive := func() bool {
		commitSSE()
		_, _ = c.Writer.Write([]byte(": keep-alive\n\n"))
		flusher.Flush()
		return true
	}
	keepAliveC, stopKeepAlive := handlers.NewPreFirstTokenKeepAliveTicker(h.CurrentConfig())
	defer stopKeepAlive()

	dataChan, resolvedHeaders, errChan, okBootstrap := h.ExecuteStreamWithPreFTKeepAlive(
		c, cliCtx, func(err error) { cliCancel(err) }, h.HandlerType(), modelName, rawJSON, h.GetAlt(c), keepAliveC, writeKeepAlive,
	)
	if !okBootstrap {
		return
	}
	upstreamHeaders = resolvedHeaders


	for {
		select {
		case <-c.Request.Context().Done():
			cliCancel(c.Request.Context().Err())
			return
		case errMsg, ok := <-errChan:
			if !ok {

				errChan = nil
				continue
			}
			if committed {
				stopKeepAlive()
				pendingErrors := make(chan *interfaces.ErrorMessage, 1)
				pendingErrors <- errMsg
				close(pendingErrors)
				h.handleStreamResult(c, flusher, func(err error) { cliCancel(err) }, make(chan []byte), pendingErrors, false)
				return
			}

			h.WriteErrorResponse(c, errMsg)
			if errMsg != nil {
				cliCancel(errMsg.Error)
			} else {
				cliCancel(nil)
			}
			return
		case chunk, ok := <-dataChan:
			if !ok {
				stopKeepAlive()
				if errMsg, hasPendingError := handlers.PendingStreamError(errChan); hasPendingError {
					if committed {
						pendingErrors := make(chan *interfaces.ErrorMessage, 1)
						pendingErrors <- errMsg
						close(pendingErrors)
						h.handleStreamResult(c, flusher, func(err error) { cliCancel(err) }, make(chan []byte), pendingErrors, false)
						return
					}
					h.WriteErrorResponse(c, errMsg)
					if errMsg != nil {
						cliCancel(errMsg.Error)
					} else {
						cliCancel(nil)
					}
					return
				}

				commitSSE()
				_, _ = fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
				flusher.Flush()
				cliCancel(nil)
				return
			}


			stopKeepAlive()
			commitSSE()

			termErr := chunkTerminalError(chunk)
			if termErr != nil {
				h.LoggingAPIResponseError(c, termErr)
			}
			initialHasFinishReason := termErr != nil || chunkHasFinishReason(chunk)
			_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", string(chunk))
			flusher.Flush()
			if termErr != nil {
				cliCancel(termErr.Error)
				return
			}


			h.handleStreamResult(c, flusher, func(err error) { cliCancel(err) }, dataChan, errChan, initialHasFinishReason)
			return
		case <-keepAliveC:
			writeKeepAlive()
		}
	}
}








func (h *OpenAIAPIHandler) handleCompletionsNonStreamingResponse(c *gin.Context, rawJSON []byte) {
	c.Header("Content-Type", "application/json")


	chatCompletionsJSON := convertCompletionsRequestToChatCompletions(rawJSON)

	modelName := strings.Clone(ojson.GetBytesNoCopy(chatCompletionsJSON, "model").String())
	cliCtx, cliCancel := h.GetContextWithCancel(h, c, context.Background())
	stopKeepAlive := h.StartNonStreamingKeepAlive(c, cliCtx)
	resp, upstreamHeaders, errMsg := h.ExecuteWithAuthManager(cliCtx, h.HandlerType(), modelName, chatCompletionsJSON, "")
	stopKeepAlive()
	if errMsg != nil {
		h.WriteErrorResponse(c, errMsg)
		cliCancel(errMsg.Error)
		return
	}
	handlers.WriteUpstreamHeaders(c.Writer.Header(), upstreamHeaders)
	completionsResp := convertChatCompletionsResponseToCompletions(resp)
	_, _ = c.Writer.Write(completionsResp)
	cliCancel()
}








func (h *OpenAIAPIHandler) handleCompletionsStreamingResponse(c *gin.Context, rawJSON []byte) {

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, handlers.ErrorResponse{
			Error: handlers.ErrorDetail{
				Message: "Streaming not supported",
				Type:    "server_error",
			},
		})
		return
	}


	chatCompletionsJSON := convertCompletionsRequestToChatCompletions(rawJSON)

	modelName := strings.Clone(ojson.GetBytesNoCopy(chatCompletionsJSON, "model").String())
	cliCtx, cliCancel := h.GetContextWithCancel(h, c, context.Background())
	setSSEHeaders := func() {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Access-Control-Allow-Origin", "*")
	}
	var upstreamHeaders http.Header
	committed := false
	commitSSE := func() {
		if committed {
			return
		}
		setSSEHeaders()
		handlers.WriteUpstreamHeaders(c.Writer.Header(), upstreamHeaders)
		committed = true
	}
	writeKeepAlive := func() bool {
		commitSSE()
		_, _ = c.Writer.Write([]byte(": keep-alive\n\n"))
		flusher.Flush()
		return true
	}
	keepAliveC, stopKeepAlive := handlers.NewPreFirstTokenKeepAliveTicker(h.CurrentConfig())
	defer stopKeepAlive()

	dataChan, resolvedHeaders, errChan, okBootstrap := h.ExecuteStreamWithPreFTKeepAlive(
		c, cliCtx, func(err error) { cliCancel(err) }, h.HandlerType(), modelName, chatCompletionsJSON, "", keepAliveC, writeKeepAlive,
	)
	if !okBootstrap {
		return
	}
	upstreamHeaders = resolvedHeaders


	for {
		select {
		case <-c.Request.Context().Done():
			cliCancel(c.Request.Context().Err())
			return
		case errMsg, ok := <-errChan:
			if !ok {

				errChan = nil
				continue
			}
			if committed {
				stopKeepAlive()
				pendingErrors := make(chan *interfaces.ErrorMessage, 1)
				pendingErrors <- errMsg
				close(pendingErrors)
				h.handleStreamResult(c, flusher, func(err error) { cliCancel(err) }, make(chan []byte), pendingErrors, false)
				return
			}
			h.WriteErrorResponse(c, errMsg)
			if errMsg != nil {
				cliCancel(errMsg.Error)
			} else {
				cliCancel(nil)
			}
			return
		case chunk, ok := <-dataChan:
			if !ok {
				stopKeepAlive()
				if errMsg, hasPendingError := handlers.PendingStreamError(errChan); hasPendingError {
					if committed {
						pendingErrors := make(chan *interfaces.ErrorMessage, 1)
						pendingErrors <- errMsg
						close(pendingErrors)
						h.handleStreamResult(c, flusher, func(err error) { cliCancel(err) }, make(chan []byte), pendingErrors, false)
						return
					}
					h.WriteErrorResponse(c, errMsg)
					if errMsg != nil {
						cliCancel(errMsg.Error)
					} else {
						cliCancel(nil)
					}
					return
				}
				commitSSE()
				_, _ = fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
				flusher.Flush()
				cliCancel(nil)
				return
			}


			stopKeepAlive()
			commitSSE()

			if termErr := chunkTerminalError(chunk); termErr != nil {
				h.LoggingAPIResponseError(c, termErr)
				_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", string(chunk))
				flusher.Flush()
				cliCancel(termErr.Error)
				return
			}


			converted := convertChatCompletionsStreamChunkToCompletions(chunk)
			var initialHasFinishReason bool
			if converted != nil {
				initialHasFinishReason = chunkHasFinishReason(converted)
				_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", string(converted))
				flusher.Flush()
			}

			done := make(chan struct{})
			var doneOnce sync.Once
			stop := func() { doneOnce.Do(func() { close(done) }) }

			convertedChan := make(chan []byte)
			go func() {
				defer close(convertedChan)
				for {
					select {
					case <-done:
						return
					case chunk, ok := <-dataChan:
						if !ok {
							return
						}
						converted := convertChatCompletionsStreamChunkToCompletions(chunk)
						if converted == nil {
							continue
						}
						select {
						case <-done:
							return
						case convertedChan <- converted:
						}
					}
				}
			}()

			h.handleStreamResult(c, flusher, func(err error) {
				stop()
				cliCancel(err)
			}, convertedChan, errChan, initialHasFinishReason)
			return
		case <-keepAliveC:
			writeKeepAlive()
		}
	}
}
func chunkTerminalError(chunk []byte) *interfaces.ErrorMessage {
	chunk = bytes.TrimSpace(chunk)
	if bytes.HasPrefix(chunk, []byte("data:")) {
		chunk = bytes.TrimSpace(chunk[5:])
	}
	var fieldsBuf [2]ojson.Result
	fields := ojson.GetManyBytesNoCopy(fieldsBuf[:0], chunk, "choices", "error")
	if fields[0].Exists() || !fields[1].IsObject() {
		return nil
	}
	msg := fields[1].Get("message").String()
	if msg == "" {
		msg = string(chunk)
	}
	return &interfaces.ErrorMessage{
		StatusCode: http.StatusBadRequest,
		Error:      fmt.Errorf("%s", msg),
	}
}

func chunkHasFinishReason(chunk []byte) bool {
	chunk = bytes.TrimSpace(chunk)
	if bytes.HasPrefix(chunk, []byte("data:")) {
		chunk = bytes.TrimSpace(chunk[5:])
	}
	if chunkTerminalError(chunk) != nil {
		return true
	}
	for _, choice := range ojson.GetBytesNoCopy(chunk, "choices").Array() {
		if reason := choice.Get("finish_reason"); reason.Exists() && reason.Type != ojson.Null && reason.String() != "" {
			return true
		}
	}
	return false
}

func (h *OpenAIAPIHandler) handleStreamResult(c *gin.Context, flusher http.Flusher, cancel func(error), data <-chan []byte, errs <-chan *interfaces.ErrorMessage, initialHasFinishReason bool) {
	sawFinishReason := initialHasFinishReason
	loggedTerminalErr := false
	var terminalChunkErr *interfaces.ErrorMessage
	h.ForwardStream(c, flusher, cancel, data, errs, handlers.StreamForwardOptions{
		WriteChunk: func(chunk []byte) {
			if termErr := chunkTerminalError(chunk); termErr != nil {
				terminalChunkErr = termErr
				sawFinishReason = true
				if !loggedTerminalErr {
					h.LoggingAPIResponseError(c, termErr)
					loggedTerminalErr = true
				}
			} else if !sawFinishReason && chunkHasFinishReason(chunk) {
				sawFinishReason = true
			}
			_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", string(chunk))
		},
		ChunkError: func() *interfaces.ErrorMessage {
			if terminalChunkErr != nil && !loggedTerminalErr {
				h.LoggingAPIResponseError(c, terminalChunkErr)
				loggedTerminalErr = true
			}
			return terminalChunkErr
		},
		WriteTerminalError: func(errMsg *interfaces.ErrorMessage) {
			if errMsg == nil {
				return
			}
			if !loggedTerminalErr {
				h.LoggingAPIResponseError(c, errMsg)
				loggedTerminalErr = true
			}
			if terminalChunkErr != nil {
				return
			}
			status := http.StatusInternalServerError
			if errMsg.StatusCode > 0 {
				status = errMsg.StatusCode
			}
			errText := http.StatusText(status)
			if errMsg.Error != nil && errMsg.Error.Error() != "" {
				errText = errMsg.Error.Error()
			}
			body := handlers.BuildErrorResponseBody(status, errText)
			_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", string(body))
		},
		CloseError: func() *interfaces.ErrorMessage {
			if sawFinishReason {
				return nil
			}
			return &interfaces.ErrorMessage{
				StatusCode: http.StatusBadGateway,
				Error:      fmt.Errorf("upstream stream closed before any chunk carried finish_reason"),
			}
		},
		WriteDone: func() {
			_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
		},
	})
}
