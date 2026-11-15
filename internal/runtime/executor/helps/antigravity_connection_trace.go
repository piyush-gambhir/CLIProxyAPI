package helps

import (
	"net/http"
	"net/http/httptrace"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)



func WithAntigravityHTTPClientTrace(client *http.Client, auth *cliproxyauth.Auth, operation string) *http.Client {
	if client == nil {
		return nil
	}
	clone := *client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	authIndex := ""
	if auth != nil {
		authIndex = auth.Index
	}
	clone.Transport = &antigravityConnectionTraceTransport{
		base: base, auth: cliproxyauth.Auth{Index: authIndex}, operation: operation,
	}
	return &clone
}

type antigravityConnectionTraceTransport struct {
	base      http.RoundTripper
	auth      cliproxyauth.Auth
	operation string
}

func (t *antigravityConnectionTraceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(WithAntigravityConnectionTrace(req, &t.auth, t.operation))
}


func (t *antigravityConnectionTraceTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}



func WithAntigravityConnectionTrace(req *http.Request, auth *cliproxyauth.Auth, operation string) *http.Request {
	if req == nil || req.URL == nil || !log.IsLevelEnabled(log.DebugLevel) {
		return req
	}
	authIndex := ""
	if auth != nil {
		authIndex = auth.Index
	}
	requestID := logging.GetRequestID(req.Context())
	host := req.URL.Hostname()
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			log.WithFields(log.Fields{
				"provider":      "antigravity",
				"operation":     operation,
				"upstream_host": host,
				"auth_index":    authIndex,
				"request_id":    requestID,
				"reused":        info.Reused,
				"was_idle":      info.WasIdle,
				"idle_time":     info.IdleTime.String(),
			}).Debug("antigravity upstream connection acquired")
		},
	}
	return req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
}
