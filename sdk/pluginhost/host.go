package pluginhost

import (
	"context"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	internalpluginhost "github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	internalregistry "github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

type ModelInfo = pluginapi.ModelInfo

type ThinkingSupport = pluginapi.ThinkingSupport

type OAuthModelAlias struct {
	Name  string
	Alias string
	Fork  bool
}

type RuntimeConfig struct {
	Enabled             bool
	Dir                 string
	AuthDir             string
	ProxyURL            string
	ForceModelPrefix    bool
	OAuthModelAlias     map[string][]OAuthModelAlias
	OAuthExcludedModels map[string][]string
	Configs             map[string]PluginInstanceConfig
}

type PluginInstanceConfig struct {
	Enabled  *bool
	Priority int
	Raw      yaml.Node
}

type AuthModelResult struct {
	Provider string
	Models   []ModelInfo
	Auth     *coreauth.Auth
	Handled  bool
	Err      error
}

type RegisteredPluginInfo = internalpluginhost.RegisteredPluginInfo

type RegisteredPluginMenu = internalpluginhost.RegisteredPluginMenu

type AuthProviderEntry = pluginapi.AuthProviderEntry

type Host struct {
	inner *internalpluginhost.Host
}

func New() *Host {
	return &Host{inner: internalpluginhost.New()}
}

func (h *Host) ApplyConfig(ctx context.Context, cfg RuntimeConfig) {
	if h == nil || h.inner == nil {
		return
	}
	internalCfg := runtimeConfigToInternalConfig(cfg)
	h.inner.ApplyConfig(ctx, internalCfg)
}

func (h *Host) ShutdownAll() {
	h.ShutdownAllContext(context.Background())
}

func (h *Host) ShutdownAllContext(ctx context.Context) {
	if h == nil || h.inner == nil {
		return
	}
	h.inner.ShutdownAllContext(ctx)
}

func (h *Host) PluginBusy(id string) bool {
	return h != nil && h.inner != nil && h.inner.PluginBusy(id)
}

func (h *Host) UnloadPlugin(id string) bool {
	return h.UnloadPluginContext(context.Background(), id)
}

func (h *Host) UnloadPluginContext(ctx context.Context, id string) bool {
	if h == nil || h.inner == nil {
		return false
	}
	return h.inner.UnloadPluginContext(ctx, id)
}

func (h *Host) ParseAuth(ctx context.Context, req pluginapi.AuthParseRequest) (*coreauth.Auth, bool, error) {
	if h == nil || h.inner == nil {
		return nil, false, nil
	}
	return h.inner.ParseAuth(ctx, req)
}

func (h *Host) ParseAuths(ctx context.Context, req pluginapi.AuthParseRequest) ([]*coreauth.Auth, bool, error) {
	if h == nil || h.inner == nil {
		return nil, false, nil
	}
	return h.inner.ParseAuths(ctx, req)
}

func (h *Host) ModelsForAuth(ctx context.Context, auth *coreauth.Auth) AuthModelResult {
	if h == nil || h.inner == nil {
		return AuthModelResult{}
	}
	result := h.inner.ModelsForAuth(ctx, auth)
	return AuthModelResult{
		Provider: result.Provider,
		Models:   registryModelsToPluginModels(result.Models),
		Auth:     result.Auth,
		Handled:  result.Handled,
		Err:      result.Err,
	}
}

func (h *Host) ModelsForProvider(provider string) []ModelInfo {
	if h == nil || h.inner == nil {
		return nil
	}
	return registryModelsToPluginModels(h.inner.ModelsForProvider(provider))
}

func (h *Host) RefreshAuth(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, bool, error) {
	if h == nil || h.inner == nil {
		return nil, false, nil
	}
	return h.inner.RefreshAuth(ctx, auth)
}

func (h *Host) AuthProviderIdentifiers() []string {
	if h == nil || h.inner == nil {
		return nil
	}
	return h.inner.AuthProviderIdentifiers()
}

func (h *Host) HasAuthProvider(provider string) bool {
	return h != nil && h.inner != nil && h.inner.HasAuthProvider(provider)
}

func (h *Host) SameAuthProvider(providerA, providerB string) bool {
	return h != nil && h.inner != nil && h.inner.SameAuthProvider(providerA, providerB)
}

func (h *Host) StartLogin(ctx context.Context, provider string, baseURL string, metadata ...map[string]any) (pluginapi.AuthLoginStartResponse, bool, error) {
	if h == nil || h.inner == nil {
		return pluginapi.AuthLoginStartResponse{}, false, nil
	}
	return h.inner.StartLogin(ctx, provider, baseURL, metadata...)
}

func (h *Host) PollLogin(ctx context.Context, provider, state string, metadata ...map[string]any) (pluginapi.AuthLoginPollResponse, bool, error) {
	if h == nil || h.inner == nil {
		return pluginapi.AuthLoginPollResponse{}, false, nil
	}
	return h.inner.PollLogin(ctx, provider, state, metadata...)
}

func (h *Host) AuthDataToCoreAuth(data pluginapi.AuthData, path, fileName string) *coreauth.Auth {
	if h == nil || h.inner == nil {
		return nil
	}
	return h.inner.AuthDataToCoreAuth(data, path, fileName)
}

func (h *Host) PickAuth(ctx context.Context, req pluginapi.SchedulerPickRequest) (pluginapi.SchedulerPickResponse, bool, error) {
	if h == nil || h.inner == nil {
		return pluginapi.SchedulerPickResponse{}, false, nil
	}
	return h.inner.PickAuth(ctx, req)
}

func (h *Host) HasScheduler() bool {
	return h != nil && h.inner != nil && h.inner.HasScheduler()
}

func (h *Host) SchedulerWantsAcrossPriorities() bool {
	return h != nil && h.inner != nil && h.inner.SchedulerWantsAcrossPriorities()
}

func (h *Host) RegisteredPlugins() []RegisteredPluginInfo {
	if h == nil || h.inner == nil {
		return nil
	}
	return h.inner.RegisteredPlugins()
}

func runtimeConfigToInternalConfig(cfg RuntimeConfig) *internalconfig.Config {
	out := &internalconfig.Config{
		SDKConfig: internalconfig.SDKConfig{
			ProxyURL:         cfg.ProxyURL,
			ForceModelPrefix: cfg.ForceModelPrefix,
		},
		AuthDir:             cfg.AuthDir,
		OAuthExcludedModels: cloneStringSliceMap(cfg.OAuthExcludedModels),
		OAuthModelAlias:     oauthModelAliasToInternal(cfg.OAuthModelAlias),
		Plugins: internalconfig.PluginsConfig{
			Enabled: cfg.Enabled,
			Dir:     cfg.Dir,
			Configs: pluginConfigsToInternal(cfg.Configs),
		},
	}
	out.NormalizePluginsConfig()
	out.SanitizeOAuthModelAlias()
	return out
}

func pluginConfigsToInternal(in map[string]PluginInstanceConfig) map[string]internalconfig.PluginInstanceConfig {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]internalconfig.PluginInstanceConfig, len(in))
	for id, item := range in {
		out[id] = internalconfig.PluginInstanceConfig{
			Enabled:  item.Enabled,
			Priority: item.Priority,
			Raw:      *deepCopyYAMLNode(&item.Raw),
		}
	}
	return out
}

func oauthModelAliasToInternal(in map[string][]OAuthModelAlias) map[string][]internalconfig.OAuthModelAlias {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]internalconfig.OAuthModelAlias, len(in))
	for provider, aliases := range in {
		if len(aliases) == 0 {
			continue
		}
		items := make([]internalconfig.OAuthModelAlias, 0, len(aliases))
		for _, alias := range aliases {
			items = append(items, internalconfig.OAuthModelAlias{
				Name:  alias.Name,
				Alias: alias.Alias,
				Fork:  alias.Fork,
			})
		}
		out[provider] = items
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func registryModelsToPluginModels(models []*internalregistry.ModelInfo) []ModelInfo {
	if len(models) == 0 {
		return nil
	}
	out := make([]ModelInfo, 0, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		out = append(out, registryModelToPluginModel(model))
	}
	return out
}

func registryModelToPluginModel(model *internalregistry.ModelInfo) ModelInfo {
	if model == nil {
		return ModelInfo{}
	}
	return ModelInfo{
		ID:                         model.ID,
		Object:                     model.Object,
		Created:                    model.Created,
		OwnedBy:                    model.OwnedBy,
		Type:                       model.Type,
		DisplayName:                model.DisplayName,
		Name:                       model.Name,
		Version:                    model.Version,
		Description:                model.Description,
		InputTokenLimit:            int64(model.InputTokenLimit),
		OutputTokenLimit:           int64(model.OutputTokenLimit),
		SupportedGenerationMethods: cloneStringSlice(model.SupportedGenerationMethods),
		ContextLength:              int64(model.ContextLength),
		MaxCompletionTokens:        int64(model.MaxCompletionTokens),
		SupportedParameters:        cloneStringSlice(model.SupportedParameters),
		SupportedInputModalities:   cloneStringSlice(model.SupportedInputModalities),
		SupportedOutputModalities:  cloneStringSlice(model.SupportedOutputModalities),
		Thinking:                   thinkingSupportToPlugin(model.Thinking),
		UserDefined:                model.UserDefined,
	}
}

func thinkingSupportToPlugin(thinking *internalregistry.ThinkingSupport) *ThinkingSupport {
	if thinking == nil {
		return nil
	}
	return &ThinkingSupport{
		Min:            thinking.Min,
		Max:            thinking.Max,
		ZeroAllowed:    thinking.ZeroAllowed,
		DynamicAllowed: thinking.DynamicAllowed,
		DisableType:    thinking.DisableType,
		Levels:         cloneStringSlice(thinking.Levels),
	}
}

func cloneStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return append([]string(nil), in...)
}

func cloneStringSliceMap(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = cloneStringSlice(values)
	}
	return out
}

func deepCopyYAMLNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return &yaml.Node{}
	}
	copyNode := *node
	if len(node.Content) > 0 {
		copyNode.Content = make([]*yaml.Node, 0, len(node.Content))
		for _, child := range node.Content {
			copyNode.Content = append(copyNode.Content, deepCopyYAMLNode(child))
		}
	}
	return &copyNode
}
