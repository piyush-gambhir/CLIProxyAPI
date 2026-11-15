package pluginapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

type Plugin struct {
	Metadata Metadata

	Capabilities Capabilities

	SchemaVersion uint32
}

type Metadata struct {
	Name string

	Version string

	Author string

	GitHubRepository string

	Logo string

	ConfigFields []ConfigField
}

type ConfigFieldType string

const (
	ConfigFieldTypeString ConfigFieldType = "string"

	ConfigFieldTypeNumber ConfigFieldType = "number"

	ConfigFieldTypeInteger ConfigFieldType = "integer"

	ConfigFieldTypeBoolean ConfigFieldType = "boolean"

	ConfigFieldTypeEnum ConfigFieldType = "enum"

	ConfigFieldTypeArray ConfigFieldType = "array"

	ConfigFieldTypeObject ConfigFieldType = "object"
)

type ConfigField struct {
	Name string

	Type ConfigFieldType

	Default any

	EnumValues []string

	Description string
}

type Capabilities struct {
	ModelRegistrar ModelRegistrar

	ModelProvider ModelProvider

	AuthProvider AuthProvider

	AuthProviders []AuthProvider

	FrontendAuthProvider FrontendAuthProvider

	FrontendAuthProviderExclusive bool

	FrontendAuthProviderOmitBody bool

	FrontendAuthOmitBody bool

	Scheduler Scheduler

	SchedulerAcrossPriorities bool

	ModelRouter ModelRouter

	Executor ProviderExecutor

	ExecutorModelScope ExecutorModelScope

	ExecutorInputFormats []string

	ExecutorOutputFormats []string

	RequestTranslator RequestTranslator

	RequestNormalizer RequestNormalizer

	ResponseTranslator ResponseTranslator

	ResponseBeforeTranslator ResponseNormalizer

	ResponseAfterTranslator ResponseNormalizer

	RequestInterceptor RequestInterceptor

	RequestInterceptorOmitBodyBefore bool

	RequestInterceptorOmitBodyAfter bool

	RequestBodyPatcherBefore RequestBodyPatcher

	RequestBodyPatcherAfter RequestBodyPatcher

	RequestInterceptorPatchBodyBefore bool

	RequestInterceptorPatchBodyAfter bool

	RequestLifecyclePlugin RequestLifecyclePlugin

	ResponseInterceptor ResponseInterceptor

	StreamChunkInterceptor StreamChunkInterceptor

	WebSocketResponseObserver WebSocketResponseObserver

	ThinkingApplier ThinkingApplier

	UsagePlugin UsagePlugin

	CommandLinePlugin CommandLinePlugin

	ManagementAPI ManagementAPI

	QuotaProvider QuotaProvider
}

type ExecutorModelScope string

const (
	ExecutorModelScopeBoth ExecutorModelScope = "both"

	ExecutorModelScopeStatic ExecutorModelScope = "static"

	ExecutorModelScopeOAuth ExecutorModelScope = "oauth"
)

type ModelInfo struct {
	ID string

	Object string

	Created int64

	OwnedBy string

	Type string

	DisplayName string

	Name string

	Version string

	Description string

	InputTokenLimit int64

	OutputTokenLimit int64

	SupportedGenerationMethods []string

	ContextLength int64

	MaxCompletionTokens int64

	SupportedParameters []string

	SupportedInputModalities []string

	SupportedOutputModalities []string

	Thinking *ThinkingSupport

	UserDefined bool
}

type ThinkingSupport struct {
	Min int

	Max int

	ZeroAllowed bool

	DynamicAllowed bool

	DisableType string

	Levels []string
}

type HostConfigSummary struct {
	AuthDir string

	ProxyURL string

	ForceModelPrefix bool

	OAuthModelAlias map[string][]ModelAlias

	ExcludedModels map[string][]string
}

type ModelAlias struct {
	Name string

	Alias string
}

type AuthData struct {
	Provider string

	ID string

	FileName string

	Label string

	Prefix string

	ProxyURL string

	Disabled bool

	StorageJSON []byte

	Metadata map[string]any

	Attributes map[string]string

	NextRefreshAfter time.Time
}

type AuthParseRequest struct {
	Provider string

	Path string

	FileName string

	RawJSON []byte

	Host HostConfigSummary
}

type AuthParseResponse struct {
	Handled bool

	Auth AuthData

	Auths []AuthData
}

type AuthProvider interface {
	Identifier() string
	ParseAuth(context.Context, AuthParseRequest) (AuthParseResponse, error)
	StartLogin(context.Context, AuthLoginStartRequest) (AuthLoginStartResponse, error)
	PollLogin(context.Context, AuthLoginPollRequest) (AuthLoginPollResponse, error)
	RefreshAuth(context.Context, AuthRefreshRequest) (AuthRefreshResponse, error)
}

type MultiAuthProvider interface {
	AuthProvider
	Identifiers() []string
}

type AuthProviderEntry struct {
	Identifier string `json:"identifier"`

	Name string `json:"name,omitempty"`

	Logo string `json:"logo,omitempty"`
}

type AuthProviderEntries interface {
	AuthProvider
	AuthEntries() []AuthProviderEntry
}

type AuthLoginStartRequest struct {
	Provider string

	BaseURL string

	Host HostConfigSummary

	HTTPClient HostHTTPClient `json:"-"`

	Metadata map[string]any
}

type AuthLoginStartResponse struct {
	Provider string

	URL string

	State string

	ExpiresAt time.Time

	Metadata map[string]any
}

type AuthLoginPollRequest struct {
	Provider string

	State string

	Host HostConfigSummary

	HTTPClient HostHTTPClient `json:"-"`

	Metadata map[string]any
}

type AuthLoginStatus string

const (
	AuthLoginStatusPending AuthLoginStatus = "pending"

	AuthLoginStatusSuccess AuthLoginStatus = "success"

	AuthLoginStatusError AuthLoginStatus = "error"
)

type AuthLoginPollResponse struct {
	Status AuthLoginStatus

	Message string

	Auth AuthData

	Auths []AuthData
}

type AuthRefreshRequest struct {
	AuthID string

	AuthProvider string

	StorageJSON []byte

	Metadata map[string]any

	Attributes map[string]string

	Host HostConfigSummary

	HTTPClient HostHTTPClient `json:"-"`
}

type AuthRefreshResponse struct {
	Auth AuthData

	NextRefreshAfter time.Time
}

type ModelRegistrar interface {
	RegisterModels(context.Context, ModelRegistrationRequest) (ModelRegistrationResponse, error)
}

type ModelRegistrationRequest struct {
	Plugin Metadata

	MultiProviderModels bool `json:"multi_provider_models,omitempty"`
}

type ProviderModels struct {
	Provider string `json:"provider,omitempty"`

	Models []ModelInfo `json:"models,omitempty"`
}

type ModelRegistrationResponse struct {
	Provider string

	Models []ModelInfo

	Registrations []ProviderModels `json:"registrations,omitempty"`
}

type ModelProvider interface {
	StaticModels(context.Context, StaticModelRequest) (ModelResponse, error)
	ModelsForAuth(context.Context, AuthModelRequest) (ModelResponse, error)
}

type StaticModelRequest struct {
	Plugin Metadata

	Host HostConfigSummary

	MultiProviderModels bool `json:"multi_provider_models,omitempty"`
}

type AuthModelRequest struct {
	Plugin Metadata

	AuthID string

	AuthProvider string

	StorageJSON []byte

	Metadata map[string]any

	Attributes map[string]string

	Host HostConfigSummary

	HTTPClient HostHTTPClient `json:"-"`
}

type ModelResponse struct {
	Provider string

	Models []ModelInfo

	Registrations []ProviderModels `json:"registrations,omitempty"`

	AuthUpdate AuthData
}

type FrontendAuthProvider interface {
	Identifier() string
	Authenticate(context.Context, FrontendAuthRequest) (FrontendAuthResponse, error)
}

type FrontendAuthRequest struct {
	Method string

	Path string

	Headers http.Header

	Query url.Values

	Body []byte

	DecodedBody []byte
}

type FrontendAuthResponse struct {
	Authenticated bool

	Principal string

	Metadata map[string]string

	DenyStatusCode int

	DenyMessage string
}

const (
	SchedulerBuiltinRoundRobin = "round-robin"

	SchedulerBuiltinFillFirst = "fill-first"

	SelectionIDMetadataKey = "selection_id"
)

type Scheduler interface {
	Pick(context.Context, SchedulerPickRequest) (SchedulerPickResponse, error)
}

type ModelRouter interface {
	RouteModel(context.Context, ModelRouteRequest) (ModelRouteResponse, error)
}

type SchedulerPickRequest struct {
	Plugin Metadata

	Provider string

	Providers []string

	Model string

	Stream bool

	Options SchedulerOptions

	Candidates []SchedulerAuthCandidate
}

type SchedulerOptions struct {
	Headers map[string][]string

	Metadata map[string]any
}

type SchedulerAuthCandidate struct {
	ID string

	Provider string

	Priority int

	Status string

	Attributes map[string]string

	Metadata map[string]any
}

type SchedulerPickResponse struct {
	AuthID string

	DelegateBuiltin string

	Handled bool

	Reject bool

	RejectReason string

	RejectCode string
}

func (r *SchedulerPickResponse) UnmarshalJSON(data []byte) error {
	type rawResponse struct {
		AuthID          *string `json:"AuthID"`
		AltAuthID       *string `json:"auth_id"`
		DelegateBuiltin *string `json:"DelegateBuiltin"`
		AltDelegate     *string `json:"delegate_builtin"`
		Handled         *bool   `json:"Handled"`
		AltHandled      *bool   `json:"handled"`
		Reject          *bool   `json:"Reject"`
		AltReject       *bool   `json:"reject"`
		RejectReason    *string `json:"RejectReason"`
		AltRejectReason *string `json:"reject_reason"`
		RejectCode      *string `json:"RejectCode"`
		AltRejectCode   *string `json:"reject_code"`
	}
	var raw rawResponse
	if errUnmarshal := json.Unmarshal(data, &raw); errUnmarshal != nil {
		return errUnmarshal
	}
	if raw.AuthID != nil {
		r.AuthID = *raw.AuthID
	} else if raw.AltAuthID != nil {
		r.AuthID = *raw.AltAuthID
	}
	if raw.DelegateBuiltin != nil {
		r.DelegateBuiltin = *raw.DelegateBuiltin
	} else if raw.AltDelegate != nil {
		r.DelegateBuiltin = *raw.AltDelegate
	}
	if raw.Handled != nil {
		r.Handled = *raw.Handled
	} else if raw.AltHandled != nil {
		r.Handled = *raw.AltHandled
	}
	if raw.Reject != nil {
		r.Reject = *raw.Reject
	} else if raw.AltReject != nil {
		r.Reject = *raw.AltReject
	}
	if raw.RejectReason != nil {
		r.RejectReason = *raw.RejectReason
	} else if raw.AltRejectReason != nil {
		r.RejectReason = *raw.AltRejectReason
	}
	if raw.RejectCode != nil {
		r.RejectCode = *raw.RejectCode
	} else if raw.AltRejectCode != nil {
		r.RejectCode = *raw.AltRejectCode
	}
	return nil
}

type ModelRouteRequest struct {
	Plugin Metadata

	PluginID string

	SourceFormat string

	RequestedModel string

	Stream bool

	Headers http.Header

	Query url.Values

	Body []byte

	Metadata map[string]any

	AvailableProviders []string
}

type ModelRouteTargetKind string

const (
	ModelRouteTargetSelf ModelRouteTargetKind = "self"

	ModelRouteTargetExecutor ModelRouteTargetKind = "executor"

	ModelRouteTargetProvider ModelRouteTargetKind = "provider"
)

type ModelRouteResponse struct {
	Handled bool

	TargetKind ModelRouteTargetKind

	Target string

	TargetModel string

	Reason string
}

type ProviderExecutor interface {
	Identifier() string
	Execute(context.Context, ExecutorRequest) (ExecutorResponse, error)
	ExecuteStream(context.Context, ExecutorRequest) (ExecutorStreamResponse, error)
	CountTokens(context.Context, ExecutorRequest) (ExecutorResponse, error)
	HttpRequest(context.Context, ExecutorHTTPRequest) (ExecutorHTTPResponse, error)
}

type HostHTTPClient interface {
	Do(context.Context, HTTPRequest) (HTTPResponse, error)
	DoStream(context.Context, HTTPRequest) (HTTPStreamResponse, error)
}

type HostModelExecutionRequest struct {
	EntryProtocol string `json:"entry_protocol"`

	ExitProtocol string `json:"exit_protocol"`

	Model string `json:"model"`

	Stream bool `json:"stream"`

	Body []byte `json:"body"`

	Headers http.Header `json:"headers"`

	Query url.Values `json:"query"`

	Alt string `json:"alt"`

	ForcedProvider string `json:"forced_provider,omitempty"`

	AuthID string `json:"auth_id,omitempty"`

	ProxyURL string `json:"proxy_url,omitempty"`

	Path string `json:"path,omitempty"`

	CallerScope string `json:"caller_scope,omitempty"`
}

type HostModelExecutionResponse struct {
	StatusCode int `json:"status_code"`

	Headers http.Header `json:"headers"`

	Body []byte `json:"body"`
}

type HostModelStreamResponse struct {
	StatusCode int `json:"status_code"`

	Headers http.Header `json:"headers"`

	StreamID string `json:"stream_id"`
}

type HostModelStreamReadRequest struct {
	StreamID string `json:"stream_id"`
}

type HostModelStreamReadResponse struct {
	Payload []byte `json:"payload"`

	Error string `json:"error"`

	Done bool `json:"done"`
}

type HostModelStreamCloseRequest struct {
	StreamID string `json:"stream_id"`
}

type HostRecentRequestEntry struct {
	Time string `json:"time"`

	Success int64 `json:"success"`

	Failed int64 `json:"failed"`
}

type HostAuthFileEntry struct {
	ID string `json:"id,omitempty"`

	AuthIndex string `json:"auth_index,omitempty"`

	Name string `json:"name"`

	Type string `json:"type,omitempty"`

	Provider string `json:"provider,omitempty"`

	Label string `json:"label,omitempty"`

	Status string `json:"status,omitempty"`

	StatusMessage string `json:"status_message,omitempty"`

	Disabled bool `json:"disabled,omitempty"`

	Unavailable bool `json:"unavailable,omitempty"`

	RuntimeOnly bool `json:"runtime_only,omitempty"`

	Source string `json:"source,omitempty"`

	Path string `json:"path,omitempty"`

	Size int64 `json:"size,omitempty"`

	ModTime time.Time `json:"modtime,omitempty"`

	UpdatedAt time.Time `json:"updated_at,omitempty"`

	CreatedAt time.Time `json:"created_at,omitempty"`

	LastRefresh time.Time `json:"last_refresh,omitempty"`

	NextRetryAfter time.Time `json:"next_retry_after,omitempty"`

	Email string `json:"email,omitempty"`

	ProjectID string `json:"project_id,omitempty"`

	AccountType string `json:"account_type,omitempty"`

	Account string `json:"account,omitempty"`

	Priority int `json:"priority,omitempty"`

	Note string `json:"note,omitempty"`

	BaseURL string `json:"base_url,omitempty"`

	Websockets bool `json:"websockets,omitempty"`

	Success int64 `json:"success,omitempty"`

	Failed int64 `json:"failed,omitempty"`

	RecentRequests []HostRecentRequestEntry `json:"recent_requests,omitempty"`
}

type HostAuthGetRequest struct {
	AuthIndex string `json:"auth_index"`
}

type HostAuthGetResponse struct {
	AuthIndex string `json:"auth_index"`

	Name string `json:"name,omitempty"`

	Path string `json:"path,omitempty"`

	JSON json.RawMessage `json:"json"`
}

type HostAuthGetRuntimeResponse struct {
	Auth HostAuthFileEntry `json:"auth"`
}

type HostAuthSaveRequest struct {
	Name string `json:"name"`

	JSON json.RawMessage `json:"json"`
}

type HostAuthSaveResponse struct {
	Name string `json:"name"`

	Path string `json:"path"`
}

type HostAuthModelsRequest struct {
	AuthIndex string `json:"auth_index,omitempty"`
}

type HostAuthModelsResponse struct {
	Credentials []HostAuthModels `json:"credentials"`
}

type HostAuthModels struct {
	ID string `json:"id"`

	AuthIndex string `json:"auth_index"`

	Provider string `json:"provider"`

	Models []string `json:"models"`
}

const (
	HostAffinityStatusBound       = "bound"
	HostAffinityStatusUnbound     = "unbound"
	HostAffinityStatusAmbiguous   = "ambiguous"
	HostAffinityStatusUnsupported = "unsupported"
)

type HostAffinityLookupRequest struct {
	Provider string `json:"provider"`

	Model string `json:"model"`

	SessionID string `json:"session_id"`
}

type HostAffinityLookupResponse struct {
	Status string `json:"status"`

	AuthIndex string `json:"auth_index,omitempty"`

	ObservedAt time.Time `json:"observed_at,omitempty"`

	Disabled bool `json:"disabled,omitempty"`

	Unavailable bool `json:"unavailable,omitempty"`
}

type HostRoutingResetCooldownRequest struct {
	AuthIndex string `json:"auth_index"`
}

type HostRoutingResetCooldownResponse struct {
	AuthIndex string `json:"auth_index"`

	Models []string `json:"models,omitempty"`
}

type HTTPWireProfile struct {
	HTTP1Only bool `json:"http1_only,omitempty"`

	DisableAutoCompression bool `json:"disable_auto_compression,omitempty"`

	HeaderProfile []string `json:"header_profile,omitempty"`
}

type HTTPRequest struct {
	Method string

	URL string

	Headers http.Header

	Body []byte

	WireProfile *HTTPWireProfile `json:"wire_profile,omitempty"`
}

type HTTPResponse struct {
	StatusCode int

	Headers http.Header

	Body []byte
}

type HTTPStreamResponse struct {
	StatusCode int

	Headers http.Header

	Chunks <-chan HTTPStreamChunk
}

type HTTPStreamChunk struct {
	Payload []byte

	Err error
}

type ExecutorHTTPRequest struct {
	AuthID string

	AuthProvider string

	Method string

	URL string

	Headers http.Header

	Body []byte

	StorageJSON []byte

	Metadata map[string]any

	Attributes map[string]string

	HTTPClient HostHTTPClient `json:"-"`
}

type ExecutorHTTPResponse struct {
	StatusCode int

	Headers http.Header

	Body []byte
}

type ExecutorRequest struct {
	AuthID string

	AuthProvider string

	Model string

	Format string

	Stream bool

	Alt string

	Headers http.Header

	Query url.Values

	OriginalRequest []byte

	SourceFormat string

	Payload []byte

	Metadata map[string]any

	StorageJSON []byte

	AuthMetadata map[string]any

	AuthAttributes map[string]string

	HTTPClient HostHTTPClient `json:"-"`
}

type ExecutorResponse struct {
	Payload []byte

	Headers http.Header

	Metadata map[string]any
}

type ExecutorStreamResponse struct {
	Headers http.Header

	Chunks <-chan ExecutorStreamChunk
}

type ExecutorStreamChunk struct {
	Payload []byte

	Err error
}

type RequestTranslator interface {
	TranslateRequest(context.Context, RequestTransformRequest) (PayloadResponse, error)
}

type RequestNormalizer interface {
	NormalizeRequest(context.Context, RequestTransformRequest) (PayloadResponse, error)
}

type ResponseTranslator interface {
	TranslateResponse(context.Context, ResponseTransformRequest) (PayloadResponse, error)
}

type ResponseNormalizer interface {
	NormalizeResponse(context.Context, ResponseTransformRequest) (PayloadResponse, error)
}

type RequestInterceptor interface {
	InterceptRequestBeforeAuth(context.Context, RequestInterceptRequest) (RequestInterceptResponse, error)
	InterceptRequestAfterAuth(context.Context, RequestInterceptRequest) (RequestInterceptResponse, error)
}

type RequestLifecyclePlugin interface {
	HandleRequestComplete(context.Context, RequestCompletion) error
}

type ResponseInterceptor interface {
	InterceptResponse(context.Context, ResponseInterceptRequest) (ResponseInterceptResponse, error)
}

type StreamChunkInterceptor interface {
	InterceptStreamChunk(context.Context, StreamChunkInterceptRequest) (StreamChunkInterceptResponse, error)
}

type WebSocketResponseObserver interface {
	ObserveWebSocketResponseEvent(context.Context, WebSocketResponseEvent) error
}

const StreamChunkHeaderInitIndex = -1

const CallerScopeMetadataKey = "caller_scope"

type RequestTransformRequest struct {
	FromFormat string

	ToFormat string

	Model string

	Stream bool

	Body []byte
}

type ResponseTransformRequest struct {
	FromFormat string

	ToFormat string

	Model string

	Stream bool

	OriginalRequest []byte

	TranslatedRequest []byte

	Body []byte
}

type RequestInterceptRequest struct {
	RequestID string

	TraceID string

	SourceFormat string

	ToFormat string

	Model string

	RequestedModel string

	Stream bool

	Headers http.Header

	Body []byte

	CallerScope string

	Metadata map[string]any
}

type RequestInterceptResponse struct {
	Path string `json:"path,omitempty"`

	Headers http.Header

	Body []byte

	BodyEdits []RequestBodyEdit `json:"body_edits,omitempty"`

	Edits []RequestBodyEdit `json:"edits,omitempty"`

	ClearHeaders []string

	Terminate bool

	StatusCode int

	ResponseHeaders http.Header

	ResponseBody []byte
}

type RequestBodyPatcher interface {
	PatchRequestBody(context.Context, []byte) (RequestBodyPatchResponse, error)
}

type RequestBodyEdit struct {
	Start int `json:"start"`

	End int `json:"end"`

	Replacement string `json:"replacement,omitempty"`

	ReplacementBytes []byte `json:"replacement_bytes,omitempty"`
}

type RequestBodyPatchResponse struct {
	Edits []RequestBodyEdit `json:"edits,omitempty"`
}

type RequestCompletionOutcome string

const (
	RequestCompletionSucceeded RequestCompletionOutcome = "succeeded"

	RequestCompletionFailed RequestCompletionOutcome = "failed"

	RequestCompletionRejected RequestCompletionOutcome = "rejected"

	RequestCompletionCanceled RequestCompletionOutcome = "canceled"
)

type RequestCompletion struct {
	RequestID      string
	TraceID        string
	SourceFormat   string
	Model          string
	RequestedModel string
	Stream         bool
	Outcome        RequestCompletionOutcome
	StatusCode     int
	Error          string
	StartedAt      time.Time
	CompletedAt    time.Time
	Metadata       map[string]any
}

type ResponseInterceptRequest struct {
	RequestID       string
	SourceFormat    string
	Model           string
	RequestedModel  string
	Stream          bool
	RequestHeaders  http.Header
	ResponseHeaders http.Header
	OriginalRequest []byte
	RequestBody     []byte
	Body            []byte
	StatusCode      int
	Metadata        map[string]any
}

type ResponseInterceptResponse struct {
	Headers http.Header

	Body []byte

	ClearHeaders []string
}

type StreamChunkInterceptRequest struct {
	RequestID       string
	SourceFormat    string
	Model           string
	RequestedModel  string
	RequestHeaders  http.Header
	ResponseHeaders http.Header

	OriginalRequest []byte

	RequestBody []byte
	Body        []byte

	HistoryChunks [][]byte

	ChunkIndex int

	Metadata map[string]any
}

type StreamChunkInterceptResponse struct {
	Headers http.Header

	Body []byte

	ClearHeaders []string

	DropChunk bool
}

type WebSocketResponseEvent struct {
	RequestID      string
	TraceID        string
	SourceFormat   string
	Model          string
	RequestedModel string
	Provider       string
	AuthID         string
	AuthLabel      string
	AuthType       string
	EventType      string
	Payload        []byte
	Metadata       map[string]any
}

type PayloadResponse struct {
	Body []byte
}

type ThinkingConfig struct {
	Mode string

	Budget int

	Level string
}

type ThinkingApplyRequest struct {
	Provider string

	Model ModelInfo

	Config ThinkingConfig

	Body []byte
}

type ThinkingApplier interface {
	Identifier() string

	ApplyThinking(context.Context, ThinkingApplyRequest) (PayloadResponse, error)
}

type UsagePlugin interface {
	HandleUsage(context.Context, UsageRecord)
}

type CommandLinePlugin interface {
	RegisterCommandLine(context.Context, CommandLineRegistrationRequest) (CommandLineRegistrationResponse, error)
	ExecuteCommandLine(context.Context, CommandLineExecutionRequest) (CommandLineExecutionResponse, error)
}

type CommandLineRegistrationRequest struct {
	Plugin Metadata
}

type CommandLineRegistrationResponse struct {
	Flags []CommandLineFlag
}

type CommandLineFlag struct {
	Name string

	Usage string

	Type string

	DefaultValue string
}

type CommandLineFlagValue struct {
	Name string

	Type string

	Value string

	Set bool
}

type CommandLineExecutionRequest struct {
	Plugin Metadata

	Program string

	Args []string

	ConfigPath string

	Host HostConfigSummary

	Flags map[string]CommandLineFlagValue

	TriggeredFlags map[string]CommandLineFlagValue
}

type CommandLineExecutionResponse struct {
	Stdout []byte

	Stderr []byte

	Auths []AuthData

	ExitCode int
}

type ManagementAPI interface {
	RegisterManagement(context.Context, ManagementRegistrationRequest) (ManagementRegistrationResponse, error)
}

type ManagementRegistrationRequest struct {
	Plugin Metadata

	BasePath string

	ResourceBasePath string
}

type ManagementRegistrationResponse struct {
	Routes []ManagementRoute

	Resources []ResourceRoute
}

type ManagementRoute struct {
	Method string

	Path string

	Menu string

	Description string

	Handler ManagementHandler
}

type ResourceRoute struct {
	Path string

	Menu string

	Description string

	Handler ManagementHandler
}

type ManagementHandler interface {
	HandleManagement(context.Context, ManagementRequest) (ManagementResponse, error)
}

type ManagementRequest struct {
	Method string

	Path string

	Headers http.Header

	Query url.Values

	Body []byte
}

type ManagementResponse struct {
	StatusCode int

	Headers http.Header

	Body []byte
}

type UsageRecord struct {
	RequestID string

	TraceID string

	SelectionID string

	Provider string

	BaseURL string

	ExecutorType string

	Model string

	Alias string

	APIKey string

	IsNativeKey bool

	AccessProvider string

	SessionID string

	ParentSessionID string

	AuthID string

	AuthIndex string

	AuthType string

	Source string

	ReasoningEffort string

	ServiceTier string

	EffectiveServiceTier string

	ResponseServiceTier string

	ResponseModel string

	Generate bool

	Stream bool

	RequestedAt time.Time

	Latency time.Duration

	TTFT time.Duration

	Failed bool

	Failure UsageFailure

	Detail UsageDetail

	ResponseHeaders http.Header
}

type UsageFailure struct {
	StatusCode int

	Body string
}

type UsageDetail struct {
	InputTokens int64

	OutputTokens int64

	ReasoningTokens int64

	CachedTokens int64

	CacheReadTokens int64

	CacheCreationTokens int64

	TotalTokens int64

	TextInputTokens *int64

	ImageInputTokens *int64

	AudioInputTokens *int64

	VideoInputTokens *int64

	FileInputTokens *int64

	CachedTextTokens *int64

	CachedImageTokens *int64

	CachedAudioTokens *int64

	TextOutputTokens *int64

	ImageOutputTokens *int64

	AudioOutputTokens *int64
}

type QuotaProvider interface {
	Identifier() string

	DescribeQuota(context.Context, QuotaDescribeRequest) (QuotaDescribeResponse, error)

	FetchQuota(context.Context, QuotaFetchRequest) (QuotaFetchResponse, error)

	ResetQuota(context.Context, QuotaResetRequest) (QuotaResetResponse, error)
}

type QuotaDescribeRequest struct {
	Plugin Metadata `json:"plugin,omitempty"`
}

type QuotaDescribeResponse struct {
	SupportedProviders []string `json:"supported_providers,omitempty"`

	DisplayName string `json:"display_name,omitempty"`

	SupportsReset bool `json:"supports_reset,omitempty"`
}

type QuotaFetchRequest struct {
	AuthIndex string `json:"auth_index"`

	AuthID string `json:"auth_id"`

	Provider string `json:"provider"`

	StorageJSON []byte `json:"storage_json,omitempty"`

	Metadata map[string]any `json:"metadata,omitempty"`

	Attributes map[string]string `json:"attributes,omitempty"`

	Host HostConfigSummary `json:"host,omitempty"`

	HTTPClient HostHTTPClient `json:"-"`
}

type QuotaSubscription struct {
	Plan     string `json:"plan,omitempty"`
	TierName string `json:"tierName,omitempty"`
	TierID   string `json:"tierId,omitempty"`
}

func (s *QuotaSubscription) UnmarshalJSON(data []byte) error {
	type Alias QuotaSubscription
	aux := struct {
		*Alias
		AltTierName string `json:"tier_name"`
		AltTierID   string `json:"tier_id"`
	}{
		Alias: (*Alias)(s),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if s.TierName == "" && aux.AltTierName != "" {
		s.TierName = aux.AltTierName
	}
	if s.TierID == "" && aux.AltTierID != "" {
		s.TierID = aux.AltTierID
	}
	return nil
}

type QuotaGroup struct {
	DisplayName string        `json:"displayName,omitempty"`
	Buckets     []QuotaBucket `json:"buckets,omitempty"`
}

func (g *QuotaGroup) UnmarshalJSON(data []byte) error {
	type Alias QuotaGroup
	aux := struct {
		*Alias
		AltDisplayName string `json:"display_name"`
	}{
		Alias: (*Alias)(g),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if g.DisplayName == "" && aux.AltDisplayName != "" {
		g.DisplayName = aux.AltDisplayName
	}
	return nil
}

type QuotaBucket struct {
	Window            string  `json:"window,omitempty"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime,omitempty"`
	Description       string  `json:"description,omitempty"`
}

func (b *QuotaBucket) UnmarshalJSON(data []byte) error {
	type Alias QuotaBucket
	aux := struct {
		*Alias
		RemainingFraction    *float64 `json:"remainingFraction"`
		AltRemainingFraction *float64 `json:"remaining_fraction"`
		AltResetTime         string   `json:"reset_time"`
	}{
		Alias: (*Alias)(b),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.RemainingFraction != nil {
		b.RemainingFraction = *aux.RemainingFraction
	} else if aux.AltRemainingFraction != nil {
		b.RemainingFraction = *aux.AltRemainingFraction
	}
	if b.ResetTime == "" && aux.AltResetTime != "" {
		b.ResetTime = aux.AltResetTime
	}
	return nil
}

type QuotaMetric struct {
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit,omitempty"`
	Format   string  `json:"format,omitempty"`
	Currency string  `json:"currency,omitempty"`
}

type QuotaFetchResponse struct {
	Subscription       *QuotaSubscription `json:"subscription,omitempty"`
	Summary            []QuotaMetric      `json:"summary,omitempty"`
	ServerTimeOffsetMs int64              `json:"serverTimeOffsetMs,omitempty"`
	Groups             []QuotaGroup       `json:"groups,omitempty"`
}

func (r *QuotaFetchResponse) UnmarshalJSON(data []byte) error {
	type Alias QuotaFetchResponse
	aux := struct {
		*Alias
		AltServerTimeOffsetMs int64 `json:"server_time_offset_ms"`
	}{
		Alias: (*Alias)(r),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if r.ServerTimeOffsetMs == 0 && aux.AltServerTimeOffsetMs != 0 {
		r.ServerTimeOffsetMs = aux.AltServerTimeOffsetMs
	}
	return nil
}

type QuotaResetRequest struct {
	AuthIndex string `json:"auth_index"`

	AuthID string `json:"auth_id"`

	Provider string `json:"provider"`

	StorageJSON []byte `json:"storage_json,omitempty"`

	Metadata map[string]any `json:"metadata,omitempty"`

	Attributes map[string]string `json:"attributes,omitempty"`

	Host HostConfigSummary `json:"host,omitempty"`

	HTTPClient HostHTTPClient `json:"-"`
}

type QuotaResetResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}
