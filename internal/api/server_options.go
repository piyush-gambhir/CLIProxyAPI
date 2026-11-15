package api

import (
	"context"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type serverOptionConfig struct {
	extraMiddleware       []gin.HandlerFunc
	engineConfigurator    func(*gin.Engine)
	routerConfigurator    func(*gin.Engine, *handlers.BaseAPIHandler, *config.Config)
	requestLoggerFactory  func(*config.Config, string) logging.RequestLogger
	localPassword         string
	keepAliveEnabled      bool
	keepAliveTimeout      time.Duration
	keepAliveOnTimeout    func()
	postAuthHook          auth.PostAuthHook
	postAuthPersistHook   auth.PostAuthHook
	pluginHost            *pluginhost.Host
	configReloadHook      func(context.Context, *config.Config)
	exampleAPIKeySafeMode bool
}


type ServerOption func(*serverOptionConfig)

func defaultRequestLoggerFactory(cfg *config.Config, configPath string) logging.RequestLogger {
	configDir := filepath.Dir(configPath)
	logsDir := logging.ResolveLogDirectory(cfg)
	logger := logging.NewFileRequestLogger(cfg.RequestLog, logsDir, configDir, cfg.ErrorLogsMaxFiles)
	logger.SetHomeEnabled(cfg != nil && cfg.Home.Enabled)
	return logger
}

func effectiveSDKConfig(cfg *config.Config) *config.SDKConfig {
	if cfg == nil {
		return nil
	}
	sdkCfg := cfg.SDKConfig
	sdkCfg.CodexOrphanDelegationCompatibility = cfg.Codex.OrphanDelegationCompatibility
	sdkCfg.CodexResponseSteering = cfg.Codex.ResponseSteering
	if cfg.CommercialMode {
		sdkCfg.RequestLog = false
	}
	return &sdkCfg
}


func WithMiddleware(mw ...gin.HandlerFunc) ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.extraMiddleware = append(cfg.extraMiddleware, mw...)
	}
}


func WithEngineConfigurator(fn func(*gin.Engine)) ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.engineConfigurator = fn
	}
}


func WithRouterConfigurator(fn func(*gin.Engine, *handlers.BaseAPIHandler, *config.Config)) ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.routerConfigurator = fn
	}
}


func WithLocalManagementPassword(password string) ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.localPassword = password
	}
}


func WithKeepAliveEndpoint(timeout time.Duration, onTimeout func()) ServerOption {
	return func(cfg *serverOptionConfig) {
		if timeout <= 0 || onTimeout == nil {
			return
		}
		cfg.keepAliveEnabled = true
		cfg.keepAliveTimeout = timeout
		cfg.keepAliveOnTimeout = onTimeout
	}
}


func WithRequestLoggerFactory(factory func(*config.Config, string) logging.RequestLogger) ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.requestLoggerFactory = factory
	}
}


func WithPostAuthHook(hook auth.PostAuthHook) ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.postAuthHook = hook
	}
}


func WithPostAuthPersistHook(hook auth.PostAuthHook) ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.postAuthPersistHook = hook
	}
}


func WithPluginHost(host *pluginhost.Host) ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.pluginHost = host
	}
}


func WithConfigReloadHook(hook func(context.Context, *config.Config)) ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.configReloadHook = hook
	}
}


func WithExampleAPIKeySafeMode() ServerOption {
	return func(cfg *serverOptionConfig) {
		cfg.exampleAPIKeySafeMode = true
	}
}
