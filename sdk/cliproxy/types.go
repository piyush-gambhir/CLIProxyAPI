


package cliproxy

import (
	"context"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/watcher"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)




type TokenClientProvider interface {









	Load(ctx context.Context, cfg *config.Config) (*TokenClientResult, error)
}



type TokenClientResult struct {

	SuccessfulAuthed int
}



type APIKeyClientProvider interface {









	Load(ctx context.Context, cfg *config.Config) (*APIKeyClientResult, error)
}


type APIKeyClientResult struct {


	GeminiKeyCount int


	VertexCompatKeyCount int


	ClaudeKeyCount int


	CodexKeyCount int


	XAIKeyCount int


	MetaKeyCount int


	OpenAICompatCount int
}












type WatcherFactory func(configPath, authDir string, reload func(*config.Config)) (*WatcherWrapper, error)


type PluginAuthParser interface {
	ParseAuth(context.Context, pluginapi.AuthParseRequest) (*coreauth.Auth, bool, error)
}



type PluginMultiAuthParser interface {
	ParseAuths(context.Context, pluginapi.AuthParseRequest) ([]*coreauth.Auth, bool, error)
}


type WatcherWrapper struct {
	start func(ctx context.Context) error
	stop  func() error

	setConfig                    func(cfg *config.Config)
	snapshotAuths                func() []*coreauth.Auth
	snapshotAuthFile             func(path string) ([]*coreauth.Auth, error)
	setUpdateQueue               func(queue chan<- watcher.AuthUpdate)
	dispatchRuntimeUpdate        func(update watcher.AuthUpdate) bool
	dispatchPersistedAuth        func(update watcher.AuthUpdate) bool
	dispatchPersistedAuthWithRev func(update *watcher.AuthUpdate) (bool, uint64)
	rereadDiscardedFileSnapshot  func(auth *coreauth.Auth)
	setPluginAuthParser          func(parser PluginAuthParser)
	reloadConfigIfChanged        func()
}


func (w *WatcherWrapper) Start(ctx context.Context) error {
	if w == nil || w.start == nil {
		return nil
	}
	return w.start(ctx)
}


func (w *WatcherWrapper) Stop() error {
	if w == nil || w.stop == nil {
		return nil
	}
	return w.stop()
}


func (w *WatcherWrapper) SetConfig(cfg *config.Config) {
	if w == nil || w.setConfig == nil {
		return
	}
	w.setConfig(cfg)
}


func (w *WatcherWrapper) ReloadConfigIfChanged() bool {
	if w == nil || w.reloadConfigIfChanged == nil {
		return false
	}
	w.reloadConfigIfChanged()
	return true
}


func (w *WatcherWrapper) SetPluginAuthParser(parser PluginAuthParser) {
	if w == nil || w.setPluginAuthParser == nil {
		return
	}
	w.setPluginAuthParser(parser)
}




func (w *WatcherWrapper) DispatchRuntimeAuthUpdate(update watcher.AuthUpdate) bool {
	if w == nil || w.dispatchRuntimeUpdate == nil {
		return false
	}
	return w.dispatchRuntimeUpdate(update)
}


func (w *WatcherWrapper) DispatchPersistedAuthUpdate(update watcher.AuthUpdate) bool {
	if w == nil {
		return false
	}
	if w.dispatchPersistedAuthWithRev != nil {
		ok, _ := w.dispatchPersistedAuthWithRev(&update)
		return ok
	}
	if w.dispatchPersistedAuth != nil {
		return w.dispatchPersistedAuth(update)
	}
	return false
}



func (w *WatcherWrapper) DispatchPersistedAuthUpdateWithRevision(update *watcher.AuthUpdate) (bool, uint64) {
	if w == nil {
		return false, 0
	}
	if w.dispatchPersistedAuthWithRev != nil {
		return w.dispatchPersistedAuthWithRev(update)
	}
	if w.dispatchPersistedAuth != nil && update != nil {
		ok := w.dispatchPersistedAuth(*update)
		return ok, update.Revision()
	}
	return false, 0
}








func (w *WatcherWrapper) SnapshotAuths() []*coreauth.Auth {
	if w == nil || w.snapshotAuths == nil {
		return nil
	}
	return w.snapshotAuths()
}


func (w *WatcherWrapper) SnapshotAuthFile(path string) ([]*coreauth.Auth, error) {
	if w == nil || w.snapshotAuthFile == nil {
		return nil, fmt.Errorf("file auth snapshot is unavailable")
	}
	return w.snapshotAuthFile(path)
}



func (w *WatcherWrapper) RereadDiscardedFileSnapshot(auth *coreauth.Auth) {
	if w == nil || w.rereadDiscardedFileSnapshot == nil {
		return
	}
	w.rereadDiscardedFileSnapshot(auth)
}


func (w *WatcherWrapper) SetAuthUpdateQueue(queue chan<- watcher.AuthUpdate) {
	if w == nil || w.setUpdateQueue == nil {
		return
	}
	w.setUpdateQueue(queue)
}
