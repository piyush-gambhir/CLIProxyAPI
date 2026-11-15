package auth

import "context"

type skipPersistContextKey struct{}
type deferAPIKeyModelAliasRebuildContextKey struct{}
type authCreationIntentContextKey struct{}

func WithAuthCreationIntent(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, authCreationIntentContextKey{}, true)
}

func HasAuthCreationIntent(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v := ctx.Value(authCreationIntentContextKey{})
	enabled, ok := v.(bool)
	return ok && enabled
}

func WithSkipPersist(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, skipPersistContextKey{}, true)
}

func WithoutSkipPersist(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	if !shouldSkipPersist(ctx) {
		return ctx
	}
	return context.WithValue(ctx, skipPersistContextKey{}, false)
}

func shouldSkipPersist(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v := ctx.Value(skipPersistContextKey{})
	enabled, ok := v.(bool)
	return ok && enabled
}

func WithDeferredAPIKeyModelAliasRebuild(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, deferAPIKeyModelAliasRebuildContextKey{}, true)
}

func shouldDeferAPIKeyModelAliasRebuild(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v := ctx.Value(deferAPIKeyModelAliasRebuildContextKey{})
	enabled, ok := v.(bool)
	return ok && enabled
}
