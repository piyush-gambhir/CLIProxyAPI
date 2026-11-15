package executor

import (
	"context"
	"net/http"
	"net/url"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

const RequestedModelMetadataKey = "requested_model"

const RequestPathMetadataKey = "request_path"

const DisallowFreeAuthMetadataKey = "disallow_free_auth"

const AuthSelectionModelMetadataKey = "auth_selection_model"

const ReasoningEffortMetadataKey = "reasoning_effort"

const ServiceTierMetadataKey = "service_tier"

const GenerateMetadataKey = "generate"

const (
	PinnedAuthMetadataKey = "pinned_auth_id"

	SelectionIDMetadataKey = "selection_id"

	SelectedAuthMetadataKey = "selected_auth_id"

	SelectedAuthCallbackMetadataKey = "selected_auth_callback"

	SelectedAuthIndexMetadataKey = "selected_auth_index"

	SelectedAuthIndexCallbackMetadataKey = "selected_auth_index_callback"

	FinalizedResponseFormatCallbackMetadataKey = "finalized_response_format_callback"

	ExecutionSessionMetadataKey = "execution_session_id"

	DerivedSessionIDMetadataKey = "derived_session_id"

	LCPAffinitySessionIDMetadataKey = "lcp_affinity_session_id"

	CanonicalSessionIDMetadataKey = "canonical_session_id"

	ParentSessionIDMetadataKey = "parent_session_id"

	IsForkMetadataKey = "is_fork"

	IsCompactionMetadataKey = "is_compaction"

	NodeKindMetadataKey = "node_kind"

	LCPTailFingerprintsMetadataKey = "lcp_tail_fingerprints"

	LCPEnvironmentDigestMetadataKey = "lcp_environment_digest"

	LCPAccessGenerationMetadataKey = "lcp_access_generation"

	LCPFingerprintMetadataKey = "lcp_fingerprints"

	LCPMinPrefixLengthMetadataKey = "lcp_min_prefix_length"

	CallerScopeMetadataKey = "caller_scope"

	SessionAffinityProviderMetadataKey = "session_affinity_provider"

	SessionAffinityModelMetadataKey = "session_affinity_model"
)

type Request struct {
	Model string

	Payload []byte

	Format sdktranslator.Format

	Metadata map[string]any
}

type RequestBeforeAuthInterceptor func(context.Context, RequestAfterAuthInterceptRequest) RequestAfterAuthInterceptResponse

type RequestAfterAuthInterceptor func(context.Context, RequestAfterAuthInterceptRequest) RequestAfterAuthInterceptResponse

type RequestAfterAuthInterceptRequest struct {
	SourceFormat sdktranslator.Format

	ToFormat sdktranslator.Format

	Model string

	RequestedModel string

	Stream bool

	Headers http.Header

	Body []byte

	Metadata map[string]any
}

type RequestAfterAuthInterceptResponse struct {
	Path string

	Headers http.Header

	Body []byte

	ClearHeaders []string

	Terminate bool

	StatusCode int

	ResponseHeaders http.Header

	ResponseBody []byte
}

type RequestTerminatedError struct {
	HTTPStatus int
	Header     http.Header
	Body       []byte
}

func (e *RequestTerminatedError) Error() string {
	return "request terminated by plugin"
}

func (e *RequestTerminatedError) StatusCode() int {
	if e == nil {
		return 0
	}
	return e.HTTPStatus
}

func (e *RequestTerminatedError) ResponseHeaders() http.Header {
	if e == nil {
		return nil
	}
	return e.Header.Clone()
}

func (e *RequestTerminatedError) ResponseBody() []byte {
	if e == nil {
		return nil
	}
	return append([]byte(nil), e.Body...)
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

type WebSocketResponseObserver func(context.Context, WebSocketResponseEvent)

type Options struct {
	Stream bool

	Alt string

	Headers http.Header

	Query url.Values

	OriginalRequest []byte

	SourceFormat sdktranslator.Format

	ResponseFormat sdktranslator.Format

	Metadata map[string]any

	RequestBeforeAuthInterceptor RequestBeforeAuthInterceptor

	RequestAfterAuthInterceptor RequestAfterAuthInterceptor

	WebSocketResponseObserver WebSocketResponseObserver

	ExecutionLifecycle ExecutionLifecycle

	ProxyURL string

	KeepTopLevelEffort bool
}

func (o *Options) EnsureMetadata() map[string]any {
	if o.Metadata == nil {
		o.Metadata = make(map[string]any)
	}
	return o.Metadata
}

func ResponseFormatOrSource(opts Options) sdktranslator.Format {
	if opts.ResponseFormat != "" {
		return opts.ResponseFormat
	}
	return opts.SourceFormat
}

type Response struct {
	Payload []byte

	Incomplete bool

	Metadata map[string]any

	Headers http.Header
}

type StreamChunk struct {
	Payload []byte

	Incomplete bool

	Err error
}

type StreamResult struct {
	Headers http.Header

	Chunks <-chan StreamChunk
}

type StatusError interface {
	error
	StatusCode() int
}

type RequestScopedError interface {
	error
	IsRequestScoped() bool
}

type StreamTruncatedError interface {
	error
	IsStreamTruncated() bool
}

type TransientRateLimitError interface {
	error
	IsTransientRateLimit() bool
}
