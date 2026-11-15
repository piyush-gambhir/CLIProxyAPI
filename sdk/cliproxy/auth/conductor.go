package auth

import (
	"context"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"golang.org/x/sync/semaphore"
)

type ProviderExecutor interface {
	Identifier() string

	Execute(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error)

	ExecuteStream(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error)

	Refresh(ctx context.Context, auth *Auth) (*Auth, error)

	CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error)

	HttpRequest(ctx context.Context, auth *Auth, req *http.Request) (*http.Response, error)
}

type APIKeyConfigExecutor interface {
	ForAPIKey() ProviderExecutor
}

type RequestAuthPreparer interface {
	ShouldPrepareRequestAuth(auth *Auth) bool
	PrepareRequestAuth(ctx context.Context, auth *Auth) (*Auth, error)
}

type ExecutionSessionCloser interface {
	CloseExecutionSession(sessionID string)
}

type ExecutionSessionSharer interface {
	SharesExecutionSessionsWith(other ProviderExecutor) bool
}

type AuthExecutionSessionCloser interface {
	CloseAuthExecutionSessions(authID string)
}

type Result struct {
	AuthID string

	Provider string

	Model string

	RouteModel string

	Success bool

	CompletedResponse bool

	RetryAfter *time.Duration

	CredentialScope bool

	Error *Error

	Options cliproxyexecutor.Options

	SkipQuotaObservation bool

	AccessToken string

	CredentialVersion uint64

	RegistrationEpoch uint64

	ExecutionSeq uint64
}

type Selector interface {
	Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*Auth) (*Auth, error)
}

type PluginScheduler interface {
	PickAuth(context.Context, pluginapi.SchedulerPickRequest) (pluginapi.SchedulerPickResponse, bool, error)
}

type PluginSchedulerAcrossPriorities interface {
	SchedulerWantsAcrossPriorities() bool
}

type pluginSchedulerState interface {
	HasScheduler() bool
}

type StoppableSelector interface {
	Selector
	Stop()
}

type Hook interface {
	OnAuthRegistered(ctx context.Context, auth *Auth)

	OnAuthUpdated(ctx context.Context, auth *Auth)

	OnResult(ctx context.Context, result Result)
}

type NoopHook struct{}

func (NoopHook) OnAuthRegistered(context.Context, *Auth) {}

func (NoopHook) OnAuthUpdated(context.Context, *Auth) {}

func (NoopHook) OnResult(context.Context, Result) {}

type ResultPolicy interface {
	ApplyResultPolicy(ctx context.Context, result Result) Result
}

type ResultPolicyFunc func(ctx context.Context, result Result) Result

func (f ResultPolicyFunc) ApplyResultPolicy(ctx context.Context, result Result) Result {
	return f(ctx, result)
}

type resultPolicyHolder struct {
	policy ResultPolicy
}

type Manager struct {
	store                     Store
	cooldownStore             CooldownStateStore
	pendingCooldownStateStore CooldownStateStore
	executors                 map[string]ProviderExecutor
	selector                  Selector
	hook                      Hook
	resultPolicy              atomic.Pointer[resultPolicyHolder]
	mu                        sync.RWMutex
	selectorMu                sync.Mutex
	configCooldownMu          sync.Mutex
	syncSchedulerMu           sync.Mutex

	executionSeq       atomic.Uint64
	structuralEpoch    atomic.Uint64
	syncedVersion      atomic.Uint64
	auths              map[string]*Auth
	authEpochs         map[string]uint64
	authChangeWatchers map[string]map[chan struct{}]struct{}
	scheduler          *authScheduler

	pluginScheduler PluginScheduler

	homeRuntimeAuths map[string]map[string]*Auth

	homeRuntimeAuthOwners map[string]map[string]*HomeDispatchSelection

	homeSessionSelections map[string]map[homeSessionSelectionKey]*HomeDispatchSelection
	homeSessionLocks      sync.Map
	homeSessionAliases    homeSessionAliasCache

	providerOffsets             map[string]int
	homeDispatchBundle          atomic.Pointer[HomeDispatchBundle]
	homeInFlightPublisherConfig atomic.Pointer[HomeInFlightPublisherConfig]

	requestRetry        atomic.Int32
	maxRetryCredentials atomic.Int32
	maxRetryInterval    atomic.Int64

	oauthModelAlias atomic.Value

	apiKeyModelRouting atomic.Value

	modelPoolOffsets map[string]int

	runtimeConfig atomic.Value

	rtProvider RoundTripperProvider

	refreshCancel context.CancelFunc
	refreshLoop   *authAutoRefreshLoop

	refreshJobs map[string]*authRefreshJob

	requestPrepareLocks sync.Map

	refreshLocks sync.Map

	persistLocks sync.Map

	authMutationLocks      sync.Map
	metadataPersistCancels sync.Map
	metadataSaveGate       sync.Map

	authLoadGate *semaphore.Weighted
}

func NewManager(store Store, selector Selector, hook Hook) *Manager {
	if selector == nil {
		selector = &RoundRobinSelector{}
	}
	if hook == nil {
		hook = NoopHook{}
	}
	manager := &Manager{
		store:                 store,
		authLoadGate:          semaphore.NewWeighted(math.MaxInt64),
		executors:             make(map[string]ProviderExecutor),
		selector:              selector,
		hook:                  hook,
		auths:                 make(map[string]*Auth),
		authEpochs:            make(map[string]uint64),
		homeRuntimeAuths:      make(map[string]map[string]*Auth),
		homeRuntimeAuthOwners: make(map[string]map[string]*HomeDispatchSelection),
		homeSessionSelections: make(map[string]map[homeSessionSelectionKey]*HomeDispatchSelection),
		providerOffsets:       make(map[string]int),
		modelPoolOffsets:      make(map[string]int),
	}

	manager.runtimeConfig.Store(&internalconfig.Config{})
	manager.apiKeyModelRouting.Store(&apiKeyModelRoutingSnapshot{config: &internalconfig.Config{}})
	defaultInFlightConfig, errInFlightConfig := HomeInFlightPublisherConfigFromConfig(internalconfig.DefaultCredentialInFlightConfig())
	if errInFlightConfig == nil {
		manager.ApplyHomeInFlightPublisherConfig(defaultInFlightConfig)
	}
	manager.scheduler = newAuthScheduler(selector)
	return manager
}

func (m *Manager) SetResultPolicy(policy ResultPolicy) {
	if m == nil {
		return
	}
	if policy == nil {
		m.resultPolicy.Store(nil)
		return
	}
	m.resultPolicy.Store(&resultPolicyHolder{policy: policy})
}

func (m *Manager) ResultPolicy() ResultPolicy {
	if m == nil {
		return nil
	}
	holder := m.resultPolicy.Load()
	if holder == nil {
		return nil
	}
	return holder.policy
}
