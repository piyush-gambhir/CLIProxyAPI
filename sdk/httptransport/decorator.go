package httptransport

import (
	"context"
	"net/http"
)

type decoratorKey struct{}

type Decorator func(next http.RoundTripper) http.RoundTripper

func WithDecorator(ctx context.Context, decorator Decorator) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, decoratorKey{}, decorator)
}

func WithoutDecorator(ctx context.Context) context.Context {
	return WithDecorator(ctx, nil)
}

func InheritDecorator(ctx, fallback context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if fallback == nil {
		return ctx
	}
	if _, ok := ctx.Value(decoratorKey{}).(Decorator); ok {
		return ctx
	}
	if decorator, ok := fallback.Value(decoratorKey{}).(Decorator); ok && decorator != nil {
		return WithDecorator(ctx, decorator)
	}
	return ctx
}

func Decorate(ctx context.Context, client *http.Client) *http.Client {
	if ctx == nil || client == nil {
		return client
	}
	decorator, _ := ctx.Value(decoratorKey{}).(Decorator)
	if decorator == nil {
		return client
	}
	next := client.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	wrapped := decorator(next)
	if wrapped == nil {
		return client
	}
	clone := *client
	clone.Transport = wrapped
	return &clone
}
