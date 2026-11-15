package cliproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"os"
	"path/filepath"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/watcher"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func waitForAntigravityClientModel(t *testing.T, clientID, modelID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !GlobalModelRegistry().ClientSupportsModel(clientID, modelID) {
		if time.Now().After(deadline) {
			t.Fatalf("client %s model %s was not published within 2s", clientID, modelID)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAntigravityDynamicRegistrationLifecycle(t *testing.T) {
	resetAntigravityCapabilityCache()
	t.Cleanup(resetAntigravityCapabilityCache)
	const allowed = "gemini-3.1-flash-lite"
	var response atomic.Value
	response.Store(map[string]any{"models": map[string]any{allowed: map[string]any{"maxTokens": 1}, "not-in-static": map[string]any{}}, "webSearchModelIds": []string{allowed}})
	var status atomic.Int32
	status.Store(200)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(int(status.Load()))
		_ = json.NewEncoder(w).Encode(response.Load())
	}))
	defer server.Close()
	cfg := &config.Config{OAuthModelAlias: map[string][]config.OAuthModelAlias{"antigravity": {{Name: allowed, Alias: "my-model"}}}}
	manager := coreauth.NewManager(nil, nil, nil)
	svc := &Service{cfg: cfg, coreManager: manager}
	auth := antigravityTestAuth("dynamic-account", server.URL)
	auth.Prefix = "tenant"
	var err error
	auth, err = manager.Register(t.Context(), auth)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { GlobalModelRegistry().UnregisterClient(auth.ID) })
	svc.registerModelsForAuth(t.Context(), auth)
	svc.WaitAntigravityProbes()
	assertModels := func(want bool) {
		t.Helper()
		models := GlobalModelRegistry().GetModelsForClient(auth.ID)
		if !want {
			if len(models) != 0 {
				t.Fatalf("expected empty catalog: %v", models)
			}
			return
		}
		if len(models) != 2 {
			t.Fatalf("expected alias and prefixed alias, got %v", models)
		}
		for _, m := range models {
			if m.ID != "my-model" && m.ID != "tenant/my-model" {
				t.Fatalf("unentitled model %s", m.ID)
			}
			if m.ContextLength <= 1 || !m.SupportsWebSearch {
				t.Fatalf("lost static info or fetched capability: %+v", m)
			}
		}
	}
	assertModels(true)
	svc.refreshAntigravityModels(t.Context())
	svc.WaitAntigravityProbes()
	if calls.Load() != 1 {
		t.Fatal("fresh cache probed")
	}
	expire := func() {
		key := svc.antigravityCapabilityKey(auth)
		antigravityCapabilityMu.Lock()
		entry := antigravityCapabilityCache[key]
		entry.expiresAt = time.Time{}
		antigravityCapabilityCache[key] = entry
		antigravityAuthFailureCache = make(map[string]antigravityFailureState)
		antigravityCapabilityMu.Unlock()
	}

	expire()
	status.Store(403)
	svc.refreshAntigravityModels(t.Context())
	svc.WaitAntigravityProbes()
	assertModels(true)

	expire()
	status.Store(200)
	response.Store(map[string]any{"models": map[string]any{}})
	svc.refreshAntigravityModels(t.Context())
	svc.WaitAntigravityProbes()
	assertModels(false)
	current, _ := manager.GetByID(auth.ID)
	if current.Disabled {
		t.Fatal("empty catalog disabled credential")
	}

	svc.registerModelsForAuth(t.Context(), current)
	svc.WaitAntigravityProbes()
	assertModels(false)
	expire()
	response.Store(map[string]any{"models": map[string]any{allowed: map[string]any{}}, "webSearchModelIds": []string{allowed}})
	svc.refreshAntigravityModels(t.Context())
	svc.WaitAntigravityProbes()
	assertModels(true)
	if len(registry.GetAntigravityModels()) < 3 {
		t.Fatal("static catalog mutated")
	}
}

func TestAntigravityDynamicRegistrationExclusions(t *testing.T) {
	svc := &Service{cfg: &config.Config{OAuthExcludedModels: map[string][]string{"antigravity": {"gemini-3.1-flash-lite"}}}}
	auth := antigravityTestAuth("excluded", "http://127.0.0.1:1")
	hints := antigravityModelCapabilityHints{ModelIDs: map[string]struct{}{"gemini-3.1-flash-lite": {}}}
	if models := svc.antigravityModelsForHints(auth, hints); len(models) != 0 {
		t.Fatalf("excluded models restored: %v", models)
	}
}

func TestAntigravityDynamicRegistrationBatches(t *testing.T) {
	resetAntigravityCapabilityCache()
	t.Cleanup(resetAntigravityCapabilityCache)
	const workers = modelRegistrationMaxWorkersPerCategory
	var active, peak, calls atomic.Int32
	started := make(chan struct{}, workers*2+3)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		calls.Add(1)
		for previous := peak.Load(); current > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, current) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte(`{"models":{"gemini-3.1-flash-lite":{}}}`))
	}))
	defer server.Close()
	manager := coreauth.NewManager(nil, nil, nil)
	svc := &Service{cfg: &config.Config{}, coreManager: manager}
	for i := range workers*2 + 3 {
		auth := antigravityTestAuth(fmt.Sprint("batch-", i), server.URL)
		if _, err := manager.Register(t.Context(), auth); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { GlobalModelRegistry().UnregisterClient(auth.ID) })
	}
	done := make(chan struct{})
	go func() { defer close(done); svc.refreshAntigravityModels(t.Context()) }()
	for range workers {
		<-started
	}
	if peak.Load() > workers {
		t.Fatal("unbounded probes")
	}
	unblock()
	<-done
	svc.WaitAntigravityProbes()
	if calls.Load() != workers*2+3 || peak.Load() > workers {
		t.Fatalf("calls=%d peak=%d", calls.Load(), peak.Load())
	}
	for _, auth := range manager.List() {
		if len(GlobalModelRegistry().GetModelsForClient(auth.ID)) != 1 {
			t.Fatal("batch registration incomplete")
		}
	}
}

func TestAntigravityDynamicRegistrationDropsStaleFullCatalog(t *testing.T) {
	resetAntigravityCapabilityCache()
	t.Cleanup(resetAntigravityCapabilityCache)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"models":{"gemini-3.1-flash-lite":{}}}`))
	}))
	defer server.Close()
	manager := coreauth.NewManager(nil, nil, nil)
	svc := &Service{cfg: &config.Config{}, coreManager: manager}
	auth := antigravityTestAuth("stale-full", server.URL)
	auth, _ = manager.Register(t.Context(), auth)
	t.Cleanup(func() { GlobalModelRegistry().UnregisterClient(auth.ID) })
	svc.registerModelsForAuth(t.Context(), auth)
	<-started
	GlobalModelRegistry().UnregisterClient(auth.ID)
	manager.Remove(context.Background(), auth.ID)
	unblock()
	svc.WaitAntigravityProbes()
	if len(GlobalModelRegistry().GetModelsForClient(auth.ID)) != 0 {
		t.Fatal("removed credential resurrected")
	}
}

func TestAntigravityRestartSeedsPersistedHintsBeforeProbeAndAcrossProbeFailure(t *testing.T) {
	resetAntigravityCapabilityCache()
	t.Cleanup(resetAntigravityCapabilityCache)

	var status atomic.Int32
	status.Store(http.StatusOK)
	var blockProbe atomic.Bool
	probeStarted := make(chan struct{}, 4)
	releaseProbe := make(chan struct{})
	var once sync.Once
	unblockProbe := func() { once.Do(func() { close(releaseProbe) }) }

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if blockProbe.Load() {
			probeStarted <- struct{}{}
			select {
			case <-releaseProbe:
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(int(status.Load()))
		_, _ = w.Write([]byte(`{"models":{"gemini-3-flash":{},"gemini-3.1-flash-lite":{}},"webSearchModelIds":["gemini-3-flash"]}`))
	}))
	defer server.Close()
	defer unblockProbe()

	authDir := t.TempDir()
	store := sdkAuth.NewFileTokenStore()
	store.SetBaseDir(authDir)

	manager := coreauth.NewManager(store, nil, nil)
	svc := &Service{cfg: &config.Config{AuthDir: authDir}, coreManager: manager}
	auth := &coreauth.Auth{
		ID:       "antigravity-restart.json",
		FileName: "antigravity-restart.json",
		Provider: "antigravity",
		Attributes: map[string]string{
			coreauth.AttributePath:          filepath.Join(authDir, "antigravity-restart.json"),
			coreauth.AttributeSource:        filepath.Join(authDir, "antigravity-restart.json"),
			coreauth.AttributeSourceBackend: coreauth.AuthSourceFile,
		},
		Metadata: map[string]any{
			"type":         "antigravity",
			"access_token": "token-restart",
			"project_id":   "project-restart",
			"base_url":     server.URL,
		},
	}
	auth, errRegister := manager.Register(t.Context(), auth)
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	t.Cleanup(func() { GlobalModelRegistry().UnregisterClient(auth.ID) })

	svc.registerModelsForAuth(t.Context(), auth)
	svc.WaitAntigravityProbes()
	if !GlobalModelRegistry().ClientSupportsModel(auth.ID, "gemini-3-flash") {
		t.Fatal("initial probe did not register gemini-3-flash")
	}

	GlobalModelRegistry().UnregisterClient(auth.ID)
	resetAntigravityCapabilityCache()
	blockProbe.Store(true)
	status.Store(http.StatusServiceUnavailable)

	restartedManager := coreauth.NewManager(store, nil, nil)
	if errLoad := restartedManager.Load(t.Context()); errLoad != nil {
		t.Fatal(errLoad)
	}
	loadedAuth, ok := restartedManager.GetByID(auth.ID)
	if !ok || loadedAuth == nil {
		t.Fatal("restarted manager did not load auth from disk")
	}
	restartedSvc := &Service{cfg: &config.Config{AuthDir: authDir}, coreManager: restartedManager}

	restartedSvc.registerModelsForAuth(t.Context(), loadedAuth)
	<-probeStarted

	models := GlobalModelRegistry().GetModelsForClient(loadedAuth.ID)
	if len(models) != 2 || !GlobalModelRegistry().ClientSupportsModel(loadedAuth.ID, "gemini-3-flash") {
		t.Fatalf("expected persisted models to be registered before startup probe answers, got %v", models)
	}
	var flashModel *ModelInfo
	for _, m := range models {
		if m != nil && m.ID == "gemini-3-flash" {
			flashModel = m
			break
		}
	}
	if flashModel == nil || !flashModel.SupportsWebSearch {
		t.Fatalf("expected gemini-3-flash web search capability to be restored before probe answers, got %+v", flashModel)
	}

	unblockProbe()
	restartedSvc.WaitAntigravityProbes()
	if !GlobalModelRegistry().ClientSupportsModel(loadedAuth.ID, "gemini-3-flash") {
		t.Fatal("failed startup probe made previously routable account unroutable")
	}

	GlobalModelRegistry().UnregisterClient(loadedAuth.ID)
	resetAntigravityCapabilityCache()
	changedProjectAuth := loadedAuth.Clone()
	changedProjectAuth.Metadata["project_id"] = "different-project"
	if hints := restartedSvc.cachedAntigravityHints(changedProjectAuth); hints.ModelIDs != nil {
		t.Fatalf("expected project change to ignore persisted hints, got %v", hints)
	}
	changedEndpointAuth := loadedAuth.Clone()
	changedEndpointAuth.Metadata["base_url"] = "https://different-endpoint.invalid"
	if hints := restartedSvc.cachedAntigravityHints(changedEndpointAuth); hints.ModelIDs != nil {
		t.Fatalf("expected endpoint change to ignore persisted hints, got %v", hints)
	}
}

func TestAntigravityAuthUpdateWithSkipPersistStillPersistsHintsAcrossRestart(t *testing.T) {
	resetAntigravityCapabilityCache()
	t.Cleanup(resetAntigravityCapabilityCache)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":{"gemini-3-flash":{}}}`))
	}))
	defer server.Close()

	authDir := t.TempDir()
	authPath := filepath.Join(authDir, "antigravity-watcher.json")
	initialJSON := fmt.Sprintf(`{"type":"antigravity","access_token":"token-watcher","project_id":"project-watcher","base_url":%q}`, server.URL)
	if errWrite := os.WriteFile(authPath, []byte(initialJSON), 0o600); errWrite != nil {
		t.Fatal(errWrite)
	}

	store := sdkAuth.NewFileTokenStore()
	store.SetBaseDir(authDir)
	manager := coreauth.NewManager(store, nil, nil)
	svc := &Service{cfg: &config.Config{AuthDir: authDir}, coreManager: manager}
	auth := &coreauth.Auth{
		ID:       "antigravity-watcher.json",
		FileName: "antigravity-watcher.json",
		Provider: "antigravity",
		Attributes: map[string]string{
			coreauth.AttributePath:          authPath,
			coreauth.AttributeSource:        authPath,
			coreauth.AttributeSourceBackend: coreauth.AuthSourceFile,
		},
		Metadata: map[string]any{
			"type":         "antigravity",
			"access_token": "token-watcher",
			"project_id":   "project-watcher",
			"base_url":     server.URL,
		},
	}
	t.Cleanup(func() { GlobalModelRegistry().UnregisterClient(auth.ID) })

	svc.handleAuthUpdate(coreauth.WithSkipPersist(t.Context()), watcher.AuthUpdate{
		Action: watcher.AuthUpdateActionAdd,
		ID:     auth.ID,
		Auth:   auth,
	})
	svc.WaitAntigravityProbes()
	if !GlobalModelRegistry().ClientSupportsModel(auth.ID, "gemini-3-flash") {
		t.Fatal("initial watcher auth update did not register gemini-3-flash")
	}

	GlobalModelRegistry().UnregisterClient(auth.ID)
	resetAntigravityCapabilityCache()
	restartedManager := coreauth.NewManager(store, nil, nil)
	if errLoad := restartedManager.Load(t.Context()); errLoad != nil {
		t.Fatal(errLoad)
	}
	loadedAuth, ok := restartedManager.GetByID(auth.ID)
	if !ok || loadedAuth == nil {
		t.Fatal("restarted manager did not load auth")
	}
	restartedSvc := &Service{cfg: &config.Config{AuthDir: authDir}, coreManager: restartedManager}
	if hints := restartedSvc.cachedAntigravityHints(loadedAuth); hints.ModelIDs == nil {
		t.Fatal("expected capability hints to be persisted to disk when auth was registered via WithSkipPersist")
	}
}

type flakyAntigravityHintStore struct {
	*sdkAuth.FileTokenStore
	saveMu           sync.Mutex
	failSaves        atomic.Bool
	blockFirst       atomic.Bool
	blockAll         atomic.Bool
	ignoreCtxOnBlock atomic.Bool
	firstSaveIn      chan struct{}
	saveEntered      chan string
	releaseSave      chan struct{}
	remoteMu         sync.Mutex
	remoteFiles      map[string][]byte
}

func (s *flakyAntigravityHintStore) Save(ctx context.Context, auth *coreauth.Auth) (string, error) {
	if s.blockAll.Load() && s.saveEntered != nil && auth != nil {
		s.saveEntered <- auth.ID
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	path, errSave := s.FileTokenStore.Save(ctx, auth)
	if errSave != nil {
		return "", errSave
	}
	if s.blockFirst.CompareAndSwap(true, false) {
		close(s.firstSaveIn)
		if s.ignoreCtxOnBlock.Load() {
			<-s.releaseSave
		} else {
			select {
			case <-s.releaseSave:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
	}
	if s.blockAll.Load() {
		if s.ignoreCtxOnBlock.Load() {
			<-s.releaseSave
		} else {
			select {
			case <-s.releaseSave:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
	}
	if s.failSaves.Load() {
		return "", fmt.Errorf("simulated remote store write failure")
	}
	if path != "" {
		if data, errRead := os.ReadFile(path); errRead == nil {
			s.remoteMu.Lock()
			if s.remoteFiles == nil {
				s.remoteFiles = make(map[string][]byte)
			}
			s.remoteFiles[path] = data
			s.remoteMu.Unlock()
		}
	}
	return path, nil
}

func (s *flakyAntigravityHintStore) restoreRemoteToDisk() error {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	for path, data := range s.remoteFiles {
		if errWrite := os.WriteFile(path, data, 0o600); errWrite != nil {
			return errWrite
		}
	}
	return nil
}

func TestAntigravityPersistedHintsFencesStaleRevision(t *testing.T) {
	resetAntigravityCapabilityCache()
	t.Cleanup(resetAntigravityCapabilityCache)

	authDir := t.TempDir()
	baseStore := sdkAuth.NewFileTokenStore()
	baseStore.SetBaseDir(authDir)
	store := &flakyAntigravityHintStore{
		FileTokenStore: baseStore,
		firstSaveIn:    make(chan struct{}),
		releaseSave:    make(chan struct{}),
	}

	manager := coreauth.NewManager(store, nil, nil)
	svc := &Service{cfg: &config.Config{AuthDir: authDir}, coreManager: manager}
	auth := &coreauth.Auth{
		ID:       "antigravity-fence-persist.json",
		FileName: "antigravity-fence-persist.json",
		Provider: "antigravity",
		Attributes: map[string]string{
			coreauth.AttributePath:          filepath.Join(authDir, "antigravity-fence-persist.json"),
			coreauth.AttributeSource:        filepath.Join(authDir, "antigravity-fence-persist.json"),
			coreauth.AttributeSourceBackend: coreauth.AuthSourceFile,
		},
		Metadata: map[string]any{
			"type":         "antigravity",
			"access_token": "token-fence-persist",
			"project_id":   "project-fence-persist",
			"base_url":     "https://daily.invalid",
		},
	}
	auth, errRegister := manager.Register(t.Context(), auth)
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	t.Cleanup(func() { GlobalModelRegistry().UnregisterClient(auth.ID) })

	key := svc.antigravityCapabilityKey(auth)
	oldHints := antigravityModelCapabilityHints{
		ModelIDs: map[string]struct{}{"gemini-3-flash": {}},
		revision: 1,
	}
	newHints := antigravityModelCapabilityHints{
		ModelIDs: map[string]struct{}{"gemini-3.1-flash-lite": {}},
		revision: 2,
	}

	antigravityCapabilityMu.Lock()
	antigravityCapabilityCache[key] = antigravityCapabilityCacheEntry{
		hints:     oldHints.clone(),
		expiresAt: time.Now().Add(antigravityCapabilityCacheTTL),
	}
	antigravityCapabilityMu.Unlock()
	store.blockFirst.Store(true)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		svc.persistAntigravityCapabilityHints(t.Context(), auth, key, "https://daily.invalid", oldHints)
	}()
	<-store.firstSaveIn

	antigravityCapabilityMu.Lock()
	antigravityCapabilityCache[key] = antigravityCapabilityCacheEntry{
		hints:     newHints.clone(),
		expiresAt: time.Now().Add(antigravityCapabilityCacheTTL),
	}
	antigravityCapabilityMu.Unlock()
	go func() {
		defer wg.Done()
		svc.persistAntigravityCapabilityHints(t.Context(), auth, key, "https://daily.invalid", newHints)
	}()
	close(store.releaseSave)
	wg.Wait()

	svc.persistAntigravityCapabilityHints(t.Context(), auth, key, "https://daily.invalid", oldHints)

	resetAntigravityCapabilityCache()
	restartedManager := coreauth.NewManager(baseStore, nil, nil)
	if errLoad := restartedManager.Load(t.Context()); errLoad != nil {
		t.Fatal(errLoad)
	}
	loadedAuth, _ := restartedManager.GetByID(auth.ID)
	restartedSvc := &Service{cfg: &config.Config{AuthDir: authDir}, coreManager: restartedManager}
	hints := restartedSvc.cachedAntigravityHints(loadedAuth)
	if _, ok := hints.ModelIDs["gemini-3.1-flash-lite"]; !ok || len(hints.ModelIDs) != 1 {
		t.Fatalf("stale revision overwrote newer persisted hints: got %v", hints.ModelIDs)
	}
}

func TestAntigravityPersistedHintsRetriesAfterTransientStoreFailure(t *testing.T) {
	resetAntigravityCapabilityCache()
	t.Cleanup(resetAntigravityCapabilityCache)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":{"gemini-3-flash":{}}}`))
	}))
	defer server.Close()

	authDir := t.TempDir()
	baseStore := sdkAuth.NewFileTokenStore()
	baseStore.SetBaseDir(authDir)
	store := &flakyAntigravityHintStore{FileTokenStore: baseStore}

	manager := coreauth.NewManager(store, nil, nil)
	svc := &Service{cfg: &config.Config{AuthDir: authDir}, coreManager: manager}
	auth := &coreauth.Auth{
		ID:       "antigravity-retry-persist.json",
		FileName: "antigravity-retry-persist.json",
		Provider: "antigravity",
		Attributes: map[string]string{
			coreauth.AttributePath:          filepath.Join(authDir, "antigravity-retry-persist.json"),
			coreauth.AttributeSource:        filepath.Join(authDir, "antigravity-retry-persist.json"),
			coreauth.AttributeSourceBackend: coreauth.AuthSourceFile,
		},
		Metadata: map[string]any{
			"type":         "antigravity",
			"access_token": "token-retry-persist",
			"project_id":   "project-retry-persist",
			"base_url":     server.URL,
		},
	}
	auth, errRegister := manager.Register(t.Context(), auth)
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	t.Cleanup(func() { GlobalModelRegistry().UnregisterClient(auth.ID) })

	store.failSaves.Store(true)
	svc.registerModelsForAuth(t.Context(), auth)
	svc.WaitAntigravityProbes()

	store.failSaves.Store(false)
	svc.refreshAntigravityModels(t.Context())
	svc.WaitAntigravityProbes()

	if errRemove := os.RemoveAll(authDir); errRemove != nil {
		t.Fatal(errRemove)
	}
	if errMkdir := os.MkdirAll(authDir, 0o700); errMkdir != nil {
		t.Fatal(errMkdir)
	}
	if errRestore := store.restoreRemoteToDisk(); errRestore != nil {
		t.Fatal(errRestore)
	}
	GlobalModelRegistry().UnregisterClient(auth.ID)
	resetAntigravityCapabilityCache()
	restartedManager := coreauth.NewManager(baseStore, nil, nil)
	if errLoad := restartedManager.Load(t.Context()); errLoad != nil {
		t.Fatal(errLoad)
	}
	loadedAuth, _ := restartedManager.GetByID(auth.ID)
	restartedSvc := &Service{cfg: &config.Config{AuthDir: authDir}, coreManager: restartedManager}
	if hints := restartedSvc.cachedAntigravityHints(loadedAuth); hints.ModelIDs == nil {
		t.Fatal("expected identical hints to be persisted after transient store failure recovered")
	}
}

func TestAntigravityBlockedHintPersistenceDoesNotBlockPublicationOrOtherAccounts(t *testing.T) {
	resetAntigravityCapabilityCache()
	t.Cleanup(resetAntigravityCapabilityCache)

	var authAModel atomic.Value
	authAModel.Store("gemini-3-flash")
	authBProbed := make(chan struct{}, 2)
	authARefreshed := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer token-slow-b" {
			_, _ = w.Write([]byte(`{"models":{"gemini-3-flash":{}}}`))
			authBProbed <- struct{}{}

			time.Sleep(200 * time.Millisecond)
			return
		}
		model := authAModel.Load().(string)
		_, _ = fmt.Fprintf(w, `{"models":{%q:{}}}`, model)
		if model == "gemini-3.1-flash-lite" {
			authARefreshed <- struct{}{}
		}
	}))
	defer server.Close()

	authDir := t.TempDir()
	baseStore := sdkAuth.NewFileTokenStore()
	baseStore.SetBaseDir(authDir)
	store := &flakyAntigravityHintStore{
		FileTokenStore: baseStore,
		firstSaveIn:    make(chan struct{}),
		saveEntered:    make(chan string, 8),
		releaseSave:    make(chan struct{}),
	}

	lifetime, cancelLifetime := context.WithCancel(t.Context())
	defer cancelLifetime()
	manager := coreauth.NewManager(store, nil, nil)
	svc := &Service{cfg: &config.Config{AuthDir: authDir}, coreManager: manager, antigravityContext: lifetime}

	authA := &coreauth.Auth{
		ID:       "antigravity-slow-save-a.json",
		FileName: "antigravity-slow-save-a.json",
		Provider: "antigravity",
		Attributes: map[string]string{
			coreauth.AttributePath:          filepath.Join(authDir, "antigravity-slow-save-a.json"),
			coreauth.AttributeSource:        filepath.Join(authDir, "antigravity-slow-save-a.json"),
			coreauth.AttributeSourceBackend: coreauth.AuthSourceFile,
		},
		Metadata: map[string]any{
			"type":         "antigravity",
			"access_token": "token-slow-a",
			"project_id":   "project-slow-a",
			"base_url":     server.URL,
		},
	}
	authB := &coreauth.Auth{
		ID:       "antigravity-slow-save-b.json",
		FileName: "antigravity-slow-save-b.json",
		Provider: "antigravity",
		Attributes: map[string]string{
			coreauth.AttributePath:          filepath.Join(authDir, "antigravity-slow-save-b.json"),
			coreauth.AttributeSource:        filepath.Join(authDir, "antigravity-slow-save-b.json"),
			coreauth.AttributeSourceBackend: coreauth.AuthSourceFile,
		},
		Metadata: map[string]any{
			"type":         "antigravity",
			"access_token": "token-slow-b",
			"project_id":   "project-slow-b",
			"base_url":     server.URL,
		},
	}
	authA, _ = manager.Register(t.Context(), authA)
	authB, _ = manager.Register(t.Context(), authB)
	t.Cleanup(func() {
		GlobalModelRegistry().UnregisterClient(authA.ID)
		GlobalModelRegistry().UnregisterClient(authB.ID)
	})

	store.blockAll.Store(true)
	svc.registerModelsForAuth(t.Context(), authA)
	if got := <-store.saveEntered; got != authA.ID {
		t.Fatalf("first saveEntered = %q, want %q", got, authA.ID)
	}

	if !GlobalModelRegistry().ClientSupportsModel(authA.ID, "gemini-3-flash") {
		cancelLifetime()
		svc.WaitAntigravityProbes()
		t.Fatal("blocked metadata persistence delayed model publication to registry")
	}

	keyA := svc.antigravityCapabilityKey(authA)
	antigravityCapabilityMu.Lock()
	entryA := antigravityCapabilityCache[keyA]
	entryA.expiresAt = time.Time{}
	antigravityCapabilityCache[keyA] = entryA
	antigravityCapabilityMu.Unlock()
	authAModel.Store("gemini-3.1-flash-lite")

	svc.refreshAntigravityModels(t.Context())
	select {
	case <-authBProbed:
	case <-time.After(time.Second):
		cancelLifetime()
		svc.WaitAntigravityProbes()
		t.Fatal("stalled persistence on authA blocked authB probe")
	}
	select {
	case <-authARefreshed:
	case <-time.After(time.Second):
		cancelLifetime()
		svc.WaitAntigravityProbes()
		t.Fatal("stalled persistence on authA blocked subsequent probe for authA")
	}

	waitForAntigravityClientModel(t, authB.ID, "gemini-3-flash")

	select {
	case <-store.saveEntered:
	case <-time.After(time.Second):
		cancelLifetime()
		svc.WaitAntigravityProbes()
		t.Fatal("subsequent persistence task did not start after registry publication")
	}

	store.blockAll.Store(false)
	updateBDone := make(chan error, 1)
	go func() {
		updatedB := authB.Clone()
		updatedB.Prefix = "b-prefix"
		_, errUpdateB := manager.Update(t.Context(), updatedB)
		updateBDone <- errUpdateB
	}()
	select {
	case errUpdateB := <-updateBDone:
		if errUpdateB != nil {
			cancelLifetime()
			svc.WaitAntigravityProbes()
			t.Fatal(errUpdateB)
		}
	case <-time.After(time.Second):
		cancelLifetime()
		svc.WaitAntigravityProbes()
		t.Fatal("persisting Update on authB blocked while store Save was stalled on authA")
	}

	svc.WaitAntigravityProbes()
	store.firstSaveIn = make(chan struct{})
	store.releaseSave = make(chan struct{})
	store.ignoreCtxOnBlock.Store(true)
	store.blockFirst.Store(true)
	newerHintsA := antigravityModelCapabilityHints{
		ModelIDs: map[string]struct{}{"gemini-3-flash": {}},
		revision: 99,
	}
	antigravityCapabilityMu.Lock()
	antigravityCapabilityCache[keyA] = antigravityCapabilityCacheEntry{
		hints:     newerHintsA.clone(),
		expiresAt: time.Now().Add(antigravityCapabilityCacheTTL),
	}
	antigravityCapabilityMu.Unlock()
	svc.queueAntigravityHintPersistence(t.Context(), authA, keyA, server.URL, newerHintsA)
	<-store.firstSaveIn

	ctxB, cancelB := context.WithCancel(t.Context())
	canceledUpdateBDone := make(chan error, 1)
	go func() {
		currentB, _ := manager.GetByID(authB.ID)
		nextB := currentB.Clone()
		nextB.Prefix = "b-prefix-2"
		_, errUpdate := manager.Update(ctxB, nextB)
		canceledUpdateBDone <- errUpdate
	}()
	cancelB()
	select {
	case <-canceledUpdateBDone:
	case <-time.After(time.Second):
		close(store.releaseSave)
		cancelLifetime()
		svc.WaitAntigravityProbes()
		t.Fatal("canceled foreground Update on authB remained stuck on authA's uncancelable store Save")
	}
	close(store.releaseSave)
	if !GlobalModelRegistry().ClientSupportsModel(authB.ID, "gemini-3-flash") {
		cancelLifetime()
		svc.WaitAntigravityProbes()
		t.Fatal("authB models were not published while authA persistence was stalled")
	}
	if !GlobalModelRegistry().ClientSupportsModel(authA.ID, "gemini-3.1-flash-lite") {
		cancelLifetime()
		svc.WaitAntigravityProbes()
		t.Fatal("authA updated models were not published while authA initial persistence was stalled")
	}

	cancelLifetime()
	doneWait := make(chan struct{})
	go func() {
		defer close(doneWait)
		svc.WaitAntigravityProbes()
	}()
	select {
	case <-doneWait:
	case <-time.After(time.Second):
		close(store.releaseSave)
		t.Fatal("service context cancellation did not cancel blocked metadata persistence")
	}
}

func TestAntigravityConcurrentAuthUpdatePreservesBothNewTokenAndPersistedHints(t *testing.T) {
	resetAntigravityCapabilityCache()
	t.Cleanup(resetAntigravityCapabilityCache)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":{"gemini-3-flash":{}}}`))
	}))
	defer server.Close()

	authDir := t.TempDir()
	baseStore := sdkAuth.NewFileTokenStore()
	baseStore.SetBaseDir(authDir)
	store := &flakyAntigravityHintStore{
		FileTokenStore: baseStore,
		firstSaveIn:    make(chan struct{}),
		releaseSave:    make(chan struct{}),
	}

	manager := coreauth.NewManager(store, nil, nil)
	svc := &Service{cfg: &config.Config{AuthDir: authDir}, coreManager: manager}
	auth := &coreauth.Auth{
		ID:       "antigravity-concurrent-update.json",
		FileName: "antigravity-concurrent-update.json",
		Provider: "antigravity",
		Attributes: map[string]string{
			coreauth.AttributePath:          filepath.Join(authDir, "antigravity-concurrent-update.json"),
			coreauth.AttributeSource:        filepath.Join(authDir, "antigravity-concurrent-update.json"),
			coreauth.AttributeSourceBackend: coreauth.AuthSourceFile,
		},
		Metadata: map[string]any{
			"type":         "antigravity",
			"access_token": "initial-token",
			"project_id":   "project-concurrent",
			"base_url":     server.URL,
		},
	}
	auth, errRegister := manager.Register(t.Context(), auth)
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	t.Cleanup(func() { GlobalModelRegistry().UnregisterClient(auth.ID) })

	staleSnapshot := auth.Clone()

	store.blockFirst.Store(true)
	staleSnapshot.Metadata["access_token"] = "rotated-token-1"
	updateDone := make(chan error, 1)
	go func() {
		_, errUpdate := manager.Update(t.Context(), staleSnapshot)
		updateDone <- errUpdate
	}()
	<-store.firstSaveIn

	key := svc.antigravityCapabilityKey(auth)
	hints := antigravityModelCapabilityHints{
		ModelIDs: map[string]struct{}{"gemini-3-flash": {}},
		revision: 1,
	}
	antigravityCapabilityMu.Lock()
	antigravityCapabilityCache[key] = antigravityCapabilityCacheEntry{
		hints:     hints.clone(),
		expiresAt: time.Now().Add(antigravityCapabilityCacheTTL),
	}
	antigravityCapabilityMu.Unlock()

	persistDone := make(chan struct{})
	go func() {
		defer close(persistDone)
		svc.persistAntigravityCapabilityHints(t.Context(), auth, key, server.URL, hints)
	}()
	close(store.releaseSave)
	if errUpdate := <-updateDone; errUpdate != nil {
		t.Fatal(errUpdate)
	}
	<-persistDone
	svc.WaitAntigravityProbes()

	snapshotWithOldHints, _ := manager.GetByID(auth.ID)
	snapshotWithOldHints = snapshotWithOldHints.Clone()
	snapshotWithOldHints.Metadata["access_token"] = "rotated-token-2"

	store.firstSaveIn = make(chan struct{})
	store.releaseSave = make(chan struct{})
	store.blockFirst.Store(true)

	newerHints := antigravityModelCapabilityHints{
		ModelIDs: map[string]struct{}{
			"gemini-3-flash":        {},
			"gemini-3.1-flash-lite": {},
		},
		revision: 2,
	}
	antigravityCapabilityMu.Lock()
	antigravityCapabilityCache[key] = antigravityCapabilityCacheEntry{
		hints:             newerHints.clone(),
		persistedRevision: 1,
		expiresAt:         time.Now().Add(antigravityCapabilityCacheTTL),
	}
	antigravityCapabilityMu.Unlock()

	svc.queueAntigravityHintPersistence(t.Context(), auth, key, server.URL, newerHints)
	<-store.firstSaveIn

	if _, errUpdate := manager.Update(t.Context(), snapshotWithOldHints); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	svc.WaitAntigravityProbes()

	snapshotWithOldHints.Metadata["access_token"] = "rotated-token-2"
	if _, errUpdate := manager.Update(t.Context(), snapshotWithOldHints); errUpdate != nil {
		t.Fatal(errUpdate)
	}

	GlobalModelRegistry().UnregisterClient(auth.ID)
	resetAntigravityCapabilityCache()
	restartedManager := coreauth.NewManager(baseStore, nil, nil)
	if errLoad := restartedManager.Load(t.Context()); errLoad != nil {
		t.Fatal(errLoad)
	}
	loadedAuth, ok := restartedManager.GetByID(auth.ID)
	if !ok || loadedAuth == nil {
		t.Fatal("restarted manager did not load auth")
	}
	if gotToken, _ := loadedAuth.Metadata["access_token"].(string); gotToken != "rotated-token-2" {
		t.Fatalf("loaded access_token = %q, want rotated-token-2", gotToken)
	}
	restartedSvc := &Service{cfg: &config.Config{AuthDir: authDir}, coreManager: restartedManager}
	loadedHints := restartedSvc.cachedAntigravityHints(loadedAuth)
	if _, ok := loadedHints.ModelIDs["gemini-3.1-flash-lite"]; !ok || len(loadedHints.ModelIDs) != 2 {
		t.Fatalf("expected version 2 hints to survive preemption and stale-snapshot Update, got %v", loadedHints.ModelIDs)
	}
}
