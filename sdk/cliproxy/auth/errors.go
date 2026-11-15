package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)



const ErrorCodeRequestScoped = "request_scoped"

const requestScopedErrorCode = ErrorCodeRequestScoped



const ErrorCodeConnectionLifecycle = "connection_lifecycle"

const connectionLifecycleErrorCode = ErrorCodeConnectionLifecycle



const ErrorCodeTransientTransport = "transient_transport"

const transientTransportErrorCode = ErrorCodeTransientTransport


const ErrorCodeForceCooldown = "force_cooldown"





const ErrorCodeStreamTruncated = "stream_truncated"

const streamTruncatedErrorCode = ErrorCodeStreamTruncated






const ErrorCodeTransientRateLimit = "transient_rate_limit"

const transientRateLimitErrorCode = ErrorCodeTransientRateLimit



const ErrorCodeCredentialQuota = "credential_quota"

const credentialQuotaErrorCode = ErrorCodeCredentialQuota


type Error struct {

	Code string `json:"code,omitempty"`

	Message string `json:"message"`

	Retryable bool `json:"retryable"`

	HTTPStatus int `json:"http_status,omitempty"`
}


func IsTerminalAuthError(err error) bool {
	if err == nil {
		return false
	}
	type terminalAuthProvider interface {
		IsTerminalAuth() bool
	}
	var tap terminalAuthProvider
	if errors.As(err, &tap) && tap != nil {
		return tap.IsTerminalAuth()
	}
	return false
}

type terminalAuthError struct {
	*errorWithCause
}

func (e *terminalAuthError) IsTerminalAuth() bool {
	return true
}


func NewTerminalAuthError(err *Error, cause error) error {
	if err == nil {
		return nil
	}
	base := WithCause(err, cause)
	if ewc, ok := base.(*errorWithCause); ok && ewc != nil {
		return &terminalAuthError{errorWithCause: ewc}
	}
	return &terminalAuthError{errorWithCause: &errorWithCause{base: err, cause: cause}}
}


func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
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



func (e *Error) IsRequestScoped() bool {
	return e != nil && e.Code == ErrorCodeRequestScoped
}


func (e *Error) MarkRequestScoped() *Error {
	if e != nil {
		e.Code = ErrorCodeRequestScoped
	}
	return e
}



func NewRequestScopedError(message string, httpStatus int) *Error {
	return &Error{
		Code:       ErrorCodeRequestScoped,
		Message:    message,
		HTTPStatus: httpStatus,
	}
}

type errorWithCause struct {
	base  *Error
	cause error
}

func (e *errorWithCause) Error() string {
	if e == nil || e.base == nil {
		return ""
	}
	baseText := e.base.Error()
	if e.cause == nil {
		return baseText
	}
	summary := ExtractUpstreamErrorSummary(e.cause.Error())
	if summary != "" && !strings.Contains(baseText, summary) {
		return fmt.Sprintf("%s (last upstream error: %s)", baseText, summary)
	}
	return baseText
}

func (e *errorWithCause) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *errorWithCause) As(target any) bool {
	if e == nil {
		return false
	}
	if t, ok := target.(**Error); ok {
		*t = e.base
		return true
	}
	return false
}

func (e *errorWithCause) Is(target error) bool {
	if e == nil {
		return target == nil
	}
	if target == e || target == e.base {
		return true
	}
	if other, ok := target.(*errorWithCause); ok && other != nil {
		return e.base == other.base
	}
	if otherErr, ok := target.(*Error); ok && otherErr != nil {
		return e.base == otherErr
	}
	return false
}

func (e *errorWithCause) StatusCode() int {
	if e == nil || e.base == nil {
		return 0
	}
	return e.base.HTTPStatus
}

func (e *errorWithCause) IsRequestScoped() bool {
	if e == nil || e.base == nil {
		return false
	}
	return e.base.IsRequestScoped()
}

func (e *errorWithCause) MarkRequestScoped() *Error {
	if e == nil || e.base == nil {
		return nil
	}
	return e.base.MarkRequestScoped()
}

func (e *errorWithCause) MarshalJSON() ([]byte, error) {
	if e == nil || e.base == nil {
		return []byte("null"), nil
	}
	return json.Marshal(e.base)
}


type authUnavailableError struct {
	cause      error
	base       *Error
	retryAfter time.Duration
}

func newAuthUnavailableError(next, now time.Time) error {
	return newAuthUnavailableErrorWithCause(next, now, nil)
}

func newAuthUnavailableErrorWithCause(next, now time.Time, cause error) error {
	err := &Error{Code: "auth_unavailable", Message: "no auth available"}
	if next.After(now) {
		err.HTTPStatus = http.StatusServiceUnavailable
		err.Retryable = true
		return &authUnavailableError{
			cause:      cause,
			base:       err,
			retryAfter: next.Sub(now),
		}
	}
	if cause != nil {
		return WithCause(err, cause)
	}
	return err
}

func (e *authUnavailableError) Error() string {
	if e == nil || e.base == nil {
		return ""
	}
	baseText := e.base.Error()
	if e.cause == nil {
		return baseText
	}
	summary := ExtractUpstreamErrorSummary(e.cause.Error())
	if summary != "" && !strings.Contains(baseText, summary) {
		return fmt.Sprintf("%s (last upstream error: %s)", baseText, summary)
	}
	return baseText
}

func (e *authUnavailableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *authUnavailableError) As(target any) bool {
	if e == nil {
		return false
	}
	if t, ok := target.(**Error); ok {
		*t = e.base
		return true
	}
	return false
}

func (e *authUnavailableError) Is(target error) bool {
	if e == nil {
		return target == nil
	}
	if target == e || target == e.base {
		return true
	}
	if other, ok := target.(*authUnavailableError); ok && other != nil {
		return e.base == other.base
	}
	if otherErr, ok := target.(*Error); ok && otherErr != nil {
		return e.base == otherErr
	}
	return false
}

func (e *authUnavailableError) StatusCode() int {
	if e == nil || e.base == nil {
		return 0
	}
	return e.base.HTTPStatus
}

func (e *authUnavailableError) IsRequestScoped() bool {
	if e == nil || e.base == nil {
		return false
	}
	return e.base.IsRequestScoped()
}

func (e *authUnavailableError) MarkRequestScoped() *Error {
	if e == nil || e.base == nil {
		return nil
	}
	return e.base.MarkRequestScoped()
}

func (e *authUnavailableError) MarshalJSON() ([]byte, error) {
	if e == nil || e.base == nil {
		return []byte("null"), nil
	}
	return json.Marshal(e.base)
}


func (e *authUnavailableError) WithAuthError(cause *Error) error {
	if e == nil {
		return cause
	}
	return &authUnavailableError{
		cause:      e.cause,
		base:       cause,
		retryAfter: e.retryAfter,
	}
}


func (e *authUnavailableError) Headers() http.Header {
	if e == nil {
		return nil
	}
	return safeRetryAfterHeader(e.retryAfter)
}






type trustedRetryAfterError struct {
	cause error



	deadline time.Time
}

func trustedRetryAfterHint(err error) *time.Duration {
	if trusted := trustedRetryAfterFromError(err); trusted != nil {
		return trusted
	}
	type retryAfterProvider interface {
		RetryAfter() *time.Duration
	}
	var rap retryAfterProvider
	if !errors.As(err, &rap) || rap == nil {
		return nil
	}
	retryAfter := rap.RetryAfter()
	if retryAfter == nil {
		return nil
	}
	value := *retryAfter
	return &value
}

func withTrustedRetryAfter(err error, retryAfter *time.Duration) error {
	if err == nil || retryAfter == nil || *retryAfter <= 0 {
		return err
	}
	if _, ok := err.(*upstreamExecutionAttemptError); ok {
		return markUpstreamExecutionAttempt(withTrustedRetryAfter(unwrapUpstreamExecutionAttempt(err), retryAfter))
	}
	if bootstrap, ok := err.(*streamBootstrapError); ok && bootstrap != nil {
		wrapped := withTrustedRetryAfter(bootstrap.cause, retryAfter)
		if wrapped == bootstrap.cause {
			return err
		}



		copyBootstrap := *bootstrap
		copyBootstrap.cause = wrapped
		*bootstrap = copyBootstrap
		return bootstrap
	}
	if requestStop, ok := err.(requestStopError); ok {
		wrapped := withTrustedRetryAfter(requestStop.error, retryAfter)
		if wrapped == requestStop.error {
			return err
		}
		return requestStopError{error: wrapped}
	}
	var existing *trustedRetryAfterError
	if errors.As(err, &existing) && existing != nil {
		return err
	}
	return &trustedRetryAfterError{cause: err, deadline: time.Now().Add(*retryAfter)}
}






func annotateTrustedRetryAfterByHint(err error) error {
	if err == nil {
		return err
	}
	return withTrustedRetryAfter(err, trustedRetryAfterHint(err))
}

func (e *trustedRetryAfterError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *trustedRetryAfterError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *trustedRetryAfterError) As(target any) bool {
	if e == nil {
		return false
	}
	if t, ok := target.(**Error); ok {
		var base *Error
		if errors.As(e.cause, &base) && base != nil {
			*t = base
			return true
		}
	}
	return false
}

func (e *trustedRetryAfterError) StatusCode() int {
	if e == nil {
		return 0
	}
	return statusCodeFromError(e.cause)
}

func (e *trustedRetryAfterError) RetryAfter() *time.Duration {
	if e == nil || e.deadline.IsZero() {
		return nil
	}
	remaining := time.Until(e.deadline)
	return &remaining
}




func HasTrustedRetryDeadline(err error) bool {
	if err == nil {
		return false
	}



	var exhausted *homeRetryRoundExhaustedError
	if errors.As(err, &exhausted) && exhausted != nil {
		return true
	}
	var unavailable *authUnavailableError
	if errors.As(err, &unavailable) && unavailable != nil && unavailable.retryAfter > 0 {
		return true
	}
	var cooldown *modelCooldownError
	if errors.As(err, &cooldown) && cooldown != nil && cooldown.resetIn > 0 {
		return true
	}



	if SafeResponseHeaders(err).Get("Retry-After") != "" {
		return true
	}
	var trusted *trustedRetryAfterError
	return errors.As(err, &trusted) && trusted != nil && !trusted.deadline.IsZero()
}




func trustedRetryAfterFromError(err error) *time.Duration {
	if err == nil {
		return nil
	}
	var trusted *trustedRetryAfterError
	if errors.As(err, &trusted) && trusted != nil {
		if remaining := trusted.RetryAfter(); remaining != nil && *remaining > 0 {
			return remaining
		}
	}
	return nil
}




func (e *trustedRetryAfterError) Headers() http.Header {
	if e == nil || e.cause == nil {
		return nil
	}
	var carrier interface{ Headers() http.Header }
	if errors.As(e.cause, &carrier) && carrier != nil {
		return carrier.Headers()
	}
	return nil
}


func (e *trustedRetryAfterError) SafeResponseHeaders() http.Header {
	if e == nil || e.deadline.IsZero() {
		return nil
	}
	return safeRetryAfterHeader(time.Until(e.deadline))
}


func WithCause(err *Error, cause error) error {
	if err == nil {
		return nil
	}
	if cause == nil {
		return err
	}
	return &errorWithCause{
		base:  err,
		cause: cause,
	}
}
