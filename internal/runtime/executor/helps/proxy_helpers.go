package helps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/httptransport"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
	log "github.com/sirupsen/logrus"
)

func NewProxyAwareHTTPClient(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, timeout time.Duration) (clientOut *http.Client) {
	defer func() { clientOut = httptransport.Decorate(ctx, clientOut) }()
	httpClient := &http.Client{}
	if timeout > 0 {
		httpClient.Timeout = timeout
	}

	proxyURL := effectiveProxyURL(ctx, cfg, auth)

	if proxyURL != "" {
		if transport := pooledProxyTransport(proxyURL, cfg, auth); transport != nil {
			httpClient.Transport = transport
			return httpClient
		}

		log.Debugf("failed to setup proxy from URL: %s, falling back to context transport", proxyutil.Redact(proxyURL))
	}

	if ctx != nil {
		if rt, ok := ctx.Value("cliproxy.roundtripper").(http.RoundTripper); ok && rt != nil {
			httpClient.Transport = rt
			return httpClient
		}
	}

	if transport := pooledProxyTransport("", cfg, auth); transport != nil {
		httpClient.Transport = transport
	}

	return httpClient
}

type proxyPoolKey struct {
	publisher   uint64
	kind        proxyPoolKind
	scope       string
	proxy       string
	idle        time.Duration
	max         int
	invalidIdle string
}

type proxyPoolKind uint8

const (
	proxyPoolKindStandard proxyPoolKind = iota
	proxyPoolKindClaudeCode
)

const (
	proxyPoolDefaultIdleConnTimeout = 300 * time.Second

	proxyPoolDefaultMaxIdleConnsPerHost = 2

	proxyPoolKeepAlivePeriod = 30 * time.Second

	proxyPoolCapacity = 512
)

var proxyTransportCache = NewTransportCache[proxyPoolKey](proxyPoolCapacity)

var proxyPoolFence struct {
	sync.RWMutex
	nextPublisher  uint64
	nextGeneration uint64
	current        map[uint64]uint64
}

func RegisterProxyPoolPublisher() uint64 {
	proxyPoolFence.Lock()
	defer proxyPoolFence.Unlock()
	proxyPoolFence.nextPublisher++
	return proxyPoolFence.nextPublisher
}

func AdvanceProxyPoolGeneration(publisher uint64, cfg *config.Config) {
	if cfg == nil {
		return
	}
	proxyPoolFence.Lock()
	proxyPoolFence.nextGeneration++
	generation := proxyPoolFence.nextGeneration
	if proxyPoolFence.current == nil {
		proxyPoolFence.current = make(map[uint64]uint64)
	}
	proxyPoolFence.current[publisher] = generation
	cfg.ProxyPoolPublisher = publisher
	cfg.ProxyPoolGeneration = generation

	toClose := proxyTransportCache.detachMatching(func(key proxyPoolKey) bool { return key.publisher == publisher })
	proxyPoolFence.Unlock()
	for _, t := range toClose {
		t.CloseIdleConnections()
	}
}

func RetireProxyPoolPublisher(publisher uint64) {
	if publisher == 0 {
		return
	}
	proxyPoolFence.Lock()
	delete(proxyPoolFence.current, publisher)
	toClose := proxyTransportCache.detachMatching(func(key proxyPoolKey) bool { return key.publisher == publisher })
	proxyPoolFence.Unlock()
	for _, t := range toClose {
		t.CloseIdleConnections()
	}
}

func ResetProxyTransports() { proxyTransportCache.Purge() }

func pooledProxyTransport(proxyURL string, cfg *config.Config, auth *cliproxyauth.Auth) *http.Transport {
	tr, pooled := pooledTransport(proxyPoolKindStandard, proxyURL, cfg, auth, func() (*http.Transport, error) {
		return buildPooledBaseTransport(proxyURL)
	})
	if pooled {
		return tr
	}

	return buildProxyTransport(proxyURL)
}

func pooledTransport(kind proxyPoolKind, proxyURL string, cfg *config.Config, auth *cliproxyauth.Auth, build func() (*http.Transport, error)) (*http.Transport, bool) {
	settings, enabled := resolveProxyPoolSettings(auth)
	if !enabled {
		return nil, false
	}
	setting, errParse := proxyutil.Parse(proxyURL)
	if errParse != nil {
		return nil, false
	}

	if kind == proxyPoolKindStandard && strings.TrimSpace(proxyURL) == "" {
		if base, ok := http.DefaultTransport.(*http.Transport); !ok || base == nil {
			return nil, false
		}
	}
	idle := proxyPoolDefaultIdleConnTimeout
	invalidIdle := ""
	if raw := strings.TrimSpace(settings.IdleConnTimeout); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			if d <= 0 {
				return unpooledTransport(build), true
			}
			idle = d
		} else {
			invalidIdle = raw
		}
	}
	max := proxyPoolDefaultMaxIdleConnsPerHost
	if settings.MaxIdleConnsPerHost != nil {
		if *settings.MaxIdleConnsPerHost <= 0 {
			return unpooledTransport(build), true
		}
		max = *settings.MaxIdleConnsPerHost
	}
	var publisher uint64
	if cfg != nil {
		publisher = cfg.ProxyPoolPublisher
	}
	cacheProxy, poolLabel := proxyPoolCacheTarget(setting)
	key := proxyPoolKey{
		publisher:   publisher,
		kind:        kind,
		scope:       proxyPoolScope(auth),
		proxy:       cacheProxy,
		idle:        idle,
		max:         max,
		invalidIdle: invalidIdle,
	}

	if cfg != nil && cfg.ProxyPoolGeneration != 0 {
		proxyPoolFence.RLock()
		defer proxyPoolFence.RUnlock()
		if cfg.ProxyPoolGeneration != proxyPoolFence.current[cfg.ProxyPoolPublisher] {
			return unpooledTransport(build), true
		}
	}
	tr, err := proxyTransportCache.Get(key, func() (*http.Transport, error) {
		tr, errBuild := build()
		if errBuild != nil {
			return nil, errBuild
		}
		if tr == nil {
			return nil, fmt.Errorf("build returned no transport")
		}
		if invalidIdle != "" {
			log.Warnf("proxy connection pool: invalid idle-conn-timeout %q, using %s", invalidIdle, proxyPoolDefaultIdleConnTimeout)
		}
		tr.IdleConnTimeout = idle
		tr.MaxIdleConnsPerHost = max

		tr.MaxIdleConns = max
		if dial := tr.DialContext; dial != nil {
			tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				conn, errDial := dial(ctx, network, addr)
				if errDial != nil {
					return nil, errDial
				}
				return stampProxyKeepAlive(conn), nil
			}
		}
		log.Debugf("proxy connection pool: created transport kind=%d %s scope=%s idle=%s max=%d", kind, poolLabel, proxyPoolSecretScope("scope:", key.scope), idle, max)
		return tr, nil
	})
	if err != nil {
		log.Errorf("proxy connection pool: %v", err)
		return nil, true
	}
	return tr, true
}

func resolveProxyPoolSettings(auth *cliproxyauth.Auth) (config.ProxyConnectionPoolConfig, bool) {
	override := proxyPoolOverride(auth)
	if override == nil {
		return config.ProxyConnectionPoolConfig{}, false
	}
	if override.Enabled != nil && !*override.Enabled {
		return config.ProxyConnectionPoolConfig{}, false
	}
	return config.ProxyConnectionPoolConfig{
		Enabled:             override.Enabled,
		IdleConnTimeout:     strings.TrimSpace(override.IdleConnTimeout),
		MaxIdleConnsPerHost: override.MaxIdleConnsPerHost,
	}, true
}

func proxyPoolOverride(auth *cliproxyauth.Auth) *cliproxyauth.ProxyConnectionPoolOverride {
	if auth == nil {
		return nil
	}
	return auth.ProxyConnectionPool
}

func proxyPoolCacheTarget(setting proxyutil.Setting) (cacheProxy, poolLabel string) {
	switch setting.Mode {
	case proxyutil.ModeDirect:

		return "direct", "direct"
	case proxyutil.ModeInherit:
		return "inherit", "inherit"
	default:
		redacted := proxyutil.Redact(setting.Raw)
		if setting.URL != nil && setting.URL.User != nil {
			return redacted + "#" + proxyPoolSecretScope("proxyauth:", setting.URL.User.String()), redacted
		}
		return setting.Raw, redacted
	}
}

const proxyPoolAnonymousScope = "anonymous"

func proxyPoolScope(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return proxyPoolAnonymousScope
	}
	var identity string
	if id := strings.TrimSpace(auth.ID); id != "" {
		identity = "id:" + id
	} else if auth.Attributes != nil {
		if path := strings.TrimSpace(auth.Attributes[cliproxyauth.AttributePath]); path != "" {
			identity = "path:" + path
		} else if source := strings.TrimSpace(auth.Attributes[cliproxyauth.AttributeSource]); source != "" {
			identity = "source:" + source
		}
	}

	secret := proxyPoolCredentialSecretScope(auth)
	if identity != "" && secret != "" {
		return identity + "#" + secret
	}
	if identity != "" {
		return identity
	}
	if secret != "" {
		return secret
	}
	return proxyPoolAnonymousScope
}

func proxyPoolCredentialSecretScope(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return ""
	}
	if refresh := authMetadataString(auth.Metadata, "refresh_token"); refresh != "" {
		return proxyPoolSecretScope("refresh:", refresh)
	}
	if access := authMetadataString(auth.Metadata, "access_token"); access != "" {
		return proxyPoolSecretScope("token:", access)
	}
	if auth.Attributes != nil {
		if apiKey := strings.TrimSpace(auth.Attributes[cliproxyauth.AttributeAPIKey]); apiKey != "" {
			return proxyPoolSecretScope("apikey:", apiKey)
		}
	}
	return ""
}

func proxyPoolSecretScope(prefix, secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return prefix + hex.EncodeToString(digest[:8])
}

func authMetadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func unpooledTransport(build func() (*http.Transport, error)) *http.Transport {
	tr, errBuild := build()
	if errBuild != nil {
		log.Errorf("%v", errBuild)
		return nil
	}
	if tr != nil {
		tr.DisableKeepAlives = true
	}
	return tr
}

func buildPooledBaseTransport(proxyURL string) (*http.Transport, error) {
	if strings.TrimSpace(proxyURL) == "" {
		if base, ok := http.DefaultTransport.(*http.Transport); ok && base != nil {
			return base.Clone(), nil
		}
		return nil, fmt.Errorf("http.DefaultTransport is not *http.Transport")
	}
	tr, _, errBuild := proxyutil.BuildHTTPTransport(proxyURL)
	if errBuild != nil {
		return nil, errBuild
	}
	return tr, nil
}

func stampProxyKeepAlive(conn net.Conn) net.Conn {
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return conn
	}
	if err := tcp.SetKeepAlive(true); err != nil {
		return conn
	}
	if err := tcp.SetKeepAlivePeriod(proxyPoolKeepAlivePeriod); err != nil {
		return conn
	}
	return conn
}

var devinTransportCache = NewTransportCache[string](DefaultTransportCacheCapacity)

func NewDevinHTTPClient(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, timeout time.Duration) (clientOut *http.Client) {
	defer func() { clientOut = httptransport.Decorate(ctx, clientOut) }()

	if cliproxyexecutor.RequestProxyURL(ctx) == "" && ctx != nil {
		if rt, ok := ctx.Value("cliproxy.roundtripper").(http.RoundTripper); ok && rt != nil {
			if tr, ok := rt.(*http.Transport); ok {
				key := fmt.Sprintf("rt:%p", tr)
				cloned, err := devinTransportCache.Get(key, func() (*http.Transport, error) {
					c := tr.Clone()
					c.DisableCompression = true
					return c, nil
				})
				if err == nil && cloned != nil {
					return &http.Client{
						Transport: cloned,
						Timeout:   timeout,
					}
				}
			}
			return &http.Client{
				Transport: devinNoGzipRoundTripper{base: rt},
				Timeout:   timeout,
			}
		}
	}

	proxyURL := effectiveProxyURL(ctx, cfg, auth)

	tr, err := devinTransportCache.Get(proxyURL, func() (*http.Transport, error) {
		var base *http.Transport
		if proxyURL != "" {
			base = buildProxyTransport(proxyURL)
		}
		if base == nil {
			if dt, ok := http.DefaultTransport.(*http.Transport); ok {
				base = dt.Clone()
			} else {
				base = &http.Transport{}
			}
		}
		base.DisableCompression = true
		return base, nil
	})
	if err != nil || tr == nil {
		tr = &http.Transport{DisableCompression: true}
	}

	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
	}
}

type devinNoGzipRoundTripper struct {
	base http.RoundTripper
}

func (rt devinNoGzipRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "identity")
	}
	return rt.base.RoundTrip(req)
}

func effectiveProxyURL(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth) string {
	if proxyURL := cliproxyexecutor.RequestProxyURL(ctx); proxyURL != "" {
		return proxyURL
	}
	if auth != nil {
		if proxyURL := strings.TrimSpace(auth.ProxyURL); proxyURL != "" {
			return proxyURL
		}
	}
	if cfg != nil {
		return strings.TrimSpace(cfg.ProxyURL)
	}
	return ""
}

func buildProxyTransport(proxyURL string) *http.Transport {
	transport, _, errBuild := proxyutil.BuildHTTPTransport(proxyURL)
	if errBuild != nil {
		log.Errorf("%v", errBuild)
		return nil
	}
	return transport
}
