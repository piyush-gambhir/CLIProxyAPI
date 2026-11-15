package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestScheduler_ModelCooldown_DoesNotTriggerRebuild(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerProviderTestExecutor{provider: "gemini"})

	model := "scheduler-cooldown-no-rebuild-model"
	auth := &Auth{
		ID:       "auth-cooldown-no-rebuild",
		Provider: "gemini",
	}
	if _, errRegister := manager.Register(ctx, auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	registerSchedulerModels(t, "gemini", model, auth.ID)
	manager.RefreshSchedulerEntry(auth.ID)

	picked, errPick := manager.scheduler.pickSingle(ctx, "gemini", model, cliproxyexecutor.Options{}, nil)
	if errPick != nil || picked == nil || picked.ID != auth.ID {
		t.Fatalf("initial pickSingle failed: picked=%v, err=%v", picked, errPick)
	}

	manager.MarkResult(ctx, Result{
		AuthID:   auth.ID,
		Provider: "gemini",
		Model:    model,
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusTooManyRequests, Message: "quota exceeded"},
	})

	_, errCooldown := manager.scheduler.pickSingle(ctx, "gemini", model, cliproxyexecutor.Options{}, nil)
	var targetCooldownErr *modelCooldownError
	if !errors.As(errCooldown, &targetCooldownErr) {
		t.Fatalf("expected pickSingle to return *modelCooldownError, got: %v", errCooldown)
	}
	if shouldRetrySchedulerPick(errCooldown) {
		t.Fatalf("shouldRetrySchedulerPick returned true for modelCooldownError; cooldown must not trigger scheduler rebuild")
	}

	manager.scheduler.mu.Lock()
	providerSchedulerBefore := manager.scheduler.providers["gemini"]
	manager.scheduler.mu.Unlock()

	_, _, errPickNext := manager.pickNext(ctx, "gemini", model, cliproxyexecutor.Options{}, nil)
	if errPickNext == nil {
		t.Fatal("expected pickNext to fail with cooldown error, got nil")
	}
	if !errors.As(errPickNext, &targetCooldownErr) {
		t.Fatalf("expected pickNext error to be *modelCooldownError, got: %v", errPickNext)
	}

	manager.scheduler.mu.Lock()
	providerSchedulerAfter := manager.scheduler.providers["gemini"]
	manager.scheduler.mu.Unlock()

	if providerSchedulerBefore != providerSchedulerAfter {
		t.Fatalf("pickNext unexpectedly triggered a full scheduler rebuild on cooldown error")
	}

	if manager.currentVersion() != manager.syncedVersion.Load() {
		t.Fatalf("syncedVersion (%d) did not advance to currentVersion (%d)", manager.syncedVersion.Load(), manager.currentVersion())
	}

	_, _, errSubsequent := manager.pickNext(ctx, "gemini", model, cliproxyexecutor.Options{}, nil)
	if !errors.As(errSubsequent, &targetCooldownErr) {
		t.Fatalf("expected subsequent pickNext error to be *modelCooldownError, got: %v", errSubsequent)
	}
}

func TestScheduler_ConcurrentCooldownPicks_DoNotBlockHealthyModel(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerProviderTestExecutor{provider: "gemini"})

	cooledModel := "model-cooling"
	healthyModel := "model-healthy"

	authCool := &Auth{ID: "auth-cool", Provider: "gemini"}
	authHealthy := &Auth{ID: "auth-healthy", Provider: "gemini"}

	if _, errRegisterCool := manager.Register(ctx, authCool); errRegisterCool != nil {
		t.Fatalf("register authCool: %v", errRegisterCool)
	}
	if _, errRegisterHealthy := manager.Register(ctx, authHealthy); errRegisterHealthy != nil {
		t.Fatalf("register authHealthy: %v", errRegisterHealthy)
	}

	registerSchedulerModels(t, "gemini", cooledModel, authCool.ID)
	registerSchedulerModels(t, "gemini", healthyModel, authHealthy.ID)
	manager.RefreshSchedulerEntry(authCool.ID)
	manager.RefreshSchedulerEntry(authHealthy.ID)

	manager.MarkResult(ctx, Result{
		AuthID:   authCool.ID,
		Provider: "gemini",
		Model:    cooledModel,
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusTooManyRequests, Message: "quota exceeded"},
	})

	manager.scheduler.mu.Lock()
	providerBefore := manager.scheduler.providers["gemini"]
	manager.scheduler.mu.Unlock()

	var wg sync.WaitGroup
	const concurrency = 30
	startGate := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startGate
			_, _, _ = manager.pickNext(ctx, "gemini", cooledModel, cliproxyexecutor.Options{}, nil)
		}()
	}

	healthyDone := make(chan struct{})
	go func() {
		<-startGate
		picked, exec, errPick := manager.pickNext(ctx, "gemini", healthyModel, cliproxyexecutor.Options{}, nil)
		if errPick != nil || picked == nil || exec == nil {
			t.Errorf("healthy pick failed: picked=%v, err=%v", picked, errPick)
		}
		close(healthyDone)
	}()

	workersDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(workersDone)
	}()

	close(startGate)

	select {
	case <-healthyDone:
	case <-time.After(5 * time.Second):
		t.Fatal("healthy model pick was blocked/starved by concurrent cooldown picks")
	}

	select {
	case <-workersDone:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent cooldown pick workers did not complete within timeout")
	}

	manager.scheduler.mu.Lock()
	providerAfter := manager.scheduler.providers["gemini"]
	manager.scheduler.mu.Unlock()

	if providerBefore != providerAfter {
		t.Fatalf("concurrent cooldown picks triggered scheduler rebuild(s)")
	}
}

func TestScheduler_InterleavedMarkResult_DoesNotTriggerRebuild(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerProviderTestExecutor{provider: "gemini"})

	modelActive := "model-active"
	modelCooldown := "model-in-cooldown"

	authActive := &Auth{ID: "auth-active", Provider: "gemini"}
	authCooled := &Auth{ID: "auth-cooled", Provider: "gemini"}

	if _, errRegisterActive := manager.Register(ctx, authActive); errRegisterActive != nil {
		t.Fatalf("register authActive: %v", errRegisterActive)
	}
	if _, errRegisterCooled := manager.Register(ctx, authCooled); errRegisterCooled != nil {
		t.Fatalf("register authCooled: %v", errRegisterCooled)
	}

	registerSchedulerModels(t, "gemini", modelActive, authActive.ID)
	registerSchedulerModels(t, "gemini", modelCooldown, authCooled.ID)
	manager.RefreshSchedulerEntry(authActive.ID)
	manager.RefreshSchedulerEntry(authCooled.ID)

	manager.MarkResult(ctx, Result{
		AuthID:   authCooled.ID,
		Provider: "gemini",
		Model:    modelCooldown,
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusTooManyRequests, Message: "quota exceeded"},
	})

	manager.scheduler.mu.Lock()
	providerBefore := manager.scheduler.providers["gemini"]
	manager.scheduler.mu.Unlock()

	var stopFlag atomic.Bool
	var wg sync.WaitGroup

	const resultWorkers = 5
	for i := 0; i < resultWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stopFlag.Load() {
				manager.MarkResult(ctx, Result{
					AuthID:   authActive.ID,
					Provider: "gemini",
					Model:    modelActive,
					Success:  true,
				})
			}
		}()
	}

	const pickWorkers = 20
	pickDone := make(chan struct{})
	go func() {
		var pickWg sync.WaitGroup
		for i := 0; i < pickWorkers; i++ {
			pickWg.Add(1)
			go func() {
				defer pickWg.Done()
				for j := 0; j < 10; j++ {
					_, _, errPick := manager.pickNext(ctx, "gemini", modelCooldown, cliproxyexecutor.Options{}, nil)
					if errPick == nil {
						t.Errorf("expected pickNext on cooldown model to fail")
					}
				}
			}()
		}
		pickWg.Wait()
		close(pickDone)
	}()

	select {
	case <-pickDone:
	case <-time.After(2 * time.Second):
		stopFlag.Store(true)
		t.Fatal("pick workers timed out while MarkResult was running")
	}

	stopFlag.Store(true)
	wg.Wait()

	manager.scheduler.mu.Lock()
	providerAfter := manager.scheduler.providers["gemini"]
	manager.scheduler.mu.Unlock()

	if providerBefore != providerAfter {
		t.Fatalf("interleaved MarkResult caused unexpected scheduler rebuild(s)")
	}
}

func TestScheduler_LargeCredentialSet_CooldownDoesNotLockStarve(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerProviderTestExecutor{provider: "antigravity"})

	const credentialCount = 40
	targetModel := "antigravity-cooldown-model"
	siblingModel := "antigravity-healthy-model"

	reg := registry.GetGlobalRegistry()
	for i := 0; i < credentialCount; i++ {
		authID := fmt.Sprintf("ag-auth-%d", i)
		auth := &Auth{
			ID:       authID,
			Provider: "antigravity",
		}
		if _, errRegister := manager.Register(ctx, auth); errRegister != nil {
			t.Fatalf("register auth %d: %v", i, errRegister)
		}

		reg.RegisterClient(authID, "antigravity", []*registry.ModelInfo{
			{ID: targetModel},
			{ID: siblingModel},
		})
		t.Cleanup(func(id string) func() {
			return func() { reg.UnregisterClient(id) }
		}(authID))
		manager.RefreshSchedulerEntry(authID)
	}

	initialPicked, errInitial := manager.scheduler.pickSingle(ctx, "antigravity", targetModel, cliproxyexecutor.Options{}, nil)
	if errInitial != nil || initialPicked == nil {
		t.Fatalf("initial targetModel pick failed: %v", errInitial)
	}

	for i := 0; i < credentialCount; i++ {
		authID := fmt.Sprintf("ag-auth-%d", i)
		manager.MarkResult(ctx, Result{
			AuthID:   authID,
			Provider: "antigravity",
			Model:    targetModel,
			Success:  false,
			Error:    &Error{HTTPStatus: http.StatusTooManyRequests, Message: "quota exceeded"},
		})
	}

	_, errTargetCooldown := manager.scheduler.pickSingle(ctx, "antigravity", targetModel, cliproxyexecutor.Options{}, nil)
	var cooldownErr *modelCooldownError
	if !errors.As(errTargetCooldown, &cooldownErr) {
		t.Fatalf("expected targetModel error to be *modelCooldownError, got: %v", errTargetCooldown)
	}

	manager.scheduler.mu.Lock()
	providerBefore := manager.scheduler.providers["antigravity"]
	manager.scheduler.mu.Unlock()

	var wg sync.WaitGroup
	const concurrentPicks = 30
	startGate := make(chan struct{})

	for i := 0; i < concurrentPicks; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startGate
			_, _, errPick := manager.pickNext(ctx, "antigravity", targetModel, cliproxyexecutor.Options{}, nil)
			if errPick == nil {
				t.Errorf("expected pickNext on targetModel to fail with cooldown error")
			}
			var cdErr *modelCooldownError
			if !errors.As(errPick, &cdErr) {
				t.Errorf("expected pickNext error to be *modelCooldownError, got: %v", errPick)
			}
		}()
	}

	healthyDone := make(chan struct{})
	go func() {
		<-startGate
		picked, _, errPick := manager.pickNext(ctx, "antigravity", siblingModel, cliproxyexecutor.Options{}, nil)
		if errPick != nil || picked == nil {
			t.Errorf("sibling model pick failed: picked=%v, err=%v", picked, errPick)
		}
		close(healthyDone)
	}()

	close(startGate)

	select {
	case <-healthyDone:
	case <-time.After(5 * time.Second):
		t.Fatal("healthy sibling model pick was starved by large credential set cooldown picks")
	}

	picksDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(picksDone)
	}()

	select {
	case <-picksDone:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent targetModel picks timed out")
	}

	manager.scheduler.mu.Lock()
	providerAfter := manager.scheduler.providers["antigravity"]
	manager.scheduler.mu.Unlock()

	if providerBefore != providerAfter {
		t.Fatalf("large credential set cooldown picks triggered full scheduler rebuild")
	}
}

func TestScheduler_RebuildDuringConcurrentMarkResult_PreservesLatestState(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerProviderTestExecutor{provider: "gemini"})

	model := "model-concurrent-mark-rebuild"
	auth := &Auth{
		ID:       "auth-concurrent-mark",
		Provider: "gemini",
	}
	if _, errRegister := manager.Register(ctx, auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	registerSchedulerModels(t, "gemini", model, auth.ID)
	manager.RefreshSchedulerEntry(auth.ID)

	manager.mu.RLock()
	healthySnapshot := manager.auths[auth.ID].Clone()
	manager.mu.RUnlock()

	manager.MarkResult(ctx, Result{
		AuthID:   auth.ID,
		Provider: "gemini",
		Model:    model,
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusTooManyRequests, Message: "quota exceeded"},
	})

	manager.scheduler.rebuild([]*Auth{healthySnapshot})

	_, errPickCooldown := manager.scheduler.pickSingle(ctx, "gemini", model, cliproxyexecutor.Options{}, nil)
	var targetCooldownErr *modelCooldownError
	if !errors.As(errPickCooldown, &targetCooldownErr) {
		t.Fatalf("rebuild with stale snapshot reverted cooldown state; expected *modelCooldownError, got: %v", errPickCooldown)
	}

	manager.mu.RLock()
	cooledSnapshot := manager.auths[auth.ID].Clone()
	manager.mu.RUnlock()

	if _, _, errReset := manager.ResetQuota(ctx, auth.ID); errReset != nil {
		t.Fatalf("reset quota: %v", errReset)
	}

	manager.scheduler.rebuild([]*Auth{cooledSnapshot})

	pickedRecovered, errPickRecovered := manager.scheduler.pickSingle(ctx, "gemini", model, cliproxyexecutor.Options{}, nil)
	if errPickRecovered != nil || pickedRecovered == nil || pickedRecovered.ID != auth.ID {
		t.Fatalf("rebuild with stale snapshot reverted recovered state: picked=%v, err=%v", pickedRecovered, errPickRecovered)
	}
}

func TestScheduler_ModelProjectionCooldown_DoesNotInvalidateFastPath(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerProviderTestExecutor{provider: "gemini"})

	model := "model-projection-fastpath"
	auth := &Auth{
		ID:       "auth-proj-fastpath",
		Provider: "gemini",
	}
	if _, errRegister := manager.Register(ctx, auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	registerSchedulerModels(t, "gemini", model, auth.ID)
	manager.RefreshSchedulerEntry(auth.ID)

	manager.syncScheduler()
	initialVersion := manager.currentVersion()
	if initialVersion != manager.syncedVersion.Load() {
		t.Fatalf("expected syncedVersion (%d) to equal currentVersion (%d)", manager.syncedVersion.Load(), initialVersion)
	}

	manager.MarkResult(ctx, Result{
		AuthID:   auth.ID,
		Provider: "gemini",
		Model:    model,
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusTooManyRequests, Message: "quota exceeded"},
	})

	if manager.currentVersion() != initialVersion {
		t.Fatalf("currentVersion changed from %d to %d on quota cooldown; cooldown must not invalidate structural registration epoch", initialVersion, manager.currentVersion())
	}

	_, errPick := manager.scheduler.pickSingle(ctx, "gemini", model, cliproxyexecutor.Options{}, nil)
	if manager.shouldRetrySchedulerPick(errPick, initialVersion) {
		t.Fatal("shouldRetrySchedulerPick unexpectedly returned true on cooldown error")
	}
}

func TestScheduler_RebuildDuringConcurrentUpdate_DoesNotResurrectDisabledCredential(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerProviderTestExecutor{provider: "gemini"})

	model := "model-disabled-rebuild"
	auth := &Auth{
		ID:       "auth-disabled-rebuild",
		Provider: "gemini",
		Status:   StatusActive,
	}
	if _, errRegister := manager.Register(ctx, auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	registerSchedulerModels(t, "gemini", model, auth.ID)
	manager.RefreshSchedulerEntry(auth.ID)

	manager.mu.RLock()
	activeSnapshotG1 := manager.auths[auth.ID].Clone()
	manager.mu.RUnlock()

	disabledAuth := activeSnapshotG1.Clone()
	disabledAuth.Disabled = true
	disabledAuth.Status = StatusDisabled
	if _, errUpdate := manager.Update(ctx, disabledAuth); errUpdate != nil {
		t.Fatalf("update auth to disabled: %v", errUpdate)
	}

	manager.scheduler.rebuild([]*Auth{activeSnapshotG1})

	picked, errPick := manager.scheduler.pickSingle(ctx, "gemini", model, cliproxyexecutor.Options{}, nil)
	if errPick == nil || picked != nil {
		t.Fatalf("rebuild with stale active snapshot resurrected disabled credential: %v", picked)
	}
}

func TestScheduler_IncrementalRefresh_AllowsFailedPickToRetryAndSucceed(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerProviderTestExecutor{provider: "gemini"})

	initialModel := "model-incremental-initial"
	newModel := "model-incremental-new"

	auth := &Auth{
		ID:       "auth-incremental-test",
		Provider: "gemini",
	}
	if _, errRegister := manager.Register(ctx, auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	registerSchedulerModels(t, "gemini", initialModel, auth.ID)
	manager.RefreshSchedulerEntry(auth.ID)
	manager.syncScheduler()

	beforeVer := manager.syncedVersion.Load()
	_, errInitialPick := manager.scheduler.pickSingle(ctx, "gemini", newModel, cliproxyexecutor.Options{}, nil)
	if errInitialPick == nil {
		t.Fatal("expected pickSingle on newModel to fail initially")
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, "gemini", []*registry.ModelInfo{
		{ID: initialModel},
		{ID: newModel},
	})
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})
	manager.RefreshSchedulerEntry(auth.ID)

	if !manager.shouldRetrySchedulerPick(errInitialPick, beforeVer) {
		t.Fatal("shouldRetrySchedulerPick returned false after incremental RefreshSchedulerEntry; expected true to allow retry")
	}

	picked, errRetry := manager.scheduler.pickSingle(ctx, "gemini", newModel, cliproxyexecutor.Options{}, nil)
	if errRetry != nil || picked == nil || picked.ID != auth.ID {
		t.Fatalf("retry pickSingle failed: picked=%v, err=%v", picked, errRetry)
	}
}

func TestScheduler_ConcurrentPicks_OneSyncAllowsOthersToRetry(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerProviderTestExecutor{provider: "gemini"})

	modelInitial := "model-concurrent-init"
	modelNew := "model-concurrent-new"

	auth := &Auth{
		ID:       "auth-concurrent-retry",
		Provider: "gemini",
	}
	if _, errRegister := manager.Register(ctx, auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	registerSchedulerModels(t, "gemini", modelInitial, auth.ID)
	manager.RefreshSchedulerEntry(auth.ID)
	manager.syncScheduler()

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, "gemini", []*registry.ModelInfo{
		{ID: modelInitial},
		{ID: modelNew},
	})
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})

	const numRequests = 10
	var wg sync.WaitGroup
	results := make([]error, numRequests)
	phase1Done := make(chan struct{})
	var phase1Count atomic.Int32
	phase2Start := make(chan struct{})

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()

			beforeVer := manager.syncedVersion.Load()
			selected, errPick := manager.scheduler.pickSingle(ctx, "gemini", modelNew, cliproxyexecutor.Options{}, nil)
			if errPick != nil && modelNew != "" {
				if phase1Count.Add(1) == int32(numRequests) {
					close(phase1Done)
				}
				<-phase2Start
				if manager.shouldRetrySchedulerPick(errPick, beforeVer) {
					selected, errPick = manager.scheduler.pickSingle(ctx, "gemini", modelNew, cliproxyexecutor.Options{}, nil)
				}
			}
			if errPick == nil && (selected == nil || selected.ID != auth.ID) {
				results[idx] = errors.New("picked nil or wrong auth")
			} else {
				results[idx] = errPick
			}
		}()
	}

	select {
	case <-phase1Done:
	case <-time.After(2 * time.Second):
		t.Fatal("phase 1 timed out waiting for all initial pick attempts to fail")
	}

	close(phase2Start)
	wg.Wait()

	for i, err := range results {
		if err != nil {
			t.Errorf("request %d failed to discover newly synced model: %v", i, err)
		}
	}
}

func TestScheduler_RegisterClient_InvalidatesFastPath(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerProviderTestExecutor{provider: "gemini"})

	initialModel := "model-init"
	dynamicModel := "model-dynamic"

	auth := &Auth{
		ID:       "auth-dynamic-reg",
		Provider: "gemini",
	}
	if _, errRegister := manager.Register(ctx, auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	registerSchedulerModels(t, "gemini", initialModel, auth.ID)
	manager.RefreshSchedulerEntry(auth.ID)

	manager.syncScheduler()
	if manager.currentVersion() != manager.syncedVersion.Load() {
		t.Fatalf("expected syncedVersion (%d) to equal currentVersion (%d)", manager.syncedVersion.Load(), manager.currentVersion())
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, "gemini", []*registry.ModelInfo{
		{ID: initialModel},
		{ID: dynamicModel},
	})
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})

	if manager.currentVersion() <= manager.syncedVersion.Load() {
		t.Fatalf("currentVersion (%d) did not increase after RegisterClient (syncedVersion=%d)", manager.currentVersion(), manager.syncedVersion.Load())
	}

	picked, exec, errPick := manager.pickNext(ctx, "gemini", dynamicModel, cliproxyexecutor.Options{}, nil)
	if errPick != nil {
		t.Fatalf("pickNext failed to discover dynamically registered model: %v", errPick)
	}
	if picked == nil || picked.ID != auth.ID || exec == nil {
		t.Fatalf("pickNext returned unexpected auth=%v, exec=%v", picked, exec)
	}
}

func TestScheduler_UnschedulableAuth_DoesNotTriggerRebuildLoop(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerProviderTestExecutor{provider: "gemini"})

	model := "model-unschedulable-test"

	validAuth := &Auth{
		ID:       "auth-valid-cooling",
		Provider: "gemini",
	}
	if _, errRegister := manager.Register(ctx, validAuth); errRegister != nil {
		t.Fatalf("register valid auth: %v", errRegister)
	}
	registerSchedulerModels(t, "gemini", model, validAuth.ID)
	manager.RefreshSchedulerEntry(validAuth.ID)

	unschedulableAuth := &Auth{
		ID:       "auth-empty-provider",
		Provider: "",
	}
	if _, errRegister := manager.Register(ctx, unschedulableAuth); errRegister != nil {
		t.Fatalf("register unschedulable auth: %v", errRegister)
	}

	manager.MarkResult(ctx, Result{
		AuthID:   validAuth.ID,
		Provider: "gemini",
		Model:    model,
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusTooManyRequests, Message: "quota exceeded"},
	})

	_, _, errPickInitial := manager.pickNext(ctx, "gemini", model, cliproxyexecutor.Options{}, nil)
	if errPickInitial == nil {
		t.Fatal("expected pickNext to fail with cooldown error")
	}

	if manager.currentVersion() != manager.syncedVersion.Load() {
		t.Fatalf("syncedVersion (%d) did not converge to currentVersion (%d) in presence of unschedulable auth",
			manager.syncedVersion.Load(), manager.currentVersion())
	}

	manager.scheduler.mu.Lock()
	providersBefore := manager.scheduler.providers["gemini"]
	manager.scheduler.mu.Unlock()

	for i := 0; i < 5; i++ {
		_, _, _ = manager.pickNext(ctx, "gemini", model, cliproxyexecutor.Options{}, nil)
	}

	manager.scheduler.mu.Lock()
	providersAfter := manager.scheduler.providers["gemini"]
	manager.scheduler.mu.Unlock()

	if providersBefore != providersAfter {
		t.Fatalf("subsequent picks triggered scheduler rebuild(s) due to unschedulable auth mismatch")
	}
}
