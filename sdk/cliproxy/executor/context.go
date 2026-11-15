package executor

import (
	"context"
	"sync/atomic"
)

type downstreamWebsocketContextKey struct{}
type requireUpstreamWebsocketContextKey struct{}
type upstreamAttemptTrackerContextKey struct{}
type localWebsocketTurnTrackerContextKey struct{}

type upstreamAttemptTracker struct {
	attempted atomic.Bool
}

type localWebsocketTurnTracker struct {
	executed atomic.Bool
}

func WithDownstreamWebsocket(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, downstreamWebsocketContextKey{}, true)
}

func DownstreamWebsocket(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	raw := ctx.Value(downstreamWebsocketContextKey{})
	enabled, ok := raw.(bool)
	return ok && enabled
}

func WithRequiredUpstreamWebsocket(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requireUpstreamWebsocketContextKey{}, true)
}

func RequiredUpstreamWebsocket(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	raw := ctx.Value(requireUpstreamWebsocketContextKey{})
	enabled, ok := raw.(bool)
	return ok && enabled
}

func WithUpstreamAttemptTracker(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, upstreamAttemptTrackerContextKey{}, &upstreamAttemptTracker{})
}

func MarkUpstreamAttempt(ctx context.Context) {
	if ctx == nil {
		return
	}
	tracker, ok := ctx.Value(upstreamAttemptTrackerContextKey{}).(*upstreamAttemptTracker)
	if !ok || tracker == nil {
		return
	}
	tracker.attempted.Store(true)
}

func UpstreamAttempted(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	tracker, ok := ctx.Value(upstreamAttemptTrackerContextKey{}).(*upstreamAttemptTracker)
	return ok && tracker != nil && tracker.attempted.Load()
}

func WithLocalWebsocketTurnTracker(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, localWebsocketTurnTrackerContextKey{}, &localWebsocketTurnTracker{})
}

func MarkLocalWebsocketTurn(ctx context.Context) {
	if ctx == nil {
		return
	}
	tracker, ok := ctx.Value(localWebsocketTurnTrackerContextKey{}).(*localWebsocketTurnTracker)
	if !ok || tracker == nil {
		return
	}
	tracker.executed.Store(true)
}

func ResetLocalWebsocketTurn(ctx context.Context) {
	if ctx == nil {
		return
	}
	tracker, ok := ctx.Value(localWebsocketTurnTrackerContextKey{}).(*localWebsocketTurnTracker)
	if !ok || tracker == nil {
		return
	}
	tracker.executed.Store(false)
}

func LocalWebsocketTurnExecuted(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	tracker, ok := ctx.Value(localWebsocketTurnTrackerContextKey{}).(*localWebsocketTurnTracker)
	return ok && tracker != nil && tracker.executed.Load()
}
