package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	coresession "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/session"
	coreusage "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/httptransport"
	"golang.org/x/net/context"
)

type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Message string `json:"message"`

	Type string `json:"type"`

	Code string `json:"code,omitempty"`

	Retryable *bool `json:"retryable,omitempty"`
}

const idempotencyKeyMetadataKey = "idempotency_key"

const (
	defaultStreamingKeepAliveSeconds = 0
	defaultStreamingBootstrapRetries = 0

	maxStreamInterceptorHistoryChunks = 64
	maxStreamInterceptorHistoryBytes  = 1 << 20
)

func BuildErrorResponseBody(status int, errText string) []byte {
	return BuildErrorResponseBodyWithError(status, errText, nil)
}

func BuildErrorResponseBodyWithError(status int, errText string, err error) []byte {
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	if strings.TrimSpace(errText) == "" {
		errText = http.StatusText(status)
	}

	trimmed := strings.TrimSpace(errText)

	if coreauth.IsTerminalAuthError(err) {
		message := errText
		if trimmed != "" && json.Valid([]byte(trimmed)) {
			var parsed map[string]any
			if errUnmarshal := json.Unmarshal([]byte(trimmed), &parsed); errUnmarshal == nil {
				if msg, ok := parsed["message"].(string); ok && msg != "" {
					message = msg
				} else if errMap, ok := parsed["error"].(map[string]any); ok {
					if msg, ok := errMap["message"].(string); ok && msg != "" {
						message = msg
					}
				}
			}
		}
		r := false
		payload, errMarshal := json.Marshal(ErrorResponse{
			Error: ErrorDetail{
				Message:   message,
				Type:      "authentication_error",
				Code:      "upstream_authentication_required",
				Retryable: &r,
			},
		})
		if errMarshal != nil {
			return []byte(fmt.Sprintf(`{"error":{"message":%q,"type":"authentication_error","code":"upstream_authentication_required","retryable":false}}`, message))
		}
		return payload
	}

	if trimmed != "" && json.Valid([]byte(trimmed)) {
		var compact bytes.Buffer
		if errCompact := json.Compact(&compact, []byte(trimmed)); errCompact == nil {
			return compact.Bytes()
		}
		return []byte(trimmed)
	}

	errType := "invalid_request_error"
	var code string
	switch status {
	case http.StatusUnauthorized:
		errType = "authentication_error"
		code = "invalid_api_key"
	case http.StatusForbidden:
		errType = "permission_error"
		code = "insufficient_quota"
	case http.StatusTooManyRequests:
		errType = "rate_limit_error"
		code = "rate_limit_exceeded"
	case http.StatusNotFound:
		errType = "invalid_request_error"
		code = "model_not_found"
	case http.StatusRequestTimeout:
		errType = "server_error"
		code = "request_timeout"
	default:
		if status >= http.StatusInternalServerError {
			errType = "server_error"
			code = "internal_server_error"
		}
	}

	payload, errMarshal := json.Marshal(ErrorResponse{
		Error: ErrorDetail{
			Message: errText,
			Type:    errType,
			Code:    code,
		},
	})
	if errMarshal != nil {
		return []byte(fmt.Sprintf(`{"error":{"message":%q,"type":"server_error","code":"internal_server_error"}}`, errText))
	}
	return payload
}

var streamingKeepAliveTickerFactory func(interval time.Duration) (<-chan time.Time, func())
var streamingKeepAliveTickerWithResetFactory func(interval time.Duration) (<-chan time.Time, func(), func())

func defaultStreamingKeepAliveTickerWithReset(interval time.Duration) (<-chan time.Time, func(), func()) {
	ticker := time.NewTicker(interval)
	reset := func() {
		ticker.Reset(interval)
		select {
		case <-ticker.C:
		default:
		}
	}
	return ticker.C, ticker.Stop, reset
}

func StreamingKeepAliveInterval(cfg *config.SDKConfig) time.Duration {
	seconds := defaultStreamingKeepAliveSeconds
	if cfg != nil {
		seconds = cfg.Streaming.KeepAliveSeconds
	}
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func StreamingSendKeepAliveBeforeFT(cfg *config.SDKConfig) bool {
	return cfg != nil && cfg.Streaming.SendKeepAliveBeforeFT && StreamingKeepAliveInterval(cfg) > 0
}

func NewStreamingKeepAliveTickerWithReset(interval time.Duration) (<-chan time.Time, func(), func()) {
	if interval <= 0 {
		return nil, func() {}, func() {}
	}
	if streamingKeepAliveTickerWithResetFactory != nil {
		return streamingKeepAliveTickerWithResetFactory(interval)
	}
	if streamingKeepAliveTickerFactory != nil {
		keepAliveC, stop := streamingKeepAliveTickerFactory(interval)
		return keepAliveC, stop, func() {}
	}
	return defaultStreamingKeepAliveTickerWithReset(interval)
}

func NewStreamingKeepAliveTicker(interval time.Duration) (<-chan time.Time, func()) {
	keepAliveC, stop, _ := NewStreamingKeepAliveTickerWithReset(interval)
	return keepAliveC, stop
}

func NewPreFirstTokenKeepAliveTicker(cfg *config.SDKConfig) (<-chan time.Time, func()) {
	if !StreamingSendKeepAliveBeforeFT(cfg) {
		return nil, func() {}
	}
	return NewStreamingKeepAliveTicker(StreamingKeepAliveInterval(cfg))
}

func SetStreamingKeepAliveTickerFactoryForTest(factory func(interval time.Duration) (<-chan time.Time, func())) func() {
	prev := streamingKeepAliveTickerFactory
	if factory != nil {
		streamingKeepAliveTickerFactory = factory
	}
	return func() {
		streamingKeepAliveTickerFactory = prev
	}
}

func SetStreamingKeepAliveTickerWithResetFactoryForTest(factory func(interval time.Duration) (<-chan time.Time, func(), func())) func() {
	prev := streamingKeepAliveTickerWithResetFactory
	if factory != nil {
		streamingKeepAliveTickerWithResetFactory = factory
	}
	return func() {
		streamingKeepAliveTickerWithResetFactory = prev
	}
}

func NonStreamingKeepAliveInterval(cfg *config.SDKConfig) time.Duration {
	seconds := 0
	if cfg != nil {
		seconds = cfg.NonStreamKeepAliveInterval
	}
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func StreamingBootstrapRetries(cfg *config.SDKConfig) int {
	retries := defaultStreamingBootstrapRetries
	if cfg != nil {
		retries = cfg.Streaming.BootstrapRetries
	}
	if retries < 0 {
		retries = 0
	}
	return retries
}

func PassthroughHeadersEnabled(cfg *config.SDKConfig) bool {
	return cfg != nil && cfg.PassthroughHeaders
}

func executionPassthroughHeaders(cfg *config.SDKConfig, internal bool) bool {
	return internal || PassthroughHeadersEnabled(cfg)
}

func requestExecutionMetadata(ctx context.Context) map[string]any {

	key := ""
	requestPath := ""
	var ginCtx *gin.Context
	if ctx != nil {
		if requestGinCtx, ok := ctx.Value("gin").(*gin.Context); ok && requestGinCtx != nil && requestGinCtx.Request != nil {
			ginCtx = requestGinCtx
			key = strings.TrimSpace(ginCtx.GetHeader("Idempotency-Key"))
			requestPath = strings.TrimSpace(ginCtx.FullPath())
			if requestPath == "" && ginCtx.Request.URL != nil {
				requestPath = strings.TrimSpace(ginCtx.Request.URL.Path)
			}
		}
	}

	meta := make(map[string]any)
	if key != "" {
		meta[idempotencyKeyMetadataKey] = key
	}
	if requestPath != "" {
		meta[coreexecutor.RequestPathMetadataKey] = requestPath
	}
	if pinnedAuthID := pinnedAuthIDFromContext(ctx); pinnedAuthID != "" {
		meta[coreexecutor.PinnedAuthMetadataKey] = pinnedAuthID
	}
	if selectedCallback := selectedAuthIDCallbackFromContext(ctx); selectedCallback != nil {
		meta[coreexecutor.SelectedAuthCallbackMetadataKey] = selectedCallback
	}
	if finalizedFormatCallback := finalizedResponseFormatCallbackFromContext(ctx); finalizedFormatCallback != nil {
		meta[coreexecutor.FinalizedResponseFormatCallbackMetadataKey] = finalizedFormatCallback
	}
	if ginCtx != nil && !websocket.IsWebSocketUpgrade(ginCtx.Request) {
		if traceCallback := logging.GinCPATraceIDCallback(ginCtx); traceCallback != nil {
			meta[coreexecutor.SelectedAuthIndexCallbackMetadataKey] = traceCallback
		}
	}
	if executionSessionID := executionSessionIDFromContext(ctx); executionSessionID != "" {
		meta[coreexecutor.ExecutionSessionMetadataKey] = executionSessionID
	}
	if callerScope := requestCallerScope(ginCtx); callerScope != "" {
		meta[coreexecutor.CallerScopeMetadataKey] = callerScope
	}
	if disallowFreeAuthFromContext(ctx) {
		meta[coreexecutor.DisallowFreeAuthMetadataKey] = true
	}
	return meta
}

func requestClientIP(request *http.Request) string {
	if request == nil {
		return ""
	}
	remoteAddr := strings.TrimSpace(request.RemoteAddr)
	if host, _, errSplit := net.SplitHostPort(remoteAddr); errSplit == nil {
		return strings.TrimSpace(host)
	}
	return remoteAddr
}

func extractSessionIDsFromRequest(request *http.Request) (string, string) {
	if request == nil || request.Header == nil {
		return "", ""
	}
	if info, ok := coresession.ExtractSessionInfo(request.Header, nil, nil); ok {
		return info.SessionID, info.ParentSessionID
	}
	return "", ""
}

func EnrichContextWithSessionHierarchy(ctx context.Context, headers http.Header, payload []byte, metadata map[string]any) context.Context {
	meta := logging.GetClientRequestMetadata(ctx)
	if info, ok := coresession.ExtractSessionInfo(headers, payload, metadata); ok {
		meta.SessionID = info.SessionID
		meta.ParentSessionID = info.ParentSessionID
		if meta.SessionID != "" && meta.SessionID == meta.ParentSessionID {
			meta.ParentSessionID = ""
		}
		ctx = logging.WithClientRequestMetadata(ctx, meta)
		return util.WithSessionID(ctx, meta.SessionID)
	}
	if meta.SessionID != "" || meta.ParentSessionID != "" || util.SessionIDFromContext(ctx) != "" {
		meta.SessionID = ""
		meta.ParentSessionID = ""
		ctx = logging.WithClientRequestMetadata(ctx, meta)
		return util.WithSessionID(ctx, "")
	}
	return ctx
}

func enrichContextWithSessionHierarchy(ctx context.Context, headers http.Header, payload []byte, metadata map[string]any) context.Context {
	return EnrichContextWithSessionHierarchy(ctx, headers, payload, metadata)
}

func requestCallerScope(ginCtx *gin.Context) string {
	if ginCtx == nil {
		return ""
	}
	value, exists := ginCtx.Get("userApiKey")
	if !exists || value == nil {
		return ""
	}
	return coresession.CallerScope(fmt.Sprint(value))
}

func addCallerScopeMetadata(meta map[string]any, callerScope string) {
	if meta == nil {
		return
	}
	callerScope = strings.TrimSpace(callerScope)
	if callerScope == "" {
		return
	}
	meta[coreexecutor.CallerScopeMetadataKey] = callerScope
}

func callerScopeFromMetadata(meta map[string]any) string {
	if len(meta) == 0 {
		return ""
	}
	value, ok := meta[coreexecutor.CallerScopeMetadataKey].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func addAuthSelectionModelMetadata(meta map[string]any, model string) {
	if meta == nil {
		return
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	meta[coreexecutor.AuthSelectionModelMetadataKey] = model
}

func setReasoningEffortMetadata(meta map[string]any, handlerType, model string, rawJSON []byte) {
	if meta == nil {
		return
	}
	effort := thinking.ExtractReasoningEffort(rawJSON, handlerType, model)
	if effort == "" {
		return
	}
	meta[coreexecutor.ReasoningEffortMetadataKey] = effort
}

func setServiceTierMetadata(meta map[string]any, rawJSON []byte) {
	if meta == nil {
		return
	}
	serviceTier := coreusage.AutoServiceTier
	node := ojson.GetBytesNoCopy(rawJSON, "service_tier")
	if node.Exists() {
		value := strings.TrimSpace(node.String())
		if value != "" {
			serviceTier = strings.Clone(value)
		}
	}
	meta[coreexecutor.ServiceTierMetadataKey] = serviceTier
}

func setGenerateMetadata(meta map[string]any, rawJSON []byte) {
	if meta == nil {
		return
	}

	generate := true
	node := ojson.GetBytesNoCopy(rawJSON, "generate")
	if node.Exists() && node.IsBool() && !node.Bool() {
		generate = false
	}
	meta[coreexecutor.GenerateMetadataKey] = generate
}

type BaseAPIHandler struct {
	runtimeMu sync.RWMutex

	AuthManager *coreauth.Manager

	Cfg *config.SDKConfig

	PluginHost PluginInterceptorHost

	ModelRouterHost PluginModelRouterHost
}

func NewBaseAPIHandlers(cfg *config.SDKConfig, authManager *coreauth.Manager) *BaseAPIHandler {
	return &BaseAPIHandler{
		Cfg:         cfg,
		AuthManager: authManager,
	}
}

func (h *BaseAPIHandler) UpdateClients(cfg *config.SDKConfig) {
	if h == nil {
		return
	}
	h.runtimeMu.Lock()
	h.Cfg = cfg
	h.runtimeMu.Unlock()
}

func (h *BaseAPIHandler) CurrentConfig() *config.SDKConfig {
	if h == nil {
		return nil
	}
	h.runtimeMu.RLock()
	cfg := h.Cfg
	h.runtimeMu.RUnlock()
	return cfg
}

func (h *BaseAPIHandler) SetPluginHost(host PluginInterceptorHost) {
	if h == nil {
		return
	}
	if isNilPluginInterceptorHost(host) {
		host = nil
	}
	h.runtimeMu.Lock()
	h.PluginHost = host
	h.runtimeMu.Unlock()
}

func (h *BaseAPIHandler) SetModelRouterHost(host PluginModelRouterHost) {
	if h == nil {
		return
	}
	if isNilPluginModelRouterHost(host) {
		host = nil
	}
	h.runtimeMu.Lock()
	h.ModelRouterHost = host
	h.runtimeMu.Unlock()
}

func (h *BaseAPIHandler) ModelRouterHostSnapshot() PluginModelRouterHost {
	if h == nil {
		return nil
	}
	h.runtimeMu.RLock()
	host := h.ModelRouterHost
	h.runtimeMu.RUnlock()
	if isNilPluginModelRouterHost(host) {
		return nil
	}
	return host
}

func isNilPluginInterceptorHost(host PluginInterceptorHost) bool {
	return isNilInterface(host)
}

func isNilPluginModelRouterHost(host PluginModelRouterHost) bool {
	return isNilInterface(host)
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}

	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func (h *BaseAPIHandler) GetAlt(c *gin.Context) string {
	var alt string
	var hasAlt bool
	alt, hasAlt = c.GetQuery("alt")
	if !hasAlt {
		alt, _ = c.GetQuery("$alt")
	}
	if alt == "sse" {
		return ""
	}
	return alt
}

func (h *BaseAPIHandler) GetContextWithCancel(handler interfaces.APIHandler, c *gin.Context, ctx context.Context) (context.Context, APIHandlerCancelFunc) {
	parentCtx := ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	var requestCtx context.Context
	if c != nil && c.Request != nil {
		requestCtx = c.Request.Context()
	}

	if requestCtx != nil {
		parentCtx = httptransport.InheritDecorator(parentCtx, requestCtx)
		parentCtx = logging.InheritDeferredCapture(parentCtx, requestCtx)
	}
	if c != nil && logging.DeferredAPICaptureFromContext(parentCtx) == nil && !logging.DeferredAPIRequestCaptureDisabled(parentCtx) {
		if value, exists := c.Get(logging.DeferredAPIResponseContextKey); exists {
			if capture, ok := value.(*logging.DeferredAPICapture); ok && capture != nil {
				parentCtx = logging.WithDeferredAPICapture(parentCtx, capture)
			}
		}
	}
	if requestCtx != nil && logging.GetRequestID(parentCtx) == "" {
		if requestID := logging.GetRequestID(requestCtx); requestID != "" {
			parentCtx = logging.WithRequestID(parentCtx, requestID)
		} else if requestID = logging.GetGinRequestID(c); requestID != "" {
			parentCtx = logging.WithRequestID(parentCtx, requestID)
		}
	}
	newCtx, cancel := context.WithCancel(parentCtx)

	endpoint := ""
	if c != nil && c.Request != nil {
		path := strings.TrimSpace(c.FullPath())
		if path == "" && c.Request.URL != nil {
			path = strings.TrimSpace(c.Request.URL.Path)
		}
		if path != "" {
			method := strings.TrimSpace(c.Request.Method)
			if method != "" {
				endpoint = method + " " + path
			} else {
				endpoint = path
			}
		}
	}
	if endpoint != "" {
		newCtx = logging.WithEndpoint(newCtx, endpoint)
	}
	if c != nil && c.Request != nil {
		sessionID, parentSessionID := extractSessionIDsFromRequest(c.Request)
		newCtx = logging.WithClientRequestMetadata(newCtx, logging.ClientRequestMetadata{
			ClientIP:         requestClientIP(c.Request),
			ResolvedClientIP: strings.TrimSpace(c.ClientIP()),
			XForwardedFor:    strings.TrimSpace(strings.Join(c.Request.Header.Values("X-Forwarded-For"), ", ")),
			UserAgent:        strings.TrimSpace(c.Request.UserAgent()),
			SessionID:        sessionID,
			ParentSessionID:  parentSessionID,
		})
	}
	newCtx = logging.WithResponseStatusHolder(newCtx)
	newCtx = logging.WithResponseHeadersHolder(newCtx)

	cancelCtx := newCtx
	if requestCtx != nil && requestCtx != parentCtx {
		go func() {
			select {
			case <-requestCtx.Done():
				cancel()
			case <-cancelCtx.Done():
			}
		}()
	}
	newCtx = context.WithValue(newCtx, "gin", c)
	newCtx = coreusage.CaptureIdentity(newCtx)
	newCtx = coreauth.WithCodexClientRequest(newCtx, coreauth.IsCodexClientRequest(newCtx))
	newCtx = context.WithValue(newCtx, "handler", handler)
	return newCtx, func(params ...interface{}) {
		if c != nil {
			logging.SetResponseStatus(cancelCtx, c.Writer.Status())
		}
		if cfg := h.CurrentConfig(); cfg != nil && cfg.RequestLog && len(params) == 1 {
			if captured, exists := c.Get(logging.APIResponseCapturedContextKey); exists {
				if capturedBool, ok := captured.(bool); ok && capturedBool {
					cancel()
					return
				}
			}
			if existing, exists := c.Get("API_RESPONSE"); exists {
				if existingBytes, ok := existing.([]byte); ok && len(bytes.TrimSpace(existingBytes)) > 0 {
					switch params[0].(type) {
					case error, string:
						cancel()
						return
					}
				}
			}

			var payload []byte
			switch data := params[0].(type) {
			case []byte:
				payload = data
			case error:
				if data != nil {
					payload = []byte(data.Error())
				}
			case string:
				payload = []byte(data)
			}
			if len(payload) > 0 {
				if existing, exists := c.Get("API_RESPONSE"); exists {
					if existingBytes, ok := existing.([]byte); ok && len(existingBytes) > 0 {
						trimmedPayload := bytes.TrimSpace(payload)
						if len(trimmedPayload) > 0 && bytes.Contains(existingBytes, trimmedPayload) {
							cancel()
							return
						}
					}
				}
				appendAPIResponse(c, payload)
			}
		}

		cancel()
	}
}

func (h *BaseAPIHandler) StartNonStreamingKeepAlive(c *gin.Context, ctx context.Context) func() {
	if h == nil || c == nil {
		return func() {}
	}
	interval := NonStreamingKeepAliveInterval(h.CurrentConfig())
	if interval <= 0 {
		return func() {}
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	stopChan := make(chan struct{})
	var stopOnce sync.Once
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stopChan:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = c.Writer.Write([]byte("\n"))
				flusher.Flush()
			}
		}
	}()

	return func() {
		stopOnce.Do(func() {
			close(stopChan)
		})
		wg.Wait()
	}
}

func appendAPIResponse(c *gin.Context, data []byte) {
	if c == nil || len(data) == 0 {
		return
	}

	if _, exists := c.Get("API_RESPONSE_TIMESTAMP"); !exists {
		c.Set("API_RESPONSE_TIMESTAMP", time.Now())
	}

	if existing, exists := c.Get("API_RESPONSE"); exists {
		if existingBytes, ok := existing.([]byte); ok && len(existingBytes) > 0 {
			combined := make([]byte, 0, util.ByteCapacity(len(existingBytes), len(data), 1))
			combined = append(combined, existingBytes...)
			if existingBytes[len(existingBytes)-1] != '\n' {
				combined = append(combined, '\n')
			}
			combined = append(combined, data...)
			c.Set("API_RESPONSE", combined)
			return
		}
	}

	c.Set("API_RESPONSE", bytes.Clone(data))
}

type APIHandlerCancelFunc func(params ...interface{})
