package executor

import (
	"context"
	"errors"
	"fmt"
)

type ConfiguredTLSHandshakeTimeoutError struct {
	cause error
}

func NewConfiguredTLSHandshakeTimeoutError(cause error) error {
	if cause == nil {
		return nil
	}
	return &ConfiguredTLSHandshakeTimeoutError{cause: cause}
}

func (e *ConfiguredTLSHandshakeTimeoutError) Error() string {
	if e == nil || e.cause == nil {
		return "tls handshake timeout"
	}
	return fmt.Sprintf("tls handshake timeout: %v", e.cause)
}

func (e *ConfiguredTLSHandshakeTimeoutError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func IsConfiguredTLSHandshakeTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	foundConfiguredDeadline, hasUnownedContextError := inspectConfiguredTLSHandshakeTimeout(err)
	return foundConfiguredDeadline && !hasUnownedContextError
}

func inspectConfiguredTLSHandshakeTimeout(err error) (foundConfiguredDeadline bool, hasUnownedContextError bool) {
	if err == nil {
		return false, false
	}
	if configured, ok := err.(*ConfiguredTLSHandshakeTimeoutError); ok && configured != nil {

		return errors.Is(configured.cause, context.DeadlineExceeded), false
	}

	hasUnownedContextError = hasOwnContextIdentity(err)
	for _, child := range unwrapChildErrors(err) {
		childFound, childUnowned := inspectConfiguredTLSHandshakeTimeout(child)
		foundConfiguredDeadline = foundConfiguredDeadline || childFound
		hasUnownedContextError = hasUnownedContextError || childUnowned
	}
	return foundConfiguredDeadline, hasUnownedContextError
}

func hasOwnContextIdentity(err error) bool {
	if err == context.Canceled || err == context.DeadlineExceeded {
		return true
	}
	if checker, ok := err.(interface{ Is(error) bool }); ok {
		return checker.Is(context.Canceled) || checker.Is(context.DeadlineExceeded)
	}
	return false
}

func unwrapChildErrors(err error) []error {
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		if children := multi.Unwrap(); len(children) > 0 {
			return children
		}
	}
	if single, ok := err.(interface{ Unwrap() error }); ok {
		if child := single.Unwrap(); child != nil {
			return []error{child}
		}
	}
	return nil
}
