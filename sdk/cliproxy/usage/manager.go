package usage

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	internallogging "github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	log "github.com/sirupsen/logrus"
)


const DefaultServiceTier = "default"




const AutoServiceTier = "auto"


type Record struct {

	RequestID string

	TraceID string

	SelectionID string

	HomeSelection *HomeSelection
	Provider      string

	BaseURL string

	ExecutorType string
	Model        string
	Alias        string
	APIKey       string

	IsNativeKey bool

	AccessProvider  string
	SessionID       string
	ParentSessionID string
	AuthID          string
	AuthIndex       string

	AccessTokenSHA256 string
	AuthType          string
	Source            string

	ReasoningEffort string

	ServiceTier string


	EffectiveServiceTier string


	RequestServiceTier string

	ResponseServiceTier string

	ResponseModel string



	Generate *bool

	Stream      bool
	RequestedAt time.Time
	Latency     time.Duration
	TTFT        time.Duration
	Failed      bool
	Fail        Failure
	Detail      Detail

	ResponseHeaders http.Header
}



const FailureStreamTruncatedCode = "stream_truncated"



const FailureRequestScopedCode = "request_scoped"



const FailureForceCooldownCode = "force_cooldown"


type Failure struct {
	StatusCode int
	Body       string



	Code string
}


type Detail struct {
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
	TokenBreakdown      TokenBreakdown
	ResponseServiceTier string

	CacheCreation5mTokens    *int64
	CacheCreation30mTokens   *int64
	CacheCreation1hTokens    *int64
	CacheCreation12hTokens   *int64
	TextInputTokens          *int64
	ImageInputTokens         *int64
	AudioInputTokens         *int64
	VideoInputTokens         *int64
	FileInputTokens          *int64
	CachedTextTokens         *int64
	CachedImageTokens        *int64
	CachedAudioTokens        *int64
	TextOutputTokens         *int64
	ImageOutputTokens        *int64
	AudioOutputTokens        *int64
	AcceptedPredictionTokens *int64
	RejectedPredictionTokens *int64
	CacheDetails             *CacheDetails
	WebSearchRequests        *int64
	ImageCount               *int64
	RequestedImageDetails    *ImageDetails
	ImageDetails             *ImageDetails
	MediaCounts              *MediaCounts
	ToolUsageDetails         *ToolUsageDetails
	Iterations               *[]IterationUsage
	CostDetails              *CostDetails
	Speed                    string
	InferenceGeo             string
	UpstreamServiceTier      string
}

type requestedModelAliasContextKey struct{}
type reasoningEffortContextKey struct{}
type serviceTierContextKey struct{}
type generateContextKey struct{}
type streamContextKey struct{}
type executionRequestIDContextKey struct{}
type executionTraceIDContextKey struct{}
type selectionIDContextKey struct{}
type apiKeyContextKey struct{}
type accessProviderContextKey struct{}
type isNativeKeyContextKey struct{}
type capturedIdentityContextKey struct{}

type capturedIdentityHolder struct {
	mu             sync.RWMutex
	apiKey         string
	accessProvider string
	isNativeKey    bool
	hasNativeKey   bool
}


func WithExecutionRequestID(ctx context.Context, requestID string) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithValue(ctx, executionRequestIDContextKey{}, strings.TrimSpace(requestID))
}


func ExecutionRequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(executionRequestIDContextKey{}).(string); ok {
		return v
	}
	return ""
}


func WithTraceID(ctx context.Context, traceID string) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithValue(ctx, executionTraceIDContextKey{}, strings.TrimSpace(traceID))
}


func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(executionTraceIDContextKey{}).(string); ok {
		return v
	}
	return ""
}


func WithSelectionID(ctx context.Context, selectionID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, selectionIDContextKey{}, strings.TrimSpace(selectionID))
}


func SelectionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	raw := ctx.Value(selectionIDContextKey{})
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		return ""
	}
}


func WithRequestedModelAlias(ctx context.Context, alias string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return ctx
	}
	return context.WithValue(ctx, requestedModelAliasContextKey{}, alias)
}


func RequestedModelAliasFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	raw := ctx.Value(requestedModelAliasContextKey{})
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		return ""
	}
}


func WithReasoningEffort(ctx context.Context, effort string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	effort = strings.TrimSpace(effort)
	if effort == "" {
		return ctx
	}
	return context.WithValue(ctx, reasoningEffortContextKey{}, effort)
}


func ReasoningEffortFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	raw := ctx.Value(reasoningEffortContextKey{})
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		return ""
	}
}


func WithServiceTier(ctx context.Context, tier string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	tier = strings.TrimSpace(tier)
	if tier == "" {
		tier = DefaultServiceTier
	}
	return context.WithValue(ctx, serviceTierContextKey{}, tier)
}


func ServiceTierFromContext(ctx context.Context) string {
	if ctx == nil {
		return DefaultServiceTier
	}
	raw := ctx.Value(serviceTierContextKey{})
	switch value := raw.(type) {
	case string:
		tier := strings.TrimSpace(value)
		if tier == "" {
			return DefaultServiceTier
		}
		return tier
	case []byte:
		tier := strings.TrimSpace(string(value))
		if tier == "" {
			return DefaultServiceTier
		}
		return tier
	default:
		return DefaultServiceTier
	}
}



func WithGenerate(ctx context.Context, generate bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, generateContextKey{}, generate)
}



func GenerateFromContext(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	raw := ctx.Value(generateContextKey{})
	switch value := raw.(type) {
	case bool:
		return value
	default:
		return true
	}
}


func WithStream(ctx context.Context, stream bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, streamContextKey{}, stream)
}



func StreamFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	raw := ctx.Value(streamContextKey{})
	switch value := raw.(type) {
	case bool:
		return value
	default:
		return false
	}
}


func WithAccessProvider(ctx context.Context, provider string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return ctx
	}
	ctx = context.WithValue(ctx, accessProviderContextKey{}, provider)
	if holder, ok := ctx.Value(capturedIdentityContextKey{}).(*capturedIdentityHolder); ok && holder != nil {
		holder.mu.RLock()
		cloned := &capturedIdentityHolder{
			apiKey:         holder.apiKey,
			accessProvider: provider,
			isNativeKey:    holder.isNativeKey,
			hasNativeKey:   holder.hasNativeKey,
		}
		holder.mu.RUnlock()
		ctx = context.WithValue(ctx, capturedIdentityContextKey{}, cloned)
	}
	return ctx
}


func WithAPIKey(ctx context.Context, apiKey string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return ctx
	}
	ctx = context.WithValue(ctx, apiKeyContextKey{}, apiKey)
	if holder, ok := ctx.Value(capturedIdentityContextKey{}).(*capturedIdentityHolder); ok && holder != nil {
		holder.mu.RLock()
		cloned := &capturedIdentityHolder{
			apiKey:         apiKey,
			accessProvider: holder.accessProvider,
			isNativeKey:    holder.isNativeKey,
			hasNativeKey:   holder.hasNativeKey,
		}
		holder.mu.RUnlock()
		ctx = context.WithValue(ctx, capturedIdentityContextKey{}, cloned)
	}
	return ctx
}



func WithCapturedIdentity(ctx context.Context, apiKey, accessProvider string, isNativeKey ...bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	holder := &capturedIdentityHolder{
		apiKey:         strings.TrimSpace(apiKey),
		accessProvider: strings.TrimSpace(accessProvider),
	}
	if len(isNativeKey) > 0 {
		holder.isNativeKey = isNativeKey[0]
		holder.hasNativeKey = true
	}
	return context.WithValue(ctx, capturedIdentityContextKey{}, holder)
}



func CaptureIdentity(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	if HasCapturedIdentity(ctx) {
		return ctx
	}
	apiKey := apiKeyFromGinContext(ctx)
	if apiKey == "" {
		apiKey = APIKeyFromContext(ctx)
	}
	accessProvider := AccessProviderFromContext(ctx)
	if isNative, ok := IsNativeKeyFromContext(ctx); ok {
		return WithCapturedIdentity(ctx, apiKey, accessProvider, isNative)
	}
	if accessProvider != "" {
		return WithCapturedIdentity(ctx, apiKey, accessProvider, IsNativeAccessProvider(accessProvider))
	}
	return WithCapturedIdentity(ctx, apiKey, accessProvider)
}


func HasCapturedIdentity(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	holder, ok := ctx.Value(capturedIdentityContextKey{}).(*capturedIdentityHolder)
	return ok && holder != nil
}


func SetCapturedAPIKey(ctx context.Context, apiKey string) {
	if ctx == nil {
		return
	}
	if holder, ok := ctx.Value(capturedIdentityContextKey{}).(*capturedIdentityHolder); ok && holder != nil {
		holder.mu.Lock()
		holder.apiKey = strings.TrimSpace(apiKey)
		holder.mu.Unlock()
	}
}

func contextValueGetter(ctx context.Context) interface{ Get(string) (any, bool) } {
	if ctx == nil {
		return nil
	}
	raw := ctx.Value("gin")
	if raw == nil {
		return nil
	}
	rv := reflect.ValueOf(raw)
	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return nil
	}
	getter, ok := raw.(interface{ Get(string) (any, bool) })
	if !ok {
		return nil
	}
	return getter
}

func apiKeyFromGinContext(ctx context.Context) string {
	if ginCtx := contextValueGetter(ctx); ginCtx != nil {
		if v, exists := ginCtx.Get("userApiKey"); exists {
			switch value := v.(type) {
			case string:
				return strings.TrimSpace(value)
			case fmt.Stringer:
				return strings.TrimSpace(value.String())
			default:
				return strings.TrimSpace(fmt.Sprintf("%v", value))
			}
		}
	}
	return ""
}


func APIKeyFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if holder, ok := ctx.Value(capturedIdentityContextKey{}).(*capturedIdentityHolder); ok && holder != nil {
		holder.mu.RLock()
		key := holder.apiKey
		holder.mu.RUnlock()
		return key
	}
	raw := ctx.Value(apiKeyContextKey{})
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	}
	return apiKeyFromGinContext(ctx)
}


func AccessProviderFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if holder, ok := ctx.Value(capturedIdentityContextKey{}).(*capturedIdentityHolder); ok && holder != nil {
		holder.mu.RLock()
		provider := holder.accessProvider
		holder.mu.RUnlock()
		return provider
	}
	raw := ctx.Value(accessProviderContextKey{})
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	}
	if ginCtx := contextValueGetter(ctx); ginCtx != nil {
		if v, exists := ginCtx.Get("accessProvider"); exists {
			switch value := v.(type) {
			case string:
				return strings.TrimSpace(value)
			case []byte:
				return strings.TrimSpace(string(value))
			}
		}
	}
	return ""
}


func WithIsNativeKey(ctx context.Context, isNative bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, isNativeKeyContextKey{}, isNative)
	if holder, ok := ctx.Value(capturedIdentityContextKey{}).(*capturedIdentityHolder); ok && holder != nil {
		holder.mu.RLock()
		cloned := &capturedIdentityHolder{
			apiKey:         holder.apiKey,
			accessProvider: holder.accessProvider,
			isNativeKey:    isNative,
			hasNativeKey:   true,
		}
		holder.mu.RUnlock()
		ctx = context.WithValue(ctx, capturedIdentityContextKey{}, cloned)
	}
	return ctx
}


func IsNativeKeyFromContext(ctx context.Context) (bool, bool) {
	if ctx == nil {
		return false, false
	}
	if holder, ok := ctx.Value(capturedIdentityContextKey{}).(*capturedIdentityHolder); ok && holder != nil {
		holder.mu.RLock()
		isNative, hasNative := holder.isNativeKey, holder.hasNativeKey
		holder.mu.RUnlock()
		if hasNative {
			return isNative, true
		}
	}
	raw := ctx.Value(isNativeKeyContextKey{})
	if value, ok := raw.(bool); ok {
		return value, true
	}
	return false, false
}


func IsNativeAccessProvider(provider string) bool {
	p := strings.TrimSpace(provider)
	return strings.EqualFold(p, "config-inline") || strings.EqualFold(p, "config-api-key")
}


func GenerateFlag(generate bool) *bool {
	return &generate
}



func GenerateEnabled(generate *bool) bool {
	if generate == nil {
		return true
	}
	return *generate
}


type Plugin interface {
	HandleUsage(ctx context.Context, record Record)
}

type queueItem struct {
	ctx    context.Context
	record Record
}


type Manager struct {
	once     sync.Once
	stopOnce sync.Once
	cancel   context.CancelFunc

	mu     sync.Mutex
	cond   *sync.Cond
	queue  []queueItem
	closed bool

	pluginsMu sync.RWMutex
	plugins   []Plugin
	named     map[string]int
}


func NewManager(buffer int) *Manager {
	m := &Manager{}
	m.cond = sync.NewCond(&m.mu)
	return m
}


func (m *Manager) Start(ctx context.Context) {
	if m == nil {
		return
	}
	m.once.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		var workerCtx context.Context
		workerCtx, m.cancel = context.WithCancel(ctx)
		go m.run(workerCtx)
	})
}


func (m *Manager) Stop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() {
		if m.cancel != nil {
			m.cancel()
		}
		m.mu.Lock()
		m.closed = true
		m.mu.Unlock()
		m.cond.Broadcast()
	})
}


func (m *Manager) Register(plugin Plugin) {
	if m == nil || plugin == nil {
		return
	}
	m.pluginsMu.Lock()
	m.plugins = append(m.plugins, plugin)
	m.pluginsMu.Unlock()
}


func (m *Manager) RegisterNamed(name string, plugin Plugin) {
	if m == nil || plugin == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	m.pluginsMu.Lock()
	if m.named == nil {
		m.named = make(map[string]int)
	}
	if index, exists := m.named[name]; exists && index >= 0 && index < len(m.plugins) {
		m.plugins[index] = plugin
		m.pluginsMu.Unlock()
		return
	}
	m.named[name] = len(m.plugins)
	m.plugins = append(m.plugins, plugin)
	m.pluginsMu.Unlock()
}



func (m *Manager) Publish(ctx context.Context, record Record) {
	if m == nil {
		return
	}
	if strings.TrimSpace(record.RequestID) == "" {
		if reqID := ExecutionRequestIDFromContext(ctx); reqID != "" {
			record.RequestID = reqID
		} else {
			record.RequestID = uuid.NewString()
		}
	}
	if strings.TrimSpace(record.TraceID) == "" {
		if trID := TraceIDFromContext(ctx); trID != "" {
			record.TraceID = trID
		} else if trID := internallogging.GetRequestID(ctx); trID != "" {
			record.TraceID = trID
		}
	}
	if record.HomeSelection == nil {
		record.HomeSelection = HomeSelectionFromContext(ctx)
	}
	if strings.TrimSpace(record.SelectionID) == "" {
		if selID := SelectionIDFromContext(ctx); selID != "" {
			record.SelectionID = selID
		}
	}
	if strings.TrimSpace(record.APIKey) == "" {
		if key := APIKeyFromContext(ctx); key != "" {
			record.APIKey = key
		}
	}
	if strings.TrimSpace(record.AccessProvider) == "" {
		if provider := AccessProviderFromContext(ctx); provider != "" {
			record.AccessProvider = provider
		}
	}
	if !record.IsNativeKey {
		if nativeExplicit, ok := IsNativeKeyFromContext(ctx); ok {
			record.IsNativeKey = nativeExplicit
		} else if record.AccessProvider != "" {
			record.IsNativeKey = IsNativeAccessProvider(record.AccessProvider)
		}
	}
	ctx = WithCapturedIdentity(ctx, record.APIKey, record.AccessProvider, record.IsNativeKey)

	m.Start(context.Background())
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.queue = append(m.queue, queueItem{ctx: ctx, record: record})
	m.mu.Unlock()
	m.cond.Signal()
}

func (m *Manager) run(ctx context.Context) {
	for {
		m.mu.Lock()
		for !m.closed && len(m.queue) == 0 {
			m.cond.Wait()
		}
		if len(m.queue) == 0 && m.closed {
			m.mu.Unlock()
			return
		}
		item := m.queue[0]
		m.queue[0] = queueItem{}
		m.queue = m.queue[1:]
		m.mu.Unlock()
		m.dispatch(item)
	}
}

func (m *Manager) dispatch(item queueItem) {
	m.pluginsMu.RLock()
	plugins := make([]Plugin, len(m.plugins))
	copy(plugins, m.plugins)
	m.pluginsMu.RUnlock()
	if len(plugins) == 0 {
		return
	}
	for _, plugin := range plugins {
		if plugin == nil {
			continue
		}
		safeInvoke(plugin, item.ctx, item.record)
	}
}

func safeInvoke(plugin Plugin, ctx context.Context, record Record) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("usage: plugin panic recovered: %v", r)
		}
	}()
	plugin.HandleUsage(ctx, record)
}

var defaultManager = NewManager(512)


func DefaultManager() *Manager { return defaultManager }


func RegisterPlugin(plugin Plugin) { DefaultManager().Register(plugin) }


func RegisterNamedPlugin(name string, plugin Plugin) { DefaultManager().RegisterNamed(name, plugin) }


func PublishRecord(ctx context.Context, record Record) { DefaultManager().Publish(ctx, record) }


func StartDefault(ctx context.Context) { DefaultManager().Start(ctx) }


func StopDefault() { DefaultManager().Stop() }
