package cliproxy

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/api"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/home"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/homeplugins"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/watcher"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/wsrelay"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executionregistry"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	sdkpluginstore "github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginstore"
)

type Service struct {
	cfg *config.Config

	cfgMu sync.RWMutex

	configUpdateMu sync.Mutex

	configRuntimeMu        sync.Mutex
	executorRegistrationMu sync.Mutex
	authUpdateMu           sync.Mutex
	authRevisions          map[string]uint64
	authRegWaitMu          sync.Mutex
	authRegWaiters         map[string]chan struct{}
	configSequence         uint64
	appliedRoutingState    *routingRuntimeState

	proxyPoolPublisher uint64

	configPath string

	tokenProvider TokenClientProvider

	apiKeyProvider APIKeyClientProvider

	watcherFactory WatcherFactory

	hooks Hooks

	serverOptions []api.ServerOption

	server *api.Server

	pprofServer *pprofServer

	discoveryManager *discoveryAdvertiserManager

	kvStoreManaged atomic.Bool

	serverErr chan error

	watcher *WatcherWrapper

	watcherCancel context.CancelFunc

	authUpdates chan watcher.AuthUpdate

	authQueueStop context.CancelFunc

	authManager *sdkAuth.Manager

	accessManager *sdkaccess.Manager

	coreManager *coreauth.Manager

	cooldownStateStore coreauth.CooldownStateStore

	pluginHost *pluginhost.Host

	shutdownOnce sync.Once

	wsGateway *wsrelay.Manager

	homeLifecycleMu              sync.Mutex
	homeOwnershipMu              sync.Mutex
	homeConfigCommitMu           sync.Mutex
	homeConfigStageHook          func()
	homeConfigCommitHook         func()
	homeConfigRuntimeHook        func()
	applyPprofConfigContextFn    func(context.Context, *config.Config) bool
	updateServerClientsContextFn func(context.Context, *config.Config) bool
	homeSupervisor               *homeSubscriberSupervisor
	homeMu                       sync.Mutex
	homeGeneration               uint64
	homeClient                   *home.Client
	homeRegistry                 *executionregistry.Registry
	homeDispatchBundle           *coreauth.HomeDispatchBundle
	homeDrainBound               time.Duration
	homeCancel                   context.CancelFunc
	runCancel                    context.CancelFunc
	homeLogForwarder             homeLogForwarder
	homeLogForwarderClient       *home.Client
	homePluginSyncMu             sync.Mutex
	homePluginSyncKey            string
	homePluginSyncFetch          func(context.Context, sdkpluginstore.PluginSyncRequest) (sdkpluginstore.PluginSyncResponse, error)
	homePluginDeleteTask         func(context.Context, *config.Config, home.PluginTask) homeplugins.SyncReport
	antigravityProbeWg           sync.WaitGroup

	antigravityContext       context.Context
	antigravityProbeMu       sync.Mutex
	antigravityAccountProbes map[string]map[*antigravityAccountProbe]struct{}
	antigravityRefreshWake   chan struct{}

	antigravityRefreshPending map[string]*antigravityModelRefreshRequest
	antigravityPersistPending map[string]*antigravityHintPersistRequest
	antigravityPersistActive  map[string]*antigravityHintPersistRequest
}

func (s *Service) SetResultPolicy(policy coreauth.ResultPolicy) {
	if s == nil || s.coreManager == nil {
		return
	}
	s.coreManager.SetResultPolicy(policy)
}

func (s *Service) ResultPolicy() coreauth.ResultPolicy {
	if s == nil || s.coreManager == nil {
		return nil
	}
	return s.coreManager.ResultPolicy()
}
