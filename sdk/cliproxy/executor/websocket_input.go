package executor

import (
	"context"
	"errors"
)

var ErrNoActiveUpstreamWebsocket = errors.New("no active upstream websocket for response.interrupt")

var ErrUpstreamWebsocketInterruptRejected = errors.New("response.interrupt does not target the active upstream response")

type WebsocketInput struct {
	Payload []byte
	Err     error

	Epoch uint64
}
type websocketInputKey struct{}

func WithWebsocketInput(ctx context.Context, input <-chan WebsocketInput) context.Context {
	return context.WithValue(ctx, websocketInputKey{}, input)
}
func WebsocketInputFromContext(ctx context.Context) <-chan WebsocketInput {
	if ctx == nil {
		return nil
	}
	input, _ := ctx.Value(websocketInputKey{}).(<-chan WebsocketInput)
	return input
}

type websocketAuthCheckKey struct{}

func WithWebsocketAuthCheck(ctx context.Context, check func(string) bool) context.Context {
	return context.WithValue(ctx, websocketAuthCheckKey{}, check)
}
func WebsocketAuthEnabled(ctx context.Context, authID string) bool {
	if ctx == nil {
		return true
	}
	check, _ := ctx.Value(websocketAuthCheckKey{}).(func(string) bool)
	return check == nil || check(authID)
}

type websocketTurnAuthorizerKey struct{}

func WithWebsocketTurnAuthorizer(ctx context.Context, authorize func([]byte) ([]byte, error)) context.Context {
	return context.WithValue(ctx, websocketTurnAuthorizerKey{}, authorize)
}

func AuthorizeWebsocketTurn(ctx context.Context, payload []byte) ([]byte, error) {
	if ctx == nil {
		return nil, nil
	}
	authorize, _ := ctx.Value(websocketTurnAuthorizerKey{}).(func([]byte) ([]byte, error))
	if authorize == nil {
		return nil, nil
	}
	return authorize(payload)
}
