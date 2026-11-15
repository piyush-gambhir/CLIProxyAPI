package watcher

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/watcher/synthesizer"
	"gopkg.in/yaml.v3"

	sdkAuth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

type storePersister interface {
	PersistConfig(ctx context.Context) error
	PersistAuthFiles(ctx context.Context, message string, paths ...string) error
}

type authDirProvider interface {
	AuthDir() string
}

type Watcher struct {
	configPath                 string
	authDir                    string
	watchMu                    sync.Mutex
	started                    bool
	watchedAuthDir             string
	config                     *config.Config
	clientsMutex               sync.RWMutex
	authRescanMu               sync.Mutex
	configReloadMu             sync.Mutex
	configReloadTimer          *time.Timer
	configApplyMu              sync.Mutex
	serverUpdateMu             sync.Mutex
	serverUpdateTimer          *time.Timer
	serverUpdateLast           time.Time
	serverUpdatePend           bool
	stopped                    atomic.Bool
	reloadCallback             func(*config.Config)
	watcher                    *fsnotify.Watcher
	lastAuthHashes             map[string]string
	lastAuthContents           map[string]*coreauth.Auth
	fileAuthsByPath            map[string]map[string]*coreauth.Auth
	lastRemoveTimes            map[string]time.Time
	lastConfigHash             string
	authQueue                  chan<- AuthUpdate
	currentAuths               map[string]*coreauth.Auth
	pendingProviderReloadAuths map[string]*coreauth.Auth
	authRevisions              map[string]uint64
	persistedAuthRevisions     map[string]uint64
	fileObservations           map[string]uint64
	activeAuthScans            int
	runtimeAuths               map[string]*coreauth.Auth
	dispatchMu                 sync.Mutex
	dispatchCond               *sync.Cond
	pendingUpdates             map[string]AuthUpdate
	pendingOrder               []string
	dispatchCancel             context.CancelFunc
	storePersister             storePersister
	pluginAuthParser           synthesizer.PluginAuthParser
	mirroredAuthDir            string
	oldConfigYaml              []byte
}

type AuthUpdateAction string

const (
	AuthUpdateActionAdd    AuthUpdateAction = "add"
	AuthUpdateActionModify AuthUpdateAction = "modify"
	AuthUpdateActionDelete AuthUpdateAction = "delete"
)

type AuthUpdate struct {
	Action      AuthUpdateAction
	ID          string
	Auth        *coreauth.Auth
	revision    uint64
	fileRemoval bool
}

func (u AuthUpdate) IsFileRemoval() bool {
	return u.fileRemoval
}

func (u *AuthUpdate) SetFileRemoval(fileRemoval bool) {
	if u != nil {
		u.fileRemoval = fileRemoval
	}
}

func (u AuthUpdate) Revision() uint64 {
	return u.revision
}

func (u *AuthUpdate) SetRevision(rev uint64) {
	if u != nil {
		u.revision = rev
	}
}

const (
	replaceCheckDelay        = 50 * time.Millisecond
	configReloadDebounce     = 150 * time.Millisecond
	authRemoveDebounceWindow = 1 * time.Second
	serverUpdateDebounce     = 1 * time.Second
)

func NewWatcher(configPath, authDir string, reloadCallback func(*config.Config)) (*Watcher, error) {
	watcher, errNewWatcher := fsnotify.NewWatcher()
	if errNewWatcher != nil {
		return nil, errNewWatcher
	}
	w := &Watcher{
		configPath:      configPath,
		authDir:         authDir,
		reloadCallback:  reloadCallback,
		watcher:         watcher,
		lastAuthHashes:  make(map[string]string),
		fileAuthsByPath: make(map[string]map[string]*coreauth.Auth),
	}
	w.dispatchCond = sync.NewCond(&w.dispatchMu)
	if store := sdkAuth.GetTokenStore(); store != nil {
		if persister, ok := store.(storePersister); ok {
			w.storePersister = persister
			log.Debug("persistence-capable token store detected; watcher will propagate persisted changes")
		}
		if provider, ok := store.(authDirProvider); ok {
			if fixed := strings.TrimSpace(provider.AuthDir()); fixed != "" {
				w.mirroredAuthDir = fixed
				log.Debugf("mirrored auth directory locked to %s", fixed)
			}
		}
	}
	return w, nil
}

func (w *Watcher) Start(ctx context.Context) error {
	return w.start(ctx)
}

func (w *Watcher) Stop() error {
	w.stopped.Store(true)
	w.stopDispatch()
	w.stopConfigReloadTimer()
	w.stopServerUpdateTimer()
	return w.watcher.Close()
}

func (w *Watcher) SetConfig(cfg *config.Config) {
	cloned := cfg.CloneForRuntime()
	w.clientsMutex.Lock()
	if cloned != nil {
		if w.mirroredAuthDir != "" {
			cloned.AuthDir = w.mirroredAuthDir
			w.authDir = w.mirroredAuthDir
		} else if trimmedAuthDir := strings.TrimSpace(cloned.AuthDir); trimmedAuthDir != "" {
			if resolvedAuthDir, errResolveAuthDir := util.ResolveAuthDir(trimmedAuthDir); errResolveAuthDir == nil && resolvedAuthDir != "" {
				cloned.AuthDir = resolvedAuthDir
				w.authDir = resolvedAuthDir
			} else if w.authDir == "" {
				w.authDir = trimmedAuthDir
			}
		}
	}
	w.config = cloned
	w.oldConfigYaml, _ = yaml.Marshal(cloned)
	w.clientsMutex.Unlock()
	w.syncAuthDirWatch()
}

func (w *Watcher) currentAuthDirLocked() string {
	if w == nil {
		return ""
	}
	if authDir := strings.TrimSpace(w.authDir); authDir != "" {
		return authDir
	}
	if w.config != nil {
		if cfgAuthDir := strings.TrimSpace(w.config.AuthDir); cfgAuthDir != "" {
			if resolved, errResolve := util.ResolveAuthDir(cfgAuthDir); errResolve == nil && resolved != "" {
				return resolved
			}
			return cfgAuthDir
		}
	}
	return ""
}

func (w *Watcher) currentAuthDir() string {
	if w == nil {
		return ""
	}
	w.clientsMutex.RLock()
	defer w.clientsMutex.RUnlock()
	return w.currentAuthDirLocked()
}

func (w *Watcher) isPathInAuthDir(path, authDir string) bool {
	normalizedPath := w.normalizeAuthPath(path)
	normalizedDir := w.normalizeAuthPath(authDir)
	if normalizedPath == "" || normalizedDir == "" {
		return false
	}
	return w.sameNormalizedAuthPath(filepath.Dir(normalizedPath), normalizedDir)
}

func (w *Watcher) isPathInCurrentAuthDirLocked(path string) bool {
	authDir := w.currentAuthDirLocked()
	if authDir == "" {
		return true
	}
	return w.isPathInAuthDir(path, authDir)
}

func (w *Watcher) isPathInCurrentAuthDir(path string) bool {
	if w == nil {
		return false
	}
	w.clientsMutex.RLock()
	defer w.clientsMutex.RUnlock()
	return w.isPathInCurrentAuthDirLocked(path)
}

func (w *Watcher) SetPluginAuthParser(parser synthesizer.PluginAuthParser) {
	w.clientsMutex.Lock()
	defer w.clientsMutex.Unlock()
	w.pluginAuthParser = parser
}

func (w *Watcher) SetAuthUpdateQueue(queue chan<- AuthUpdate) {
	w.setAuthUpdateQueue(queue)
}

func (w *Watcher) DispatchRuntimeAuthUpdate(update AuthUpdate) bool {
	return w.dispatchRuntimeAuthUpdate(update)
}

func (w *Watcher) DispatchPersistedAuthUpdate(update AuthUpdate) bool {
	return w.dispatchPersistedAuthUpdate(update)
}

func (w *Watcher) DispatchPersistedAuthUpdateWithRevision(update *AuthUpdate) (bool, uint64) {
	return w.dispatchPersistedAuthUpdateWithRevision(update)
}

func (w *Watcher) RereadDiscardedFileSnapshot(auth *coreauth.Auth) {
	w.rereadDiscardedFileSnapshot(auth)
}

func (w *Watcher) SnapshotCoreAuths() []*coreauth.Auth {
	w.clientsMutex.RLock()
	cfg := w.config
	authDir := w.currentAuthDirLocked()
	parser := w.pluginAuthParser
	w.clientsMutex.RUnlock()
	return snapshotCoreAuths(cfg, authDir, parser)
}
