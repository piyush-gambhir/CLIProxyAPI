package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

// TestQuotaSchedulerStatusRouteIsRegisteredAndGated checks the status endpoint
// exists under both management groups and is behind the management key like
// every other management route. The v8 path is canonical; v0 is kept because the
// external client that consumes this contract was written against it.
func TestQuotaSchedulerStatusRouteIsRegisteredAndGated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("port: 8317\nremote-management: {secret-key: test-password}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, errLoad := config.LoadConfig(path)
	if errLoad != nil {
		t.Fatal(errLoad)
	}
	cfg.Plugins.Dir = filepath.Dir(path)
	h := management.NewHandler(cfg, path, nil)
	h.SetLocalPassword("test-password")
	server := &Server{cfg: cfg, engine: gin.New(), mgmt: h}
	server.managementRoutesEnabled.Store(true)
	server.registerManagementRoutes()

	routes := make(map[string]bool)
	for _, route := range server.engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, want := range []string{
		"GET /v8/management/quota-scheduler/status",
		"GET /v0/management/quota-scheduler/status",
	} {
		if !routes[want] {
			t.Fatalf("route %q is not registered", want)
		}
	}

	for _, route := range []string{
		"/v8/management/quota-scheduler/status",
		"/v0/management/quota-scheduler/status",
	} {
		unauthorized := httptest.NewRequest(http.MethodGet, route, nil)
		unauthorized.RemoteAddr = "127.0.0.1:1234"
		recorder := httptest.NewRecorder()
		server.engine.ServeHTTP(recorder, unauthorized)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s without a key: status = %d, want 401", route, recorder.Code)
		}

		authorized := httptest.NewRequest(http.MethodGet, route, nil)
		authorized.RemoteAddr = "127.0.0.1:1234"
		authorized.Header.Set("Authorization", "Bearer test-password")
		recorder = httptest.NewRecorder()
		server.engine.ServeHTTP(recorder, authorized)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s with a key: status = %d, want 200, body = %s", route, recorder.Code, recorder.Body.String())
		}
	}
}
