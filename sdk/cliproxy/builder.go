package cliproxy

import (
	"context"
	"fmt"

	configaccess "github.com/router-for-me/CLIProxyAPI/v8/internal/access/config_access"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/watcher"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

type Builder struct {
	cfg *config.Config

	configPath string

	tokenProvider TokenClientProvider

	apiKeyProvider APIKeyClientProvider

	watcherFactory WatcherFactory

	hooks Hooks

	authManager *sdkAuth.Manager

	accessManager *sdkaccess.Manager

	coreManager *coreauth.Manager

	cooldownStateStore coreauth.CooldownStateStore

	pluginHost *pluginhost.Host

	postAuthHook coreauth.PostAuthHook

	resultPolicy coreauth.ResultPolicy

	serverOptions []api.ServerOption
}

type Hooks struct {
	OnBeforeStart func(*config.Config)

	OnAfterStart func(*Service)
}

func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) WithConfig(cfg *config.Config) *Builder {
	b.cfg = cfg
	return b
}

func (b *Builder) WithConfigPath(path string) *Builder {
	b.configPath = path
	return b
}

func (b *Builder) WithTokenClientProvider(provider TokenClientProvider) *Builder {
	b.tokenProvider = provider
	return b
}

func (b *Builder) WithAPIKeyClientProvider(provider APIKeyClientProvider) *Builder {
	b.apiKeyProvider = provider
	return b
}

func (b *Builder) WithWatcherFactory(factory WatcherFactory) *Builder {
	b.watcherFactory = factory
	return b
}

func (b *Builder) WithHooks(h Hooks) *Builder {
	b.hooks = h
	return b
}

func (b *Builder) WithAuthManager(mgr *sdkAuth.Manager) *Builder {
	b.authManager = mgr
	return b
}

func (b *Builder) WithRequestAccessManager(mgr *sdkaccess.Manager) *Builder {
	b.accessManager = mgr
	return b
}

func (b *Builder) WithCoreAuthManager(mgr *coreauth.Manager) *Builder {
	b.coreManager = mgr
	return b
}

func (b *Builder) WithCooldownStateStore(store coreauth.CooldownStateStore) *Builder {
	b.cooldownStateStore = store
	return b
}

func (b *Builder) WithPluginHost(host *pluginhost.Host) *Builder {
	b.pluginHost = host
	return b
}

func (b *Builder) WithServerOptions(opts ...api.ServerOption) *Builder {
	b.serverOptions = append(b.serverOptions, opts...)
	return b
}

func (b *Builder) WithLocalManagementPassword(password string) *Builder {
	if password == "" {
		return b
	}
	b.serverOptions = append(b.serverOptions, api.WithLocalManagementPassword(password))
	return b
}

func (b *Builder) WithPostAuthHook(hook coreauth.PostAuthHook) *Builder {
	if hook == nil {
		return b
	}
	b.postAuthHook = hook
	return b
}

func (b *Builder) WithResultPolicy(policy coreauth.ResultPolicy) *Builder {
	b.resultPolicy = policy
	return b
}

func (b *Builder) Build() (*Service, error) {
	if b.cfg == nil {
		return nil, fmt.Errorf("cliproxy: configuration is required")
	}
	if b.configPath == "" {
		return nil, fmt.Errorf("cliproxy: configuration path is required")
	}
	if errValidate := b.cfg.ValidateCredentialWeights(); errValidate != nil {
		return nil, fmt.Errorf("cliproxy: validate credential weights: %w", errValidate)
	}
	b.cfg.NormalizePluginsConfig()
	if errResolvePluginsDir := b.cfg.ResolvePluginsDir(); errResolvePluginsDir != nil && b.cfg.Plugins.Enabled {
		return nil, fmt.Errorf("cliproxy: %w", errResolvePluginsDir)
	}

	tokenProvider := b.tokenProvider
	if tokenProvider == nil {
		tokenProvider = NewFileTokenClientProvider()
	}

	apiKeyProvider := b.apiKeyProvider
	if apiKeyProvider == nil {
		apiKeyProvider = NewAPIKeyClientProvider()
	}

	watcherFactory := b.watcherFactory
	if watcherFactory == nil {
		watcherFactory = defaultWatcherFactory
	}

	authManager := b.authManager
	if authManager == nil {
		authManager = newDefaultAuthManager()
	}

	accessManager := b.accessManager
	if accessManager == nil {
		accessManager = sdkaccess.NewManager()
	}

	configaccess.Register(&b.cfg.SDKConfig)
	pluginHost := b.pluginHost
	if pluginHost == nil {
		pluginHost = pluginhost.New()
	}
	if b.cfg != nil {
		pluginHost.ApplyConfig(context.Background(), b.cfg)
		pluginHost.RegisterFrontendAuthProviders()
	}
	accessManager.SetProviders(sdkaccess.RegisteredProviders())

	coreManager := b.coreManager
	cooldownStateStore := b.cooldownStateStore
	var appliedRoutingState *routingRuntimeState
	if coreManager == nil {
		tokenStore := sdkAuth.GetTokenStore()
		if dirSetter, ok := tokenStore.(interface{ SetBaseDir(string) }); ok && b.cfg != nil {
			dirSetter.SetBaseDir(b.cfg.AuthDir)
		}
		if cooldownStateStore == nil {
			if provider, ok := tokenStore.(coreauth.CooldownStateStoreProvider); ok {
				cooldownStateStore = provider.CooldownStateStore()
			}
		}

		routingState := normalizedRoutingRuntimeState(b.cfg)
		coreManager = coreauth.NewManager(tokenStore, newRoutingSelector(routingState), nil)
		appliedRoutingState = &routingState
	}

	coreManager.SetRoundTripperProvider(newDefaultRoundTripperProvider())
	coreManager.SetConfig(b.cfg)
	coreManager.SetOAuthModelAlias(b.cfg.OAuthModelAlias)
	if pluginHost != nil {
		coreManager.SetPluginScheduler(pluginHost)
	}
	if b.resultPolicy != nil {
		coreManager.SetResultPolicy(b.resultPolicy)
	}

	service := &Service{
		cfg:                 b.cfg.CloneForRuntime(),
		configPath:          b.configPath,
		tokenProvider:       tokenProvider,
		apiKeyProvider:      apiKeyProvider,
		watcherFactory:      watcherFactory,
		hooks:               b.hooks,
		authManager:         authManager,
		accessManager:       accessManager,
		coreManager:         coreManager,
		cooldownStateStore:  cooldownStateStore,
		pluginHost:          pluginHost,
		discoveryManager:    newDiscoveryAdvertiserManager(),
		appliedRoutingState: appliedRoutingState,
		serverOptions:       append([]api.ServerOption(nil), b.serverOptions...),
	}
	service.proxyPoolPublisher = helps.RegisterProxyPoolPublisher()
	helps.AdvanceProxyPoolGeneration(service.proxyPoolPublisher, service.cfg)
	if b.postAuthHook != nil {
		service.serverOptions = append(service.serverOptions, api.WithPostAuthHook(b.postAuthHook))
	}
	service.serverOptions = append(service.serverOptions,
		api.WithPostAuthPersistHook(service.runtimeAuthSyncHook()),
		api.WithPluginHost(pluginHost),
		api.WithConfigReloadHook(func(_ context.Context, _ *config.Config) {
			service.reloadConfigFromWatcher()
		}),
	)
	return service, nil
}

func (s *Service) runtimeAuthSyncHook() coreauth.PostAuthHook {
	return func(ctx context.Context, auth *coreauth.Auth) error {
		if s == nil || auth == nil || auth.ID == "" {
			return nil
		}
		action := watcher.AuthUpdateActionAdd
		if s.coreManager != nil {
			if _, ok := s.coreManager.GetByID(auth.ID); ok {
				action = watcher.AuthUpdateActionModify
			}
		}
		update := watcher.AuthUpdate{
			Action: action,
			ID:     auth.ID,
			Auth:   auth,
		}
		if watcher := s.currentWatcher(); watcher != nil {
			_, rev := watcher.DispatchPersistedAuthUpdateWithRevision(&update)
			if rev > 0 {
				update.SetRevision(rev)
			}
		}

		syncCtx := coreauth.WithSkipPersist(context.Background())
		s.handleAuthUpdate(syncCtx, update)
		return nil
	}
}
