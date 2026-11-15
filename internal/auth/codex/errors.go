package codex

import (
	"errors"
	"fmt"
	"net/http"
)

type OAuthError struct {
	Code string `json:"error"`

	Description string `json:"error_description,omitempty"`

	URI string `json:"error_uri,omitempty"`

	StatusCode int `json:"-"`
}

func (e *OAuthError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("OAuth error %s: %s", e.Code, e.Description)
	}
	return fmt.Sprintf("OAuth error: %s", e.Code)
}

func NewOAuthError(code, description string, statusCode int) *OAuthError {
	return &OAuthError{
		Code:        code,
		Description: description,
		StatusCode:  statusCode,
	}
}

type AuthenticationError struct {
	Type string `json:"type"`

	Message string `json:"message"`

	Code int `json:"code"`

	Cause error `json:"-"`
}

func (e *AuthenticationError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Type, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

var (
	ErrInvalidState = &AuthenticationError{
		Type:    "invalid_state",
		Message: "OAuth state parameter is invalid",
		Code:    http.StatusBadRequest,
	}

	ErrCodeExchangeFailed = &AuthenticationError{
		Type:    "code_exchange_failed",
		Message: "Failed to exchange authorization code for tokens",
		Code:    http.StatusBadRequest,
	}

	ErrServerStartFailed = &AuthenticationError{
		Type:    "server_start_failed",
		Message: "Failed to start OAuth callback server",
		Code:    http.StatusInternalServerError,
	}

	ErrPortInUse = &AuthenticationError{
		Type:    "port_in_use",
		Message: "OAuth callback port is already in use",
		Code:    13,
	}

	ErrCallbackTimeout = &AuthenticationError{
		Type:    "callback_timeout",
		Message: "Timeout waiting for OAuth callback",
		Code:    http.StatusRequestTimeout,
	}

	ErrBrowserOpenFailed = &AuthenticationError{
		Type:    "browser_open_failed",
		Message: "Failed to open browser for authentication",
		Code:    http.StatusInternalServerError,
	}
)

func NewAuthenticationError(baseErr *AuthenticationError, cause error) *AuthenticationError {
	return &AuthenticationError{
		Type:    baseErr.Type,
		Message: baseErr.Message,
		Code:    baseErr.Code,
		Cause:   cause,
	}
}

func IsAuthenticationError(err error) bool {
	var authenticationError *AuthenticationError
	ok := errors.As(err, &authenticationError)
	return ok
}

func IsOAuthError(err error) bool {
	var oAuthError *OAuthError
	ok := errors.As(err, &oAuthError)
	return ok
}

func GetUserFriendlyMessage(err error) string {
	switch {
	case IsAuthenticationError(err):
		var authErr *AuthenticationError
		errors.As(err, &authErr)
		switch authErr.Type {
		case "token_expired":
			return "Your authentication has expired. Please log in again."
		case "token_invalid":
			return "Your authentication is invalid. Please log in again."
		case "authentication_required":
			return "Please log in to continue."
		case "port_in_use":
			return "The required port is already in use. Please close any applications using port 3000 and try again."
		case "callback_timeout":
			return "Authentication timed out. Please try again."
		case "browser_open_failed":
			return "Could not open your browser automatically. Please copy and paste the URL manually."
		default:
			return "Authentication failed. Please try again."
		}
	case IsOAuthError(err):
		var oauthErr *OAuthError
		errors.As(err, &oauthErr)
		switch oauthErr.Code {
		case "access_denied":
			return "Authentication was cancelled or denied."
		case "invalid_request":
			return "Invalid authentication request. Please try again."
		case "server_error":
			return "Authentication server error. Please try again later."
		default:
			return fmt.Sprintf("Authentication failed: %s", oauthErr.Description)
		}
	default:
		return "An unexpected error occurred. Please try again."
	}
}
