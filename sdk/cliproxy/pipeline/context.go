package pipeline

import (
	"context"
	"net/http"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)


type Context struct {

	Request cliproxyexecutor.Request

	Options cliproxyexecutor.Options

	Auth *cliproxyauth.Auth

	Translator *sdktranslator.Pipeline

	HTTPClient *http.Client
}


type Hook interface {
	BeforeExecute(ctx context.Context, execCtx *Context)
	AfterExecute(ctx context.Context, execCtx *Context, resp cliproxyexecutor.Response, err error)
	OnStreamChunk(ctx context.Context, execCtx *Context, chunk cliproxyexecutor.StreamChunk)
}


type HookFunc struct {
	Before func(context.Context, *Context)
	After  func(context.Context, *Context, cliproxyexecutor.Response, error)
	Stream func(context.Context, *Context, cliproxyexecutor.StreamChunk)
}


func (h HookFunc) BeforeExecute(ctx context.Context, execCtx *Context) {
	if h.Before != nil {
		h.Before(ctx, execCtx)
	}
}


func (h HookFunc) AfterExecute(ctx context.Context, execCtx *Context, resp cliproxyexecutor.Response, err error) {
	if h.After != nil {
		h.After(ctx, execCtx, resp, err)
	}
}


func (h HookFunc) OnStreamChunk(ctx context.Context, execCtx *Context, chunk cliproxyexecutor.StreamChunk) {
	if h.Stream != nil {
		h.Stream(ctx, execCtx, chunk)
	}
}


type RoundTripperProvider interface {
	RoundTripperFor(auth *cliproxyauth.Auth) http.RoundTripper
}
