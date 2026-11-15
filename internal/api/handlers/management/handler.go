package management

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/buildinfo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginstore"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

type attemptInfo struct {
	count        int
	blockedUntil time.Time
	lastActivity time.Time
}

const attemptCleanupInterval = 1 * time.Hour

const attemptMaxIdleTime = 2 * time.Hour

type Handler struct {
	cfg                     *config.Config
	configFilePath          string
	mu                      sync.Mutex
	authStatusMu            sync.Mutex
	reloadMu                sync.Mutex
	reloadGeneration        uint64
	appliedReloadGeneration uint64
	attemptsMu              sync.Mutex
	failedAttempts          map[string]*attemptInfo
	authManager             *coreauth.Manager
	tokenStore              coreauth.Store
	localPassword           string
	allowRemoteOverride     bool
	envSecret               string
	logDir                  string
	postAuthHook            coreauth.PostAuthHook
	postAuthPersistHook     coreauth.PostAuthHook
	pluginHost              *pluginhost.Host
	configReloadHook        func(context.Context, *config.Config)
	pluginStoreRegistryURL  string
	pluginStoreHTTPClient   pluginstore.HTTPDoer
	pluginStoreRateLimiter  *pluginstore.GitHubRateLimiter
	pluginReleases          pluginReleaseCache
	siwcOAuthFactory        func(*config.Config) siwcOAuthService
	siwcCommitMu            sync.Mutex
	pluginLoginSaveMu       sync.Mutex
}

type configReloadSnapshot struct {
	cfg        *config.Config
	generation uint64
}

func NewHandler(cfg *config.Config, configFilePath string, manager *coreauth.Manager) *Handler {
	envSecret, _ := os.LookupEnv("MANAGEMENT_PASSWORD")
	envSecret = strings.TrimSpace(envSecret)

	h := &Handler{
		cfg:                 cfg.CloneForRuntime(),
		configFilePath:      configFilePath,
		failedAttempts:      make(map[string]*attemptInfo),
		authManager:         manager,
		tokenStore:          sdkAuth.GetTokenStore(),
		allowRemoteOverride: envSecret != "",
		envSecret:           envSecret,
	}
	h.startAttemptCleanup()
	return h
}

func (h *Handler) startAttemptCleanup() {
	go func() {
		ticker := time.NewTicker(attemptCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			h.purgeStaleAttempts()
		}
	}()
}

func (h *Handler) purgeStaleAttempts() {
	now := time.Now()
	h.attemptsMu.Lock()
	defer h.attemptsMu.Unlock()
	for ip, ai := range h.failedAttempts {

		if !ai.blockedUntil.IsZero() && now.Before(ai.blockedUntil) {
			continue
		}

		if now.Sub(ai.lastActivity) > attemptMaxIdleTime {
			delete(h.failedAttempts, ip)
		}
	}
}

func NewHandlerWithoutConfigFilePath(cfg *config.Config, manager *coreauth.Manager) *Handler {
	return NewHandler(cfg, "", manager)
}

func (h *Handler) SetConfig(cfg *config.Config) {
	if h == nil {
		return
	}
	cloned := cfg.CloneForRuntime()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.isStaleConfigFileSnapshotLocked(cloned) {
		log.Debug("management: ignoring config reload parsed from an older config file version")
		return
	}
	h.cfg = cloned
}

func (h *Handler) configSnapshot() *config.Config {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg
}

func (h *Handler) oauthConfigWithProxy(proxyURL string) *config.Config {
	cfg := h.configSnapshot()
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return cfg
	}
	if cfg == nil {
		return &config.Config{
			SDKConfig: config.SDKConfig{ProxyURL: proxyURL},
		}
	}
	cloned := cfg.CloneForRuntime()
	if cloned == nil {
		copied := *cfg
		cloned = &copied
	}
	cloned.ProxyURL = proxyURL
	return cloned
}

func (h *Handler) isStaleConfigFileSnapshotLocked(cfg *config.Config) bool {
	if cfg == nil || cfg.SourceHash == "" || h.configFilePath == "" {
		return false
	}
	data, errRead := os.ReadFile(h.configFilePath)
	if errRead != nil {
		return false
	}
	return config.ComputeSourceHash(data) != cfg.SourceHash
}

func (h *Handler) SetAuthManager(manager *coreauth.Manager) {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.authManager != manager {
		h.authManager = manager
	}
	h.mu.Unlock()
}

func (h *Handler) SetPluginHost(host *pluginhost.Host) {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.pluginHost != host {
		h.pluginHost = host
	}
	h.mu.Unlock()
}

func (h *Handler) SetConfigReloadHook(hook func(context.Context, *config.Config)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.configReloadHook = hook
	h.mu.Unlock()
}

func (h *Handler) reloadSnapshotConfigLocked() configReloadSnapshot {
	if h == nil || h.cfg == nil {
		return configReloadSnapshot{}
	}
	h.reloadGeneration++
	return configReloadSnapshot{
		cfg:        h.cfg.CloneForRuntime(),
		generation: h.reloadGeneration,
	}
}

func (h *Handler) saveConfigAndSnapshotLocked(c *gin.Context) (configReloadSnapshot, bool) {
	if errSave := config.SaveConfigPreserveComments(h.configFilePath, h.cfg, c.GetBool(ConfigV8ContextKey)); errSave != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save config: %v", errSave)})
		return configReloadSnapshot{}, false
	}
	return h.reloadSnapshotConfigLocked(), true
}

func (h *Handler) reloadConfigAfterManagementSave(ctx context.Context, snapshot configReloadSnapshot) {
	if h == nil || snapshot.cfg == nil || snapshot.generation == 0 {
		return
	}
	h.reloadMu.Lock()
	defer h.reloadMu.Unlock()

	h.mu.Lock()
	if snapshot.generation < h.appliedReloadGeneration {
		h.mu.Unlock()
		return
	}
	hook := h.configReloadHook
	host := h.pluginHost
	h.mu.Unlock()
	if hook != nil {
		hook(ctx, snapshot.cfg)
	} else if host != nil {
		host.ApplyConfig(ctx, snapshot.cfg)
	}

	h.mu.Lock()
	if snapshot.generation > h.appliedReloadGeneration {
		h.appliedReloadGeneration = snapshot.generation
	}
	h.mu.Unlock()
}

func (h *Handler) reloadConfigAfterManagementSaveAsync(ctx context.Context, snapshot configReloadSnapshot) {
	if h == nil || snapshot.cfg == nil || snapshot.generation == 0 {
		return
	}
	reloadCtx := context.Background()
	if ctx != nil {
		reloadCtx = context.WithoutCancel(ctx)
	}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.WithField("panic", recovered).Error("management: async config reload panicked")
			}
		}()
		h.reloadConfigAfterManagementSave(reloadCtx, snapshot)
	}()
}

func (h *Handler) SetLocalPassword(password string) { h.localPassword = password }

func (h *Handler) SetLogDirectory(dir string) {
	if dir == "" {
		return
	}
	if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	h.logDir = dir
}

func (h *Handler) SetPostAuthHook(hook coreauth.PostAuthHook) {
	h.postAuthHook = hook
}

func (h *Handler) SetPostAuthPersistHook(hook coreauth.PostAuthHook) {
	h.postAuthPersistHook = hook
}

func (h *Handler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-CPA-VERSION", buildinfo.Version)
		c.Header("X-CPA-COMMIT", buildinfo.Commit)
		c.Header("X-CPA-BUILD-DATE", buildinfo.BuildDate)
		c.Header("X-CPA-SUPPORT-PLUGIN", pluginhost.SupportPluginHeaderValue())

		clientIP := c.ClientIP()
		localClient := clientIP == "127.0.0.1" || clientIP == "::1"

		var provided string
		if ah := c.GetHeader("Authorization"); ah != "" {
			parts := strings.SplitN(ah, " ", 2)
			if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
				provided = parts[1]
			} else {
				provided = ah
			}
		}
		if provided == "" {
			provided = c.GetHeader("X-Management-Key")
		}

		allowed, statusCode, errMsg := h.AuthenticateManagementKey(clientIP, localClient, provided)
		if !allowed {
			c.AbortWithStatusJSON(statusCode, gin.H{"error": errMsg})
			return
		}
		c.Next()
	}
}

func (h *Handler) AuthenticateManagementKey(clientIP string, localClient bool, provided string) (bool, int, string) {
	const maxFailures = 5
	const banDuration = 30 * time.Minute

	if h == nil {
		return false, http.StatusForbidden, "remote management disabled"
	}

	h.mu.Lock()
	cfg := h.cfg
	var (
		allowRemote bool
		secretHash  string
	)
	if cfg != nil {
		allowRemote = cfg.RemoteManagement.AllowRemote
		secretHash = cfg.RemoteManagement.SecretKey
	}
	h.mu.Unlock()
	if h.allowRemoteOverride {
		allowRemote = true
	}
	envSecret := h.envSecret

	now := time.Now()
	h.attemptsMu.Lock()
	ai := h.failedAttempts[clientIP]
	if ai != nil && !ai.blockedUntil.IsZero() {
		if now.Before(ai.blockedUntil) {
			remaining := ai.blockedUntil.Sub(now).Round(time.Second)
			h.attemptsMu.Unlock()
			return false, http.StatusForbidden, fmt.Sprintf("IP banned due to too many failed attempts. Try again in %s", remaining)
		}

		ai.blockedUntil = time.Time{}
		ai.count = 0
	}
	h.attemptsMu.Unlock()

	if !localClient && !allowRemote {
		return false, http.StatusForbidden, "remote management disabled"
	}

	fail := func() {
		h.attemptsMu.Lock()
		aip := h.failedAttempts[clientIP]
		if aip == nil {
			aip = &attemptInfo{}
			h.failedAttempts[clientIP] = aip
		}
		aip.count++
		aip.lastActivity = time.Now()
		if aip.count >= maxFailures {
			aip.blockedUntil = time.Now().Add(banDuration)
			aip.count = 0
		}
		h.attemptsMu.Unlock()
	}

	reset := func() {
		h.attemptsMu.Lock()
		if ai := h.failedAttempts[clientIP]; ai != nil {
			ai.count = 0
			ai.blockedUntil = time.Time{}
		}
		h.attemptsMu.Unlock()
	}

	if secretHash == "" && envSecret == "" {
		return false, http.StatusForbidden, "remote management key not set"
	}

	if provided == "" {
		fail()
		return false, http.StatusUnauthorized, "missing management key"
	}

	if localClient {
		if lp := h.localPassword; lp != "" {
			if subtle.ConstantTimeCompare([]byte(provided), []byte(lp)) == 1 {
				reset()
				return true, 0, ""
			}
		}
	}

	if envSecret != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(envSecret)) == 1 {
		reset()
		return true, 0, ""
	}

	if secretHash == "" || bcrypt.CompareHashAndPassword([]byte(secretHash), []byte(provided)) != nil {
		fail()
		return false, http.StatusUnauthorized, "invalid management key"
	}

	reset()

	return true, 0, ""
}

func (h *Handler) persistLocked(c *gin.Context) bool {

	if err := config.SaveConfigPreserveComments(h.configFilePath, h.cfg, c.GetBool(ConfigV8ContextKey)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save config: %v", err)})
		return false
	}
	snapshot := h.reloadSnapshotConfigLocked()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
	var reqCtx context.Context
	if c != nil && c.Request != nil {
		reqCtx = c.Request.Context()
	}
	h.reloadConfigAfterManagementSaveAsync(reqCtx, snapshot)
	return true
}

func (h *Handler) updateBoolField(c *gin.Context, set func(bool)) {
	var body struct {
		Value *bool `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	set(*body.Value)
	h.persistLocked(c)
}

func (h *Handler) updateIntField(c *gin.Context, set func(int)) {
	var body struct {
		Value *int `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	set(*body.Value)
	h.persistLocked(c)
}

func (h *Handler) updateStringField(c *gin.Context, set func(string)) {
	var body struct {
		Value *string `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	set(*body.Value)
	h.persistLocked(c)
}
