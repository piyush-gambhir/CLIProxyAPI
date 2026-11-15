package api

import (
	"time"

	"github.com/gin-gonic/gin"
	internalapi "github.com/router-for-me/CLIProxyAPI/v8/internal/api"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/logging"
)

type ServerOption = internalapi.ServerOption

func WithMiddleware(mw ...gin.HandlerFunc) ServerOption { return internalapi.WithMiddleware(mw...) }

func WithEngineConfigurator(fn func(*gin.Engine)) ServerOption {
	return internalapi.WithEngineConfigurator(fn)
}

func WithRouterConfigurator(fn func(*gin.Engine, *handlers.BaseAPIHandler, *config.Config)) ServerOption {
	return internalapi.WithRouterConfigurator(fn)
}

func WithLocalManagementPassword(password string) ServerOption {
	return internalapi.WithLocalManagementPassword(password)
}

func WithKeepAliveEndpoint(timeout time.Duration, onTimeout func()) ServerOption {
	return internalapi.WithKeepAliveEndpoint(timeout, onTimeout)
}

func WithRequestLoggerFactory(factory func(*config.Config, string) logging.RequestLogger) ServerOption {
	return internalapi.WithRequestLoggerFactory(factory)
}
