package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	management "github.com/router-for-me/CLIProxyAPI/v7/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	consolecore "github.com/router-for-me/CLIProxyAPI/v7/internal/console"
)

func nativeConsoleTestServer(t *testing.T) (*Server, *gatewayTestExecutor) {
	t.Helper()
	s, e := gatewayTestServer(t)
	s.cfg = &config.Config{Port: 8317, AuthDir: t.TempDir()}
	s.configFilePath = filepath.Join(t.TempDir(), "proxy.yaml")
	t.Setenv("CLIPROXY_CONSOLE_DATA_DIR", t.TempDir())
	s.cfg.RemoteManagement.SecretKey = "test-hash-placeholder"
	s.mgmt = management.NewHandler(s.cfg, s.configFilePath, s.handlers.AuthManager)
	s.mgmt.SetLocalPassword("test-management")
	s.managementRoutesEnabled.Store(true)
	s.setupConsole()
	if s.console.err != nil {
		t.Fatal(s.console.err)
	}
	t.Cleanup(func() { _ = s.console.store.Close() })
	s.registerManagementRoutes()
	return s, e
}
func nativeRequest(s *Server, method, route, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8317/v0/management/console/"+route, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:41000"
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	out := httptest.NewRecorder()
	s.engine.ServeHTTP(out, r)
	return out
}
func TestNativeConsoleAuthenticationAndNoSecrets(t *testing.T) {
	s, _ := nativeConsoleTestServer(t)
	if r := nativeRequest(s, "GET", "settings", "", ""); r.Code != 401 {
		t.Fatalf("unauthenticated %d", r.Code)
	}
	if r := nativeRequest(s, "GET", "settings", "", "test-management"); r.Code != 200 {
		t.Fatalf("settings %d %s", r.Code, r.Body)
	}
	r := nativeRequest(s, "PUT", "settings", `{"clientApiKey":"secret-client"}`, "test-management")
	if r.Code != 200 || strings.Contains(r.Body.String(), "secret-client") {
		t.Fatalf("public settings: %d %s", r.Code, r.Body)
	}
	r = nativeRequest(s, "PUT", "settings", `{"proxyUrl":"https://example.com"}`, "test-management")
	if r.Code != 400 {
		t.Fatal("accepted another backend")
	}
}
func TestNativeConsoleRejectsRemoteAndSpoofedForwardedAddress(t *testing.T) {
	s, _ := nativeConsoleTestServer(t)
	r := httptest.NewRequest("GET", "http://127.0.0.1:8317/v0/management/console/settings", nil)
	r.RemoteAddr = "203.0.113.10:40000"
	r.Header.Set("X-Forwarded-For", "127.0.0.1")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = r
	c.Params = gin.Params{{Key: "path", Value: "/settings"}}
	s.handleConsole(c)
	if w.Code != 403 {
		t.Fatalf("remote local-file access accepted %d", w.Code)
	}
}
func TestNativeConsoleProfilesOwnRoutesImmediately(t *testing.T) {
	s, e := nativeConsoleTestServer(t)
	for _, a := range s.handlers.AuthManager.List() {
		if a.ID == "gateway-selected" {
			a.FileName = "claude-selected.json"
			if _, err := s.handlers.AuthManager.Update(context.Background(), a); err != nil {
				t.Fatal(err)
			}
		}
	}
	r := nativeRequest(s, "POST", "profiles", `{"name":"Selected","authFile":"claude-selected.json"}`, "test-management")
	if r.Code != 200 {
		t.Fatalf("create %d %s", r.Code, r.Body)
	}
	var p consolecore.Profile
	if err := json.Unmarshal(r.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	infer := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/inference/"+p.ID+"/v1/messages", strings.NewReader(`{"model":"claude-opus-5","messages":[{"role":"user","content":"private message"}],"max_tokens":10}`))
		w := httptest.NewRecorder()
		s.engine.ServeHTTP(w, req)
		return w
	}
	if r = infer(); r.Code != 200 {
		t.Fatalf("infer %d %s", r.Code, r.Body)
	}
	if len(e.calls) != 1 || e.calls[0] != "gateway-selected" {
		t.Fatal("wrong account")
	}
	history := nativeRequest(s, "GET", "requests", "", "test-management")
	if history.Code != 200 || !strings.Contains(history.Body.String(), `"accountConfirmed":true`) || strings.Contains(history.Body.String(), "private message") {
		t.Fatalf("history %d %s", history.Code, history.Body)
	}
	if r = nativeRequest(s, "DELETE", "profiles/"+p.ID, "", "test-management"); r.Code != 200 {
		t.Fatalf("delete %d %s", r.Code, r.Body)
	}
	if r = infer(); r.Code != 404 {
		t.Fatal("removed route survived")
	}
	if len(e.calls) != 1 {
		t.Fatal("deleted account executed")
	}
	if _, ok := s.accountGateway.route("profile-a"); ok {
		t.Fatal("legacy mirror reactivated after last profile deleted")
	}
}
func TestNativeConsoleReceiptsSurviveProcessRestart(t *testing.T) {
	s, _ := nativeConsoleTestServer(t)
	store := s.console.store
	s.accountGateway.record(accountReceipt{ID: "persist", StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Profile: "p", Model: "claude-opus-5", Completed: true, Status: 200})
	other, err := consolecore.Open(store.Directory, store.Home, store.AuthDir, store.ConfigFile, store.Origin)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	rows, err := other.Receipts("p")
	if err != nil || len(rows) != 1 {
		t.Fatalf("history %d %v", len(rows), err)
	}
}
