package helps

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)



func enabledPoolConfig() *config.Config { return testConfig() }




func TestProxyPoolIsolatesCredentials(t *testing.T) {
	ResetProxyTransports()
	t.Cleanup(ResetProxyTransports)
	cfg := enabledPoolConfig()

	first := poolOptInAuth("key-a")
	second := poolOptInAuth("key-b")
	for _, proxyURL := range []string{
		"",
		"direct",
		"http://127.0.0.1:8080",
		"socks5://127.0.0.1:1080",
		"http://user:pass@127.0.0.1:8080",
		"socks5://user:pass@127.0.0.1:1080",
	} {
		firstTransport := pooledProxyTransport(proxyURL, cfg, first)
		secondTransport := pooledProxyTransport(proxyURL, cfg, second)
		if firstTransport == nil || secondTransport == nil {
			t.Fatalf("proxy %q: pooled transports missing", proxyURL)
		}
		if firstTransport == secondTransport {
			t.Fatalf("proxy %q: different credentials shared one pool", proxyURL)
		}
		if pooledProxyTransport(proxyURL, cfg, first) != firstTransport {
			t.Fatalf("proxy %q: the same credential must reuse its pool", proxyURL)
		}
	}

	if pooledProxyTransport("http://127.0.0.1:8080", cfg, first) == pooledProxyTransport("http://127.0.0.1:8081", cfg, first) {
		t.Fatal("different unauthenticated proxies shared one pool")
	}


	keyOne := poolAuth("", poolEnabled())
	keyOne.Attributes = map[string]string{cliproxyauth.AttributeAPIKey: "sk-one"}
	keyTwo := poolAuth("", poolEnabled())
	keyTwo.Attributes = map[string]string{cliproxyauth.AttributeAPIKey: "sk-two"}
	if pooledProxyTransport("direct", cfg, keyOne) == pooledProxyTransport("direct", cfg, keyTwo) {
		t.Fatal("different API keys share one pool")
	}
	if scope := proxyPoolScope(keyOne); strings.Contains(scope, "sk-one") {
		t.Fatalf("pool scope leaks credential material: %q", scope)
	}
	if proxyPoolScope(keyOne) != proxyPoolScope(&cliproxyauth.Auth{Attributes: map[string]string{cliproxyauth.AttributeAPIKey: "sk-one"}}) {
		t.Fatal("identical credentials must map to one scope")
	}

	if proxyPoolScope(&cliproxyauth.Auth{Label: "same"}) != proxyPoolScope(&cliproxyauth.Auth{Label: "same"}) {
		t.Fatal("label-only auths must share the anonymous scope")
	}
	if got := proxyPoolScope(&cliproxyauth.Auth{Label: "shared"}); got != proxyPoolAnonymousScope {
		t.Fatalf("label must not become a scope, got %q", got)
	}


	rotatedBefore := poolAuth("stable-id", poolEnabled())
	rotatedBefore.Attributes = map[string]string{cliproxyauth.AttributeAPIKey: "sk-old"}
	rotatedAfter := poolAuth("stable-id", poolEnabled())
	rotatedAfter.Attributes = map[string]string{cliproxyauth.AttributeAPIKey: "sk-new"}
	if pooledProxyTransport("direct", cfg, rotatedBefore) == pooledProxyTransport("direct", cfg, rotatedAfter) {
		t.Fatal("rotating API key under the same Auth.ID must not reuse the old key's pool")
	}
}



func TestProxyPoolBoundsIdleSocketsPerCredential(t *testing.T) {
	ResetProxyTransports()
	t.Cleanup(ResetProxyTransports)
	tr := pooledProxyTransport("direct", enabledPoolConfig(), poolOptInAuth("bounded"))
	if tr == nil {
		t.Fatal("pooled transport missing")
	}
	if tr.MaxIdleConns != proxyPoolDefaultMaxIdleConnsPerHost {
		t.Fatalf("MaxIdleConns = %d, want %d", tr.MaxIdleConns, proxyPoolDefaultMaxIdleConnsPerHost)
	}
	if tr.MaxIdleConnsPerHost != proxyPoolDefaultMaxIdleConnsPerHost {
		t.Fatalf("MaxIdleConnsPerHost = %d, want %d", tr.MaxIdleConnsPerHost, proxyPoolDefaultMaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout != proxyPoolDefaultIdleConnTimeout {
		t.Fatalf("IdleConnTimeout = %s, want %s", tr.IdleConnTimeout, proxyPoolDefaultIdleConnTimeout)
	}
}




func TestProxyPoolCapacityBoundsManyCredentials(t *testing.T) {
	ResetProxyTransports()
	t.Cleanup(ResetProxyTransports)
	cfg := enabledPoolConfig()
	total := proxyPoolCapacity + 120
	for i := 0; i < total; i++ {
		auth := poolOptInAuth("key-" + strconv.Itoa(i))
		if tr := pooledProxyTransport("direct", cfg, auth); tr == nil {
			t.Fatalf("credential %d got no transport", i)
		}
	}
	if got := proxyTransportCache.Len(); got != proxyPoolCapacity {
		t.Fatalf("cache holds %d pools, want %d", got, proxyPoolCapacity)
	}

	hot := poolOptInAuth("hot")
	hotTransport := pooledProxyTransport("direct", cfg, hot)
	for i := 0; i < 16; i++ {
		pooledProxyTransport("direct", cfg, poolOptInAuth("cold-"+strconv.Itoa(i)))
	}
	if pooledProxyTransport("direct", cfg, hot) != hotTransport {
		t.Fatal("recently used credential lost its pool")
	}
	if got := proxyTransportCache.Len(); got > proxyPoolCapacity {
		t.Fatalf("cache grew past capacity: %d", got)
	}
}





func TestEvictedTransportDoesNotRebuildIdlePool(t *testing.T) {
	ResetProxyTransports()
	t.Cleanup(ResetProxyTransports)
	cfg := enabledPoolConfig()

	closedCh := make(chan struct{}, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			select {
			case closedCh <- struct{}{}:
			default:
			}
		}
	}
	server.Start()
	defer server.Close()

	staleAuth := poolOptInAuth("evicted-victim")
	staleAuth.ProxyURL = "direct"
	staleClient := NewProxyAwareHTTPClient(context.Background(), cfg, staleAuth, 0)


	for i := 0; i < proxyPoolCapacity+8; i++ {
		other := poolOptInAuth("evict-filler-" + strconv.Itoa(i))
		if tr := pooledProxyTransport("direct", cfg, other); tr == nil {
			t.Fatalf("filler %d got no transport", i)
		}
	}

	resp, err := staleClient.Get(server.URL)
	if err != nil {
		t.Fatalf("request on evicted transport failed: %v", err)
	}
	if _, errCopy := io.Copy(io.Discard, resp.Body); errCopy != nil {
		t.Fatalf("read body: %v", errCopy)
	}
	if errClose := resp.Body.Close(); errClose != nil {
		t.Fatalf("close body: %v", errClose)
	}



	<-closedCh
}




func TestProxyPoolDropsUnexpectedlyClosedConnection(t *testing.T) {
	ResetProxyTransports()
	t.Cleanup(ResetProxyTransports)
	var opened atomic.Int32
	var mu sync.Mutex
	var live []net.Conn
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	server.Config.ConnState = func(conn net.Conn, state http.ConnState) {
		if state != http.StateNew {
			return
		}
		opened.Add(1)
		mu.Lock()
		live = append(live, conn)
		mu.Unlock()
	}
	server.Start()
	defer server.Close()

	cfg := enabledPoolConfig()
	auth := poolAuth("dead-peer", poolEnabled())
	auth.ProxyURL = "direct"
	request := func() {
		client := NewProxyAwareHTTPClient(context.Background(), cfg, auth, 0)
		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if _, errCopy := io.Copy(io.Discard, resp.Body); errCopy != nil {
			t.Fatalf("read body: %v", errCopy)
		}
		if errClose := resp.Body.Close(); errClose != nil {
			t.Fatalf("close body: %v", errClose)
		}
	}

	request()
	if got := opened.Load(); got != 1 {
		t.Fatalf("connections after first request = %d, want 1", got)
	}


	mu.Lock()
	for _, conn := range live {
		_ = conn.Close()
	}
	mu.Unlock()
	request()
	if got := opened.Load(); got < 2 {
		t.Fatalf("connections = %d, want a fresh dial after the peer closed", got)
	}
}
