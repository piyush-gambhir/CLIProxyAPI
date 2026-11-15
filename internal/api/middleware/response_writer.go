


package middleware

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/clienterror"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/httpwire"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	log "github.com/sirupsen/logrus"
)

const requestBodyOverrideContextKey = "REQUEST_BODY_OVERRIDE"
const responseBodyOverrideContextKey = "RESPONSE_BODY_OVERRIDE"
const websocketTimelineOverrideContextKey = "WEBSOCKET_TIMELINE_OVERRIDE"


type RequestInfo struct {
	URL                 string
	Method              string
	Headers             map[string][]string
	Body                []byte
	RequestID           string
	Timestamp           time.Time
	deferredBodyCapture *deferredRequestBodyCapture
}



type ResponseWriterWrapper struct {
	gin.ResponseWriter
	body                *bytes.Buffer
	isStreaming         bool
	streamWriter        logging.StreamingLogWriter
	chunkChannel        chan []byte
	streamDone          chan struct{}
	logger              logging.RequestLogger
	requestInfo         *RequestInfo
	ginContext          *gin.Context
	statusCode          int
	headers             map[string][]string
	logOnErrorOnly      bool
	deferredAPICapture  *logging.DeferredAPICapture
	hijacked            bool
	panicked            bool
	finalized           bool
	firstChunkTimestamp time.Time
}











func (w *ResponseWriterWrapper) Unwrap() http.ResponseWriter {
	if w == nil {
		return nil
	}
	return w.ResponseWriter
}

func (w *ResponseWriterWrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, errHijack := w.ResponseWriter.Hijack()
	if errHijack == nil {
		w.hijacked = true
	}
	return conn, rw, errHijack
}

func NewResponseWriterWrapper(w gin.ResponseWriter, logger logging.RequestLogger, requestInfo *RequestInfo) *ResponseWriterWrapper {
	return &ResponseWriterWrapper{
		ResponseWriter: w,
		body:           &bytes.Buffer{},
		logger:         logger,
		requestInfo:    requestInfo,
		headers:        make(map[string][]string),
	}
}






func (w *ResponseWriterWrapper) Write(data []byte) (int, error) {
	w.closeConnectionIfRequestBodyUnconsumed(w.effectiveStatusCode())


	w.ensureHeadersCaptured()


	n, err := w.ResponseWriter.Write(data)


	if w.isStreaming && w.chunkChannel != nil {

		if w.firstChunkTimestamp.IsZero() {
			w.firstChunkTimestamp = time.Now()
		}

		select {
		case w.chunkChannel <- append([]byte(nil), data...):
		default:
		}
		return n, err
	}

	if w.shouldBufferResponseBody() {
		w.body.Write(data)
	}

	return n, err
}


func (w *ResponseWriterWrapper) FlushError() error {
	return httpwire.FlushResponse(w.ResponseWriter)
}

func (w *ResponseWriterWrapper) effectiveStatusCode() int {
	status := w.statusCode
	if status == 0 {
		if statusWriter, ok := w.ResponseWriter.(interface{ Status() int }); ok && statusWriter != nil {
			status = statusWriter.Status()
		} else {
			status = http.StatusOK
		}
	}
	return status
}

func (w *ResponseWriterWrapper) hasUnconsumedRequestBody() bool {
	if w == nil || w.requestInfo == nil {
		return false
	}
	if len(w.requestInfo.Body) > 0 {
		return false
	}
	if w.requestInfo.deferredBodyCapture != nil {
		return !w.requestInfo.deferredBodyCapture.isComplete()
	}
	return false
}

func (w *ResponseWriterWrapper) closeConnectionIfRequestBodyUnconsumed(statusCode int) {
	if !w.hijacked && statusCode >= http.StatusOK && w.hasUnconsumedRequestBody() {
		w.ResponseWriter.Header().Set("Connection", "close")
		_ = http.NewResponseController(w.ResponseWriter).SetReadDeadline(time.Unix(1, 0))
	}
}

func (w *ResponseWriterWrapper) shouldBufferResponseBody() bool {
	status := w.effectiveStatusCode()
	apiErrors := extractAPIResponseErrors(w.ginContext)
	if status < http.StatusBadRequest {
		if apiErrStatus := actionableAPIResponseErrorStatus(apiErrors); apiErrStatus >= http.StatusBadRequest {
			status = apiErrStatus
		}
	}
	if !w.panicked && isUnroutedNotFound(w.ginContext, status, apiErrors) {
		return false
	}
	if w.logger != nil && w.logger.IsEnabled() {
		return true
	}
	if !w.logOnErrorOnly {
		return false
	}
	return status >= http.StatusBadRequest && status != clienterror.StatusClientClosedRequest
}




func (w *ResponseWriterWrapper) WriteString(data string) (int, error) {
	w.closeConnectionIfRequestBodyUnconsumed(w.effectiveStatusCode())
	w.ensureHeadersCaptured()


	n, err := w.ResponseWriter.WriteString(data)


	if w.isStreaming && w.chunkChannel != nil {

		if w.firstChunkTimestamp.IsZero() {
			w.firstChunkTimestamp = time.Now()
		}
		select {
		case w.chunkChannel <- []byte(data):
		default:
		}
		return n, err
	}

	if w.shouldBufferResponseBody() {
		w.body.WriteString(data)
	}
	return n, err
}




func (w *ResponseWriterWrapper) WriteHeader(statusCode int) {
	if w.ResponseWriter.Written() {
		if statusCode >= http.StatusBadRequest && statusCode != clienterror.StatusClientClosedRequest {
			w.panicked = true
		}
		return
	}
	w.statusCode = statusCode
	w.closeConnectionIfRequestBodyUnconsumed(statusCode)


	w.captureCurrentHeaders()


	contentType := w.ResponseWriter.Header().Get("Content-Type")
	w.isStreaming = w.detectStreaming(contentType) || w.streamWriter != nil


	if !w.finalized && w.isStreaming && w.logger != nil && w.logger.IsEnabled() {
		if w.streamWriter == nil {
			if !w.panicked && isUnroutedNotFound(w.ginContext, statusCode, extractAPIResponseErrors(w.ginContext)) {
				w.isStreaming = false
			} else {
				streamWriter, err := w.logger.LogStreamingRequest(
					w.requestInfo.URL,
					w.requestInfo.Method,
					w.requestInfo.Headers,
					w.currentCapturedRequestBody(),
					w.requestInfo.RequestID,
				)
				if err == nil {
					chunkChan := make(chan []byte, 100)
					doneChan := make(chan struct{})
					w.streamWriter = streamWriter
					w.chunkChannel = chunkChan
					w.streamDone = doneChan


					go w.processStreamingChunks(doneChan, chunkChan, streamWriter)


					_ = streamWriter.WriteStatus(statusCode, w.headers)
				}
			}
		} else {
			_ = w.streamWriter.WriteStatus(statusCode, w.headers)
		}
	}


	w.ResponseWriter.WriteHeader(statusCode)
}




func (w *ResponseWriterWrapper) ensureHeadersCaptured() {

	w.captureCurrentHeaders()
}



func (w *ResponseWriterWrapper) captureCurrentHeaders() {

	if w.headers == nil {
		w.headers = make(map[string][]string)
	}


	for key, values := range w.ResponseWriter.Header() {

		headerValues := make([]string, len(values))
		copy(headerValues, values)
		w.headers[key] = headerValues
	}
}




func (w *ResponseWriterWrapper) detectStreaming(contentType string) bool {

	if strings.Contains(contentType, "text/event-stream") {
		return true
	}



	if strings.TrimSpace(contentType) != "" {
		return false
	}


	if body := w.currentCapturedRequestBody(); len(body) > 0 {
		return bytes.Contains(body, []byte(`"stream": true`)) ||
			bytes.Contains(body, []byte(`"stream":true`))
	}

	return false
}

func (w *ResponseWriterWrapper) currentCapturedRequestBody() []byte {
	if w == nil || w.requestInfo == nil {
		return nil
	}
	if len(w.requestInfo.Body) > 0 {
		return w.requestInfo.Body
	}
	if w.requestInfo.deferredBodyCapture == nil || w.requestInfo.deferredBodyCapture.bytesRead == 0 {
		return nil
	}
	body, _, errRead := w.requestInfo.deferredBodyCapture.Bytes()
	if errRead != nil || len(body) == 0 {
		return nil
	}
	encoding := ""
	for key, values := range w.requestInfo.Headers {
		if strings.EqualFold(key, "Content-Encoding") && len(values) > 0 {
			encoding = values[0]
			break
		}
	}
	w.requestInfo.Body = decodeCapturedRequestBodyForLogWithLimit(body, encoding, maxDeferredErrorRequestBodyBytes)
	return w.requestInfo.Body
}



func (w *ResponseWriterWrapper) processStreamingChunks(done chan struct{}, chunkChan <-chan []byte, streamWriter logging.StreamingLogWriter) {
	if done == nil {
		return
	}

	defer close(done)

	if streamWriter == nil || chunkChan == nil {
		return
	}

	for chunk := range chunkChan {
		streamWriter.WriteChunkAsync(chunk)
	}
}





func (w *ResponseWriterWrapper) Finalize(c *gin.Context) error {
	if w == nil || w.finalized {
		return nil
	}
	w.finalized = true
	if w.requestInfo != nil && w.requestInfo.deferredBodyCapture != nil {
		defer w.requestInfo.deferredBodyCapture.Cleanup()
	}
	defer w.closeDeferredAPICapture(c)
	if w.logger == nil {
		return nil
	}

	finalStatusCode := w.statusCode
	if finalStatusCode == 0 {
		if statusWriter, ok := w.ResponseWriter.(interface{ Status() int }); ok {
			finalStatusCode = statusWriter.Status()
		} else {
			finalStatusCode = 200
		}
	}

	var slicesAPIResponseError []*interfaces.ErrorMessage
	apiResponseError, isExist := c.Get("API_RESPONSE_ERROR")
	if isExist {
		if apiErrors, ok := apiResponseError.([]*interfaces.ErrorMessage); ok {
			slicesAPIResponseError = apiErrors
		}
	}
	if finalStatusCode < http.StatusBadRequest {
		if errStatus := actionableAPIResponseErrorStatus(slicesAPIResponseError); errStatus >= http.StatusBadRequest {
			finalStatusCode = errStatus
		}
	}

	hasAPIError := w.panicked || hasActionableError(c, finalStatusCode, slicesAPIResponseError)
	forceLog := w.logOnErrorOnly && hasAPIError && !w.logger.IsEnabled()
	websocketTimelineSource := w.extractWebsocketTimelineSource(c)
	apiRequestSource := w.extractAPIRequestSource(c)
	apiResponseSource := w.extractAPIResponseSource(c)
	apiWebsocketTimelineSource := w.extractAPIWebsocketTimelineSource(c)
	if (!w.logger.IsEnabled() && !forceLog) || (!w.panicked && isUnroutedNotFound(c, finalStatusCode, slicesAPIResponseError)) {
		errDiscard := w.discardStreamingLogWriter()
		cleanupFileBodySources(websocketTimelineSource, apiRequestSource, apiResponseSource, apiWebsocketTimelineSource)
		return errDiscard
	}

	if w.isStreaming && w.streamWriter != nil {
		if w.chunkChannel != nil {
			close(w.chunkChannel)
			w.chunkChannel = nil
		}

		if w.streamDone != nil {
			<-w.streamDone
			w.streamDone = nil
		}

		w.streamWriter.SetFirstChunkTimestamp(w.firstChunkTimestamp)


		apiRequest := w.extractAPIRequest(c)
		apiResponse := w.extractAPIResponse(c)
		if sourceWriter, ok := w.streamWriter.(interface {
			WriteAPIRequestSource(*logging.FileBodySource) error
			WriteAPIResponseSource(*logging.FileBodySource) error
		}); ok {
			if len(apiRequest) > 0 {
				_ = w.streamWriter.WriteAPIRequest(apiRequest)
			}
			if apiRequestSource != nil && apiRequestSource.HasPayload() {
				_ = sourceWriter.WriteAPIRequestSource(apiRequestSource)
			}
			if len(apiResponse) > 0 {
				_ = w.streamWriter.WriteAPIResponse(apiResponse)
			}
			if apiResponseSource != nil && apiResponseSource.HasPayload() {
				_ = sourceWriter.WriteAPIResponseSource(apiResponseSource)
			}
		} else {
			var errMerge error
			apiRequest, errMerge = mergeFileBodySource(apiRequest, apiRequestSource)
			if errMerge != nil {
				cleanupFileBodySources(websocketTimelineSource, apiResponseSource, apiWebsocketTimelineSource)
				return errMerge
			}
			apiResponse, errMerge = mergeFileBodySource(apiResponse, apiResponseSource)
			if errMerge != nil {
				cleanupFileBodySources(websocketTimelineSource, apiWebsocketTimelineSource)
				return errMerge
			}
			if len(apiRequest) > 0 {
				_ = w.streamWriter.WriteAPIRequest(apiRequest)
			}
			if len(apiResponse) > 0 {
				_ = w.streamWriter.WriteAPIResponse(apiResponse)
			}
		}
		apiWebsocketTimeline := w.extractAPIWebsocketTimeline(c)
		var errMerge error
		apiWebsocketTimeline, errMerge = mergeFileBodySource(apiWebsocketTimeline, apiWebsocketTimelineSource)
		if errMerge != nil {
			cleanupFileBodySources(websocketTimelineSource, apiRequestSource, apiResponseSource)
			return errMerge
		}
		if len(apiWebsocketTimeline) > 0 {
			_ = w.streamWriter.WriteAPIWebsocketTimeline(apiWebsocketTimeline)
		}
		if finalStatusCode >= http.StatusBadRequest {
			_ = w.streamWriter.WriteStatus(finalStatusCode, nil)
		}
		if len(slicesAPIResponseError) > 0 {
			_ = w.streamWriter.WriteAPIResponseErrors(slicesAPIResponseError)
		}
		if err := w.streamWriter.Close(); err != nil {
			w.streamWriter = nil
			cleanupFileBodySources(websocketTimelineSource, apiRequestSource, apiResponseSource)
			return err
		}
		w.streamWriter = nil
		cleanupFileBodySources(websocketTimelineSource, apiRequestSource, apiResponseSource)
		return nil
	}

	apiRequest := w.extractAPIRequest(c)
	if forceLog && len(apiRequest) == 0 {
		apiRequest = w.extractDeferredAPIRequest(c)
	}
	apiResponse := w.extractAPIResponse(c)
	if forceLog {
		if deferredResponse := w.extractDeferredAPIResponse(c); len(deferredResponse) > 0 {
			apiResponse = deferredResponse
		}
	}
	return w.logRequest(w.extractRequestBody(c), finalStatusCode, w.cloneHeaders(), w.extractResponseBody(c), w.extractWebsocketTimeline(c), websocketTimelineSource, apiRequest, apiRequestSource, apiResponse, apiResponseSource, w.extractAPIWebsocketTimeline(c), apiWebsocketTimelineSource, w.extractAPIResponseTimestamp(c), slicesAPIResponseError, forceLog)
}

func (w *ResponseWriterWrapper) cloneHeaders() map[string][]string {
	w.ensureHeadersCaptured()

	finalHeaders := make(map[string][]string, len(w.headers))
	for key, values := range w.headers {
		headerValues := make([]string, len(values))
		copy(headerValues, values)
		finalHeaders[key] = headerValues
	}

	return finalHeaders
}

func (w *ResponseWriterWrapper) extractAPIRequest(c *gin.Context) []byte {
	apiRequest, isExist := c.Get("API_REQUEST")
	if !isExist {
		return nil
	}
	data, ok := apiRequest.([]byte)
	if !ok || len(data) == 0 {
		return nil
	}
	return data
}

func (w *ResponseWriterWrapper) extractDeferredAPIRequest(c *gin.Context) []byte {
	if w != nil && w.deferredAPICapture != nil && w.deferredAPICapture.HasRequests() {
		return w.deferredAPICapture.RenderAPIRequests()
	}
	if c == nil {
		return nil
	}
	if value, exists := c.Get(logging.DeferredAPIResponseContextKey); exists {
		if capture, ok := value.(*logging.DeferredAPICapture); ok && capture != nil && capture.HasRequests() {
			return capture.RenderAPIRequests()
		}
	}
	value, exists := c.Get(logging.DeferredAPIRequestContextKey)
	if !exists {
		return nil
	}
	requests, ok := value.([]logging.DeferredAPIRequest)
	if !ok || len(requests) == 0 {
		return nil
	}
	var body bytes.Buffer
	for _, buildRequest := range requests {
		if buildRequest == nil {
			continue
		}
		body.Write(buildRequest())
	}
	return body.Bytes()
}

func (w *ResponseWriterWrapper) extractDeferredAPIResponse(c *gin.Context) []byte {
	if w != nil && w.deferredAPICapture != nil {
		if !w.deferredAPICapture.HasPayload() {
			return nil
		}
		return w.deferredAPICapture.RenderAPIResponse()
	}
	if c == nil {
		return nil
	}
	value, exists := c.Get(logging.DeferredAPIResponseContextKey)
	if !exists {
		return nil
	}
	deferred, ok := value.(logging.DeferredAPIResponse)
	if !ok || deferred == nil || !deferred.HasPayload() {
		return nil
	}
	return deferred.RenderAPIResponse()
}

func (w *ResponseWriterWrapper) closeDeferredAPICapture(c *gin.Context) {
	if w != nil && w.deferredAPICapture != nil {
		w.deferredAPICapture.Close()
	}
	if c == nil {
		return
	}
	if value, exists := c.Get(logging.DeferredAPIResponseContextKey); exists {
		if closer, ok := value.(interface{ Close() }); ok && closer != nil {
			closer.Close()
		}
	}
}

func (w *ResponseWriterWrapper) extractAPIResponse(c *gin.Context) []byte {
	apiResponse, isExist := c.Get("API_RESPONSE")
	if !isExist {
		return nil
	}
	data, ok := apiResponse.([]byte)
	if !ok || len(data) == 0 {
		return nil
	}
	return data
}

func (w *ResponseWriterWrapper) extractAPIRequestSource(c *gin.Context) *logging.FileBodySource {
	return extractFileBodySource(c, logging.APIRequestSourceContextKey)
}

func (w *ResponseWriterWrapper) extractAPIResponseSource(c *gin.Context) *logging.FileBodySource {
	return extractFileBodySource(c, logging.APIResponseSourceContextKey)
}

func (w *ResponseWriterWrapper) extractAPIWebsocketTimeline(c *gin.Context) []byte {
	apiTimeline, isExist := c.Get("API_WEBSOCKET_TIMELINE")
	if !isExist {
		return nil
	}
	data, ok := apiTimeline.([]byte)
	if !ok || len(data) == 0 {
		return nil
	}
	return bytes.Clone(data)
}

func (w *ResponseWriterWrapper) extractAPIWebsocketTimelineSource(c *gin.Context) *logging.FileBodySource {
	return extractFileBodySource(c, logging.APIWebsocketTimelineSourceContextKey)
}

func (w *ResponseWriterWrapper) extractAPIResponseTimestamp(c *gin.Context) time.Time {
	ts, isExist := c.Get("API_RESPONSE_TIMESTAMP")
	if !isExist {
		return time.Time{}
	}
	if t, ok := ts.(time.Time); ok {
		return t
	}
	return time.Time{}
}

func (w *ResponseWriterWrapper) extractRequestBody(c *gin.Context) []byte {
	if body := extractBodyOverride(c, requestBodyOverrideContextKey); len(body) > 0 {
		return body
	}
	if w.requestInfo == nil {
		return nil
	}
	if len(w.requestInfo.Body) > 0 {
		return w.requestInfo.Body
	}
	if w.requestInfo.deferredBodyCapture == nil {
		return nil
	}
	body, statusMarker, errRead := w.requestInfo.deferredBodyCapture.Bytes()
	if errRead != nil {
		log.WithError(errRead).Warn("failed to read deferred request body capture")
		return nil
	}
	encoding := ""
	for key, values := range w.requestInfo.Headers {
		if strings.EqualFold(key, "Content-Encoding") && len(values) > 0 {
			encoding = values[0]
			break
		}
	}
	body = decodeCapturedRequestBodyForLogWithLimit(body, encoding, maxDeferredErrorRequestBodyBytes)
	if statusMarker == "" {
		return body
	}
	if len(body) > 0 && !bytes.HasSuffix(body, []byte("\n")) {
		body = append(body, '\n')
	}
	return append(body, statusMarker...)
}

func (w *ResponseWriterWrapper) extractResponseBody(c *gin.Context) []byte {
	if body := extractBodyOverride(c, responseBodyOverrideContextKey); len(body) > 0 {
		return body
	}
	if w.body == nil || w.body.Len() == 0 {
		return nil
	}
	return bytes.Clone(w.body.Bytes())
}

func (w *ResponseWriterWrapper) extractWebsocketTimeline(c *gin.Context) []byte {
	return extractBodyOverride(c, websocketTimelineOverrideContextKey)
}

func (w *ResponseWriterWrapper) extractWebsocketTimelineSource(c *gin.Context) *logging.FileBodySource {
	return extractFileBodySource(c, logging.WebsocketTimelineSourceContextKey)
}

func extractFileBodySource(c *gin.Context, key string) *logging.FileBodySource {
	if c == nil {
		return nil
	}
	value, exists := c.Get(key)
	if !exists {
		return nil
	}
	source, ok := value.(*logging.FileBodySource)
	if !ok || source == nil {
		return nil
	}
	return source
}

func extractBodyOverride(c *gin.Context, key string) []byte {
	if c == nil {
		return nil
	}
	bodyOverride, isExist := c.Get(key)
	if !isExist {
		return nil
	}
	switch value := bodyOverride.(type) {
	case []byte:
		if len(value) > 0 {
			return bytes.Clone(value)
		}
	case string:
		if strings.TrimSpace(value) != "" {
			return []byte(value)
		}
	}
	return nil
}

func (w *ResponseWriterWrapper) logRequest(requestBody []byte, statusCode int, headers map[string][]string, body, websocketTimeline []byte, websocketTimelineSource *logging.FileBodySource, apiRequestBody []byte, apiRequestSource *logging.FileBodySource, apiResponseBody []byte, apiResponseSource *logging.FileBodySource, apiWebsocketTimeline []byte, apiWebsocketTimelineSource *logging.FileBodySource, apiResponseTimestamp time.Time, apiResponseErrors []*interfaces.ErrorMessage, forceLog bool) error {
	if w.requestInfo == nil {
		cleanupFileBodySources(websocketTimelineSource, apiRequestSource, apiResponseSource, apiWebsocketTimelineSource)
		return nil
	}

	if loggerWithAllSources, ok := w.logger.(interface {
		LogRequestWithOptionsAndAllSources(string, string, map[string][]string, []byte, int, map[string][]string, []byte, []byte, *logging.FileBodySource, []byte, *logging.FileBodySource, []byte, *logging.FileBodySource, []byte, *logging.FileBodySource, []*interfaces.ErrorMessage, bool, string, time.Time, time.Time) error
	}); ok {
		return loggerWithAllSources.LogRequestWithOptionsAndAllSources(
			w.requestInfo.URL,
			w.requestInfo.Method,
			w.requestInfo.Headers,
			requestBody,
			statusCode,
			headers,
			body,
			websocketTimeline,
			websocketTimelineSource,
			apiRequestBody,
			apiRequestSource,
			apiResponseBody,
			apiResponseSource,
			apiWebsocketTimeline,
			apiWebsocketTimelineSource,
			apiResponseErrors,
			forceLog,
			w.requestInfo.RequestID,
			w.requestInfo.Timestamp,
			apiResponseTimestamp,
		)
	}

	if loggerWithSources, ok := w.logger.(interface {
		LogRequestWithOptionsAndSources(string, string, map[string][]string, []byte, int, map[string][]string, []byte, []byte, *logging.FileBodySource, []byte, []byte, []byte, *logging.FileBodySource, []*interfaces.ErrorMessage, bool, string, time.Time, time.Time) error
	}); ok {
		var errMerge error
		apiRequestBody, errMerge = mergeFileBodySource(apiRequestBody, apiRequestSource)
		if errMerge != nil {
			cleanupFileBodySources(websocketTimelineSource, apiResponseSource, apiWebsocketTimelineSource)
			return errMerge
		}
		apiResponseBody, errMerge = mergeFileBodySource(apiResponseBody, apiResponseSource)
		if errMerge != nil {
			cleanupFileBodySources(websocketTimelineSource, apiWebsocketTimelineSource)
			return errMerge
		}
		return loggerWithSources.LogRequestWithOptionsAndSources(
			w.requestInfo.URL,
			w.requestInfo.Method,
			w.requestInfo.Headers,
			requestBody,
			statusCode,
			headers,
			body,
			websocketTimeline,
			websocketTimelineSource,
			apiRequestBody,
			apiResponseBody,
			apiWebsocketTimeline,
			apiWebsocketTimelineSource,
			apiResponseErrors,
			forceLog,
			w.requestInfo.RequestID,
			w.requestInfo.Timestamp,
			apiResponseTimestamp,
		)
	}

	var errMerge error
	websocketTimeline, errMerge = mergeFileBodySource(websocketTimeline, websocketTimelineSource)
	if errMerge != nil {
		cleanupFileBodySources(apiRequestSource, apiResponseSource, apiWebsocketTimelineSource)
		return errMerge
	}
	apiRequestBody, errMerge = mergeFileBodySource(apiRequestBody, apiRequestSource)
	if errMerge != nil {
		cleanupFileBodySources(apiResponseSource, apiWebsocketTimelineSource)
		return errMerge
	}
	apiResponseBody, errMerge = mergeFileBodySource(apiResponseBody, apiResponseSource)
	if errMerge != nil {
		cleanupFileBodySources(apiWebsocketTimelineSource)
		return errMerge
	}
	apiWebsocketTimeline, errMerge = mergeFileBodySource(apiWebsocketTimeline, apiWebsocketTimelineSource)
	if errMerge != nil {
		return errMerge
	}

	if loggerWithOptions, ok := w.logger.(interface {
		LogRequestWithOptions(string, string, map[string][]string, []byte, int, map[string][]string, []byte, []byte, []byte, []byte, []byte, []*interfaces.ErrorMessage, bool, string, time.Time, time.Time) error
	}); ok {
		return loggerWithOptions.LogRequestWithOptions(
			w.requestInfo.URL,
			w.requestInfo.Method,
			w.requestInfo.Headers,
			requestBody,
			statusCode,
			headers,
			body,
			websocketTimeline,
			apiRequestBody,
			apiResponseBody,
			apiWebsocketTimeline,
			apiResponseErrors,
			forceLog,
			w.requestInfo.RequestID,
			w.requestInfo.Timestamp,
			apiResponseTimestamp,
		)
	}

	return w.logger.LogRequest(
		w.requestInfo.URL,
		w.requestInfo.Method,
		w.requestInfo.Headers,
		requestBody,
		statusCode,
		headers,
		body,
		websocketTimeline,
		apiRequestBody,
		apiResponseBody,
		apiWebsocketTimeline,
		apiResponseErrors,
		w.requestInfo.RequestID,
		w.requestInfo.Timestamp,
		apiResponseTimestamp,
	)
}

func mergeFileBodySource(payload []byte, source *logging.FileBodySource) ([]byte, error) {
	if source == nil {
		return payload, nil
	}
	defer cleanupFileBodySources(source)
	if !source.HasPayload() {
		return payload, nil
	}
	var buf bytes.Buffer
	if len(payload) > 0 {
		buf.Write(payload)
		if !bytes.HasSuffix(payload, []byte("\n")) {
			buf.WriteByte('\n')
		}
		buf.WriteByte('\n')
	}
	if _, errWrite := source.WriteTo(&buf); errWrite != nil {
		return nil, errWrite
	}
	return buf.Bytes(), nil
}

func (w *ResponseWriterWrapper) discardStreamingLogWriter() error {
	if w == nil || w.streamWriter == nil {
		return nil
	}
	if w.chunkChannel != nil {
		close(w.chunkChannel)
		w.chunkChannel = nil
	}
	if w.streamDone != nil {
		<-w.streamDone
		w.streamDone = nil
	}
	streamWriter := w.streamWriter
	w.streamWriter = nil
	if discarder, ok := streamWriter.(interface{ Discard() error }); ok {
		return discarder.Discard()
	}
	return streamWriter.Close()
}

func cleanupFileBodySources(sources ...*logging.FileBodySource) {
	for _, source := range sources {
		if source == nil {
			continue
		}
		if errCleanup := source.Cleanup(); errCleanup != nil {
			log.WithError(errCleanup).Warn("failed to clean up log part files")
		}
	}
}

func isClientCancellationErrorMessage(errMsg *interfaces.ErrorMessage) bool {
	if errMsg == nil {
		return true
	}
	return clienterror.IsClientCancellation(errMsg.StatusCode, errMsg.Error)
}

func hasActionableAPIResponseErrors(apiErrors []*interfaces.ErrorMessage) bool {
	for _, err := range apiErrors {
		if !isClientCancellationErrorMessage(err) {
			return true
		}
	}
	return false
}

func extractAPIResponseErrors(c *gin.Context) []*interfaces.ErrorMessage {
	if c == nil {
		return nil
	}
	if apiResponseError, exists := c.Get("API_RESPONSE_ERROR"); exists {
		if apiErrors, ok := apiResponseError.([]*interfaces.ErrorMessage); ok {
			return apiErrors
		}
	}
	return nil
}

func actionableAPIResponseErrorStatus(apiErrors []*interfaces.ErrorMessage) int {
	for i := len(apiErrors) - 1; i >= 0; i-- {
		errMsg := apiErrors[i]
		if isClientCancellationErrorMessage(errMsg) {
			continue
		}
		if errMsg.StatusCode >= http.StatusBadRequest && errMsg.StatusCode != clienterror.StatusClientClosedRequest {
			return errMsg.StatusCode
		}
		if status := clienterror.HTTPStatusFromError(errMsg.Error); status >= http.StatusBadRequest && status != clienterror.StatusClientClosedRequest {
			return status
		}
	}
	return 0
}

func isContextCanceled(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	ctx := c.Request.Context()
	return ctx != nil && errors.Is(ctx.Err(), context.Canceled)
}

func isUnroutedNotFound(c *gin.Context, statusCode int, apiErrors []*interfaces.ErrorMessage) bool {
	if statusCode != http.StatusNotFound {
		return false
	}
	if hasActionableAPIResponseErrors(apiErrors) {
		return false
	}
	if c == nil || c.FullPath() != "" {
		return false
	}
	if hasRecordedUpstreamActivity(c) {
		return false
	}
	return true
}

func hasRecordedUpstreamActivity(c *gin.Context) bool {
	if c == nil {
		return false
	}
	for _, key := range []string{"API_REQUEST", "API_RESPONSE", "API_WEBSOCKET_TIMELINE"} {
		if value, exists := c.Get(key); exists {
			if data, ok := value.([]byte); ok && len(data) > 0 {
				return true
			}
		}
	}
	if value, exists := c.Get(logging.DeferredAPIRequestContextKey); exists {
		if requests, ok := value.([]logging.DeferredAPIRequest); ok && len(requests) > 0 {
			return true
		}
	}
	if value, exists := c.Get(logging.DeferredAPIResponseContextKey); exists {
		if capture, ok := value.(*logging.DeferredAPICapture); ok && capture != nil && capture.HasRequests() {
			return true
		}
		if response, ok := value.(logging.DeferredAPIResponse); ok && response != nil && response.HasPayload() {
			return true
		}
	}
	for _, key := range []string{
		logging.APIRequestSourceContextKey,
		logging.APIResponseSourceContextKey,
		logging.APIWebsocketTimelineSourceContextKey,
	} {
		if source := extractFileBodySource(c, key); source != nil && source.HasPayload() {
			return true
		}
	}
	return false
}

func hasActionableError(c *gin.Context, statusCode int, apiErrors []*interfaces.ErrorMessage) bool {
	if hasActionableAPIResponseErrors(apiErrors) {
		return true
	}
	if statusCode == clienterror.StatusClientClosedRequest {
		return false
	}
	if isUnroutedNotFound(c, statusCode, apiErrors) {
		return false
	}
	if isContextCanceled(c) && statusCode < http.StatusBadRequest {
		return false
	}
	return statusCode >= http.StatusBadRequest
}
