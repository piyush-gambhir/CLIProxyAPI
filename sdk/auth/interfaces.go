package auth

import (
	"context"
	"errors"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

var ErrRefreshNotSupported = errors.New("cliproxy auth: refresh not supported")



type LoginOptions struct {
	NoBrowser    bool
	ProjectID    string
	CallbackPort int
	Metadata     map[string]string
	Prompt       func(prompt string) (string, error)
	ProxyURL     string
}


type Authenticator interface {
	Provider() string
	Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error)
	RefreshLead() *time.Duration
}
