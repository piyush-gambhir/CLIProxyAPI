package executor

import (
	"context"
	"strings"
)

type requestProxyContextKey struct{}



func WithRequestProxyURL(ctx context.Context, proxyURL string) context.Context {
	proxyURL = strings.TrimSpace(proxyURL)
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requestProxyContextKey{}, proxyURL)
}



func WithoutRequestProxyURL(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithValue(ctx, requestProxyContextKey{}, "")
}


func RequestProxyURL(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	proxyURL, _ := ctx.Value(requestProxyContextKey{}).(string)
	return strings.TrimSpace(proxyURL)
}
