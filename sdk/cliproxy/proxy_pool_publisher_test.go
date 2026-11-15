package cliproxy

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestServiceProxyPoolFenceIsPerService(t *testing.T) {
	helps.ResetProxyTransports()
	t.Cleanup(helps.ResetProxyTransports)

	build := func() *Service {
		t.Helper()
		dir := t.TempDir()
		service, errBuild := NewBuilder().
			WithConfig(&config.Config{AuthDir: dir}).
			WithConfigPath(filepath.Join(dir, "config.yaml")).
			Build()
		if errBuild != nil {
			t.Fatalf("build service: %v", errBuild)
		}
		return service
	}

	auth := &coreauth.Auth{
		ID:                  "service-pool-key",
		ProxyURL:            "direct",
		ProxyConnectionPool: &coreauth.ProxyConnectionPoolOverride{},
	}
	transportFor := func(service *Service) *http.Transport {
		t.Helper()
		client := helps.NewProxyAwareHTTPClient(context.Background(), service.currentConfig(), auth, 0)
		transport, ok := client.Transport.(*http.Transport)
		if !ok || transport == nil {
			t.Fatalf("transport = %T, want a pooled *http.Transport", client.Transport)
		}
		return transport
	}

	first := build()
	if first.proxyPoolPublisher == 0 {
		t.Fatal("service did not reserve a pool publisher")
	}
	firstTransport := transportFor(first)
	if got := transportFor(first); got != firstTransport {
		t.Fatal("first service did not reuse its pool")
	}

	second := build()
	if second.proxyPoolPublisher == first.proxyPoolPublisher {
		t.Fatal("services share one pool publisher")
	}
	if got := transportFor(first); got != firstTransport {
		t.Fatal("building another service retired the active service's pool")
	}

	if commit := second.commitConfigUpdate(&config.Config{AuthDir: second.currentConfig().AuthDir}); commit.cfg == nil {
		t.Fatal("config update was rejected")
	}
	if got := transportFor(first); got != firstTransport {
		t.Fatal("reloading another service retired the active service's pool")
	}

	secondTransport := transportFor(second)
	secondCfgBeforeShutdown := second.currentConfig()
	if errShutdown := second.Shutdown(context.Background()); errShutdown != nil {
		t.Fatalf("shutdown second service: %v", errShutdown)
	}
	if got := transportFor(first); got != firstTransport {
		t.Fatal("shutting down another service retired the active service's pool")
	}
	afterShutdownClient := helps.NewProxyAwareHTTPClient(context.Background(), secondCfgBeforeShutdown, auth, 0)
	afterShutdownTransport, ok := afterShutdownClient.Transport.(*http.Transport)
	if !ok || afterShutdownTransport == nil || afterShutdownTransport == secondTransport || !afterShutdownTransport.DisableKeepAlives {
		t.Fatalf("post-shutdown snapshot must be fenced into an unpooled transport, got %#v", afterShutdownClient.Transport)
	}
}

func TestOpenAICompatExecutorRebindsWhenConfigGenerationAdvances(t *testing.T) {
	helps.ResetProxyTransports()
	t.Cleanup(helps.ResetProxyTransports)

	dir := t.TempDir()
	service, errBuild := NewBuilder().
		WithConfig(&config.Config{AuthDir: dir}).
		WithConfigPath(filepath.Join(dir, "config.yaml")).
		Build()
	if errBuild != nil {
		t.Fatalf("build service: %v", errBuild)
	}

	compatAuth := &coreauth.Auth{
		ID:       "compat-pool-key",
		Provider: "openai-compatibility",
		Label:    "ollama",
		Attributes: map[string]string{
			"compat_name":  "ollama",
			"provider_key": "ollama",
			"base_url":     "https://ollama.com/v1",
		},
		ProxyConnectionPool: &coreauth.ProxyConnectionPoolOverride{},
	}

	service.ensureExecutorsForAuthWithContext(context.Background(), compatAuth, false)
	exec1, ok := service.coreManager.Executor("openai-compatible-ollama")
	if !ok || exec1 == nil {
		t.Fatal("expected initial openai-compatible-ollama executor")
	}

	if commit := service.commitConfigUpdate(&config.Config{AuthDir: dir}); commit.cfg == nil {
		t.Fatal("config update rejected")
	}

	service.ensureExecutorsForAuthWithContext(context.Background(), compatAuth, false)
	exec2, ok := service.coreManager.Executor("openai-compatible-ollama")
	if !ok || exec2 == nil {
		t.Fatal("expected rebound openai-compatible-ollama executor")
	}
	if exec2 == exec1 {
		t.Fatal("ensureExecutorsForAuth kept stale OpenAICompatExecutor holding retired config generation")
	}
}
