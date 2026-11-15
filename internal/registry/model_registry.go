


package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	misc "github.com/router-for-me/CLIProxyAPI/v8/internal/misc"
	log "github.com/sirupsen/logrus"
)


const OpenAIImageModelType = "openai-image"

const (
	DefaultClaudeMaxInputTokens  = 200000
	DefaultClaudeMaxOutputTokens = 64000
)


type NativeCapabilities struct {


	WebSearch *bool `json:"web_search,omitempty"`



	ToolSearch *bool `json:"tool_search,omitempty"`
}


type ModelInfo struct {

	ID string `json:"id"`


	MetadataModelID string `json:"-"`

	ExplicitThinking bool `json:"-"`

	ExplicitInputModalities bool `json:"-"`

	Object string `json:"object"`

	Created int64 `json:"created"`

	OwnedBy string `json:"owned_by"`

	Type string `json:"type"`

	DisplayName string `json:"display_name,omitempty"`

	Name string `json:"name,omitempty"`

	Version string `json:"version,omitempty"`

	Description string `json:"description,omitempty"`

	InputTokenLimit int `json:"inputTokenLimit,omitempty"`

	OutputTokenLimit int `json:"outputTokenLimit,omitempty"`

	SupportedGenerationMethods []string `json:"supportedGenerationMethods,omitempty"`

	ContextLength int `json:"context_length,omitempty"`


	MaxContextLength int `json:"-"`

	MaxCompletionTokens int `json:"max_completion_tokens,omitempty"`

	SupportedParameters []string `json:"supported_parameters,omitempty"`

	SupportedInputModalities []string `json:"supportedInputModalities,omitempty"`

	SupportedOutputModalities []string `json:"supportedOutputModalities,omitempty"`


	SupportsWebSearch bool `json:"supports_web_search,omitempty"`


	SupportConfigurationUpdate bool `json:"-"`



	NativeCapabilities *NativeCapabilities `json:"-"`



	Thinking *ThinkingSupport `json:"thinking,omitempty"`


	Config *ModelConfig `json:"config,omitempty"`




	UserDefined bool `json:"-"`



	IsCompat bool `json:"-"`
}


type ModelConfig struct {


	OverrideHeader map[string]string `json:"override_header,omitempty"`
}



func (m *ModelInfo) UnmarshalJSON(data []byte) error {
	type modelInfoAlias ModelInfo
	aux := struct {
		*modelInfoAlias
		NativeCapabilities         *NativeCapabilities `json:"native_capabilities"`
		SupportConfigurationUpdate bool                `json:"support_configuration_update"`
	}{modelInfoAlias: (*modelInfoAlias)(m)}
	if errUnmarshal := json.Unmarshal(data, &aux); errUnmarshal != nil {
		return errUnmarshal
	}
	m.NativeCapabilities = aux.NativeCapabilities
	m.SupportConfigurationUpdate = aux.SupportConfigurationUpdate
	return nil
}

type availableModelsCacheEntry struct {
	models    []map[string]any
	expiresAt time.Time
}



type ThinkingSupport struct {

	Min int `json:"min,omitempty" yaml:"min,omitempty"`

	Max int `json:"max,omitempty" yaml:"max,omitempty"`

	ZeroAllowed bool `json:"zero_allowed,omitempty" yaml:"zero-allowed,omitempty"`

	DynamicAllowed bool `json:"dynamic_allowed,omitempty" yaml:"dynamic-allowed,omitempty"`


	DisableType string `json:"disable_type,omitempty" yaml:"disable-type,omitempty"`


	Levels []string `json:"levels,omitempty" yaml:"levels,omitempty"`
}


type ModelRegistration struct {

	Info *ModelInfo

	InfoByProvider map[string]*ModelInfo

	Count int

	LastUpdated time.Time

	QuotaExceededClients map[string]*time.Time

	Providers map[string]int

	SuspendedClients map[string]string


	SuspendedClientDeadlines map[string]time.Time
}



type ModelRegistryHook interface {
	OnModelsRegistered(ctx context.Context, provider, clientID string, models []*ModelInfo)
	OnModelsUnregistered(ctx context.Context, provider, clientID string)
}


type ModelRegistry struct {

	models map[string]*ModelRegistration

	clientModels map[string][]string


	clientModelInfos map[string]map[string]*ModelInfo

	clientProviders map[string]string

	clientEpochs map[string]uint64

	clientGenerations map[string]uint64

	mutex *sync.RWMutex

	availableModelsCache map[string]availableModelsCacheEntry




	webSearchClientCounts map[string]int


	webSearchProviderCounts map[string]map[string]int

	generation uint64

	registrationEpoch atomic.Uint64

	hook ModelRegistryHook

	nowFunc func() time.Time
}


var globalRegistry *ModelRegistry
var registryOnce sync.Once


func GetGlobalRegistry() *ModelRegistry {
	registryOnce.Do(func() {
		globalRegistry = &ModelRegistry{
			models:                make(map[string]*ModelRegistration),
			clientModels:          make(map[string][]string),
			clientModelInfos:      make(map[string]map[string]*ModelInfo),
			clientProviders:       make(map[string]string),
			clientEpochs:          make(map[string]uint64),
			clientGenerations:     make(map[string]uint64),
			availableModelsCache:  make(map[string]availableModelsCacheEntry),
			webSearchClientCounts: map[string]int{},
			mutex:                 &sync.RWMutex{},
		}
	})
	return globalRegistry
}
func (r *ModelRegistry) now() time.Time {
	if r != nil && r.nowFunc != nil {
		return r.nowFunc()
	}
	return time.Now()
}

func (r *ModelRegistry) ensureAvailableModelsCacheLocked() {
	if r.availableModelsCache == nil {
		r.availableModelsCache = make(map[string]availableModelsCacheEntry)
	}
}

func (r *ModelRegistry) invalidateAvailableModelsCacheLocked() {
	r.generation++
	if len(r.availableModelsCache) == 0 {
		return
	}
	clear(r.availableModelsCache)
}


func (r *ModelRegistry) GetGeneration() uint64 {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.generation
}



func (r *ModelRegistry) RegistrationEpoch() uint64 {
	if r == nil {
		return 0
	}
	return r.registrationEpoch.Load()
}


func LookupModelInfo(modelID string, provider ...string) *ModelInfo {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil
	}

	p := ""
	if len(provider) > 0 {
		p = strings.ToLower(strings.TrimSpace(provider[0]))
	}

	if info := GetGlobalRegistry().GetModelInfo(modelID, p); info != nil {
		return cloneModelInfo(info)
	}
	return cloneModelInfo(LookupStaticModelInfo(modelID))
}


type NativeCapabilityRoute struct {
	Provider           string
	NativeCapabilities *NativeCapabilities
}




func ResolveResponsesWebSearchCapability(routes []NativeCapabilityRoute) *bool {
	if len(routes) == 0 {
		return nil
	}

	hasUnknown := false
	for _, route := range routes {
		if route.NativeCapabilities != nil && route.NativeCapabilities.WebSearch != nil && !*route.NativeCapabilities.WebSearch {
			return boolPointer(false)
		}
		pathSupport := responsesWebSearchProviderPathSupport(route.Provider)
		if pathSupport == nil {
			hasUnknown = true
			continue
		}
		if !*pathSupport {
			return boolPointer(false)
		}
		if route.NativeCapabilities == nil || route.NativeCapabilities.WebSearch == nil {
			hasUnknown = true
		}
	}
	if hasUnknown {
		return nil
	}
	return boolPointer(true)
}

func responsesWebSearchProviderPathSupport(provider string) *bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "codex", "xai", "claude", "antigravity":
		return boolPointer(true)
	case "openai", "openai-compatibility", "gemini", "aistudio", "vertex", "kimi", "kimi-ai", "kimi.ai", "kimi.com", "interactions", "gemini-interactions":
		return boolPointer(false)
	default:
		if strings.HasPrefix(provider, "openai-compatible-") {
			return boolPointer(false)
		}
		return nil
	}
}

func boolPointer(value bool) *bool {
	return &value
}



func (r *ModelRegistry) GetResponsesWebSearchCapability(modelID string) *bool {
	modelID = strings.TrimSpace(modelID)
	if r == nil || modelID == "" {
		return nil
	}

	r.mutex.RLock()
	defer r.mutex.RUnlock()

	routes := make([]NativeCapabilityRoute, 0)
	for clientID, modelIDs := range r.clientModels {
		for _, registeredID := range modelIDs {
			if strings.TrimSpace(registeredID) != modelID {
				continue
			}
			var nativeCapabilities *NativeCapabilities
			if info := r.clientModelInfos[clientID][registeredID]; info != nil {
				nativeCapabilities = info.NativeCapabilities
			}
			routes = append(routes, NativeCapabilityRoute{
				Provider:           r.clientProviders[clientID],
				NativeCapabilities: nativeCapabilities,
			})
		}
	}
	return ResolveResponsesWebSearchCapability(routes)
}



func ModelOverrideHeaders(modelID string, provider ...string) map[string]string {
	info := LookupModelInfo(modelID, provider...)
	if info == nil || info.Config == nil || len(info.Config.OverrideHeader) == 0 {
		return nil
	}
	out := make(map[string]string, len(info.Config.OverrideHeader))
	for key, value := range info.Config.OverrideHeader {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}


func (r *ModelRegistry) SetHook(hook ModelRegistryHook) {
	if r == nil {
		return
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.hook = hook
}

const defaultModelRegistryHookTimeout = 5 * time.Second
const modelQuotaExceededWindow = 5 * time.Minute

func (r *ModelRegistry) triggerModelsRegistered(provider, clientID string, models []*ModelInfo) {
	hook := r.hook
	if hook == nil {
		return
	}
	modelsCopy := cloneModelInfosUnique(models)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Errorf("model registry hook OnModelsRegistered panic: %v", recovered)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), defaultModelRegistryHookTimeout)
		defer cancel()
		hook.OnModelsRegistered(ctx, provider, clientID, modelsCopy)
	}()
}

func (r *ModelRegistry) triggerModelsUnregistered(provider, clientID string) {
	hook := r.hook
	if hook == nil {
		return
	}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Errorf("model registry hook OnModelsUnregistered panic: %v", recovered)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), defaultModelRegistryHookTimeout)
		defer cancel()
		hook.OnModelsUnregistered(ctx, provider, clientID)
	}()
}






func (r *ModelRegistry) RegisterClient(clientID, clientProvider string, models []*ModelInfo) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.registerClientLocked(clientID, clientProvider, models, false)
}






func (r *ModelRegistry) RegisterClientIfEpochCurrent(clientID, clientProvider string, expectedEpoch uint64, models []*ModelInfo, publish func() bool) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.clientEpochs[clientID] != expectedEpoch || publish == nil || !publish() {
		return false
	}
	r.registerClientLocked(clientID, clientProvider, models, false)
	return true
}






func (r *ModelRegistry) UnregisterClientIfEpochCurrent(clientID string, expectedEpoch uint64, publish func() bool) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.clientEpochs[clientID] != expectedEpoch || publish == nil || !publish() {
		return false
	}
	r.unregisterClientInternal(clientID)
	r.invalidateAvailableModelsCacheLocked()
	return true
}







func (r *ModelRegistry) ReplaceClientModels(clientID, clientProvider string, expectedEpoch uint64, models []*ModelInfo) (uint64, bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.clientEpochs[clientID] != expectedEpoch {
		return 0, false
	}
	r.registerClientLocked(clientID, clientProvider, models, true)
	return r.clientEpochs[clientID], true
}


func (r *ModelRegistry) registerClientLocked(clientID, clientProvider string, models []*ModelInfo, preserveState bool) {
	r.ensureAvailableModelsCacheLocked()

	if r.clientGenerations == nil {
		r.clientGenerations = make(map[string]uint64)
	}
	if r.clientEpochs == nil {
		r.clientEpochs = make(map[string]uint64)
	}

	provider := strings.ToLower(clientProvider)
	uniqueModelIDs := make([]string, 0, len(models))
	rawModelIDs := make([]string, 0, len(models))
	newModels := make(map[string]*ModelInfo, len(models))
	newCounts := make(map[string]int, len(models))
	for _, model := range models {
		if model == nil || model.ID == "" {
			continue
		}
		rawModelIDs = append(rawModelIDs, model.ID)
		newCounts[model.ID]++
		if _, exists := newModels[model.ID]; exists {
			continue
		}
		newModels[model.ID] = model
		uniqueModelIDs = append(uniqueModelIDs, model.ID)
	}

	if len(uniqueModelIDs) == 0 {


		r.unregisterClientInternal(clientID)
		delete(r.clientModels, clientID)
		delete(r.clientModelInfos, clientID)
		delete(r.clientProviders, clientID)
		r.invalidateAvailableModelsCacheLocked()
		misc.LogCredentialSeparator()
		return
	}

	oldModels, hadExisting := r.clientModels[clientID]
	oldProvider := r.clientProviders[clientID]
	providerChanged := oldProvider != provider



	r.clientEpochs[clientID]++
	if !preserveState || !hadExisting || providerChanged {
		r.clientGenerations[clientID] = uint64(0)
	}
	r.registrationEpoch.Add(1)

	now := r.now()
	if !hadExisting {


		r.clearOrphanedClientModelInfosLocked(clientID)
		for _, modelID := range rawModelIDs {
			model := newModels[modelID]
			r.addModelRegistration(modelID, provider, model, now, clientID)
		}
		r.clientModels[clientID] = append([]string(nil), rawModelIDs...)

		clientInfos := make(map[string]*ModelInfo, len(newModels))
		for id, m := range newModels {
			clientInfos[id] = cloneModelInfo(m)
		}
		r.clientModelInfos[clientID] = clientInfos
		if provider != "" {
			r.clientProviders[clientID] = provider
		} else {
			delete(r.clientProviders, clientID)
		}
		r.addClientWebSearchCountsLocked(clientID)
		r.invalidateAvailableModelsCacheLocked()
		r.triggerModelsRegistered(provider, clientID, models)
		log.Debugf("Registered client %s from provider %s with %d models", clientID, clientProvider, len(rawModelIDs))
		misc.LogCredentialSeparator()
		return
	}

	oldCounts := make(map[string]int, len(oldModels))
	for _, id := range oldModels {
		oldCounts[id]++
	}

	added := make([]string, 0)
	for _, id := range uniqueModelIDs {
		if oldCounts[id] == 0 {
			added = append(added, id)
		}
	}

	removed := make([]string, 0)
	for id := range oldCounts {
		if newCounts[id] == 0 {
			removed = append(removed, id)
		}
	}


	if providerChanged && oldProvider != "" {
		for id, newCount := range newCounts {
			if newCount == 0 {
				continue
			}
			oldCount := oldCounts[id]
			if oldCount == 0 {
				continue
			}
			toRemove := newCount
			if oldCount < toRemove {
				toRemove = oldCount
			}
			if reg, ok := r.models[id]; ok && reg.Providers != nil {
				if count, okProv := reg.Providers[oldProvider]; okProv {
					if count <= toRemove {
						delete(reg.Providers, oldProvider)
						if reg.InfoByProvider != nil {
							delete(reg.InfoByProvider, oldProvider)
						}
					} else {
						reg.Providers[oldProvider] = count - toRemove
						if reg.InfoByProvider != nil && reg.InfoByProvider[oldProvider] != nil {
							reg.InfoByProvider[oldProvider].SupportsWebSearch = r.hasClientSupportingWebSearchLocked(id, oldProvider, clientID)
						}
					}
				}
			}
		}
	}


	for _, id := range removed {
		oldCount := oldCounts[id]
		for i := 0; i < oldCount; i++ {
			r.removeModelRegistration(clientID, id, oldProvider, now, false)
		}
	}

	for id, oldCount := range oldCounts {
		newCount := newCounts[id]
		if newCount == 0 || oldCount <= newCount {
			continue
		}
		overage := oldCount - newCount
		for i := 0; i < overage; i++ {
			r.removeModelRegistration(clientID, id, oldProvider, now, preserveState && !providerChanged)
		}
	}


	for id, newCount := range newCounts {
		oldCount := oldCounts[id]
		if newCount <= oldCount {
			continue
		}
		model := newModels[id]
		diff := newCount - oldCount
		for i := 0; i < diff; i++ {
			r.addModelRegistration(id, provider, model, now, clientID)
		}
	}


	addedSet := make(map[string]struct{}, len(added))
	for _, id := range added {
		addedSet[id] = struct{}{}
	}
	for _, id := range uniqueModelIDs {
		model := newModels[id]
		if reg, ok := r.models[id]; ok {
			hasWebSearch := model.SupportsWebSearch || r.hasClientSupportingWebSearchLocked(id, "", clientID)
			reg.Info = cloneModelInfo(model)
			reg.Info.SupportsWebSearch = hasWebSearch
			if provider != "" {
				if reg.InfoByProvider == nil {
					reg.InfoByProvider = make(map[string]*ModelInfo)
				}
				hasProvWebSearch := model.SupportsWebSearch || r.hasClientSupportingWebSearchLocked(id, provider, clientID)
				reg.InfoByProvider[provider] = cloneModelInfo(model)
				reg.InfoByProvider[provider].SupportsWebSearch = hasProvWebSearch
			}
			if providerChanged && oldProvider != "" && reg.InfoByProvider != nil && reg.InfoByProvider[oldProvider] != nil {
				reg.InfoByProvider[oldProvider].SupportsWebSearch = r.hasClientSupportingWebSearchLocked(id, oldProvider, clientID)
			}
			reg.LastUpdated = now


			if !preserveState || providerChanged || oldCounts[id] == 0 {
				delete(reg.QuotaExceededClients, clientID)
				delete(reg.SuspendedClients, clientID)
				if reg.SuspendedClientDeadlines != nil {
					delete(reg.SuspendedClientDeadlines, clientID)
				}
			}
			if providerChanged && provider != "" {
				if _, newlyAdded := addedSet[id]; newlyAdded {
					continue
				}
				overlapCount := newCounts[id]
				if oldCount := oldCounts[id]; oldCount < overlapCount {
					overlapCount = oldCount
				}
				if overlapCount <= 0 {
					continue
				}
				if reg.Providers == nil {
					reg.Providers = make(map[string]int)
				}
				reg.Providers[provider] += overlapCount
			}
		}
	}




	r.dropClientWebSearchCountsLocked(clientID)
	if len(rawModelIDs) > 0 {
		r.clientModels[clientID] = append([]string(nil), rawModelIDs...)
	}

	clientInfos := make(map[string]*ModelInfo, len(newModels))
	for id, m := range newModels {
		clientInfos[id] = cloneModelInfo(m)
	}
	r.clientModelInfos[clientID] = clientInfos
	if provider != "" {
		r.clientProviders[clientID] = provider
	} else {
		delete(r.clientProviders, clientID)
	}
	r.addClientWebSearchCountsLocked(clientID)

	r.invalidateAvailableModelsCacheLocked()
	r.triggerModelsRegistered(provider, clientID, models)
	if len(added) == 0 && len(removed) == 0 && !providerChanged {

		return
	}

	log.Debugf("Reconciled client %s (provider %s) models: +%d, -%d", clientID, provider, len(added), len(removed))
	misc.LogCredentialSeparator()
}

func (r *ModelRegistry) addModelRegistration(modelID, provider string, model *ModelInfo, now time.Time, excludeClientID string) {
	if model == nil || modelID == "" {
		return
	}
	if existing, exists := r.models[modelID]; exists {
		existing.Count++
		existing.LastUpdated = now
		hasWebSearch := model.SupportsWebSearch || r.hasClientSupportingWebSearchLocked(modelID, "", excludeClientID)
		existing.Info = cloneModelInfo(model)
		existing.Info.SupportsWebSearch = hasWebSearch
		if existing.SuspendedClients == nil {
			existing.SuspendedClients = make(map[string]string)
		}
		if existing.SuspendedClientDeadlines == nil {
			existing.SuspendedClientDeadlines = make(map[string]time.Time)
		}
		if existing.InfoByProvider == nil {
			existing.InfoByProvider = make(map[string]*ModelInfo)
		}
		if provider != "" {
			if existing.Providers == nil {
				existing.Providers = make(map[string]int)
			}
			existing.Providers[provider]++
			hasProvWebSearch := model.SupportsWebSearch || r.hasClientSupportingWebSearchLocked(modelID, provider, excludeClientID)
			existing.InfoByProvider[provider] = cloneModelInfo(model)
			existing.InfoByProvider[provider].SupportsWebSearch = hasProvWebSearch
		}
		log.Debugf("Incremented count for model %s, now %d clients", modelID, existing.Count)
		return
	}

	registration := &ModelRegistration{
		Info:                     cloneModelInfo(model),
		InfoByProvider:           make(map[string]*ModelInfo),
		Count:                    1,
		LastUpdated:              now,
		QuotaExceededClients:     make(map[string]*time.Time),
		SuspendedClients:         make(map[string]string),
		SuspendedClientDeadlines: make(map[string]time.Time),
	}
	if provider != "" {
		registration.Providers = map[string]int{provider: 1}
		registration.InfoByProvider[provider] = cloneModelInfo(model)
	}
	r.models[modelID] = registration
	log.Debugf("Registered new model %s from provider %s", modelID, provider)
}


func (r *ModelRegistry) removeModelRegistration(clientID, modelID, provider string, now time.Time, preserveState bool) {
	registration, exists := r.models[modelID]
	if !exists {
		return
	}
	registration.Count--
	registration.LastUpdated = now
	if !preserveState {
		delete(registration.QuotaExceededClients, clientID)
		delete(registration.SuspendedClients, clientID)
		if registration.SuspendedClientDeadlines != nil {
			delete(registration.SuspendedClientDeadlines, clientID)
		}
	}
	if registration.Count < 0 {
		registration.Count = 0
	}
	if provider != "" && registration.Providers != nil {
		if count, ok := registration.Providers[provider]; ok {
			if count <= 1 {
				delete(registration.Providers, provider)
				if registration.InfoByProvider != nil {
					delete(registration.InfoByProvider, provider)
				}
			} else {
				registration.Providers[provider] = count - 1
			}
		}
	}
	log.Debugf("Decremented count for model %s, now %d clients", modelID, registration.Count)
	if registration.Count <= 0 {
		delete(r.models, modelID)
		log.Debugf("Removed model %s as no clients remain", modelID)
	} else {
		if registration.Info != nil {
			registration.Info.SupportsWebSearch = r.hasClientSupportingWebSearchLocked(modelID, "", clientID)
		}
		if provider != "" && registration.InfoByProvider != nil && registration.InfoByProvider[provider] != nil {
			registration.InfoByProvider[provider].SupportsWebSearch = r.hasClientSupportingWebSearchLocked(modelID, provider, clientID)
		}
	}
}

func cloneModelInfo(model *ModelInfo) *ModelInfo {
	if model == nil {
		return nil
	}
	copyModel := *model
	if model.NativeCapabilities != nil {
		copyCapabilities := *model.NativeCapabilities
		if model.NativeCapabilities.WebSearch != nil {
			webSearch := *model.NativeCapabilities.WebSearch
			copyCapabilities.WebSearch = &webSearch
		}
		copyModel.NativeCapabilities = &copyCapabilities
	}
	if len(model.SupportedGenerationMethods) > 0 {
		copyModel.SupportedGenerationMethods = append([]string(nil), model.SupportedGenerationMethods...)
	}
	if len(model.SupportedParameters) > 0 {
		copyModel.SupportedParameters = append([]string(nil), model.SupportedParameters...)
	}
	if len(model.SupportedInputModalities) > 0 {
		copyModel.SupportedInputModalities = append([]string(nil), model.SupportedInputModalities...)
	}
	if len(model.SupportedOutputModalities) > 0 {
		copyModel.SupportedOutputModalities = append([]string(nil), model.SupportedOutputModalities...)
	}
	if model.Thinking != nil {
		copyThinking := *model.Thinking
		if len(model.Thinking.Levels) > 0 {
			copyThinking.Levels = append([]string(nil), model.Thinking.Levels...)
		}
		copyModel.Thinking = &copyThinking
	}
	if model.Config != nil {
		copyConfig := *model.Config
		if len(model.Config.OverrideHeader) > 0 {
			copyConfig.OverrideHeader = make(map[string]string, len(model.Config.OverrideHeader))
			for key, value := range model.Config.OverrideHeader {
				copyConfig.OverrideHeader[key] = value
			}
		}
		copyModel.Config = &copyConfig
	}
	return &copyModel
}

func cloneModelInfosUnique(models []*ModelInfo) []*ModelInfo {
	if len(models) == 0 {
		return nil
	}
	cloned := make([]*ModelInfo, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		if model == nil || model.ID == "" {
			continue
		}
		if _, exists := seen[model.ID]; exists {
			continue
		}
		seen[model.ID] = struct{}{}
		cloned = append(cloned, cloneModelInfo(model))
	}
	return cloned
}




func (r *ModelRegistry) UnregisterClient(clientID string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.unregisterClientInternal(clientID)
	r.invalidateAvailableModelsCacheLocked()
}


func (r *ModelRegistry) unregisterClientInternal(clientID string) {
	if r.clientGenerations == nil {
		r.clientGenerations = make(map[string]uint64)
	}
	if r.clientEpochs == nil {
		r.clientEpochs = make(map[string]uint64)
	}
	r.clientEpochs[clientID]++
	r.clientGenerations[clientID]++
	r.registrationEpoch.Add(1)

	models, exists := r.clientModels[clientID]
	provider, hasProvider := r.clientProviders[clientID]
	if !exists {
		r.clearOrphanedClientModelInfosLocked(clientID)
		if hasProvider {
			delete(r.clientProviders, clientID)
		}
		return
	}

	now := r.now()
	for _, modelID := range models {
		if registration, isExists := r.models[modelID]; isExists {
			registration.Count--
			registration.LastUpdated = now


			delete(registration.QuotaExceededClients, clientID)
			if registration.SuspendedClients != nil {
				delete(registration.SuspendedClients, clientID)
			}
			if registration.SuspendedClientDeadlines != nil {
				delete(registration.SuspendedClientDeadlines, clientID)
			}

			if hasProvider && registration.Providers != nil {
				if count, ok := registration.Providers[provider]; ok {
					if count <= 1 {
						delete(registration.Providers, provider)
						if registration.InfoByProvider != nil {
							delete(registration.InfoByProvider, provider)
						}
					} else {
						registration.Providers[provider] = count - 1
					}
				}
			}

			log.Debugf("Decremented count for model %s, now %d clients", modelID, registration.Count)


			if registration.Count <= 0 {
				delete(r.models, modelID)
				log.Debugf("Removed model %s as no clients remain", modelID)
			} else {
				if registration.Info != nil {
					registration.Info.SupportsWebSearch = r.hasClientSupportingWebSearchLocked(modelID, "", clientID)
				}
				if hasProvider && registration.InfoByProvider != nil && registration.InfoByProvider[provider] != nil {
					registration.InfoByProvider[provider].SupportsWebSearch = r.hasClientSupportingWebSearchLocked(modelID, provider, clientID)
				}
			}
		}
	}




	r.dropClientWebSearchCountsLocked(clientID)

	delete(r.clientModels, clientID)
	delete(r.clientModelInfos, clientID)
	if hasProvider {
		delete(r.clientProviders, clientID)
	}
	log.Debugf("Unregistered client %s", clientID)

	misc.LogCredentialSeparator()
	r.triggerModelsUnregistered(provider, clientID)
}





func (r *ModelRegistry) SetModelQuotaExceeded(clientID, modelID string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.ensureAvailableModelsCacheLocked()

	if registration, exists := r.models[modelID]; exists {
		now := r.now()
		registration.QuotaExceededClients[clientID] = &now
		r.invalidateAvailableModelsCacheLocked()
		log.Debugf("Marked model %s as quota exceeded for client %s", modelID, clientID)
	}
}





func (r *ModelRegistry) ClearModelQuotaExceeded(clientID, modelID string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.ensureAvailableModelsCacheLocked()

	if registration, exists := r.models[modelID]; exists {
		delete(registration.QuotaExceededClients, clientID)
		r.invalidateAvailableModelsCacheLocked()

	}
}


type ClientModelProjection struct {
	ModelID       string
	Suspended     bool
	SuspendReason string


	SuspendUntil  time.Time
	QuotaExceeded bool
}




func (r *ModelRegistry) ApplyClientModelProjections(clientID string, epoch uint64, generation uint64, projections []ClientModelProjection) bool {
	if r == nil {
		return false
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return false
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.clientEpochs == nil {
		r.clientEpochs = make(map[string]uint64)
	}
	if r.clientGenerations == nil {
		r.clientGenerations = make(map[string]uint64)
	}

	clientRegisteredModels, exists := r.clientModels[clientID]
	if !exists || len(clientRegisteredModels) == 0 {
		return false
	}

	currentEpoch := r.clientEpochs[clientID]
	if epoch != currentEpoch {
		return false
	}

	currentGen := r.clientGenerations[clientID]
	if generation < currentGen {
		return false
	}

	registeredSet := make(map[string]struct{}, len(clientRegisteredModels))
	for _, m := range clientRegisteredModels {
		trimmedModel := strings.TrimSpace(m)
		if trimmedModel != "" {
			registeredSet[trimmedModel] = struct{}{}
		}
	}

	hasValidModel := false
	for _, proj := range projections {
		modelID := strings.TrimSpace(proj.ModelID)
		if modelID == "" {
			continue
		}
		if _, isOwned := registeredSet[modelID]; isOwned {
			if registration, existsModel := r.models[modelID]; existsModel && registration != nil {
				hasValidModel = true
				break
			}
		}
	}
	if !hasValidModel {
		return false
	}

	r.clientGenerations[clientID] = generation
	r.ensureAvailableModelsCacheLocked()

	now := r.now()
	changed := false
	for _, proj := range projections {
		modelID := strings.TrimSpace(proj.ModelID)
		if modelID == "" {
			continue
		}
		if _, isOwned := registeredSet[modelID]; !isOwned {
			continue
		}
		registration, existsModel := r.models[modelID]
		if !existsModel || registration == nil {
			continue
		}


		if proj.Suspended {
			if registration.SuspendedClients == nil {
				registration.SuspendedClients = make(map[string]string)
			}
			if registration.SuspendedClientDeadlines == nil {
				registration.SuspendedClientDeadlines = make(map[string]time.Time)
			}
			deadlineChanged := false
			if currentDeadline, ok := registration.SuspendedClientDeadlines[clientID]; ok {
				deadlineChanged = !proj.SuspendUntil.Equal(currentDeadline)
			} else {
				deadlineChanged = !proj.SuspendUntil.IsZero()
			}
			if currentReason, ok := registration.SuspendedClients[clientID]; !ok || currentReason != proj.SuspendReason || deadlineChanged {
				registration.SuspendedClients[clientID] = proj.SuspendReason
				if proj.SuspendUntil.IsZero() {
					delete(registration.SuspendedClientDeadlines, clientID)
				} else {
					registration.SuspendedClientDeadlines[clientID] = proj.SuspendUntil
				}
				registration.LastUpdated = now
				changed = true
			}
		} else {
			if registration.SuspendedClients != nil {
				if _, ok := registration.SuspendedClients[clientID]; ok {
					delete(registration.SuspendedClients, clientID)
					if registration.SuspendedClientDeadlines != nil {
						delete(registration.SuspendedClientDeadlines, clientID)
					}
					registration.LastUpdated = now
					changed = true
				}
			}
		}


		if proj.QuotaExceeded {
			if registration.QuotaExceededClients == nil {
				registration.QuotaExceededClients = make(map[string]*time.Time)
			}
			if _, ok := registration.QuotaExceededClients[clientID]; !ok {
				registration.QuotaExceededClients[clientID] = &now
				registration.LastUpdated = now
				changed = true
			}
		} else {
			if registration.QuotaExceededClients != nil {
				if _, ok := registration.QuotaExceededClients[clientID]; ok {
					delete(registration.QuotaExceededClients, clientID)
					registration.LastUpdated = now
					changed = true
				}
			}
		}
	}

	if changed {
		r.invalidateAvailableModelsCacheLocked()
	}
	return true
}




func (r *ModelRegistry) ApplyClientModelCapabilities(clientID string, expectedEpoch uint64, mutate func(modelID string, info *ModelInfo)) bool {
	if r == nil || mutate == nil {
		return false
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return false
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.clientEpochs == nil || r.clientEpochs[clientID] != expectedEpoch {
		return false
	}
	clientInfos, exists := r.clientModelInfos[clientID]
	if !exists || len(clientInfos) == 0 {
		return false
	}

	provider := r.clientProviders[clientID]



	r.dropClientWebSearchCountsLocked(clientID)
	for id, info := range clientInfos {
		if info != nil {
			mutate(id, info)


			if info.SupportsWebSearch {
				r.incrementWebSearchCountLocked(id, provider)
			}
			if reg, okReg := r.models[id]; okReg && reg != nil {
				hasWebSearch := r.hasClientSupportingWebSearchLocked(id, "", "")
				if reg.Info != nil {
					reg.Info.SupportsWebSearch = hasWebSearch
				}
				if provider != "" && reg.InfoByProvider != nil && reg.InfoByProvider[provider] != nil {
					reg.InfoByProvider[provider].SupportsWebSearch = r.hasClientSupportingWebSearchLocked(id, provider, "")
				}
			}
		}
	}
	r.invalidateAvailableModelsCacheLocked()
	return true
}



func (r *ModelRegistry) hasClientSupportingWebSearchLocked(modelID, provider, excludeClientID string) bool {
	if provider != "" {
		count := r.webSearchProviderCounts[modelID][provider]
		if count <= 0 {
			return false
		}
		if r.clientProviders[excludeClientID] == provider {
			if info := r.clientModelInfos[excludeClientID][modelID]; info != nil && info.SupportsWebSearch {
				count--
			}
		}
		return count > 0
	}

	count := r.webSearchClientCounts[modelID]
	if count <= 0 {
		return false
	}
	if info := r.clientModelInfos[excludeClientID][modelID]; info != nil && info.SupportsWebSearch {
		count--
	}
	return count > 0
}


func (r *ModelRegistry) incrementWebSearchCountLocked(modelID, provider string) {
	if modelID == "" {
		return
	}
	if r.webSearchClientCounts == nil {
		r.webSearchClientCounts = make(map[string]int)
	}
	r.webSearchClientCounts[modelID]++
	if provider == "" {
		return
	}
	if r.webSearchProviderCounts == nil {
		r.webSearchProviderCounts = make(map[string]map[string]int)
	}
	counts := r.webSearchProviderCounts[modelID]
	if counts == nil {
		counts = make(map[string]int)
		r.webSearchProviderCounts[modelID] = counts
	}
	counts[provider]++
}


func (r *ModelRegistry) decrementWebSearchCountLocked(modelID, provider string) {
	if modelID == "" {
		return
	}
	if count := r.webSearchClientCounts[modelID]; count > 1 {
		r.webSearchClientCounts[modelID] = count - 1
	} else {
		delete(r.webSearchClientCounts, modelID)
	}
	if provider == "" {
		return
	}
	counts := r.webSearchProviderCounts[modelID]
	if counts == nil {
		return
	}
	if count := counts[provider]; count > 1 {
		counts[provider] = count - 1
	} else {
		delete(counts, provider)
	}
	if len(counts) == 0 {
		delete(r.webSearchProviderCounts, modelID)
	}
}




func (r *ModelRegistry) clearOrphanedClientModelInfosLocked(clientID string) {
	provider, hasProvider := r.clientProviders[clientID]
	for modelID := range r.clientModelInfos[clientID] {
		if registration := r.models[modelID]; registration != nil {
			if registration.Info != nil {
				registration.Info.SupportsWebSearch = r.hasClientSupportingWebSearchLocked(modelID, "", clientID)
			}
			if hasProvider && registration.InfoByProvider[provider] != nil {
				registration.InfoByProvider[provider].SupportsWebSearch = r.hasClientSupportingWebSearchLocked(modelID, provider, clientID)
			}
		}
	}
	r.dropClientWebSearchCountsLocked(clientID)
	delete(r.clientModelInfos, clientID)
}




func (r *ModelRegistry) dropClientWebSearchCountsLocked(clientID string) {
	infos := r.clientModelInfos[clientID]
	if len(infos) == 0 {
		return
	}
	provider := r.clientProviders[clientID]
	for modelID, info := range infos {
		if info != nil && info.SupportsWebSearch {
			r.decrementWebSearchCountLocked(modelID, provider)
		}
	}
}




func (r *ModelRegistry) addClientWebSearchCountsLocked(clientID string) {
	infos := r.clientModelInfos[clientID]
	if len(infos) == 0 {
		return
	}
	provider := r.clientProviders[clientID]
	for modelID, info := range infos {
		if info != nil && info.SupportsWebSearch {
			r.incrementWebSearchCountLocked(modelID, provider)
		}
	}
}






func (r *ModelRegistry) SuspendClientModel(clientID, modelID, reason string) {
	if clientID == "" || modelID == "" {
		return
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.ensureAvailableModelsCacheLocked()

	registration, exists := r.models[modelID]
	if !exists || registration == nil {
		return
	}
	if registration.SuspendedClients == nil {
		registration.SuspendedClients = make(map[string]string)
	}
	now := r.now()
	_, _, active := clientSuspensionActive(registration, clientID, now)
	hadDeadline := false
	if registration.SuspendedClientDeadlines != nil {
		_, hadDeadline = registration.SuspendedClientDeadlines[clientID]
	}
	if active && !hadDeadline {
		return
	}
	registration.SuspendedClients[clientID] = reason
	if registration.SuspendedClientDeadlines != nil {
		delete(registration.SuspendedClientDeadlines, clientID)
	}
	registration.LastUpdated = now
	r.invalidateAvailableModelsCacheLocked()
	if reason != "" {
		log.Debugf("Suspended client %s for model %s: %s", clientID, modelID, reason)
	} else {
		log.Debugf("Suspended client %s for model %s", clientID, modelID)
	}
}





func (r *ModelRegistry) ResumeClientModel(clientID, modelID string) {
	if clientID == "" || modelID == "" {
		return
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.ensureAvailableModelsCacheLocked()

	registration, exists := r.models[modelID]
	if !exists || registration == nil || registration.SuspendedClients == nil {
		return
	}
	if _, ok := registration.SuspendedClients[clientID]; !ok {
		return
	}
	delete(registration.SuspendedClients, clientID)
	if registration.SuspendedClientDeadlines != nil {
		delete(registration.SuspendedClientDeadlines, clientID)
	}
	registration.LastUpdated = r.now()
	r.invalidateAvailableModelsCacheLocked()
	log.Debugf("Resumed client %s for model %s", clientID, modelID)
}


func (r *ModelRegistry) ClientSupportsModel(clientID, modelID string) bool {
	clientID = strings.TrimSpace(clientID)
	modelID = strings.TrimSpace(modelID)
	if clientID == "" || modelID == "" {
		return false
	}

	r.mutex.RLock()
	defer r.mutex.RUnlock()

	models, exists := r.clientModels[clientID]
	if !exists || len(models) == 0 {
		return false
	}

	for _, id := range models {
		if strings.EqualFold(strings.TrimSpace(id), modelID) {
			return true
		}
	}

	return false
}


func (r *ModelRegistry) IsModelSuspendedForClient(clientID, modelID string) bool {
	clientID = strings.TrimSpace(clientID)
	modelID = strings.TrimSpace(modelID)
	if clientID == "" || modelID == "" {
		return false
	}

	now := r.now()
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	registration, exists := r.models[modelID]
	if !exists || registration == nil {
		return false
	}
	_, _, active := clientSuspensionActive(registration, clientID, now)
	return active
}


func (r *ModelRegistry) IsModelQuotaExceededForClient(clientID, modelID string) bool {
	clientID = strings.TrimSpace(clientID)
	modelID = strings.TrimSpace(modelID)
	if clientID == "" || modelID == "" {
		return false
	}

	r.mutex.RLock()
	defer r.mutex.RUnlock()

	registration, exists := r.models[modelID]
	if !exists || registration == nil || registration.QuotaExceededClients == nil {
		return false
	}
	_, exceeded := registration.QuotaExceededClients[clientID]
	return exceeded
}







func (r *ModelRegistry) GetAvailableModels(handlerType string) []map[string]any {
	now := r.now()

	r.mutex.RLock()
	if cache, ok := r.availableModelsCache[handlerType]; ok && (cache.expiresAt.IsZero() || now.Before(cache.expiresAt)) {
		models := cloneModelMaps(cache.models)
		r.mutex.RUnlock()
		return models
	}
	r.mutex.RUnlock()

	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.ensureAvailableModelsCacheLocked()

	if cache, ok := r.availableModelsCache[handlerType]; ok && (cache.expiresAt.IsZero() || now.Before(cache.expiresAt)) {
		return cloneModelMaps(cache.models)
	}

	models, expiresAt := r.buildAvailableModelsLocked(handlerType, now)
	r.availableModelsCache[handlerType] = availableModelsCacheEntry{
		models:    cloneModelMaps(models),
		expiresAt: expiresAt,
	}

	return models
}







func isQuotaSuspension(reason string) bool {
	return strings.EqualFold(reason, "quota") || strings.EqualFold(reason, "credential_quota")
}

func clientSuspensionActive(registration *ModelRegistration, clientID string, now time.Time) (string, time.Time, bool) {
	if registration == nil || len(registration.SuspendedClients) == 0 {
		return "", time.Time{}, false
	}
	reason, ok := registration.SuspendedClients[clientID]
	if !ok {
		return "", time.Time{}, false
	}
	if deadline, hasDeadline := registration.SuspendedClientDeadlines[clientID]; hasDeadline && !deadline.IsZero() {
		if !now.Before(deadline) {
			return "", time.Time{}, false
		}
		return reason, deadline, true
	}
	return reason, time.Time{}, true
}

func clientQuotaRecoveryAt(registration *ModelRegistration, clientID string, quotaTime *time.Time) (time.Time, bool) {
	if registration == nil || quotaTime == nil {
		return time.Time{}, false
	}
	recoveryAt := quotaTime.Add(modelQuotaExceededWindow)
	if deadline, hasDeadline := registration.SuspendedClientDeadlines[clientID]; hasDeadline && !deadline.IsZero() && deadline.Before(recoveryAt) {
		recoveryAt = deadline
	}
	return recoveryAt, true
}

func modelRegistrationAvailability(registration *ModelRegistration, now time.Time) (bool, time.Time) {
	if registration == nil {
		return false, time.Time{}
	}

	availableClients := registration.Count
	expiredClients := 0
	var expiresAt time.Time
	for clientID, quotaTime := range registration.QuotaExceededClients {
		recoveryAt, ok := clientQuotaRecoveryAt(registration, clientID, quotaTime)
		if !ok {
			continue
		}
		if now.Before(recoveryAt) {
			expiredClients++
			if expiresAt.IsZero() || recoveryAt.Before(expiresAt) {
				expiresAt = recoveryAt
			}
		}
	}

	cooldownSuspended := 0
	otherSuspended := 0
	quotaAndSuspended := 0
	if registration.SuspendedClients != nil {
		for clientID := range registration.SuspendedClients {
			reason, deadline, active := clientSuspensionActive(registration, clientID, now)
			if !active {
				continue
			}
			if !deadline.IsZero() && (expiresAt.IsZero() || deadline.Before(expiresAt)) {
				expiresAt = deadline
			}
			if recoveryAt, ok := clientQuotaRecoveryAt(registration, clientID, registration.QuotaExceededClients[clientID]); ok && now.Before(recoveryAt) {
				quotaAndSuspended++
			}
			if isQuotaSuspension(reason) {
				cooldownSuspended++
				continue
			}
			otherSuspended++
		}
	}



	effectiveClients := availableClients - expiredClients - cooldownSuspended - otherSuspended + quotaAndSuspended
	if effectiveClients < 0 {
		effectiveClients = 0
	}

	available := effectiveClients > 0 || (availableClients > 0 && (expiredClients > 0 || cooldownSuspended > 0) && otherSuspended == 0)
	return available, expiresAt
}


func (r *ModelRegistry) GetAvailableModelInfos() []*ModelInfo {
	now := r.now()
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	result := make([]*ModelInfo, 0, len(r.models))
	for _, registration := range r.models {
		available, _ := modelRegistrationAvailability(registration, now)
		if !available || registration == nil || registration.Info == nil {
			continue
		}
		result = append(result, cloneModelInfo(registration.Info))
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.TrimSpace(result[i].ID) < strings.TrimSpace(result[j].ID)
	})
	return result
}

func (r *ModelRegistry) buildAvailableModelsLocked(handlerType string, now time.Time) ([]map[string]any, time.Time) {
	models := make([]map[string]any, 0, len(r.models))
	var expiresAt time.Time

	for _, registration := range r.models {
		available, registrationExpiresAt := modelRegistrationAvailability(registration, now)
		if !registrationExpiresAt.IsZero() && (expiresAt.IsZero() || registrationExpiresAt.Before(expiresAt)) {
			expiresAt = registrationExpiresAt
		}
		if !available || registration == nil {
			continue
		}

		model := r.convertModelToMap(registration.Info, handlerType)
		if model != nil {
			models = append(models, model)
		}
	}

	return models, expiresAt
}

func cloneModelMaps(models []map[string]any) []map[string]any {
	cloned := make([]map[string]any, 0, len(models))
	for _, model := range models {
		if model == nil {
			cloned = append(cloned, nil)
			continue
		}
		copyModel := make(map[string]any, len(model))
		for key, value := range model {
			copyModel[key] = cloneModelMapValue(value)
		}
		cloned = append(cloned, copyModel)
	}
	return cloned
}

func cloneModelMapValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		copyMap := make(map[string]any, len(typed))
		for key, entry := range typed {
			copyMap[key] = cloneModelMapValue(entry)
		}
		return copyMap
	case []any:
		copySlice := make([]any, len(typed))
		for i, entry := range typed {
			copySlice[i] = cloneModelMapValue(entry)
		}
		return copySlice
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}







func (r *ModelRegistry) GetAvailableModelsByProvider(provider string) []*ModelInfo {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return nil
	}

	r.mutex.RLock()
	defer r.mutex.RUnlock()

	type providerModel struct {
		count int
		info  *ModelInfo
	}

	providerModels := make(map[string]*providerModel)

	for clientID, clientProvider := range r.clientProviders {
		if clientProvider != provider {
			continue
		}
		modelIDs := r.clientModels[clientID]
		if len(modelIDs) == 0 {
			continue
		}
		clientInfos := r.clientModelInfos[clientID]
		for _, modelID := range modelIDs {
			modelID = strings.TrimSpace(modelID)
			if modelID == "" {
				continue
			}
			entry := providerModels[modelID]
			if entry == nil {
				entry = &providerModel{}
				providerModels[modelID] = entry
			}
			entry.count++
			if entry.info == nil {
				if clientInfos != nil {
					if info := clientInfos[modelID]; info != nil {
						entry.info = info
					}
				}
				if entry.info == nil {
					if reg, ok := r.models[modelID]; ok && reg != nil && reg.Info != nil {
						entry.info = reg.Info
					}
				}
			}
		}
	}

	if len(providerModels) == 0 {
		return nil
	}

	now := r.now()
	result := make([]*ModelInfo, 0, len(providerModels))

	for modelID, entry := range providerModels {
		if entry == nil || entry.count <= 0 {
			continue
		}
		registration, ok := r.models[modelID]

		expiredClients := 0
		cooldownSuspended := 0
		otherSuspended := 0
		quotaAndSuspended := 0
		if ok && registration != nil {
			if registration.QuotaExceededClients != nil {
				for clientID, quotaTime := range registration.QuotaExceededClients {
					if clientID == "" {
						continue
					}
					if p, okProvider := r.clientProviders[clientID]; !okProvider || p != provider {
						continue
					}
					if recoveryAt, okRecovery := clientQuotaRecoveryAt(registration, clientID, quotaTime); okRecovery && now.Before(recoveryAt) {
						expiredClients++
					}
				}
			}
			if registration.SuspendedClients != nil {
				for clientID := range registration.SuspendedClients {
					if clientID == "" {
						continue
					}
					if p, okProvider := r.clientProviders[clientID]; !okProvider || p != provider {
						continue
					}
					reason, _, active := clientSuspensionActive(registration, clientID, now)
					if !active {
						continue
					}
					if recoveryAt, okRecovery := clientQuotaRecoveryAt(registration, clientID, registration.QuotaExceededClients[clientID]); okRecovery && now.Before(recoveryAt) {
						quotaAndSuspended++
					}
					if isQuotaSuspension(reason) {
						cooldownSuspended++
						continue
					}
					otherSuspended++
				}
			}
		}

		availableClients := entry.count
		effectiveClients := availableClients - expiredClients - cooldownSuspended - otherSuspended + quotaAndSuspended
		if effectiveClients < 0 {
			effectiveClients = 0
		}

		if effectiveClients > 0 || (availableClients > 0 && (expiredClients > 0 || cooldownSuspended > 0) && otherSuspended == 0) {
			if entry.info != nil {
				result = append(result, cloneModelInfo(entry.info))
				continue
			}
			if ok && registration != nil && registration.Info != nil {
				result = append(result, cloneModelInfo(registration.Info))
			}
		}
	}

	return result
}







func (r *ModelRegistry) GetModelCount(modelID string) int {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	if registration, exists := r.models[modelID]; exists {
		now := r.now()


		expiredClients := 0
		for clientID, quotaTime := range registration.QuotaExceededClients {
			if recoveryAt, ok := clientQuotaRecoveryAt(registration, clientID, quotaTime); ok && now.Before(recoveryAt) {
				expiredClients++
			}
		}
		suspendedClients := 0
		for clientID := range registration.SuspendedClients {
			if _, _, active := clientSuspensionActive(registration, clientID, now); !active {
				continue
			}
			if recoveryAt, ok := clientQuotaRecoveryAt(registration, clientID, registration.QuotaExceededClients[clientID]); ok && now.Before(recoveryAt) {
				continue
			}
			suspendedClients++
		}
		result := registration.Count - expiredClients - suspendedClients
		if result < 0 {
			return 0
		}
		return result
	}
	return 0
}







func (r *ModelRegistry) GetModelProviders(modelID string) []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	registration, exists := r.models[modelID]
	if !exists || registration == nil || len(registration.Providers) == 0 {
		return nil
	}

	type providerCount struct {
		name  string
		count int
	}
	providers := make([]providerCount, 0, len(registration.Providers))








	for name, count := range registration.Providers {
		if count <= 0 {
			continue
		}





		providers = append(providers, providerCount{name: name, count: count})
	}
	if len(providers) == 0 {
		return nil
	}

	sort.Slice(providers, func(i, j int) bool {
		if providers[i].count == providers[j].count {
			return providers[i].name < providers[j].name
		}
		return providers[i].count > providers[j].count
	})

	result := make([]string, 0, len(providers))
	for _, item := range providers {
		result = append(result, item.name)
	}
	return result
}


func (r *ModelRegistry) GetModelInfo(modelID, provider string) *ModelInfo {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	if reg, ok := r.models[modelID]; ok && reg != nil {

		if provider != "" && reg.InfoByProvider != nil {
			if reg.Providers != nil {
				if count, ok := reg.Providers[provider]; ok && count > 0 {
					if info, ok := reg.InfoByProvider[provider]; ok && info != nil {
						return cloneModelInfo(info)
					}
				}
			}
		}

		return cloneModelInfo(reg.Info)
	}
	return nil
}


func (r *ModelRegistry) convertModelToMap(model *ModelInfo, handlerType string) map[string]any {
	if model == nil {
		return nil
	}

	switch handlerType {
	case "openai":
		result := map[string]any{
			"id":       model.ID,
			"object":   "model",
			"owned_by": model.OwnedBy,
		}
		if model.Created > 0 {
			result["created"] = model.Created
		}
		if model.Type != "" {
			result["type"] = model.Type
		}
		if model.DisplayName != "" {
			result["display_name"] = model.DisplayName
		}
		if model.Version != "" {
			result["version"] = model.Version
		}
		if model.Description != "" {
			result["description"] = model.Description
		}
		if model.ContextLength > 0 {
			result["context_length"] = model.ContextLength
		}
		if model.MaxContextLength > 0 {
			result["max_context_length"] = model.MaxContextLength
		}
		if model.MaxCompletionTokens > 0 {
			result["max_completion_tokens"] = model.MaxCompletionTokens
		}
		if len(model.SupportedParameters) > 0 {
			result["supported_parameters"] = append([]string(nil), model.SupportedParameters...)
		}
		return result

	case "claude":
		result := map[string]any{
			"id":       model.ID,
			"object":   "model",
			"owned_by": model.OwnedBy,
		}
		if model.Created > 0 {
			result["created_at"] = time.Unix(model.Created, 0).UTC().Format(time.RFC3339)
		}
		result["type"] = "model"
		if model.DisplayName != "" {
			result["display_name"] = model.DisplayName
		} else {
			result["display_name"] = model.ID
		}
		maxInput := model.ContextLength
		if maxInput <= 0 {
			maxInput = DefaultClaudeMaxInputTokens
		}
		maxOutput := model.MaxCompletionTokens
		if maxOutput <= 0 {
			maxOutput = DefaultClaudeMaxOutputTokens
		}
		result["max_input_tokens"] = maxInput
		result["max_tokens"] = maxOutput
		return result

	case "gemini":
		result := map[string]any{}
		if model.Name != "" {
			result["name"] = model.Name
		} else {
			result["name"] = model.ID
		}
		if model.Version != "" {
			result["version"] = model.Version
		}
		if model.DisplayName != "" {
			result["displayName"] = model.DisplayName
		}
		if model.Description != "" {
			result["description"] = model.Description
		}
		inputTokenLimit := model.InputTokenLimit
		if inputTokenLimit <= 0 {
			inputTokenLimit = model.ContextLength
		}
		if inputTokenLimit > 0 {
			result["inputTokenLimit"] = inputTokenLimit
		}
		outputTokenLimit := model.OutputTokenLimit
		if outputTokenLimit <= 0 {
			outputTokenLimit = model.MaxCompletionTokens
		}
		if outputTokenLimit > 0 {
			result["outputTokenLimit"] = outputTokenLimit
		}
		if len(model.SupportedGenerationMethods) > 0 {
			result["supportedGenerationMethods"] = append([]string(nil), model.SupportedGenerationMethods...)
		}
		if len(model.SupportedInputModalities) > 0 {
			result["supportedInputModalities"] = append([]string(nil), model.SupportedInputModalities...)
		}
		if len(model.SupportedOutputModalities) > 0 {
			result["supportedOutputModalities"] = append([]string(nil), model.SupportedOutputModalities...)
		}
		return result

	default:

		result := map[string]any{
			"id":     model.ID,
			"object": "model",
		}
		if model.OwnedBy != "" {
			result["owned_by"] = model.OwnedBy
		}
		if model.Type != "" {
			result["type"] = model.Type
		}
		if model.Created != 0 {
			result["created"] = model.Created
		}
		return result
	}
}


func (r *ModelRegistry) CleanupExpiredQuotas() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	now := r.now()
	invalidated := false

	for modelID, registration := range r.models {
		if registration == nil {
			continue
		}
		for clientID, quotaTime := range registration.QuotaExceededClients {
			if recoveryAt, ok := clientQuotaRecoveryAt(registration, clientID, quotaTime); ok && !now.Before(recoveryAt) {
				delete(registration.QuotaExceededClients, clientID)
				invalidated = true
				log.Debugf("Cleaned up expired quota tracking for model %s, client %s", modelID, clientID)
			}
		}
		for clientID, deadline := range registration.SuspendedClientDeadlines {
			if !deadline.IsZero() && !now.Before(deadline) {
				delete(registration.SuspendedClientDeadlines, clientID)
				delete(registration.SuspendedClients, clientID)
				invalidated = true
				log.Debugf("Cleaned up expired suspension for model %s, client %s", modelID, clientID)
			}
		}
	}
	if invalidated {
		r.invalidateAvailableModelsCacheLocked()
	}
}











func (r *ModelRegistry) GetFirstAvailableModel(handlerType string) (string, error) {


	models := r.GetAvailableModels(handlerType)
	if len(models) == 0 {
		return "", fmt.Errorf("no models available for handler type: %s", handlerType)
	}


	sort.Slice(models, func(i, j int) bool {

		createdI, okI := models[i]["created"].(int64)
		createdJ, okJ := models[j]["created"].(int64)
		if !okI || !okJ {
			return false
		}
		return createdI > createdJ
	})


	for _, model := range models {
		if modelID, ok := model["id"].(string); ok {
			if count := r.GetModelCount(modelID); count > 0 {
				return modelID, nil
			}
		}
	}

	return "", fmt.Errorf("no available clients for any model in handler type: %s", handlerType)
}


func (r *ModelRegistry) GetModelsAndEpochForClient(clientID string) ([]*ModelInfo, uint64) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	var epoch uint64
	if r.clientEpochs != nil {
		epoch = r.clientEpochs[clientID]
	}

	modelIDs, exists := r.clientModels[clientID]
	if !exists || len(modelIDs) == 0 {
		return nil, epoch
	}


	clientInfos := r.clientModelInfos[clientID]

	seen := make(map[string]struct{})
	result := make([]*ModelInfo, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		if _, dup := seen[modelID]; dup {
			continue
		}
		seen[modelID] = struct{}{}


		if clientInfos != nil {
			if info, okInfo := clientInfos[modelID]; okInfo && info != nil {
				result = append(result, cloneModelInfo(info))
				continue
			}
		}

		if reg, okReg := r.models[modelID]; okReg && reg.Info != nil {
			result = append(result, cloneModelInfo(reg.Info))
		}
	}
	return result, epoch
}


func (r *ModelRegistry) ClientRegistrationEpoch(clientID string) uint64 {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	if r.clientEpochs == nil {
		return 0
	}
	return r.clientEpochs[clientID]
}







func (r *ModelRegistry) GetModelsForClient(clientID string) []*ModelInfo {
	models, _ := r.GetModelsAndEpochForClient(clientID)
	return models
}
