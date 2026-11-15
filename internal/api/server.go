package api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	managementHandlers "github.com/router-for-me/CLIProxyAPI/v8/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api/middleware"
	codexlive "github.com/router-for-me/CLIProxyAPI/v8/internal/client/codex/live"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/githubauth"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/managementasset"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/redisqueue"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers/openai"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
	"gopkg.in/yaml.v3"
)

type Server struct {
	engine *gin.Engine

	server *http.Server

	listenerMu      sync.Mutex
	muxBaseListener net.Listener

	muxHTTPListener *muxListener

	handlers         *handlers.BaseAPIHandler
	codexLiveHandler *codexlive.Handler

	cfgMu sync.RWMutex
	cfg   *config.Config

	oldConfigYaml []byte

	accessManager *sdkaccess.Manager

	requestLogger logging.RequestLogger
	loggerToggle  func(bool)

	configFilePath string

	currentPath string

	wsRouteMu     sync.Mutex
	wsRoutes      map[string]struct{}
	wsAuthChanged func(bool, bool)
	wsAuthEnabled atomic.Bool

	mgmt *managementHandlers.Handler

	pluginHost *pluginhost.Host

	managementRoutesRegistered atomic.Bool

	managementRoutesEnabled atomic.Bool

	envManagementSecret bool

	localPassword string

	keepAliveEnabled   bool
	keepAliveTimeout   time.Duration
	keepAliveOnTimeout func()
	keepAliveHeartbeat chan struct{}
	keepAliveStop      chan struct{}

	exampleAPIKeySafeModeEnabled bool
	exampleAPIKeySafeModeActive  atomic.Bool
}

func NewServer(cfg *config.Config, authManager *auth.Manager, accessManager *sdkaccess.Manager, configFilePath string, opts ...ServerOption) *Server {
	if cfg != nil {
		cfg = cfg.CloneForRuntime()
	}
	optionState := &serverOptionConfig{
		requestLoggerFactory: defaultRequestLoggerFactory,
	}
	for i := range opts {
		opts[i](optionState)
	}

	if !cfg.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	if errSetTrustedProxies := engine.SetTrustedProxies(cfg.TrustedProxies); errSetTrustedProxies != nil {
		log.WithError(errSetTrustedProxies).Error("invalid trusted-proxies configuration; forwarded client IP headers will be ignored")
		if errDisableTrustedProxies := engine.SetTrustedProxies(nil); errDisableTrustedProxies != nil {
			log.WithError(errDisableTrustedProxies).Error("failed to disable trusted proxy handling")
		}
	}
	if optionState.engineConfigurator != nil {
		optionState.engineConfigurator(engine)
	}

	wd, err := os.Getwd()
	if err != nil {
		wd = configFilePath
	}

	envAdminPassword, envAdminPasswordSet := os.LookupEnv("MANAGEMENT_PASSWORD")
	envAdminPassword = strings.TrimSpace(envAdminPassword)
	envManagementSecret := envAdminPasswordSet && envAdminPassword != ""

	s := &Server{
		engine:                       engine,
		handlers:                     handlers.NewBaseAPIHandlers(effectiveSDKConfig(cfg), authManager),
		cfg:                          cfg,
		accessManager:                accessManager,
		configFilePath:               configFilePath,
		currentPath:                  wd,
		envManagementSecret:          envManagementSecret,
		wsRoutes:                     make(map[string]struct{}),
		pluginHost:                   optionState.pluginHost,
		exampleAPIKeySafeModeEnabled: optionState.exampleAPIKeySafeMode,
	}

	engine.Use(logging.GinLogrusLogger())
	engine.Use(logging.CPATraceIDMiddleware())
	engine.Use(s.maxRequestBodyBytesMiddleware())

	var requestLogger logging.RequestLogger
	var toggle func(bool)
	if !cfg.CommercialMode {
		if optionState.requestLoggerFactory != nil {
			requestLogger = optionState.requestLoggerFactory(cfg, configFilePath)
		}
		if requestLogger != nil {
			engine.Use(middleware.RequestLoggingMiddleware(requestLogger))
			if setter, ok := requestLogger.(interface{ SetEnabled(bool) }); ok {
				toggle = setter.SetEnabled
			}
		}
	}
	s.requestLogger = requestLogger
	s.loggerToggle = toggle

	engine.Use(logging.GinLogrusRecovery())
	for _, mw := range optionState.extraMiddleware {
		engine.Use(mw)
	}

	engine.Use(corsMiddleware())
	engine.Use(s.responseCompressionMiddleware())
	openai.SetResponsesWebsocketCompressionEnabled(cfg.Codex.WebsocketCompressionEnabled())
	s.wsAuthEnabled.Store(cfg.WebsocketAuth)
	s.exampleAPIKeySafeModeActive.Store(s.exampleAPIKeySafeModeRequired(cfg))
	s.handlers.SetPluginHost(optionState.pluginHost)
	if optionState.pluginHost != nil {
		optionState.pluginHost.SetModelExecutor(s.handlers)
		optionState.pluginHost.SetAuthManager(authManager)
	}

	s.oldConfigYaml, _ = yaml.Marshal(cfg)
	s.applyAccessConfig(nil, cfg)
	if authManager != nil {
		authManager.SetRetryConfig(cfg.RequestRetry, time.Duration(cfg.MaxRetryInterval)*time.Second, cfg.MaxRetryCredentials)
	}
	githubauth.SetToken(cfg.GitHubToken)
	managementasset.SetCurrentConfig(cfg)
	auth.SetQuotaCooldownDisabled(cfg.DisableCooling)
	auth.SetTransientErrorCooldownSeconds(cfg.TransientErrorCooldownSeconds)
	applySignatureCacheConfig(nil, cfg)
	applyClaudeToolSearchConfig(cfg)

	s.mgmt = managementHandlers.NewHandler(cfg, configFilePath, authManager)
	s.mgmt.SetPluginHost(optionState.pluginHost)
	s.mgmt.SetConfigReloadHook(optionState.configReloadHook)
	if optionState.localPassword != "" {
		s.mgmt.SetLocalPassword(optionState.localPassword)
	}
	logDir := logging.ResolveLogDirectory(cfg)
	s.mgmt.SetLogDirectory(logDir)
	if optionState.postAuthHook != nil {
		s.mgmt.SetPostAuthHook(optionState.postAuthHook)
	}
	if optionState.postAuthPersistHook != nil {
		s.mgmt.SetPostAuthPersistHook(optionState.postAuthPersistHook)
	}
	s.localPassword = optionState.localPassword

	engine.Use(s.homeHeartbeatMiddleware())
	engine.Use(s.exampleAPIKeySafeModeMiddleware())

	s.setupRoutes()

	if optionState.routerConfigurator != nil {
		optionState.routerConfigurator(engine, s.handlers, cfg)
	}

	hasManagementSecret := cfg.RemoteManagement.SecretKey != "" || envManagementSecret || s.localPassword != ""
	s.managementRoutesEnabled.Store(hasManagementSecret)
	redisqueue.SetEnabled(hasManagementSecret || (cfg != nil && cfg.Home.Enabled))
	if hasManagementSecret {
		s.registerManagementRoutes()
	}
	s.refreshPluginManagementRoutes()
	engine.NoRoute(s.pluginManagementNoRoute)

	if optionState.keepAliveEnabled {
		s.enableKeepAlive(optionState.keepAliveTimeout, optionState.keepAliveOnTimeout)
	}

	s.server = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:           wrapUnconsumedRequestBodyHandler(engine),
		ReadHeaderTimeout: muxSniffDeadline,
	}

	return s
}

func (s *Server) getConfig() *config.Config {
	if s == nil {
		return nil
	}
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

func (s *Server) Handler() http.Handler {
	if s == nil || s.server == nil {
		return nil
	}
	return s.server.Handler
}

func (s *Server) Start() error {
	if s == nil || s.server == nil {
		return fmt.Errorf("failed to start HTTP server: server not initialized")
	}

	addr := s.server.Addr
	listener, errListen := net.Listen("tcp", addr)
	if errListen != nil {
		return fmt.Errorf("failed to start HTTP server: %v", errListen)
	}

	cfg := s.getConfig()
	useTLS := cfg != nil && cfg.TLS.Enable
	if useTLS {
		certPath := strings.TrimSpace(cfg.TLS.Cert)
		keyPath := strings.TrimSpace(cfg.TLS.Key)
		if certPath == "" || keyPath == "" {
			if errClose := listener.Close(); errClose != nil {
				log.Errorf("failed to close listener after TLS validation failure: %v", errClose)
			}
			return fmt.Errorf("failed to start HTTPS server: tls.cert or tls.key is empty")
		}
		certPair, errLoad := tls.LoadX509KeyPair(certPath, keyPath)
		if errLoad != nil {
			if errClose := listener.Close(); errClose != nil {
				log.Errorf("failed to close listener after TLS key pair load failure: %v", errClose)
			}
			return fmt.Errorf("failed to start HTTPS server: %v", errLoad)
		}

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{certPair},
			NextProtos:   []string{"h2", "http/1.1"},
		}
		s.server.TLSConfig = tlsConfig
		if errHTTP2 := http2.ConfigureServer(s.server, &http2.Server{}); errHTTP2 != nil {
			log.Warnf("failed to configure HTTP/2: %v", errHTTP2)
		}
		listener = tls.NewListener(listener, tlsConfig)
		log.Debugf("Starting API server on %s with TLS", addr)
	} else {
		log.Debugf("Starting API server on %s", addr)
	}

	httpListener := newMuxListener(listener.Addr(), 1024)
	s.listenerMu.Lock()
	s.muxBaseListener = listener
	s.muxHTTPListener = httpListener
	s.listenerMu.Unlock()

	httpErrCh := make(chan error, 1)
	acceptErrCh := make(chan error, 1)

	go func() {
		httpErrCh <- s.server.Serve(httpListener)
	}()
	go func() {
		acceptErrCh <- s.acceptMuxConnections(listener, httpListener)
	}()

	select {
	case errServe := <-httpErrCh:
		s.listenerMu.Lock()
		muxBase := s.muxBaseListener
		muxHTTP := s.muxHTTPListener
		s.muxBaseListener = nil
		s.muxHTTPListener = nil
		s.listenerMu.Unlock()
		if muxBase != nil {
			if errClose := muxBase.Close(); errClose != nil && !errors.Is(errClose, net.ErrClosed) {
				log.Debugf("failed to close shared listener after HTTP serve exit: %v", errClose)
			}
		}
		if muxHTTP != nil {
			_ = muxHTTP.Close()
		}
		errAccept := <-acceptErrCh
		errServe = normalizeHTTPServeError(errServe)
		errAccept = normalizeListenerError(errAccept)
		if errServe != nil {
			return fmt.Errorf("failed to start HTTP server: %v", errServe)
		}
		if errAccept != nil {
			return fmt.Errorf("failed to start HTTP server: %v", errAccept)
		}
		return nil
	case errAccept := <-acceptErrCh:
		s.listenerMu.Lock()
		muxHTTP := s.muxHTTPListener
		muxBase := s.muxBaseListener
		s.muxHTTPListener = nil
		s.muxBaseListener = nil
		s.listenerMu.Unlock()
		if muxHTTP != nil {
			_ = muxHTTP.Close()
		}
		if muxBase != nil {
			if errClose := muxBase.Close(); errClose != nil && !errors.Is(errClose, net.ErrClosed) {
				log.Debugf("failed to close shared listener after accept loop exit: %v", errClose)
			}
		}
		errServe := <-httpErrCh
		errServe = normalizeHTTPServeError(errServe)
		errAccept = normalizeListenerError(errAccept)
		if errAccept != nil {
			return fmt.Errorf("failed to start HTTP server: %v", errAccept)
		}
		if errServe != nil {
			return fmt.Errorf("failed to start HTTP server: %v", errServe)
		}
		return nil
	}
}

func (s *Server) Stop(ctx context.Context) error {
	log.Debug("Stopping API server...")

	if s.keepAliveEnabled {
		select {
		case s.keepAliveStop <- struct{}{}:
		default:
		}
	}

	s.listenerMu.Lock()
	muxHTTP := s.muxHTTPListener
	s.muxHTTPListener = nil
	muxBase := s.muxBaseListener
	s.muxBaseListener = nil
	s.listenerMu.Unlock()

	if muxHTTP != nil {
		_ = muxHTTP.Close()
	}
	if muxBase != nil {
		if errCloseBase := muxBase.Close(); errCloseBase != nil && !errors.Is(errCloseBase, net.ErrClosed) {
			log.Debugf("failed to close shared listener: %v", errCloseBase)
		}
	}

	var errCloseServer error
	if s.server != nil {
		errCloseServer = s.server.Close()
		if errors.Is(errCloseServer, http.ErrServerClosed) || errors.Is(errCloseServer, net.ErrClosed) {
			errCloseServer = nil
		}
	}
	if s.codexLiveHandler != nil {
		s.codexLiveHandler.Close()
	}
	if errCloseServer != nil {
		return fmt.Errorf("failed to shutdown HTTP server: %v", errCloseServer)
	}

	log.Debug("API server stopped")
	return nil
}
