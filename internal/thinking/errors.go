package thinking

import "net/http"

type ErrorCode string

const (
	ErrInvalidSuffix ErrorCode = "INVALID_SUFFIX"

	ErrUnknownLevel ErrorCode = "UNKNOWN_LEVEL"

	ErrThinkingNotSupported ErrorCode = "THINKING_NOT_SUPPORTED"

	ErrLevelNotSupported ErrorCode = "LEVEL_NOT_SUPPORTED"

	ErrBudgetOutOfRange ErrorCode = "BUDGET_OUT_OF_RANGE"

	ErrProviderMismatch ErrorCode = "PROVIDER_MISMATCH"
)

type ThinkingError struct {
	Code ErrorCode

	Message string

	Model string

	Details map[string]interface{}
}

func (e *ThinkingError) Error() string {
	return e.Message
}

func NewThinkingError(code ErrorCode, message string) *ThinkingError {
	return &ThinkingError{
		Code:    code,
		Message: message,
	}
}

func NewThinkingErrorWithModel(code ErrorCode, message, model string) *ThinkingError {
	return &ThinkingError{
		Code:    code,
		Message: message,
		Model:   model,
	}
}

func (e *ThinkingError) StatusCode() int {
	return http.StatusBadRequest
}
