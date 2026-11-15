package executor

import (
	"bytes"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
)

const codexIncompleteStreamMessage = "stream error: stream disconnected before completion: stream closed before response.completed"

type codexIncompleteStreamError struct {
	statusErr
}

func newCodexIncompleteStreamError() codexIncompleteStreamError {
	return codexIncompleteStreamError{statusErr: statusErr{
		code: http.StatusRequestTimeout,
		msg:  codexIncompleteStreamMessage,
	}}
}

func (codexIncompleteStreamError) IsRequestScoped() bool {
	return true
}

type codexEmptyIncompleteStreamError struct {
	statusErr
}

func newCodexEmptyIncompleteStreamError() codexEmptyIncompleteStreamError {
	return codexEmptyIncompleteStreamError{statusErr: statusErr{
		code: http.StatusBadGateway,
		msg:  helps.CodexEmptyIncompleteStreamMessage,
	}}
}

func (codexEmptyIncompleteStreamError) IsRequestScoped() bool {
	return true
}

func collectCodexOutputItemDone(eventData []byte, outputItemsByIndex map[int64][]byte, outputItemsFallback *[][]byte) {
	itemResult := ojson.GetBytesNoCopy(eventData, "item")
	if !itemResult.Exists() || itemResult.Type != ojson.JSON {
		return
	}
	outputIndexResult := ojson.GetBytesNoCopy(eventData, "output_index")
	if outputIndexResult.Exists() {
		outputItemsByIndex[outputIndexResult.Int()] = []byte(itemResult.Raw)
		return
	}
	*outputItemsFallback = append(*outputItemsFallback, []byte(itemResult.Raw))
}

func hydrateCodexCompletedOutputItemIDs(eventData []byte, outputItems []ojson.Result, outputItemsByIndex map[int64][]byte) []byte {
	var doc *ojson.Doc
	changed := false
	for outputIndex, outputItem := range outputItems {
		itemID := outputItem.Get("id")
		if itemID.Exists() && itemID.Type != ojson.Null && (itemID.Type != ojson.String || strings.TrimSpace(itemID.String()) != "") {
			continue
		}

		completedItem, ok := outputItemsByIndex[int64(outputIndex)]
		if !ok {
			continue
		}
		completedID := ojson.GetBytesNoCopy(completedItem, "id")
		if completedID.Type != ojson.String || strings.TrimSpace(completedID.String()) == "" {
			continue
		}

		if doc == nil {
			doc = ojson.NewDoc(eventData)
		}
		if errSet := doc.SetRawBytes("response.output."+strconv.Itoa(outputIndex)+".id", []byte(completedID.Raw)); errSet == nil {
			changed = true
		}
	}
	if !changed || doc == nil {
		return eventData
	}
	return doc.Bytes()
}

func patchCodexCompletedOutput(eventData []byte, outputItemsByIndex map[int64][]byte, outputItemsFallback [][]byte) []byte {
	outputResult := ojson.GetBytesNoCopy(eventData, "response.output")
	if outputResult.Exists() && outputResult.IsArray() && len(outputResult.Array()) > 0 {
		return hydrateCodexCompletedOutputItemIDs(eventData, outputResult.Array(), outputItemsByIndex)
	}

	shouldPatchOutput := (!outputResult.Exists() || !outputResult.IsArray() || len(outputResult.Array()) == 0) && (len(outputItemsByIndex) > 0 || len(outputItemsFallback) > 0)
	if !shouldPatchOutput {
		return eventData
	}

	indexes := make([]int64, 0, len(outputItemsByIndex))
	for idx := range outputItemsByIndex {
		indexes = append(indexes, idx)
	}
	sort.Slice(indexes, func(i, j int) bool {
		return indexes[i] < indexes[j]
	})

	items := make([][]byte, 0, len(outputItemsByIndex)+len(outputItemsFallback))
	for _, idx := range indexes {
		items = append(items, outputItemsByIndex[idx])
	}
	items = append(items, outputItemsFallback...)

	outputArray := []byte("[]")
	if len(items) > 0 {
		var buf bytes.Buffer
		totalLen := 2
		for _, item := range items {
			totalLen += len(item)
		}
		if len(items) > 1 {
			totalLen += len(items) - 1
		}
		buf.Grow(totalLen)
		buf.WriteByte('[')
		for i, item := range items {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.Write(item)
		}
		buf.WriteByte(']')
		outputArray = buf.Bytes()
	}

	completedDataPatched, _ := ojson.SetRawBytes(eventData, "response.output", outputArray)
	return completedDataPatched
}

func codexTerminalStreamContextLengthErr(eventData []byte) (statusErr, bool) {
	streamErr, body, ok := codexTerminalStreamErr(eventData)
	if !ok || !codexTerminalErrorIsContextLength(body) {
		return statusErr{}, false
	}
	return streamErr, true
}

func codexTerminalStreamErr(eventData []byte) (statusErr, []byte, bool) {
	return codexTerminalStreamErrWithCooling(eventData, false)
}

func codexTerminalStreamErrWithCooling(eventData []byte, modelLevelCooling bool) (statusErr, []byte, bool) {
	body, ok := codexTerminalFailureBody(eventData)
	if !ok || !codexTerminalStreamErrShouldHandle(body) {
		return statusErr{}, nil, false
	}
	return newCodexStatusErrWithCooling(http.StatusBadRequest, body, modelLevelCooling), body, true
}

func codexTerminalFailureErr(eventData []byte) (statusErr, []byte, bool) {
	return codexTerminalFailureErrWithCooling(eventData, false)
}

func codexTerminalFailureErrWithCooling(eventData []byte, modelLevelCooling bool) (statusErr, []byte, bool) {
	if streamErr, body, ok := codexTerminalStreamErrWithCooling(eventData, modelLevelCooling); ok {
		return streamErr, body, true
	}
	body, ok := codexTerminalFailureBody(eventData)
	if !ok {
		return statusErr{}, nil, false
	}
	return newCodexStatusErrWithCooling(codexTerminalFailureStatus(body), body, modelLevelCooling), body, true
}

func codexTerminalFailureStatus(body []byte) int {
	for _, path := range []string{"error.status_code", "error.status"} {
		if status := int(ojson.GetBytesNoCopy(body, path).Int()); status >= 400 && status <= 599 {
			return status
		}
	}

	errorType := strings.ToLower(strings.TrimSpace(ojson.GetBytesNoCopy(body, "error.type").String()))
	errorCode := strings.ToLower(strings.TrimSpace(ojson.GetBytesNoCopy(body, "error.code").String()))
	switch {
	case errorCode == "cyber_policy":
		return http.StatusBadRequest
	case errorType == "not_found_error", errorCode == "not_found", errorCode == "model_not_found":
		return http.StatusNotFound
	case errorType == "authentication_error", errorCode == "invalid_api_key", errorCode == "unauthorized":
		return http.StatusUnauthorized
	case errorType == "permission_error", errorCode == "forbidden", errorCode == "permission_denied":
		return http.StatusForbidden
	case errorType == "rate_limit_error", errorCode == "rate_limit_exceeded":
		return http.StatusTooManyRequests
	case errorType == "invalid_request_error", errorType == "bad_request_error", errorType == "image_generation_user_error", errorCode == "invalid_value", errorCode == "invalid_request_error":
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}

func codexTerminalFailureBody(eventData []byte) ([]byte, bool) {
	eventType := ojson.GetBytesNoCopy(eventData, "type").String()
	var body []byte
	switch eventType {
	case "error":
		body = codexTerminalErrorBody(eventData, "error")
		if len(body) == 0 {
			body = codexTerminalTopLevelErrorBody(eventData)
		}
	case "response.failed":
		body = codexTerminalErrorBody(eventData, "response.error")
		if len(body) == 0 {
			body = codexTerminalErrorBody(eventData, "error")
		}
	default:
		return nil, false
	}
	if len(body) == 0 {
		body = []byte(`{"error":{"message":"upstream stream failed without error details"}}`)
	}
	if seq := ojson.GetBytesNoCopy(eventData, "sequence_number"); seq.Exists() {
		body, _ = ojson.SetBytes(body, "sequence_number", seq.Int())
	}
	return body, true
}

func codexTerminalStreamErrShouldHandle(body []byte) bool {
	if codexTerminalErrorIsContextLength(body) {
		return true
	}
	if isCodexUsageLimitError(body) || isCodexTransientCapacityError(body) {
		return true
	}
	code, _, ok := codexStatusErrorClassification(http.StatusBadRequest, body)
	return ok && code == "thinking_signature_invalid"
}

func codexTerminalErrorBody(eventData []byte, path string) []byte {
	errorResult := ojson.GetBytesNoCopy(eventData, path)
	if !errorResult.Exists() {
		return nil
	}
	doc := ojson.NewDoc([]byte(`{"error":{}}`))
	if errorResult.Type == ojson.JSON {
		_ = doc.SetRawBytes("error", []byte(errorResult.Raw))
	} else if message := strings.TrimSpace(errorResult.String()); message != "" {
		_ = doc.Set("error.message", message)
	}
	if strings.TrimSpace(doc.Get("error.message").String()) == "" {
		if message := strings.TrimSpace(ojson.GetBytesNoCopy(eventData, "response.error.message").String()); message != "" {
			_ = doc.Set("error.message", message)
		}
	}
	if strings.TrimSpace(doc.Get("error.message").String()) == "" {
		if code := strings.TrimSpace(doc.Get("error.code").String()); code != "" {
			_ = doc.Set("error.message", code)
		}
	}
	if strings.TrimSpace(doc.Get("error.message").String()) == "" {
		if errorType := strings.TrimSpace(doc.Get("error.type").String()); errorType != "" {
			_ = doc.Set("error.message", errorType)
		}
	}
	if strings.EqualFold(strings.TrimSpace(doc.Get("error.type").String()), "image_generation_user_error") {
		msg := strings.TrimSpace(doc.Get("error.message").String())
		param := strings.ToLower(strings.TrimSpace(doc.Get("error.param").String()))
		if (param == "complexity" || strings.Contains(strings.ToLower(msg), "complexity")) && !strings.Contains(msg, "/v1/images/generations") {
			_ = doc.Set("error.message", msg+` (Set tool_choice to {"type":"image_generation"}, use a compatible Responses model such as gpt-5.4-mini, or call /v1/images/generations directly.)`)
		}
	}
	return doc.Bytes()
}

func codexTerminalTopLevelErrorBody(eventData []byte) []byte {
	message := strings.TrimSpace(ojson.GetBytesNoCopy(eventData, "message").String())
	code := strings.TrimSpace(ojson.GetBytesNoCopy(eventData, "code").String())
	errorType := strings.TrimSpace(ojson.GetBytesNoCopy(eventData, "error_type").String())
	param := strings.TrimSpace(ojson.GetBytesNoCopy(eventData, "param").String())
	if message == "" && code == "" && errorType == "" && param == "" {
		return nil
	}

	doc := ojson.NewDoc([]byte(`{"error":{}}`))
	if message != "" {
		_ = doc.Set("error.message", message)
	}
	if code != "" {
		_ = doc.Set("error.code", code)
	}
	if errorType != "" {
		_ = doc.Set("error.type", errorType)
	}
	if param != "" {
		_ = doc.Set("error.param", param)
	}
	if strings.TrimSpace(doc.Get("error.message").String()) == "" {
		if code != "" {
			_ = doc.Set("error.message", code)
		} else if errorType != "" {
			_ = doc.Set("error.message", errorType)
		}
	}
	return doc.Bytes()
}

func codexTerminalErrorIsContextLength(body []byte) bool {
	errorCode := strings.ToLower(strings.TrimSpace(ojson.GetBytesNoCopy(body, "error.code").String()))
	message := strings.ToLower(strings.TrimSpace(ojson.GetBytesNoCopy(body, "error.message").String()))
	return errorCode == "context_length_exceeded" ||
		errorCode == "context_too_large" ||
		strings.Contains(message, "context window") ||
		strings.Contains(message, "context length") ||
		strings.Contains(message, "too many tokens")
}

func newCodexStatusErr(statusCode int, body []byte) statusErr {
	return newCodexStatusErrWithCooling(statusCode, body, false)
}

func newCodexStatusErrWithHeaders(statusCode int, headers http.Header, body []byte) statusErr {
	return newCodexStatusErrWithHeadersAndCooling(statusCode, headers, body, false)
}

func newCodexStatusErrWithCooling(statusCode int, body []byte, modelLevelCooling bool) statusErr {
	return newCodexStatusErrWithHeadersAndCooling(statusCode, nil, body, modelLevelCooling)
}

func newCodexStatusErrWithHeadersAndCooling(statusCode int, headers http.Header, body []byte, modelLevelCooling bool) statusErr {
	errCode := statusCode
	isUsageLimit := isCodexUsageLimitError(body)
	credentialScoped := isUsageLimit && !modelLevelCooling
	transientCapacity := !isUsageLimit && isCodexTransientCapacityError(body)
	if transientCapacity || isUsageLimit {
		errCode = http.StatusTooManyRequests
	}
	body = classifyCodexStatusError(errCode, body)
	err := statusErr{code: errCode, msg: string(body), credentialScoped: credentialScoped, transientRateLimit: transientCapacity && errCode == http.StatusTooManyRequests}

	if retryAfter := parseCodexRetryAfter(errCode, body, time.Now()); retryAfter != nil {
		err.retryAfter = retryAfter
	} else if errCode == http.StatusTooManyRequests {
		if retryAfter := parseRetryAfterHeader(headers, time.Now()); retryAfter != nil {
			err.retryAfter = retryAfter
		}
	}
	return err
}

func classifyCodexStatusError(statusCode int, body []byte) []byte {
	code, errType, ok := codexStatusErrorClassification(statusCode, body)
	if !ok {
		return body
	}
	message := ojson.GetBytesNoCopy(body, "error.message").String()
	if message == "" {
		message = ojson.GetBytesNoCopy(body, "message").String()
	}
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if message == "" {
		message = http.StatusText(statusCode)
	}
	doc := ojson.NewDoc([]byte(`{"error":{}}`))
	_ = doc.Set("error.message", message)
	_ = doc.Set("error.type", errType)
	_ = doc.Set("error.code", code)
	return doc.Bytes()
}

func codexStatusErrorClassification(statusCode int, body []byte) (code string, errType string, ok bool) {
	errorMessage := strings.ToLower(strings.TrimSpace(ojson.GetBytesNoCopy(body, "error.message").String()))
	if errorMessage == "" {
		errorMessage = strings.ToLower(strings.TrimSpace(ojson.GetBytesNoCopy(body, "message").String()))
	}
	lower := strings.ToLower(strings.TrimSpace(string(body)))
	upstreamCode := strings.ToLower(strings.TrimSpace(ojson.GetBytesNoCopy(body, "error.code").String()))
	upstreamType := strings.ToLower(strings.TrimSpace(ojson.GetBytesNoCopy(body, "error.type").String()))
	isInvalidRequest := upstreamType == "" || upstreamType == "invalid_request_error"

	switch {
	case statusCode == http.StatusRequestEntityTooLarge || upstreamCode == "context_length_exceeded" || upstreamCode == "context_too_large" || isInvalidRequest && (strings.Contains(errorMessage, "context length") || strings.Contains(errorMessage, "context_length") || strings.Contains(errorMessage, "maximum context") || strings.Contains(errorMessage, "too many tokens")):
		return "context_too_large", "invalid_request_error", true
	case strings.Contains(lower, "invalid signature in thinking block") || strings.Contains(lower, "invalid_encrypted_content"):
		return "thinking_signature_invalid", "invalid_request_error", true
	case upstreamCode == "previous_response_not_found" || strings.Contains(lower, "previous_response_not_found") || strings.Contains(lower, "previous_response_id") && strings.Contains(lower, "not found"):
		return "previous_response_not_found", "invalid_request_error", true
	case statusCode == http.StatusUnauthorized || upstreamType == "authentication_error" || upstreamCode == "invalid_api_key" || strings.Contains(lower, "invalid or expired token") || strings.Contains(lower, "refresh_token_reused"):
		return "auth_unavailable", "authentication_error", true
	default:
		return "", "", false
	}
}

func isCodexTurnRateLimitError(errorBody []byte) bool {
	if len(errorBody) == 0 {
		return false
	}
	codes := []string{
		ojson.GetBytesNoCopy(errorBody, "error.code").String(),
		ojson.GetBytesNoCopy(errorBody, "code").String(),
		ojson.GetBytesNoCopy(errorBody, "body.error.code").String(),
	}
	hasOtherCode := false
	for _, candidate := range codes {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if strings.EqualFold(candidate, "turn_rate_limit_exceeded") {
			return true
		}
		hasOtherCode = true
	}

	if hasOtherCode {
		return false
	}
	errorTypes := []string{
		ojson.GetBytesNoCopy(errorBody, "error.type").String(),
		ojson.GetBytesNoCopy(errorBody, "type").String(),
		ojson.GetBytesNoCopy(errorBody, "body.error.type").String(),
	}
	rateLimitTyped := false
	for _, candidate := range errorTypes {
		if strings.EqualFold(strings.TrimSpace(candidate), "rate_limit_error") {
			rateLimitTyped = true
			break
		}
	}
	if !rateLimitTyped {
		return false
	}
	messages := []string{
		ojson.GetBytesNoCopy(errorBody, "error.message").String(),
		ojson.GetBytesNoCopy(errorBody, "message").String(),
		ojson.GetBytesNoCopy(errorBody, "body.error.message").String(),
	}
	for _, candidate := range messages {
		if strings.Contains(strings.ToLower(strings.TrimSpace(candidate)), "at capacity") {
			return true
		}
	}
	return false
}

func codexErrorCodeOf(errorBody []byte) string {
	for _, path := range []string{"error.code", "code", "body.error.code"} {
		if code := strings.TrimSpace(ojson.GetBytesNoCopy(errorBody, path).String()); code != "" {
			return strings.Clone(code)
		}
	}
	return ""
}

func isCodexTransientCapacityError(errorBody []byte) bool {
	if code := codexErrorCodeOf(errorBody); code != "" &&
		!strings.EqualFold(code, "turn_rate_limit_exceeded") &&
		!strings.EqualFold(code, "model_at_capacity") &&
		!strings.EqualFold(code, "model_is_at_capacity") {
		return false
	}
	return isCodexModelCapacityError(errorBody) || isCodexTurnRateLimitError(errorBody)
}

func isCodexModelCapacityError(errorBody []byte) bool {
	if len(errorBody) == 0 {
		return false
	}
	candidates := []string{
		ojson.GetBytesNoCopy(errorBody, "error.message").String(),
		ojson.GetBytesNoCopy(errorBody, "message").String(),
		string(errorBody),
	}
	for _, candidate := range candidates {
		lower := strings.ToLower(strings.TrimSpace(candidate))
		if lower == "" {
			continue
		}
		if strings.Contains(lower, "model is at capacity") ||
			strings.Contains(lower, "model_at_capacity") ||
			strings.Contains(lower, "model_is_at_capacity") ||
			(strings.Contains(lower, "model") && strings.Contains(lower, "at capacity")) {
			return true
		}
	}
	return false
}

func isCodexUsageLimitError(errorBody []byte) bool {
	if len(errorBody) == 0 {
		return false
	}
	candidates := []string{
		ojson.GetBytesNoCopy(errorBody, "error.type").String(),
		ojson.GetBytesNoCopy(errorBody, "type").String(),
		ojson.GetBytesNoCopy(errorBody, "body.error.type").String(),
	}
	for _, candidate := range candidates {
		if strings.EqualFold(strings.TrimSpace(candidate), "usage_limit_reached") {
			return true
		}
	}
	return false
}

func parseCodexRetryAfter(statusCode int, errorBody []byte, now time.Time) *time.Duration {
	if statusCode != http.StatusTooManyRequests || len(errorBody) == 0 {
		return nil
	}
	for _, quota := range []ojson.Result{ojson.GetBytesNoCopy(errorBody, "error"), util.ParseGJSONBytesNoCopy(errorBody), ojson.GetBytesNoCopy(errorBody, "body.error")} {
		if !strings.EqualFold(strings.TrimSpace(quota.Get("type").String()), "usage_limit_reached") {
			continue
		}
		if resetsAt := quota.Get("resets_at").Int(); resetsAt > 0 {
			resetAtTime := time.Unix(resetsAt, 0)
			if resetAtTime.After(now) {
				retryAfter := resetAtTime.Sub(now)
				return &retryAfter
			}
		}
		if resetsInSeconds := quota.Get("resets_in_seconds").Int(); resetsInSeconds > 0 {
			retryAfter := time.Duration(resetsInSeconds) * time.Second
			return &retryAfter
		}
	}
	return nil
}

var (
	codexBootstrapNowMu sync.RWMutex
	codexBootstrapNow   = time.Now
)

func nowCodexBootstrap() time.Time {
	codexBootstrapNowMu.RLock()
	fn := codexBootstrapNow
	codexBootstrapNowMu.RUnlock()
	if fn != nil {
		return fn()
	}
	return time.Now()
}

func setCodexBootstrapNowForTest(fn func() time.Time) func() {
	codexBootstrapNowMu.Lock()
	orig := codexBootstrapNow
	codexBootstrapNow = fn
	codexBootstrapNowMu.Unlock()
	return func() {
		codexBootstrapNowMu.Lock()
		codexBootstrapNow = orig
		codexBootstrapNowMu.Unlock()
	}
}

const codexBootstrapMaxBufferedFrames = 48

const codexBootstrapMaxBufferedBytes = 1 << 20

func isCodexBootstrapBufferableEvent(eventType string, payload []byte) bool {

	if len(bytes.TrimSpace(payload)) == 0 {
		return true
	}
	switch eventType {

	case "response.created", "response.in_progress", "codex.rate_limits", "codex.response.metadata", "response.metadata", "keepalive":
		return true
	case "response.output_item.added":
		return isCodexBufferableOutputItem(payload)
	case "response.content_part.added":
		return isCodexEmptyPart(payload)
	case "response.reasoning_summary_part.added":
		return isCodexEmptyPart(payload)
	default:
		return false
	}
}

func isCodexBufferableOutputItem(payload []byte) bool {
	item := ojson.GetBytesNoCopy(payload, "item")
	switch item.Get("type").String() {
	case "message":
		return isCodexEmptyContentList(item.Get("content"))
	case "reasoning":
		if item.Get("encrypted_content").String() != "" {
			return false
		}
		return isCodexEmptyContentList(item.Get("summary")) && isCodexEmptyContentList(item.Get("content"))
	case "function_call":
		return item.Get("arguments").String() == ""
	case "custom_tool_call":
		return item.Get("input").String() == ""
	default:
		return false
	}
}

func isCodexEmptyContentList(list ojson.Result) bool {
	for _, entry := range list.Array() {
		switch entry.Get("type").String() {
		case "output_text", "summary_text", "text", "reasoning_text":
			if entry.Get("text").String() != "" {
				return false
			}
		case "refusal":
			if entry.Get("refusal").String() != "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func isCodexEmptyPart(payload []byte) bool {
	part := ojson.GetBytesNoCopy(payload, "part")
	switch part.Get("type").String() {
	case "output_text", "summary_text", "text", "reasoning_text":
		return part.Get("text").String() == ""
	case "refusal":
		return part.Get("refusal").String() == ""
	default:
		return false
	}
}

func observeCodexTokenEvent(reporter *helps.UsageReporter, payload []byte) {
	helps.ObserveResponsesTokenEvent(reporter, payload)
	reporter.ObserveResponseModel(payload)
}

func newCodexBootstrapOverloadErr(body []byte) statusErr {
	return newCodexStatusErr(http.StatusServiceUnavailable, body)
}

func isCodexOverloadBootstrapFailure(body []byte) bool {
	if isCodexTransientCapacityError(body) {
		return true
	}
	errorType := strings.ToLower(strings.TrimSpace(ojson.GetBytesNoCopy(body, "error.type").String()))
	errorCode := strings.ToLower(strings.TrimSpace(ojson.GetBytesNoCopy(body, "error.code").String()))
	errorMessage := strings.ToLower(strings.TrimSpace(ojson.GetBytesNoCopy(body, "error.message").String()))
	if errorMessage == "" {
		errorMessage = strings.ToLower(strings.TrimSpace(ojson.GetBytesNoCopy(body, "message").String()))
	}
	switch {
	case errorType == "service_unavailable_error", errorCode == "server_is_overloaded":
		return true
	case errorType == "rate_limit_error", errorCode == "rate_limit_exceeded":
		return true
	case (errorType == "server_error" || errorCode == "server_error") && strings.Contains(errorMessage, "you can retry your request"):
		return true
	default:
		return false
	}
}
