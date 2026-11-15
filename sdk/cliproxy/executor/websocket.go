package executor

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
)

type UpstreamWebsocketReplayRequiredError struct{}

func (*UpstreamWebsocketReplayRequiredError) Error() string {
	return `{"error":{"message":"upstream transport requires full HTTP replay","type":"server_error","code":"upstream_http_replay_required","status":426}}`
}

func (*UpstreamWebsocketReplayRequiredError) StatusCode() int { return http.StatusUpgradeRequired }

func (*UpstreamWebsocketReplayRequiredError) IsRequestScoped() bool { return true }

func NewUpstreamWebsocketReplayRequiredError() error {
	return &UpstreamWebsocketReplayRequiredError{}
}

func IsUpstreamWebsocketReplayRequired(err error) bool {
	var replayErr *UpstreamWebsocketReplayRequiredError
	return errors.As(err, &replayErr)
}

type UpstreamWebsocketTurnError struct {
	Cause error

	FullContextRequired bool

	NotSent bool
	EventID string

	Idle bool

	Abandoned []*UpstreamWebsocketTurnError
}

func (e *UpstreamWebsocketTurnError) Error() string {
	if e.FullContextRequired {
		return `{"error":{"message":"Previous response is not available on this websocket; resend the full conversation input without previous_response_id","type":"invalid_request_error","code":"previous_response_not_found","param":"previous_response_id"}}`
	}
	if e.NotSent {
		return `{"error":{"message":"Operation was not sent because the upstream websocket was lost. Send a new response.create with full conversation input.","type":"invalid_request_error","code":"upstream_websocket_not_sent"}}`
	}
	return `{"error":{"message":"Upstream websocket disconnected. The turn was not replayed and its outcome may be unknown. Send a new response.create with full conversation input without previous_response_id.","type":"server_error","code":"upstream_websocket_lost"}}`
}

func (e *UpstreamWebsocketTurnError) Unwrap() error { return e.Cause }
func (e *UpstreamWebsocketTurnError) StatusCode() int {
	if e.FullContextRequired {
		return http.StatusConflict
	}
	return http.StatusBadGateway
}
func (*UpstreamWebsocketTurnError) IsRequestScoped() bool { return true }

func IsUpstreamWebsocketTurnError(err error) bool {
	var turnErr *UpstreamWebsocketTurnError
	return errors.As(err, &turnErr)
}

type websocketInputLossState struct {
	epoch        *atomic.Uint64
	messageEpoch uint64
}

type websocketInputLossContextKey struct{}

func WithWebsocketInputEpoch(ctx context.Context, epoch *atomic.Uint64, messageEpoch uint64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, websocketInputLossContextKey{}, websocketInputLossState{
		epoch:        epoch,
		messageEpoch: messageEpoch,
	})
}

func NotifyWebsocketInputLoss(ctx context.Context) {
	if ctx == nil {
		return
	}
	if state, ok := ctx.Value(websocketInputLossContextKey{}).(websocketInputLossState); ok && state.epoch != nil {
		state.epoch.Add(1)
	}
}

func WebsocketInputPredatesLoss(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	state, ok := ctx.Value(websocketInputLossContextKey{}).(websocketInputLossState)
	return ok && state.epoch != nil && state.messageEpoch < state.epoch.Load()
}

func WebsocketInputLossError(ctx context.Context) error {
	if !WebsocketInputPredatesLoss(ctx) {
		return nil
	}
	if RequiredUpstreamWebsocket(ctx) {
		return &UpstreamWebsocketTurnError{FullContextRequired: true}
	}
	return &UpstreamWebsocketTurnError{NotSent: true}
}
