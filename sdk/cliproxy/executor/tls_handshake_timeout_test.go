package executor

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestConfiguredTLSHandshakeTimeoutErrorIdentity(t *testing.T) {
	err := NewConfiguredTLSHandshakeTimeoutError(context.DeadlineExceeded)
	if err == nil {
		t.Fatal("NewConfiguredTLSHandshakeTimeoutError() = nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(%v, context.DeadlineExceeded) = false, want true", err)
	}
	if !IsConfiguredTLSHandshakeTimeoutError(err) {
		t.Fatalf("IsConfiguredTLSHandshakeTimeoutError(%v) = false, want true", err)
	}
	var configured *ConfiguredTLSHandshakeTimeoutError
	if !errors.As(err, &configured) || configured == nil {
		t.Fatalf("errors.As(%v) did not find the configured timeout type", err)
	}
	if got := NewConfiguredTLSHandshakeTimeoutError(nil); got != nil {
		t.Fatalf("NewConfiguredTLSHandshakeTimeoutError(nil) = %v, want nil", got)
	}
}

type contextIdentityError struct {
	target error
	child  error
}

func (e *contextIdentityError) Error() string {
	if e == nil || e.child == nil {
		return "caller context identity"
	}
	return "caller context identity: " + e.child.Error()
}

func (e *contextIdentityError) Is(target error) bool {
	return e != nil && target == e.target
}

func (e *contextIdentityError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.child
}

type multiContextIdentityError struct {
	target   error
	children []error
}

func (e *multiContextIdentityError) Error() string { return "caller context identity group" }

func (e *multiContextIdentityError) Is(target error) bool {
	return e != nil && target == e.target
}

func (e *multiContextIdentityError) Unwrap() []error {
	if e == nil {
		return nil
	}
	return e.children
}

func TestIsConfiguredTLSHandshakeTimeoutErrorCallerContextWins(t *testing.T) {
	configured := NewConfiguredTLSHandshakeTimeoutError(context.DeadlineExceeded)
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "wrapped configured timeout", err: fmt.Errorf("claude tls: handshake upstream: %w", configured), want: true},
		{name: "two configured timeouts", err: errors.Join(configured, NewConfiguredTLSHandshakeTimeoutError(context.DeadlineExceeded)), want: true},
		{name: "plaintext phrase", err: fmt.Errorf("tls handshake timeout: %w", context.DeadlineExceeded), want: false},
		{name: "encoded phrase", err: fmt.Errorf("tls%%20handshake%%20timeout: %w", context.DeadlineExceeded), want: false},
		{name: "joined caller deadline", err: errors.Join(configured, context.DeadlineExceeded), want: false},
		{name: "joined wrapped caller deadline", err: errors.Join(configured, fmt.Errorf("client: %w", context.DeadlineExceeded)), want: false},
		{name: "joined caller cancellation", err: errors.Join(configured, context.Canceled), want: false},
		{name: "outer caller cancellation", err: fmt.Errorf("outer: %w", errors.Join(configured, context.Canceled)), want: false},
		{name: "custom Is caller cancellation with child", err: errors.Join(configured, &contextIdentityError{target: context.Canceled, child: errors.New("diagnostic")}), want: false},
		{name: "custom Is caller deadline with child", err: errors.Join(configured, &contextIdentityError{target: context.DeadlineExceeded, child: errors.New("diagnostic")}), want: false},
		{name: "custom Is multi unwrap caller cancellation", err: &multiContextIdentityError{target: context.Canceled, children: []error{configured, errors.New("diagnostic")}}, want: false},
		{name: "wrapped custom Is caller cancellation", err: fmt.Errorf("outer: %w", errors.Join(configured, &contextIdentityError{target: context.Canceled, child: errors.New("diagnostic")})), want: false},
		{name: "configured type without deadline cause", err: NewConfiguredTLSHandshakeTimeoutError(errors.New("certificate mismatch")), want: false},
		{name: "nil", err: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsConfiguredTLSHandshakeTimeoutError(tt.err); got != tt.want {
				t.Fatalf("IsConfiguredTLSHandshakeTimeoutError(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}
