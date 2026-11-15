package auth

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestManager_MarkResult_TargetedModelShardUpdate(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	reg := registry.GetGlobalRegistry()
	authID := "auth-targeted-test"
	provider := "custom-prov"
	models := []*registry.ModelInfo{
		{ID: "model-a"},
		{ID: "model-b"},
		{ID: "model-c"},
	}
	reg.RegisterClient(authID, provider, models)
	t.Cleanup(func() {
		reg.UnregisterClient(authID)
	})

	auth := &Auth{
		ID:       authID,
		Provider: provider,
		Status:   StatusActive,
	}
	if _, errRegister := manager.Register(WithSkipPersist(context.Background()), auth); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}

	for _, m := range []string{"model-a", "model-b", "model-c"} {
		picked, errPick := manager.scheduler.pickSingle(context.Background(), provider, m, cliproxyexecutor.Options{}, nil)
		if errPick != nil || picked == nil || picked.ID != authID {
			t.Fatalf("pickSingle(%s) = (%v, %v), want %s", m, picked, errPick, authID)
		}
	}

	manager.scheduler.mu.Lock()
	pState := manager.scheduler.providers[provider]
	if pState == nil {
		manager.scheduler.mu.Unlock()
		t.Fatalf("provider state for %s is nil", provider)
	}
	shardA := pState.modelShards["model-a"]
	shardB := pState.modelShards["model-b"]
	shardC := pState.modelShards["model-c"]
	if shardA == nil || shardB == nil || shardC == nil {
		manager.scheduler.mu.Unlock()
		t.Fatalf("expected shards for model-a, model-b, and model-c to exist")
	}
	entryBBefore := shardB.entries[authID].meta
	entryCBefore := shardC.entries[authID].meta
	manager.scheduler.mu.Unlock()

	manager.MarkResult(context.Background(), Result{
		AuthID:          authID,
		Provider:        provider,
		Model:           "model-a",
		Success:         false,
		CredentialScope: false,
		Error: &Error{
			Code:       "internal_server_error",
			Message:    "500 Internal Server Error",
			HTTPStatus: http.StatusInternalServerError,
		},
	})

	manager.scheduler.mu.Lock()
	defer manager.scheduler.mu.Unlock()

	entryA := shardA.entries[authID]
	if entryA == nil || (entryA.state != scheduledStateBlocked && entryA.state != scheduledStateCooldown) {
		t.Fatalf("model-a shard state = %v, want scheduledStateBlocked or scheduledStateCooldown", entryA.state)
	}

	entryBAfter := shardB.entries[authID].meta
	entryCAfter := shardC.entries[authID].meta
	if entryBAfter != entryBBefore {
		t.Fatalf("unrelated shard model-b was touched/updated by model-a MarkResult (meta pointer changed from %p to %p)", entryBBefore, entryBAfter)
	}
	if entryCAfter != entryCBefore {
		t.Fatalf("unrelated shard model-c was touched/updated by model-a MarkResult (meta pointer changed from %p to %p)", entryCBefore, entryCAfter)
	}

	if shardB.entries[authID].state != scheduledStateReady {
		t.Fatalf("model-b state = %v, want scheduledStateReady", shardB.entries[authID].state)
	}
	if shardC.entries[authID].state != scheduledStateReady {
		t.Fatalf("model-c state = %v, want scheduledStateReady", shardC.entries[authID].state)
	}
}

func TestManager_MarkResult_SuccessTargetedUpdate(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	reg := registry.GetGlobalRegistry()
	authID := "auth-success-targeted-test"
	provider := "custom-prov-succ"
	models := []*registry.ModelInfo{
		{ID: "model-1"},
		{ID: "model-2"},
	}
	reg.RegisterClient(authID, provider, models)
	t.Cleanup(func() {
		reg.UnregisterClient(authID)
	})

	auth := &Auth{
		ID:       authID,
		Provider: provider,
		Status:   StatusActive,
	}
	if _, errRegister := manager.Register(WithSkipPersist(context.Background()), auth); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}

	for _, m := range []string{"model-1", "model-2"} {
		picked, errPick := manager.scheduler.pickSingle(context.Background(), provider, m, cliproxyexecutor.Options{}, nil)
		if errPick != nil || picked == nil || picked.ID != authID {
			t.Fatalf("pickSingle(%s) error = %v", m, errPick)
		}
	}

	manager.scheduler.mu.Lock()
	pState := manager.scheduler.providers[provider]
	shard2 := pState.modelShards["model-2"]
	entry2Before := shard2.entries[authID].meta
	manager.scheduler.mu.Unlock()

	manager.MarkResult(context.Background(), Result{
		AuthID:   authID,
		Provider: provider,
		Model:    "model-1",
		Success:  true,
	})

	manager.scheduler.mu.Lock()
	defer manager.scheduler.mu.Unlock()

	entry2After := shard2.entries[authID].meta
	if entry2After != entry2Before {
		t.Fatalf("unrelated shard model-2 was touched/updated by model-1 success MarkResult (meta pointer changed from %p to %p)", entry2Before, entry2After)
	}
}

func TestManager_MarkResult_CredentialScopedUpdatesAllShards(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	reg := registry.GetGlobalRegistry()
	authID := "auth-cred-scope-test"
	provider := "custom-prov-cred"
	models := []*registry.ModelInfo{
		{ID: "model-x"},
		{ID: "model-y"},
	}
	reg.RegisterClient(authID, provider, models)
	t.Cleanup(func() {
		reg.UnregisterClient(authID)
	})

	auth := &Auth{
		ID:       authID,
		Provider: provider,
		Status:   StatusActive,
	}
	if _, errRegister := manager.Register(WithSkipPersist(context.Background()), auth); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}

	for _, m := range []string{"model-x", "model-y"} {
		picked, errPick := manager.scheduler.pickSingle(context.Background(), provider, m, cliproxyexecutor.Options{}, nil)
		if errPick != nil || picked == nil || picked.ID != authID {
			t.Fatalf("pickSingle(%s) error = %v", m, errPick)
		}
	}

	manager.MarkResult(context.Background(), Result{
		AuthID:          authID,
		Provider:        provider,
		Model:           "model-x",
		Success:         false,
		CredentialScope: true,
		Error: &Error{
			Code:       "quota_exceeded",
			Message:    "429 Quota Exceeded",
			HTTPStatus: http.StatusTooManyRequests,
		},
	})

	manager.scheduler.mu.Lock()
	defer manager.scheduler.mu.Unlock()

	pState := manager.scheduler.providers[provider]
	shardX := pState.modelShards["model-x"]
	shardY := pState.modelShards["model-y"]

	if shardX.entries[authID].state == scheduledStateReady {
		t.Fatalf("model-x shard should not be ready after credential-scoped error")
	}
	if shardY.entries[authID].state == scheduledStateReady {
		t.Fatalf("model-y shard should not be ready after credential-scoped error")
	}
}

func TestScheduler_MarkResult_OutOfOrderCrossModelUpdates(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	reg := registry.GetGlobalRegistry()
	authID := "auth-ooo-test"
	provider := "custom-prov-ooo"
	models := []*registry.ModelInfo{
		{ID: "model-p"},
		{ID: "model-q"},
	}
	reg.RegisterClient(authID, provider, models)
	t.Cleanup(func() {
		reg.UnregisterClient(authID)
	})

	auth := &Auth{
		ID:       authID,
		Provider: provider,
		Status:   StatusActive,
	}
	if _, errRegister := manager.Register(WithSkipPersist(context.Background()), auth); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}

	for _, m := range []string{"model-p", "model-q"} {
		picked, errPick := manager.scheduler.pickSingle(context.Background(), provider, m, cliproxyexecutor.Options{}, nil)
		if errPick != nil || picked == nil || picked.ID != authID {
			t.Fatalf("pickSingle(%s) error = %v", m, errPick)
		}
	}

	now := time.Now()

	authSnapshot1 := auth.Clone()
	authSnapshot1.Generation = 1
	authSnapshot1.UpdatedAt = now
	authSnapshot1.ModelStates = map[string]*ModelState{
		"model-p": {
			Unavailable:    true,
			Status:         StatusError,
			NextRetryAfter: now.Add(10 * time.Minute),
		},
	}

	authSnapshot2 := auth.Clone()
	authSnapshot2.Generation = 2
	authSnapshot2.UpdatedAt = now.Add(time.Millisecond)
	authSnapshot2.ModelStates = map[string]*ModelState{
		"model-p": {
			Unavailable:    true,
			Status:         StatusError,
			NextRetryAfter: now.Add(10 * time.Minute),
		},
		"model-q": {
			Unavailable:    true,
			Status:         StatusError,
			NextRetryAfter: now.Add(10 * time.Minute),
		},
	}

	manager.scheduler.upsertAuthResult(authSnapshot2, []string{"model-q"}, false)

	manager.scheduler.upsertAuthResult(authSnapshot1, []string{"model-p"}, false)

	manager.scheduler.mu.Lock()
	pState := manager.scheduler.providers[provider]
	shardP := pState.modelShards["model-p"]
	shardQ := pState.modelShards["model-q"]
	manager.scheduler.mu.Unlock()

	if shardQ.entries[authID].state == scheduledStateReady {
		t.Fatalf("model-q shard state should not be ready")
	}
	if shardP.entries[authID].state == scheduledStateReady {
		t.Fatalf("model-p shard state should not be ready even when older generation arrived second")
	}

	if _, errPickP := manager.scheduler.pickSingle(context.Background(), provider, "model-p", cliproxyexecutor.Options{}, nil); errPickP == nil {
		t.Fatalf("pickSingle(model-p) should fail due to cooldown")
	}
	if _, errPickQ := manager.scheduler.pickSingle(context.Background(), provider, "model-q", cliproxyexecutor.Options{}, nil); errPickQ == nil {
		t.Fatalf("pickSingle(model-q) should fail due to cooldown")
	}
}

func TestScheduler_MarkResult_EmptyModelShardUpdated(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	reg := registry.GetGlobalRegistry()
	authID := "auth-empty-shard-test"
	provider := "custom-prov-empty"
	models := []*registry.ModelInfo{
		{ID: "only-model"},
	}
	reg.RegisterClient(authID, provider, models)
	t.Cleanup(func() {
		reg.UnregisterClient(authID)
	})

	auth := &Auth{
		ID:       authID,
		Provider: provider,
		Status:   StatusActive,
	}
	if _, errRegister := manager.Register(WithSkipPersist(context.Background()), auth); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}

	pickedEmpty, errEmpty := manager.scheduler.pickSingle(context.Background(), provider, "", cliproxyexecutor.Options{}, nil)
	if errEmpty != nil || pickedEmpty == nil || pickedEmpty.ID != authID {
		t.Fatalf("pickSingle(\"\") error = %v", errEmpty)
	}
	pickedModel, errModel := manager.scheduler.pickSingle(context.Background(), provider, "only-model", cliproxyexecutor.Options{}, nil)
	if errModel != nil || pickedModel == nil || pickedModel.ID != authID {
		t.Fatalf("pickSingle(only-model) error = %v", errModel)
	}

	manager.MarkResult(context.Background(), Result{
		AuthID:          authID,
		Provider:        provider,
		Model:           "only-model",
		Success:         false,
		CredentialScope: false,
		Error: &Error{
			Code:       "internal_server_error",
			Message:    "500 Internal Server Error",
			HTTPStatus: http.StatusInternalServerError,
		},
	})

	manager.scheduler.mu.Lock()
	pState := manager.scheduler.providers[provider]
	emptyShard := pState.modelShards[""]
	manager.scheduler.mu.Unlock()

	if emptyShard == nil {
		t.Fatalf("empty model shard should exist")
	}
	if emptyShard.entries[authID].state == scheduledStateReady {
		t.Fatalf("empty model shard should NOT remain ready after all models became unavailable")
	}
}

func TestScheduler_MarkResult_CredentialScopedReusesModelSet(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	reg := registry.GetGlobalRegistry()
	authID := "auth-reuse-modelset-test"
	provider := "custom-prov-reuse"
	models := []*registry.ModelInfo{
		{ID: "m1"},
		{ID: "m2"},
	}
	reg.RegisterClient(authID, provider, models)
	t.Cleanup(func() {
		reg.UnregisterClient(authID)
	})

	auth := &Auth{
		ID:       authID,
		Provider: provider,
		Status:   StatusActive,
	}
	if _, errRegister := manager.Register(WithSkipPersist(context.Background()), auth); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}

	picked, errPick := manager.scheduler.pickSingle(context.Background(), provider, "m1", cliproxyexecutor.Options{}, nil)
	if errPick != nil || picked == nil {
		t.Fatalf("pickSingle error = %v", errPick)
	}

	manager.scheduler.mu.Lock()
	pState := manager.scheduler.providers[provider]
	initialMeta := pState.auths[authID]
	if initialMeta == nil || len(initialMeta.supportedModelSet) == 0 {
		manager.scheduler.mu.Unlock()
		t.Fatalf("initial meta should have supportedModelSet populated")
	}

	const sentinelKey = "reused_instance_marker"
	initialMeta.supportedModelSet[sentinelKey] = struct{}{}
	manager.scheduler.mu.Unlock()

	manager.MarkResult(context.Background(), Result{
		AuthID:          authID,
		Provider:        provider,
		Model:           "m1",
		Success:         false,
		CredentialScope: true,
		Error: &Error{
			Code:       "quota_exceeded",
			Message:    "429 Quota",
			HTTPStatus: http.StatusTooManyRequests,
		},
	})

	manager.scheduler.mu.Lock()
	afterMeta := pState.auths[authID]
	manager.scheduler.mu.Unlock()

	if afterMeta == nil {
		t.Fatalf("afterMeta should not be nil")
	}

	if _, ok := afterMeta.supportedModelSet[sentinelKey]; !ok {
		t.Fatalf("sentinel %s missing from afterMeta: supportedModelSet was re-queried from registry instead of reused", sentinelKey)
	}
}

func TestScheduler_StaleDisabledSnapshotDoesNotRemoveActiveAuth(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	reg := registry.GetGlobalRegistry()
	authID := "auth-stale-disabled-test"
	provider := "custom-prov-stale-disabled"
	models := []*registry.ModelInfo{{ID: "m-stale"}}
	reg.RegisterClient(authID, provider, models)
	t.Cleanup(func() {
		reg.UnregisterClient(authID)
	})

	now := time.Now()

	activeAuth := &Auth{
		ID:                authID,
		Provider:          provider,
		Status:            StatusActive,
		RegistrationEpoch: 1,
		Generation:        10,
		UpdatedAt:         now,
	}
	if _, errRegister := manager.Register(WithSkipPersist(context.Background()), activeAuth); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}

	picked, errPick := manager.scheduler.pickSingle(context.Background(), provider, "m-stale", cliproxyexecutor.Options{}, nil)
	if errPick != nil || picked == nil || picked.ID != authID {
		t.Fatalf("pickSingle failed: %v", errPick)
	}

	staleDisabledAuth := &Auth{
		ID:                authID,
		Provider:          provider,
		Disabled:          true,
		Status:            StatusDisabled,
		RegistrationEpoch: 1,
		Generation:        5,
		UpdatedAt:         now.Add(-time.Minute),
	}

	manager.scheduler.upsertAuthResult(staleDisabledAuth, []string{"m-stale"}, false)

	manager.scheduler.mu.Lock()
	pState := manager.scheduler.providers[provider]
	hasAuth := false
	if pState != nil && pState.auths[authID] != nil {
		hasAuth = true
	}
	manager.scheduler.mu.Unlock()

	if !hasAuth {
		t.Fatalf("stale disabled snapshot incorrectly removed active auth from scheduler")
	}

	pickedAfter, errPickAfter := manager.scheduler.pickSingle(context.Background(), provider, "m-stale", cliproxyexecutor.Options{}, nil)
	if errPickAfter != nil || pickedAfter == nil || pickedAfter.ID != authID {
		t.Fatalf("pickSingle after stale disabled snapshot failed: %v", errPickAfter)
	}
}

func TestScheduler_ModelRegistryEpochInvalidatesCache(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	reg := registry.GetGlobalRegistry()
	authID := "auth-reg-epoch-test"
	provider := "custom-prov-reg-epoch"
	initialModels := []*registry.ModelInfo{{ID: "model-1"}}
	reg.RegisterClient(authID, provider, initialModels)
	t.Cleanup(func() {
		reg.UnregisterClient(authID)
	})

	auth := &Auth{
		ID:       authID,
		Provider: provider,
		Status:   StatusActive,
	}
	if _, errRegister := manager.Register(WithSkipPersist(context.Background()), auth); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}

	if _, errPick := manager.scheduler.pickSingle(context.Background(), provider, "model-1", cliproxyexecutor.Options{}, nil); errPick != nil {
		t.Fatalf("pickSingle(model-1) error = %v", errPick)
	}

	manager.scheduler.mu.Lock()
	initialMeta := manager.scheduler.providers[provider].auths[authID]
	manager.scheduler.mu.Unlock()
	if initialMeta.supportsModel("model-2") {
		t.Fatalf("model-2 should not be supported initially")
	}

	reg.RegisterClient(authID, provider, []*registry.ModelInfo{
		{ID: "model-1"},
		{ID: "model-2"},
	})

	manager.MarkResult(context.Background(), Result{
		AuthID:   authID,
		Provider: provider,
		Model:    "model-1",
		Success:  true,
	})

	manager.scheduler.mu.Lock()
	updatedMeta := manager.scheduler.providers[provider].auths[authID]
	manager.scheduler.mu.Unlock()

	if !updatedMeta.supportsModel("model-2") {
		t.Fatalf("model-2 should be supported after registry epoch changed and result was processed")
	}

	if picked, errPick2 := manager.scheduler.pickSingle(context.Background(), provider, "model-2", cliproxyexecutor.Options{}, nil); errPick2 != nil || picked == nil {
		t.Fatalf("pickSingle(model-2) failed after epoch invalidation: %v", errPick2)
	}
}

func TestScheduler_MarkResult_OutOfOrderCredentialScopedFailureUpdatesAllShards(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	reg := registry.GetGlobalRegistry()
	authID := "auth-ooo-cred-test"
	provider := "custom-prov-ooo-cred"
	models := []*registry.ModelInfo{
		{ID: "m-ooo-1"},
		{ID: "m-ooo-2"},
	}
	reg.RegisterClient(authID, provider, models)
	t.Cleanup(func() {
		reg.UnregisterClient(authID)
	})

	auth := &Auth{
		ID:       authID,
		Provider: provider,
		Status:   StatusActive,
	}
	if _, errRegister := manager.Register(WithSkipPersist(context.Background()), auth); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}

	for _, m := range []string{"m-ooo-1", "m-ooo-2"} {
		picked, errPick := manager.scheduler.pickSingle(context.Background(), provider, m, cliproxyexecutor.Options{}, nil)
		if errPick != nil || picked == nil || picked.ID != authID {
			t.Fatalf("pickSingle(%s) error = %v", m, errPick)
		}
	}

	now := time.Now()

	snap1 := auth.Clone()
	snap1.Generation = 1
	snap1.UpdatedAt = now
	snap1.Unavailable = true
	snap1.Quota.Exceeded = true
	snap1.Quota.Reason = "credential_quota"
	snap1.Quota.NextRecoverAt = now.Add(time.Hour)

	snap2 := snap1.Clone()
	snap2.Generation = 2
	snap2.UpdatedAt = now.Add(time.Millisecond)

	manager.scheduler.upsertAuthResult(snap2, []string{"m-ooo-2"}, false)

	manager.scheduler.upsertAuthResult(snap1, nil, true)

	manager.scheduler.mu.Lock()
	pState := manager.scheduler.providers[provider]
	shard1 := pState.modelShards["m-ooo-1"]
	shard2 := pState.modelShards["m-ooo-2"]
	manager.scheduler.mu.Unlock()

	if shard1.entries[authID].state == scheduledStateReady {
		t.Fatalf("m-ooo-1 shard should not be ready after out-of-order credential-scoped failure")
	}
	if shard2.entries[authID].state == scheduledStateReady {
		t.Fatalf("m-ooo-2 shard should not be ready after out-of-order credential-scoped failure")
	}
}

func TestScheduler_ModelRegistryEpochChangeSyncsExistingShards(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	reg := registry.GetGlobalRegistry()
	provider := "custom-prov-epoch-sync"

	reg.RegisterClient("auth-1", provider, []*registry.ModelInfo{{ID: "model-1"}})

	reg.RegisterClient("auth-2", provider, []*registry.ModelInfo{{ID: "model-2"}})
	t.Cleanup(func() {
		reg.UnregisterClient("auth-1")
		reg.UnregisterClient("auth-2")
	})

	auth1 := &Auth{ID: "auth-1", Provider: provider, Status: StatusActive}
	auth2 := &Auth{ID: "auth-2", Provider: provider, Status: StatusActive}
	if _, err := manager.Register(WithSkipPersist(context.Background()), auth1); err != nil {
		t.Fatalf("Register(auth-1) error = %v", err)
	}
	if _, err := manager.Register(WithSkipPersist(context.Background()), auth2); err != nil {
		t.Fatalf("Register(auth-2) error = %v", err)
	}

	if _, err := manager.scheduler.pickSingle(context.Background(), provider, "model-1", cliproxyexecutor.Options{}, nil); err != nil {
		t.Fatalf("pickSingle(model-1) error = %v", err)
	}
	if _, err := manager.scheduler.pickSingle(context.Background(), provider, "model-2", cliproxyexecutor.Options{}, nil); err != nil {
		t.Fatalf("pickSingle(model-2) error = %v", err)
	}

	manager.scheduler.mu.Lock()
	shardModel2 := manager.scheduler.providers[provider].modelShards["model-2"]
	_, hasAuth1Before := shardModel2.entries["auth-1"]
	manager.scheduler.mu.Unlock()
	if hasAuth1Before {
		t.Fatalf("model-2 shard should not have auth-1 before registration change")
	}

	reg.RegisterClient("auth-1", provider, []*registry.ModelInfo{
		{ID: "model-1"},
		{ID: "model-2"},
	})

	manager.MarkResult(context.Background(), Result{
		AuthID:   "auth-1",
		Provider: provider,
		Model:    "model-1",
		Success:  true,
	})

	manager.scheduler.mu.Lock()
	entryAuth1InShard2 := shardModel2.entries["auth-1"]
	manager.scheduler.mu.Unlock()

	if entryAuth1InShard2 == nil {
		t.Fatalf("existing model-2 shard was not synced when registry epoch changed on auth-1")
	}
	if entryAuth1InShard2.state != scheduledStateReady {
		t.Fatalf("auth-1 state in model-2 shard = %v, want scheduledStateReady", entryAuth1InShard2.state)
	}
}

func TestScheduler_CredentialLevelRecoveryViaModelSuccessUpdatesAllShards(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	reg := registry.GetGlobalRegistry()
	authID := "auth-global-recovery-test"
	provider := "custom-prov-recovery"
	models := []*registry.ModelInfo{
		{ID: "m-rec-a"},
		{ID: "m-rec-b"},
	}
	reg.RegisterClient(authID, provider, models)
	t.Cleanup(func() {
		reg.UnregisterClient(authID)
	})

	auth := &Auth{
		ID:       authID,
		Provider: provider,
		Status:   StatusActive,
	}
	if _, err := manager.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	for _, m := range []string{"m-rec-a", "m-rec-b"} {
		if _, err := manager.scheduler.pickSingle(context.Background(), provider, m, cliproxyexecutor.Options{}, nil); err != nil {
			t.Fatalf("pickSingle(%s) error = %v", m, err)
		}
	}

	manager.MarkResult(context.Background(), Result{
		AuthID:          authID,
		Provider:        provider,
		Model:           "",
		Success:         false,
		CredentialScope: true,
		Error: &Error{
			Code:       "unauthorized",
			Message:    "401 Unauthorized",
			HTTPStatus: http.StatusUnauthorized,
		},
	})

	manager.scheduler.mu.Lock()
	pState := manager.scheduler.providers[provider]
	shardA := pState.modelShards["m-rec-a"]
	shardB := pState.modelShards["m-rec-b"]
	manager.scheduler.mu.Unlock()

	if shardA.entries[authID].state == scheduledStateReady {
		t.Fatalf("m-rec-a shard should not be ready after 401")
	}
	if shardB.entries[authID].state == scheduledStateReady {
		t.Fatalf("m-rec-b shard should not be ready after 401")
	}

	manager.MarkResult(context.Background(), Result{
		AuthID:   authID,
		Provider: provider,
		Model:    "m-rec-a",
		Success:  true,
	})

	manager.scheduler.mu.Lock()
	entryB := shardB.entries[authID]
	manager.scheduler.mu.Unlock()

	if entryB.state != scheduledStateReady {
		t.Fatalf("m-rec-b shard state = %v, want scheduledStateReady after credential-level recovery", entryB.state)
	}

	pickedB, errPickB := manager.scheduler.pickSingle(context.Background(), provider, "m-rec-b", cliproxyexecutor.Options{}, nil)
	if errPickB != nil || pickedB == nil || pickedB.ID != authID {
		t.Fatalf("pickSingle(m-rec-b) failed after recovery: %v, %v", pickedB, errPickB)
	}
}

func TestScheduler_CredentialLevelRecoveryViaModelSuccess_OutOfOrder(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	reg := registry.GetGlobalRegistry()
	authID := "auth-global-recovery-ooo-test"
	provider := "custom-prov-recovery-ooo"
	models := []*registry.ModelInfo{
		{ID: "m-ooo-a"},
		{ID: "m-ooo-b"},
	}
	reg.RegisterClient(authID, provider, models)
	t.Cleanup(func() {
		reg.UnregisterClient(authID)
	})

	auth := &Auth{
		ID:       authID,
		Provider: provider,
		Status:   StatusActive,
	}
	if _, err := manager.Register(WithSkipPersist(context.Background()), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	for _, m := range []string{"m-ooo-a", "m-ooo-b"} {
		if _, err := manager.scheduler.pickSingle(context.Background(), provider, m, cliproxyexecutor.Options{}, nil); err != nil {
			t.Fatalf("pickSingle(%s) error = %v", m, err)
		}
	}

	now := time.Now()

	snap1 := auth.Clone()
	snap1.Generation = 1
	snap1.Status = StatusError
	snap1.Unavailable = true
	snap1.UpdatedAt = now

	snap2 := auth.Clone()
	snap2.Generation = 2
	snap2.Status = StatusActive
	snap2.Unavailable = false
	snap2.UpdatedAt = now.Add(time.Millisecond)

	manager.scheduler.upsertAuthResult(snap2, []string{"m-ooo-a"}, false)

	manager.scheduler.upsertAuthResult(snap1, nil, true)

	manager.scheduler.mu.Lock()
	pState := manager.scheduler.providers[provider]
	shardA := pState.modelShards["m-ooo-a"]
	shardB := pState.modelShards["m-ooo-b"]
	manager.scheduler.mu.Unlock()

	if shardA.entries[authID].state != scheduledStateReady {
		t.Fatalf("m-ooo-a shard should remain ready")
	}
	if shardB.entries[authID].state != scheduledStateReady {
		t.Fatalf("m-ooo-b shard should remain ready")
	}
}

func BenchmarkScheduler_TargetedMarkResultHighConcurrency(b *testing.B) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	reg := registry.GetGlobalRegistry()
	provider := "bench-prov"

	const authCount = 10
	const modelCount = 50

	var models []*registry.ModelInfo
	for m := 0; m < modelCount; m++ {
		models = append(models, &registry.ModelInfo{ID: fmt.Sprintf("model-%d", m)})
	}

	for a := 0; a < authCount; a++ {
		aID := fmt.Sprintf("bench-auth-%d", a)
		reg.RegisterClient(aID, provider, models)
		defer reg.UnregisterClient(aID)

		auth := &Auth{ID: aID, Provider: provider, Status: StatusActive}
		if _, err := manager.Register(WithSkipPersist(context.Background()), auth); err != nil {
			b.Fatalf("Register error: %v", err)
		}
	}

	for m := 0; m < modelCount; m++ {
		modelName := fmt.Sprintf("model-%d", m)
		if _, err := manager.scheduler.pickSingle(context.Background(), provider, modelName, cliproxyexecutor.Options{}, nil); err != nil {
			b.Fatalf("pickSingle warmup error: %v", err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		idx := 0
		for pb.Next() {
			modelName := fmt.Sprintf("model-%d", idx%modelCount)
			authName := fmt.Sprintf("bench-auth-%d", idx%authCount)
			idx++

			_, _ = manager.scheduler.pickSingle(context.Background(), provider, modelName, cliproxyexecutor.Options{}, nil)
			manager.MarkResult(context.Background(), Result{
				AuthID:   authName,
				Provider: provider,
				Model:    modelName,
				Success:  true,
			})
		}
	})
}
