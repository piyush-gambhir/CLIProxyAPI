package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)







func TestCORSPreflightExplicitlyAllowsAuthorization(t *testing.T) {
	server := newTestServer(t)

	cases := []struct {
		name           string
		method         string
		path           string
		requestHeaders string
		wantAllowed    []string
	}{
		{
			name:           "models get with authorization",
			method:         http.MethodGet,
			path:           "/v1/models",
			requestHeaders: "authorization",
			wantAllowed:    []string{"Authorization"},
		},
		{
			name:           "chat completions post with authorization and content type",
			method:         http.MethodPost,
			path:           "/v1/chat/completions",
			requestHeaders: "authorization,content-type",
			wantAllowed:    []string{"Authorization", "Content-Type"},
		},
		{
			name:           "alternative credential headers",
			method:         http.MethodGet,
			path:           "/v1/models",
			requestHeaders: "x-api-key,x-goog-api-key",
			wantAllowed:    []string{"X-Api-Key", "X-Goog-Api-Key"},
		},
		{
			name:           "anthropic headers",
			method:         http.MethodPost,
			path:           "/v1/messages",
			requestHeaders: "x-api-key,anthropic-version,anthropic-beta",
			wantAllowed:    []string{"X-Api-Key", "Anthropic-Version", "Anthropic-Beta"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodOptions, tc.path, nil)
			req.Header.Set("Origin", "http://127.0.0.1:8765")
			req.Header.Set("Access-Control-Request-Method", tc.method)
			req.Header.Set("Access-Control-Request-Headers", tc.requestHeaders)
			recorder := httptest.NewRecorder()
			server.engine.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNoContent {
				t.Fatalf("preflight status = %d, want %d", recorder.Code, http.StatusNoContent)
			}
			allowHeaders := recorder.Header().Get("Access-Control-Allow-Headers")
			if allowHeaders == "*" {
				t.Fatalf("Access-Control-Allow-Headers = %q, want an explicit list including %v", allowHeaders, tc.wantAllowed)
			}
			allowed := make(map[string]bool)
			for _, name := range strings.Split(allowHeaders, ",") {
				allowed[http.CanonicalHeaderKey(strings.TrimSpace(name))] = true
			}
			for _, want := range tc.wantAllowed {
				if !allowed[http.CanonicalHeaderKey(want)] {
					t.Errorf("Access-Control-Allow-Headers %q does not explicitly allow %s", allowHeaders, want)
				}
			}
		})
	}
}





func TestCORSPreflightEchoesRequestedAllowHeaders(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodOptions, "/v1/chat/completions", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8765")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "authorization, x-custom-sdk-header")
	recorder := httptest.NewRecorder()
	server.engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	allowHeaders := recorder.Header().Get("Access-Control-Allow-Headers")
	if allowHeaders == "*" {
		t.Fatalf("Access-Control-Allow-Headers = %q, want the requested headers echoed explicitly", allowHeaders)
	}
	allowed := make(map[string]bool)
	for _, name := range strings.Split(allowHeaders, ",") {
		allowed[http.CanonicalHeaderKey(strings.TrimSpace(name))] = true
	}
	for _, want := range []string{"Authorization", "X-Custom-Sdk-Header"} {
		if !allowed[want] {
			t.Errorf("Access-Control-Allow-Headers %q does not explicitly allow %s", allowHeaders, want)
		}
	}
}
