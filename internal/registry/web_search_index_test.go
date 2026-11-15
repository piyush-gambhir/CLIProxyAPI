package registry

import (
	"fmt"
	"sync"
	"testing"
)

func referenceHasClientSupportingWebSearch(r *ModelRegistry, modelID, provider, excludeClientID string) bool {
	for clientID, infos := range r.clientModelInfos {
		if clientID == excludeClientID {
			continue
		}
		if provider != "" && r.clientProviders[clientID] != provider {
			continue
		}
		info := infos[modelID]
		if info != nil && info.SupportsWebSearch {
			return true
		}
	}
	return false
}

func assertWebSearchIndexMatchesScan(t *testing.T, r *ModelRegistry, context string) {
	t.Helper()
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	providers := make(map[string]struct{})
	modelIDs := make(map[string]struct{})
	for clientID, infos := range r.clientModelInfos {
		providers[r.clientProviders[clientID]] = struct{}{}
		for modelID := range infos {
			modelIDs[modelID] = struct{}{}
		}
	}

	for modelID := range modelIDs {
		for provider := range providers {
			for _, exclude := range []string{"", "__none__"} {
				want := referenceHasClientSupportingWebSearch(r, modelID, provider, exclude)
				got := r.hasClientSupportingWebSearchLocked(modelID, provider, exclude)
				if got != want {
					t.Fatalf("%s: model=%q provider=%q exclude=%q: index=%v scan=%v", context, modelID, provider, exclude, got, want)
				}
			}
		}

		for clientID := range r.clientModelInfos {
			want := referenceHasClientSupportingWebSearch(r, modelID, "", clientID)
			got := r.hasClientSupportingWebSearchLocked(modelID, "", clientID)
			if got != want {
				t.Fatalf("%s: model=%q exclude=%q: index=%v scan=%v", context, modelID, clientID, got, want)
			}
		}

		registration := r.models[modelID]
		if registration == nil || registration.Info == nil {
			continue
		}
		if want, got := referenceHasClientSupportingWebSearch(r, modelID, "", ""), registration.Info.SupportsWebSearch; got != want {
			t.Fatalf("%s: model=%q Info.SupportsWebSearch=%v scan=%v", context, modelID, got, want)
		}
		for provider, info := range registration.InfoByProvider {
			if info == nil {
				continue
			}
			if want, got := referenceHasClientSupportingWebSearch(r, modelID, provider, ""), info.SupportsWebSearch; got != want {
				t.Fatalf("%s: model=%q provider=%q InfoByProvider.SupportsWebSearch=%v scan=%v", context, modelID, provider, got, want)
			}
		}
	}
}

func newWebSearchIndexRegistry() *ModelRegistry {
	reg := newTestModelRegistry()
	reg.RegisterClient("a", "codex", []*ModelInfo{
		{ID: "solo", SupportsWebSearch: true},
		{ID: "shared", SupportsWebSearch: true},
	})
	reg.RegisterClient("b", "codex", []*ModelInfo{
		{ID: "shared", SupportsWebSearch: true},
	})
	reg.RegisterClient("c", "gemini", []*ModelInfo{
		{ID: "shared", SupportsWebSearch: true},
		{ID: "off"},
	})
	return reg
}

func TestWebSearchIndexMatchesScanAcrossMutations(t *testing.T) {
	reg := newWebSearchIndexRegistry()
	assertWebSearchIndexMatchesScan(t, reg, "initial")

	reg.RegisterClient("b", "codex", []*ModelInfo{{ID: "shared"}})
	assertWebSearchIndexMatchesScan(t, reg, "re-register drops web search")

	reg.RegisterClient("b", "codex", []*ModelInfo{{ID: "shared", SupportsWebSearch: true}})
	assertWebSearchIndexMatchesScan(t, reg, "re-register restores web search")

	reg.RegisterClient("d", "codex", []*ModelInfo{{ID: "solo", SupportsWebSearch: true}})
	assertWebSearchIndexMatchesScan(t, reg, "second supporter of solo")

	reg.ApplyClientModelCapabilities("d", reg.ClientRegistrationEpoch("d"), func(_ string, info *ModelInfo) {
		info.SupportsWebSearch = false
	})
	assertWebSearchIndexMatchesScan(t, reg, "capabilities clear web search")

	reg.ApplyClientModelCapabilities("a", reg.ClientRegistrationEpoch("a"), func(_ string, info *ModelInfo) {
		info.SupportsWebSearch = true
	})
	assertWebSearchIndexMatchesScan(t, reg, "capabilities set web search")

	reg.UnregisterClient("a")
	assertWebSearchIndexMatchesScan(t, reg, "unregister sole supporter")

	reg.UnregisterClient("d")
	reg.UnregisterClient("b")
	reg.UnregisterClient("c")
	assertWebSearchIndexMatchesScan(t, reg, "unregister all")
}

func TestWebSearchIndexDropsOrphanCountsOnPureAddition(t *testing.T) {

	reg := newTestModelRegistry()
	reg.mutex.Lock()
	reg.clientModelInfos["ghost"] = map[string]*ModelInfo{
		"orphaned": {ID: "orphaned", SupportsWebSearch: true},
	}
	reg.clientProviders["ghost"] = "codex"

	reg.incrementWebSearchCountLocked("orphaned", "codex")
	reg.mutex.Unlock()
	assertWebSearchIndexMatchesScan(t, reg, "orphan seeded")

	reg.RegisterClient("ghost", "codex", []*ModelInfo{{ID: "other"}})
	assertWebSearchIndexMatchesScan(t, reg, "orphan replaced by pure addition")
	if reg.hasClientSupportingWebSearchLocked("orphaned", "", "") {
		t.Fatalf("index kept the orphaned web-search contribution after re-registration")
	}
	if reg.hasClientSupportingWebSearchLocked("orphaned", "codex", "") {
		t.Fatalf("provider index kept the orphaned web-search contribution after re-registration")
	}
}

func assertPublishedWebSearchSupport(t *testing.T, reg *ModelRegistry, modelID, provider string, want bool) {
	t.Helper()
	for _, view := range []string{"", provider} {
		info := reg.GetModelInfo(modelID, view)
		if info == nil {
			t.Errorf("GetModelInfo(%q, %q) returned nil", modelID, view)
			continue
		}
		if info.SupportsWebSearch != want {
			t.Errorf("GetModelInfo(%q, %q).SupportsWebSearch = %v, want %v", modelID, view, info.SupportsWebSearch, want)
		}
	}
}

func TestWebSearchIndexEmptyClientExclusion(t *testing.T) {
	for _, clientID := range []string{"", "supporter"} {
		t.Run(fmt.Sprintf("client=%q", clientID), func(t *testing.T) {
			reg := newTestModelRegistry()
			reg.RegisterClient(clientID, "antigravity", []*ModelInfo{{ID: "m", SupportsWebSearch: true}})
			reg.RegisterClient("observer", "antigravity", []*ModelInfo{{ID: "m"}})

			reg.mutex.RLock()
			defer reg.mutex.RUnlock()
			for _, provider := range []string{"", "antigravity", "gemini"} {
				for _, exclude := range []string{"", "supporter", "observer", "missing"} {
					want := provider != "gemini" && clientID != exclude
					if got := referenceHasClientSupportingWebSearch(reg, "m", provider, exclude); got != want {
						t.Errorf("provider=%q exclude=%q: scan=%v, want %v", provider, exclude, got, want)
					}
					if got := reg.hasClientSupportingWebSearchLocked("m", provider, exclude); got != want {
						t.Errorf("provider=%q exclude=%q: index=%v, want %v", provider, exclude, got, want)
					}
				}
			}
		})
	}
}

func TestWebSearchIndexPublishedViewsAfterSupporterUnregister(t *testing.T) {
	for _, clientID := range []string{"", "supporter"} {
		t.Run(fmt.Sprintf("client=%q", clientID), func(t *testing.T) {
			reg := newTestModelRegistry()
			reg.RegisterClient(clientID, "antigravity", []*ModelInfo{{ID: "m", SupportsWebSearch: true}})
			reg.RegisterClient("observer", "antigravity", []*ModelInfo{{ID: "m"}})
			assertPublishedWebSearchSupport(t, reg, "m", "antigravity", true)

			reg.UnregisterClient(clientID)
			assertPublishedWebSearchSupport(t, reg, "m", "antigravity", false)
			assertWebSearchIndexMatchesScan(t, reg, "supporter unregistered")
		})
	}
}

func TestWebSearchIndexPreservesRealSupporterOnOrphanAddition(t *testing.T) {
	reg := newTestModelRegistry()
	reg.RegisterClient("real", "antigravity", []*ModelInfo{{ID: "m", SupportsWebSearch: true}})
	reg.RegisterClient("ghost", "antigravity", []*ModelInfo{{ID: "m", SupportsWebSearch: true}})

	reg.mutex.Lock()
	delete(reg.clientModels, "ghost")
	reg.mutex.Unlock()
	assertWebSearchIndexMatchesScan(t, reg, "orphan seeded beside real supporter")

	reg.RegisterClient("ghost", "antigravity", []*ModelInfo{{ID: "m"}})
	assertPublishedWebSearchSupport(t, reg, "m", "antigravity", true)
	assertWebSearchIndexMatchesScan(t, reg, "orphan replaced beside real supporter")
}

func TestWebSearchIndexRefreshesOrphanedModelsOnPureAddition(t *testing.T) {
	for _, realSupportsWebSearch := range []bool{false, true} {
		t.Run(fmt.Sprintf("real_supports_web_search=%v", realSupportsWebSearch), func(t *testing.T) {
			reg := newTestModelRegistry()
			reg.RegisterClient("real", "antigravity", []*ModelInfo{{ID: "m", SupportsWebSearch: realSupportsWebSearch}})
			reg.RegisterClient("ghost", "antigravity", []*ModelInfo{{ID: "m", SupportsWebSearch: true}})

			reg.mutex.Lock()
			delete(reg.clientModels, "ghost")
			reg.mutex.Unlock()
			assertPublishedWebSearchSupport(t, reg, "m", "antigravity", true)
			assertWebSearchIndexMatchesScan(t, reg, "orphan seeded before catalog replacement")

			reg.RegisterClient("ghost", "antigravity", []*ModelInfo{{ID: "n"}})
			assertPublishedWebSearchSupport(t, reg, "m", "antigravity", realSupportsWebSearch)
			assertPublishedWebSearchSupport(t, reg, "n", "antigravity", false)
			assertWebSearchIndexMatchesScan(t, reg, "orphan replaced with a different catalog")
		})
	}
}

func TestWebSearchIndexCleansOrphanOnUnregister(t *testing.T) {
	for _, realSupportsWebSearch := range []bool{false, true} {
		t.Run(fmt.Sprintf("real_supports_web_search=%v", realSupportsWebSearch), func(t *testing.T) {
			reg := newTestModelRegistry()
			reg.RegisterClient("real", "antigravity", []*ModelInfo{{ID: "m", SupportsWebSearch: realSupportsWebSearch}})
			reg.RegisterClient("ghost", "antigravity", []*ModelInfo{{ID: "m", SupportsWebSearch: true}})

			reg.mutex.Lock()
			delete(reg.clientModels, "ghost")
			reg.mutex.Unlock()

			reg.UnregisterClient("ghost")
			reg.mutex.RLock()
			if _, exists := reg.clientModelInfos["ghost"]; exists {
				t.Error("unregister left orphaned client model metadata")
			}
			reg.mutex.RUnlock()
			assertPublishedWebSearchSupport(t, reg, "m", "antigravity", realSupportsWebSearch)

			reg.UnregisterClient("ghost")
			reg.mutex.RLock()
			wantCount := 0
			if realSupportsWebSearch {
				wantCount = 1
			}
			if got := reg.webSearchClientCounts["m"]; got != wantCount {
				t.Errorf("global count after repeated orphan cleanup = %d, want %d", got, wantCount)
			}
			if got := reg.webSearchProviderCounts["m"]["antigravity"]; got != wantCount {
				t.Errorf("provider count after repeated orphan cleanup = %d, want %d", got, wantCount)
			}
			reg.mutex.RUnlock()
			assertWebSearchIndexMatchesScan(t, reg, "orphan unregistered twice")

			reg.RegisterClient("ghost", "antigravity", []*ModelInfo{{ID: "m"}})
			assertPublishedWebSearchSupport(t, reg, "m", "antigravity", realSupportsWebSearch)
			assertWebSearchIndexMatchesScan(t, reg, "orphan re-registered after cleanup")
		})
	}
}

func TestWebSearchIndexTracksSoleSupporterAfterUnregister(t *testing.T) {
	reg := newWebSearchIndexRegistry()
	if !reg.hasClientSupportingWebSearchLocked("solo", "", "") {
		t.Fatalf("expected solo to be supported before its only supporter left")
	}
	reg.UnregisterClient("a")
	if reg.hasClientSupportingWebSearchLocked("solo", "", "") {
		t.Fatalf("sole supporter left, but the aggregate still reports web search")
	}

	if reg.models["solo"] != nil && reg.models["solo"].Info != nil && reg.models["solo"].Info.SupportsWebSearch {
		t.Fatalf("published Info.SupportsWebSearch stayed true after the sole supporter left")
	}
}

func TestWebSearchIndexToleratesNilIndexMaps(t *testing.T) {

	reg := &ModelRegistry{mutex: &sync.RWMutex{}}
	if reg.hasClientSupportingWebSearchLocked("m", "", "") {
		t.Fatalf("registry without index must not report web-search support")
	}
	reg.incrementWebSearchCountLocked("m", "codex")
	if !reg.hasClientSupportingWebSearchLocked("m", "codex", "") {
		t.Fatalf("expected the increment to be visible after lazy initialization")
	}
	reg.decrementWebSearchCountLocked("m", "codex")
	if reg.hasClientSupportingWebSearchLocked("m", "codex", "") {
		t.Fatalf("expected the decrement to clear the count")
	}
}

func TestWebSearchIndexProviderChangeRetractsOldProvider(t *testing.T) {

	reg := newTestModelRegistry()
	reg.RegisterClient("x", "codex", []*ModelInfo{
		{ID: "m", SupportsWebSearch: true},
	})
	if !reg.hasClientSupportingWebSearchLocked("m", "codex", "") {
		t.Fatalf("expected codex to support web search before the provider change")
	}

	reg.RegisterClient("x", "gemini", []*ModelInfo{
		{ID: "m", SupportsWebSearch: true},
	})
	if reg.hasClientSupportingWebSearchLocked("m", "codex", "") {
		t.Fatalf("codex must no longer report web search after the client moved to gemini")
	}
	if !reg.hasClientSupportingWebSearchLocked("m", "gemini", "") {
		t.Fatalf("gemini must report web search after the provider change")
	}
	if got := reg.webSearchProviderCounts["m"]["codex"]; got != 0 {
		t.Fatalf("stale codex provider count = %d, want 0", got)
	}
	if got := reg.webSearchProviderCounts["m"]["gemini"]; got != 1 {
		t.Fatalf("gemini provider count = %d, want 1", got)
	}
	assertWebSearchIndexMatchesScan(t, reg, "provider change")
}

func BenchmarkRegisterClientWebSearchAggregate(b *testing.B) {
	const modelsPerClient = 20

	infos := make([]*ModelInfo, 0, modelsPerClient)
	for i := 0; i < modelsPerClient; i++ {

		infos = append(infos, &ModelInfo{
			ID:                fmt.Sprintf("model-%d", i),
			SupportsWebSearch: i == 0,
		})
	}

	for _, clients := range []int{250, 500, 1000} {
		b.Run(fmt.Sprintf("clients=%d", clients), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				reg := newTestModelRegistry()
				for c := 0; c < clients; c++ {
					reg.RegisterClient(fmt.Sprintf("client-%d", c), "codex", infos)
				}
			}
		})
	}
}
