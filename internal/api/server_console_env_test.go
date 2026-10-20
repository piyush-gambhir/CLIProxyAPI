package api

import (
	"crypto/tls"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsoleEnvironmentAuthentication(t *testing.T) {
	t.Setenv("CLIPROXY_CONSOLE_MANAGEMENT_KEY", "test-management")
	s, _ := nativeConsoleTestServer(t)
	for _, tc := range []struct {
		name, peer, host, origin, site, marker, bearer string
		status                                         int
	}{
		{"local", "127.0.0.1:1234", "127.0.0.1:8317", "", "same-origin", "1", "", 200},
		{"local origin", "127.0.0.1:1234", "127.0.0.1:8317", "http://127.0.0.1:8317", "same-origin", "1", "", 200},
		{"remote peer", "203.0.113.5:1234", "127.0.0.1:8317", "", "same-origin", "1", "", 403},
		{"rebound host", "127.0.0.1:1234", "evil.example", "", "same-origin", "1", "", 403},
		{"cross origin", "127.0.0.1:1234", "127.0.0.1:8317", "https://evil.example", "same-origin", "1", "", 403},
		{"other local port", "127.0.0.1:1234", "127.0.0.1:8317", "http://127.0.0.1:9999", "same-origin", "1", "", 403},
		{"cross site", "127.0.0.1:1234", "127.0.0.1:8317", "", "cross-site", "1", "", 403},
		{"no fetch metadata", "127.0.0.1:1234", "127.0.0.1:8317", "", "", "1", "", 403},
		{"no marker", "127.0.0.1:1234", "127.0.0.1:8317", "", "same-origin", "", "", 401},
		{"explicit invalid key", "127.0.0.1:1234", "127.0.0.1:8317", "", "same-origin", "1", "invalid", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://"+tc.host+"/v0/management/console/settings", nil)
			r.RemoteAddr = tc.peer
			// Match the production protocol multiplexer on an unencrypted connection.
			r.TLS = &tls.ConnectionState{}
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			r.Header.Set("X-Cliproxy-Console", tc.marker)
			if tc.bearer != "" {
				r.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			w := httptest.NewRecorder()
			s.engine.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d", w.Code, tc.status)
			}
			if strings.Contains(w.Body.String(), "test-management") {
				t.Fatal("secret exposed")
			}
		})
	}
	s.console.managementKey = ""
	r := httptest.NewRequest("GET", "http://127.0.0.1:8317/v0/management/console/settings", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("X-Cliproxy-Console", "1")
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("unconfigured status %d", w.Code)
	}
}
