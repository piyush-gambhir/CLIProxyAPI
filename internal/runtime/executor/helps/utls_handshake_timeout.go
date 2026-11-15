package helps

import (
	"context"
	"errors"
	"net/http"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)






type tlsHandshakeTimeoutContextKey struct{}




func withTLSHandshakeTimeout(ctx context.Context, timeout time.Duration) context.Context {
	if ctx == nil || timeout <= 0 {
		return ctx
	}
	return context.WithValue(ctx, tlsHandshakeTimeoutContextKey{}, timeout)
}



func tlsHandshakeTimeoutFromContext(ctx context.Context) time.Duration {
	if ctx == nil {
		return 0
	}
	timeout, _ := ctx.Value(tlsHandshakeTimeoutContextKey{}).(time.Duration)
	if timeout <= 0 {
		return 0
	}
	return timeout
}





type tlsHandshakeTimeoutRoundTripper struct {
	base    http.RoundTripper
	timeout time.Duration
}

func (rt *tlsHandshakeTimeoutRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.timeout > 0 {
		req = req.WithContext(withTLSHandshakeTimeout(req.Context(), rt.timeout))
	}
	return rt.base.RoundTrip(req)
}












func handshakeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := tlsHandshakeTimeoutFromContext(ctx)
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}





func classifyHandshakeError(parentCtx, handshakeCtx context.Context, err error) error {
	if err == nil || tlsHandshakeTimeoutFromContext(parentCtx) <= 0 {
		return err
	}
	if parentCtx != nil && parentCtx.Err() != nil {
		return err
	}
	if handshakeCtx == nil || !errors.Is(handshakeCtx.Err(), context.DeadlineExceeded) {
		return err
	}
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return err
	}
	return cliproxyexecutor.NewConfiguredTLSHandshakeTimeoutError(err)
}
