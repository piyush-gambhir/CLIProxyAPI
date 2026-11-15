package pluginabi

import (
	"encoding/json"
	"strings"
)

const (

	ABIVersion uint32 = 1





























	SchemaVersion uint32 = 10


	SchemaVersionStreamChunkOmitRequestBody uint32 = 3


	SchemaVersionWebSocketResponseObserver uint32 = 4


	SchemaVersionStreamChunkOmitHistory uint32 = 5


	SchemaVersionRawManagementResponse uint32 = 6


	SchemaVersionRequestInterceptorOmitBody uint32 = 7


	SchemaVersionFrontendAuthOmitBody uint32 = 7


	SchemaVersionRequestInterceptorBodyPatch uint32 = 8


	SchemaVersionMultiProviderModels uint32 = 9


	SchemaVersionMultiAuthProviders uint32 = 10
)

const (
	MethodPluginRegister    = "plugin.register"
	MethodPluginQuiesce     = "plugin.quiesce"
	MethodPluginReconfigure = "plugin.reconfigure"
	MethodPluginShutdown    = "plugin.shutdown"

	MethodModelRegister = "model.register"
	MethodModelStatic   = "model.static"
	MethodModelForAuth  = "model.for_auth"

	MethodAuthIdentifier = "auth.identifier"
	MethodAuthParse      = "auth.parse"
	MethodAuthLoginStart = "auth.login.start"
	MethodAuthLoginPoll  = "auth.login.poll"
	MethodAuthRefresh    = "auth.refresh"

	MethodFrontendAuthIdentifier   = "frontend_auth.identifier"
	MethodFrontendAuthAuthenticate = "frontend_auth.authenticate"


	MethodSchedulerPick = "scheduler.pick"

	MethodModelRoute = "model.route"

	MethodExecutorIdentifier    = "executor.identifier"
	MethodExecutorExecute       = "executor.execute"
	MethodExecutorExecuteStream = "executor.execute_stream"
	MethodExecutorCountTokens   = "executor.count_tokens"
	MethodExecutorHTTPRequest   = "executor.http_request"

	MethodRequestTranslate       = "request.translate"
	MethodRequestNormalize       = "request.normalize"
	MethodRequestInterceptBefore = "request.intercept_before"
	MethodRequestInterceptAfter  = "request.intercept_after"
	MethodRequestPatchBodyBefore = "request.patch_body_before"
	MethodRequestPatchBodyAfter  = "request.patch_body_after"
	MethodRequestComplete        = "request.complete"

	MethodResponseTranslate            = "response.translate"
	MethodResponseNormalizeBefore      = "response.normalize_before"
	MethodResponseNormalizeAfter       = "response.normalize_after"
	MethodResponseInterceptAfter       = "response.intercept_after"
	MethodResponseInterceptStreamChunk = "response.intercept_stream_chunk"

	MethodWebSocketResponseEvent = "websocket.response_event"

	MethodThinkingIdentifier = "thinking.identifier"
	MethodThinkingApply      = "thinking.apply"

	MethodUsageHandle = "usage.handle"

	MethodCommandLineRegister = "command_line.register"
	MethodCommandLineExecute  = "command_line.execute"

	MethodManagementRegister = "management.register"
	MethodManagementHandle   = "management.handle"

	MethodQuotaIdentifier = "quota.identifier"
	MethodQuotaDescribe   = "quota.describe"
	MethodQuotaFetch      = "quota.fetch"
	MethodQuotaReset      = "quota.reset"

	MethodHostHTTPDo             = "host.http.do"
	MethodHostHTTPDoStream       = "host.http.do_stream"
	MethodHostHTTPOperationOpen  = "host.http.operation_open"
	MethodHostHTTPCancel         = "host.http.cancel"
	MethodHostHTTPStreamRead     = "host.http.stream_read"
	MethodHostHTTPStreamClose    = "host.http.stream_close"
	MethodHostModelExecute       = "host.model.execute"
	MethodHostModelExecuteStream = "host.model.execute_stream"
	MethodHostModelStreamRead    = "host.model.stream_read"
	MethodHostModelStreamClose   = "host.model.stream_close"
	MethodHostStreamEmit         = "host.stream.emit"
	MethodHostStreamClose        = "host.stream.close"
	MethodHostLog                = "host.log"
	MethodHostAuthList           = "host.auth.list"
	MethodHostAuthGet            = "host.auth.get"
	MethodHostAuthGetRuntime     = "host.auth.get_runtime"
	MethodHostAuthSave           = "host.auth.save"
	MethodHostAuthModels         = "host.auth.models"
	MethodHostAffinityLookup     = "host.affinity.lookup"

	MethodHostRoutingResetCooldown = "host.routing.reset_cooldown"
)



const (




	ErrorCodeCredentialQuota = "credential_quota"



	ErrorCodeTransientRateLimit = "transient_rate_limit"


	ErrorCodeRequestScoped = "request_scoped"



	ErrorCodeForceCooldown = "force_cooldown"



	ErrorCodeStreamTruncated = "stream_truncated"


	ErrorCodeConnectionLifecycle = "connection_lifecycle"


	ErrorCodeTransientTransport = "transient_transport"


	ErrorCodeModelNotFound = "model_not_found"


	ErrorCodeNotImplemented = "not_implemented"
)

type Envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

















type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`


	HTTPStatus int `json:"http_status,omitempty"`





	RetryAfterSeconds int `json:"retry_after_seconds,omitempty"`



	CredentialScoped bool `json:"credential_scoped,omitempty"`
}


func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}


func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}


func (e *Error) StatusCode() int {
	if e == nil {
		return 0
	}
	return e.HTTPStatus
}


func (e *Error) IsCredentialScoped() bool {
	if e == nil {
		return false
	}
	return e.CredentialScoped || strings.EqualFold(strings.TrimSpace(e.Code), ErrorCodeCredentialQuota)
}


func (e *Error) IsRequestScoped() bool {
	if e == nil || e.IsCredentialScoped() {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(e.Code), ErrorCodeRequestScoped)
}


func (e *Error) IsTransientRateLimit() bool {
	if e == nil || e.IsCredentialScoped() {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(e.Code), ErrorCodeTransientRateLimit)
}


func (e *Error) IsStreamTruncated() bool {
	if e == nil || e.IsCredentialScoped() {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(e.Code), ErrorCodeStreamTruncated)
}


func NewError(code, message string, httpStatus ...int) *Error {
	status := 0
	if len(httpStatus) > 0 {
		status = httpStatus[0]
	}
	return &Error{
		Code:       code,
		Message:    message,
		HTTPStatus: status,
	}
}


func NewErrorEnvelope(code, message string, httpStatus ...int) ([]byte, error) {
	return json.Marshal(Envelope{
		OK:    false,
		Error: NewError(code, message, httpStatus...),
	})
}
