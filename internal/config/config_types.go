package config

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	sdkpluginstore "github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginstore"
	"gopkg.in/yaml.v3"
)


type RequestScopedErrorRule struct {

	Status int `yaml:"status,omitempty" json:"status,omitempty"`

	Match []string `yaml:"match,omitempty" json:"match,omitempty"`

	MatchRegexr []string `yaml:"match-regexr,omitempty" json:"match-regexr,omitempty"`

	Action string `yaml:"action,omitempty" json:"action,omitempty"`
}


type PluginsConfig struct {

	Enabled bool `yaml:"enabled" json:"enabled"`

	Dir string `yaml:"dir" json:"dir"`

	StoreSources []string `yaml:"store-sources,omitempty" json:"store-sources,omitempty"`

	StoreAuth []sdkpluginstore.AuthConfig `yaml:"store-auth,omitempty" json:"store-auth,omitempty"`

	AuthRevision int64 `yaml:"auth-revision,omitempty" json:"auth-revision,omitempty"`

	Configs map[string]PluginInstanceConfig `yaml:"configs" json:"configs"`
}


type PluginInstanceConfig struct {

	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	Priority int `yaml:"priority,omitempty" json:"priority,omitempty"`

	Raw yaml.Node `yaml:"-" json:"-"`
}


func (c *PluginInstanceConfig) UnmarshalYAML(value *yaml.Node) error {
	if c == nil {
		return nil
	}

	c.Priority = 0
	defaultEnabled := false
	c.Enabled = &defaultEnabled

	if value == nil || value.Kind == 0 {
		c.Raw = *defaultPluginInstanceConfigNode()
		return nil
	}

	c.Raw = *deepCopyNode(value)
	if value.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(value.Content); i += 2 {
		key := value.Content[i]
		node := value.Content[i+1]
		if key == nil {
			continue
		}
		switch key.Value {
		case "enabled":
			var enabled bool
			if errDecodeEnabled := node.Decode(&enabled); errDecodeEnabled != nil {
				return fmt.Errorf("parse plugin enabled: %w", errDecodeEnabled)
			}
			c.Enabled = &enabled
		case "priority":
			var priority int
			if errDecodePriority := node.Decode(&priority); errDecodePriority != nil {
				return fmt.Errorf("parse plugin priority: %w", errDecodePriority)
			}
			c.Priority = priority
		}
	}

	return nil
}


func (c PluginInstanceConfig) MarshalYAML() (any, error) {
	if c.Raw.Kind == 0 {
		return defaultPluginInstanceConfigNode(), nil
	}
	return deepCopyNode(&c.Raw), nil
}

func defaultPluginInstanceConfigNode() *yaml.Node {
	return &yaml.Node{
		Kind:    yaml.MappingNode,
		Tag:     "!!map",
		Content: []*yaml.Node{},
	}
}


type ClaudeConfig struct {


	ModelLevelCooling bool `yaml:"model-level-cooling" json:"model-level-cooling"`




	ToolSearch bool `yaml:"tool-search" json:"tool-search"`





	ServerSideFallback bool `yaml:"server-side-fallback" json:"server-side-fallback"`







	SessionDatePinDays *int `yaml:"session-date-pin-days" json:"session-date-pin-days"`
}



const DefaultClaudeSessionDatePinDays = 7





func (c ClaudeConfig) SessionDatePinRetention() time.Duration {
	days := DefaultClaudeSessionDatePinDays
	if c.SessionDatePinDays != nil {
		days = *c.SessionDatePinDays
	}
	if days <= 0 {
		return 0
	}
	maxDays := int((time.Duration(math.MaxInt64) / (24 * time.Hour)))
	if days > maxDays {
		days = maxDays
	}
	return time.Duration(days) * 24 * time.Hour
}










type ClaudeHeaderDefaults struct {
	UserAgent              string   `yaml:"user-agent" json:"user-agent"`
	PackageVersion         string   `yaml:"package-version" json:"package-version"`
	RuntimeVersion         string   `yaml:"runtime-version" json:"runtime-version"`
	OS                     string   `yaml:"os" json:"os"`
	Arch                   string   `yaml:"arch" json:"arch"`
	Timeout                string   `yaml:"timeout" json:"timeout"`
	Timezone               string   `yaml:"timezone" json:"timezone"`
	StabilizeDeviceProfile *bool    `yaml:"stabilize-device-profile,omitempty" json:"stabilize-device-profile,omitempty"`
	NativeEntrypoints      []string `yaml:"native-entrypoints,omitempty" json:"native-entrypoints,omitempty"`
	AnthropicBetaStrip     []string `yaml:"anthropic-beta-strip,omitempty" json:"anthropic-beta-strip,omitempty"`
	AnthropicBetaAppend    []string `yaml:"anthropic-beta-append,omitempty" json:"anthropic-beta-append,omitempty"`
	InjectDiagnostics      *bool    `yaml:"inject-diagnostics,omitempty" json:"inject-diagnostics,omitempty"`
	ClientRequestID        string   `yaml:"client-request-id,omitempty" json:"client-request-id,omitempty"`
}



const (
	ClaudeClientRequestIDOn          = "on"
	ClaudeClientRequestIDOff         = "off"
	ClaudeClientRequestIDPassthrough = "passthrough"
)




type CodexHeaderDefaults struct {
	UserAgent    string `yaml:"user-agent" json:"user-agent"`
	BetaFeatures string `yaml:"beta-features" json:"beta-features"`
}


type XAIConfig struct {

	InjectXSearch bool `yaml:"inject-x-search" json:"inject-x-search"`
}


type DevinConfig struct {

	SensitiveWords []string `yaml:"sensitive-words,omitempty" json:"sensitive-words,omitempty"`


	SensitiveWordsReplace map[string]string `yaml:"sensitive-words-replace,omitempty" json:"sensitive-words-replace,omitempty"`
}








type ProxyConnectionPoolConfig struct {



	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`







	IdleConnTimeout string `yaml:"idle-conn-timeout,omitempty" json:"idle-conn-timeout,omitempty"`




	MaxIdleConnsPerHost *int `yaml:"max-idle-conns-per-host,omitempty" json:"max-idle-conns-per-host,omitempty"`
}


type AntigravityConfig struct {

	SensitiveWords []string `yaml:"sensitive-words,omitempty" json:"sensitive-words,omitempty"`


	SensitiveWordsReplace map[string]string `yaml:"sensitive-words-replace,omitempty" json:"sensitive-words-replace,omitempty"`


	ConnectionPool AntigravityConnectionPoolConfig `yaml:"connection-pool,omitempty" json:"connection-pool,omitempty"`
}


type AntigravityConnectionPoolConfig struct {


	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`



	IdleConnTimeout string `yaml:"idle-conn-timeout,omitempty" json:"idle-conn-timeout,omitempty"`



	MaxIdleConnsPerHost *int `yaml:"max-idle-conns-per-host,omitempty" json:"max-idle-conns-per-host,omitempty"`
}


type ResponseCompressionConfig struct {

	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	MinBytes int `yaml:"min-bytes" json:"min-bytes"`
}


func (c ResponseCompressionConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}


func (c ResponseCompressionConfig) EffectiveMinBytes() int {
	if c.MinBytes <= 0 {
		return 1024
	}
	return c.MinBytes
}


type CodexConfig struct {

	WebsocketCompression *bool `yaml:"websocket-compression,omitempty" json:"websocket-compression,omitempty"`

	UpstreamRequestCompression bool `yaml:"upstream-request-compression" json:"upstream-request-compression"`

	UpstreamRequestCompressionMinBytes int `yaml:"upstream-request-compression-min-bytes,omitempty" json:"upstream-request-compression-min-bytes,omitempty"`

	DisableCodexCloaking bool `yaml:"disable-codex-cloaking" json:"disable-codex-cloaking"`


















	StreamBootstrapBuffering bool `yaml:"stream-bootstrap-buffering" json:"stream-bootstrap-buffering"`





	StreamBootstrapTimeout string `yaml:"stream-bootstrap-timeout,omitempty" json:"stream-bootstrap-timeout,omitempty"`

	OrphanDelegationCompatibility bool `yaml:"orphan-delegation-compatibility" json:"orphan-delegation-compatibility"`


	ModelLevelCooling bool `yaml:"model-level-cooling" json:"model-level-cooling"`

	LiveMediaRelay CodexLiveMediaRelayConfig `yaml:"live-media-relay" json:"live-media-relay"`


	ResponseSteering bool `yaml:"response-steering" json:"response-steering"`



	SignInWithChatGPTIdentifier string `yaml:"signin-with-chatgpt-identifier,omitempty" json:"signin-with-chatgpt-identifier,omitempty"`
}


func (c *CodexConfig) WebsocketCompressionEnabled() bool {
	return c == nil || c.WebsocketCompression == nil || *c.WebsocketCompression
}


func (c *CodexConfig) RequestCompressionMinBytes() int {
	if c == nil || c.UpstreamRequestCompressionMinBytes <= 0 {
		return 1024
	}
	return c.UpstreamRequestCompressionMinBytes
}



const DefaultSignInWithChatGPTIdentifier = "CLIProxyAPI"



func (c CodexConfig) EffectiveSignInWithChatGPTIdentifier() string {
	if v := strings.TrimSpace(c.SignInWithChatGPTIdentifier); v != "" {
		return v
	}
	return DefaultSignInWithChatGPTIdentifier
}



const DefaultCodexStreamBootstrapTimeout = 0

const maxBootstrapTimeoutSeconds = int64(math.MaxInt64 / time.Second)





func (c *CodexConfig) StreamBootstrapTimeoutDuration() time.Duration {
	if c == nil {
		return DefaultCodexStreamBootstrapTimeout
	}
	raw := strings.TrimSpace(c.StreamBootstrapTimeout)
	if raw == "" || raw == "0" || strings.EqualFold(raw, "none") || strings.EqualFold(raw, "unlimited") || strings.EqualFold(raw, "disabled") || strings.EqualFold(raw, "off") || strings.EqualFold(raw, "never") {
		return 0
	}
	if d, err := time.ParseDuration(raw); err == nil && d >= 0 {
		return d
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs >= 0 && int64(secs) <= maxBootstrapTimeoutSeconds {
		d := time.Duration(secs) * time.Second
		if d >= 0 {
			return d
		}
	}
	return DefaultCodexStreamBootstrapTimeout
}


type CodexLiveMediaRelayConfig struct {
	Enabled                 bool                 `yaml:"enabled" json:"enabled"`
	MaxSessions             int                  `yaml:"max-sessions" json:"max-sessions"`
	DisablePrivateRemoteIPs bool                 `yaml:"disable-private-remote-ips" json:"disable-private-remote-ips"`
	PublicIP                string               `yaml:"public-ip" json:"public-ip"`
	UDPPortMin              uint16               `yaml:"udp-port-min" json:"udp-port-min"`
	UDPPortMax              uint16               `yaml:"udp-port-max" json:"udp-port-max"`
	ICEServers              []CodexLiveICEServer `yaml:"ice-servers" json:"ice-servers"`
}


type CodexLiveICEServer struct {
	URLs       []string `yaml:"urls" json:"urls"`
	Username   string   `yaml:"username" json:"-"`
	Credential string   `yaml:"credential" json:"-"`
}


type TLSConfig struct {

	Enable bool `yaml:"enable" json:"enable"`

	Cert string `yaml:"cert" json:"cert"`

	Key string `yaml:"key" json:"key"`
}


type PprofConfig struct {

	Enable bool `yaml:"enable" json:"enable"`

	Addr string `yaml:"addr" json:"addr"`
}


type DiscoveryInterfacesConfig struct {
	Include []string `yaml:"include" json:"include"`
	Exclude []string `yaml:"exclude" json:"exclude"`
}


type KVStoreConfig struct {


	Disabled bool `yaml:"disabled" json:"disabled"`



	Path string `yaml:"path,omitempty" json:"path,omitempty"`
}


func (c KVStoreConfig) EffectivePath() string {
	if trimmed := strings.TrimSpace(c.Path); trimmed != "" {
		return trimmed
	}
	return DefaultKVStorePath
}




func (c KVStoreConfig) EffectivePathForAuthDir(authDir string) string {
	if trimmed := strings.TrimSpace(c.Path); trimmed != "" {
		return trimmed
	}
	baseDir := strings.TrimSpace(authDir)
	if baseDir == "" {
		baseDir = DefaultAuthDir
	}
	return filepath.Join(baseDir, DefaultKVStorePath)
}


func (cfg *Config) EffectiveKVStorePath() string {
	if cfg == nil {
		return filepath.Join(DefaultAuthDir, DefaultKVStorePath)
	}
	return cfg.KVStore.EffectivePathForAuthDir(cfg.AuthDir)
}


type DiscoveryConfig struct {

	Enabled bool `yaml:"enabled" json:"enabled"`


	ServiceName string `yaml:"service-name" json:"service-name"`


	ServiceType string `yaml:"service-type" json:"service-type"`


	Subtypes []string `yaml:"subtypes" json:"subtypes"`


	Interfaces DiscoveryInterfacesConfig `yaml:"interfaces" json:"interfaces"`


	AuthRequired *bool `yaml:"auth-required" json:"auth-required"`



	AdvertiseManagement bool `yaml:"advertise-management" json:"advertise-management"`
}


type RemoteManagement struct {

	AllowRemote bool `yaml:"allow-remote"`

	SecretKey string `yaml:"secret-key"`

	DisableControlPanel bool `yaml:"disable-control-panel"`


	DisableAutoUpdatePanel bool `yaml:"disable-auto-update-panel"`


	PanelGitHubRepository string `yaml:"panel-github-repository"`

	BaseURL string `yaml:"base-url,omitempty" json:"base-url,omitempty"`
}



type QuotaExceeded struct {

	SwitchProject bool `yaml:"switch-project" json:"switch-project"`


	SwitchPreviewModel bool `yaml:"switch-preview-model" json:"switch-preview-model"`




	AntigravityCredits bool `yaml:"antigravity-credits" json:"antigravity-credits"`
}


type ResetSoonestConfig struct {



	FiveHourGuardPercent *float64 `yaml:"five-hour-guard-percent,omitempty" json:"five-hour-guard-percent,omitempty"`
}



type QuotaPollerConfig struct {


	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`



	ClaudeInterval string `yaml:"claude-interval,omitempty" json:"claude-interval,omitempty"`



	ClaudeMinGap string `yaml:"claude-min-gap,omitempty" json:"claude-min-gap,omitempty"`



	CodexInterval string `yaml:"codex-interval,omitempty" json:"codex-interval,omitempty"`
}


type RoutingConfig struct {



	Strategy string `yaml:"strategy,omitempty" json:"strategy,omitempty"`


	ResetSoonest ResetSoonestConfig `yaml:"reset-soonest,omitempty" json:"reset-soonest,omitempty"`


	QuotaPoller QuotaPollerConfig `yaml:"quota-poller,omitempty" json:"quota-poller,omitempty"`










	SessionAffinity bool `yaml:"session-affinity,omitempty" json:"session-affinity,omitempty"`



	SessionAffinityTTL string `yaml:"session-affinity-ttl,omitempty" json:"session-affinity-ttl,omitempty"`






	SessionAffinitySubagents *bool `yaml:"session-affinity-subagents,omitempty" json:"session-affinity-subagents,omitempty"`











	SessionAffinityRebindOn string `yaml:"session-affinity-rebind-on,omitempty" json:"session-affinity-rebind-on,omitempty"`
}





type OAuthModelAlias struct {

	OpenAPI *bool `yaml:"openapi,omitempty" json:"openapi,omitempty"`

	Name  string `yaml:"name" json:"name"`
	Alias string `yaml:"alias" json:"alias"`
	Fork  bool   `yaml:"fork,omitempty" json:"fork,omitempty"`


	DisplayName string `yaml:"display-name,omitempty" json:"display-name,omitempty"`

	ForceMapping bool `yaml:"force-mapping,omitempty" json:"force-mapping,omitempty"`
}


type OAuthModelSetting struct {

	OpenAPI *bool `yaml:"openapi,omitempty" json:"openapi,omitempty"`

	Name  string `yaml:"name" json:"name"`
	Alias string `yaml:"alias,omitempty" json:"alias,omitempty"`


	MaxContextLength int `yaml:"max-context-length,omitempty" json:"max-context-length,omitempty"`
}


func (s OAuthModelSetting) GetMaxContextLength() int { return s.MaxContextLength }




func ResolveOAuthModelSetting(settings []OAuthModelSetting, modelID, metadataModelID, modelName string) *OAuthModelSetting {
	if len(settings) == 0 {
		return nil
	}
	id := strings.ToLower(strings.TrimSpace(modelID))
	metaID := strings.ToLower(strings.TrimSpace(metadataModelID))
	name := strings.ToLower(strings.TrimSpace(modelName))

	var aliasMatch *OAuthModelSetting
	var nameMatch *OAuthModelSetting

	for i := range settings {
		entry := &settings[i]
		entryName := strings.ToLower(strings.TrimSpace(entry.Name))
		if entryName == "" {
			continue
		}
		entryAlias := strings.ToLower(strings.TrimSpace(entry.Alias))

		if entryAlias != "" && id != "" && id == entryAlias {
			aliasMatch = entry
		} else if (entryAlias == "" || entryAlias == id) && (id == entryName || (metaID != "" && metaID == entryName) || (name != "" && name == entryName)) {
			nameMatch = entry
		}
	}

	if aliasMatch != nil {
		return aliasMatch
	}
	return nameMatch
}


type PayloadConfig struct {

	Default []PayloadRule `yaml:"default" json:"default"`

	DefaultRaw []PayloadRule `yaml:"default-raw" json:"default-raw"`

	Override []PayloadRule `yaml:"override" json:"override"`

	OverrideRaw []PayloadRule `yaml:"override-raw" json:"override-raw"`

	Filter []PayloadFilterRule `yaml:"filter" json:"filter"`
}


type PayloadFilterRule struct {

	Models []PayloadModelRule `yaml:"models" json:"models"`

	Params []string `yaml:"params" json:"params"`
}


type PayloadRule struct {

	Models []PayloadModelRule `yaml:"models" json:"models"`


	Params map[string]any `yaml:"params" json:"params"`
}


type PayloadModelRule struct {

	Name string `yaml:"name" json:"name"`

	Protocol string `yaml:"protocol" json:"protocol"`

	Headers map[string]string `yaml:"headers" json:"headers"`

	FromProtocol string `yaml:"from-protocol" json:"from-protocol"`

	Match []map[string]any `yaml:"match" json:"match"`

	NotMatch []map[string]any `yaml:"not-match" json:"not-match"`

	Exist []string `yaml:"exist" json:"exist"`

	NotExist []string `yaml:"not-exist" json:"not-exist"`
}



type CloakConfig struct {





	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`




	StrictMode bool `yaml:"strict-mode,omitempty" json:"strict-mode,omitempty"`



	SensitiveWords []string `yaml:"sensitive-words,omitempty" json:"sensitive-words,omitempty"`


	SensitiveWordsReplace map[string]string `yaml:"sensitive-words-replace,omitempty" json:"sensitive-words-replace,omitempty"`



	CacheUserID *bool `yaml:"cache-user-id,omitempty" json:"cache-user-id,omitempty"`
}



type ClaudeKey struct {

	APIKey string `yaml:"api-key" json:"api-key"`



	Priority int `yaml:"priority,omitempty" json:"priority,omitempty"`



	Weight *int `yaml:"weight,omitempty" json:"weight,omitempty"`


	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`



	BaseURL string `yaml:"base-url" json:"base-url"`


	ProxyURL string `yaml:"proxy-url" json:"proxy-url"`




	ConnectionPool *ProxyConnectionPoolConfig `yaml:"connection-pool,omitempty" json:"connection-pool,omitempty"`


	Models []ClaudeModel `yaml:"models" json:"models"`


	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`


	ExcludedModels []string `yaml:"excluded-models,omitempty" json:"excluded-models,omitempty"`


	RebuildMidSystemMessage bool `yaml:"rebuild-mid-system-message,omitempty" json:"rebuild-mid-system-message,omitempty"`




	NativeToolSearch bool `yaml:"native-tool-search,omitempty" json:"native-tool-search,omitempty"`



	DisableCooling *bool `yaml:"disable-cooling,omitempty" json:"disable-cooling,omitempty"`



	RequestRetry *int `yaml:"request-retry,omitempty" json:"request-retry,omitempty"`


	RequestScopedErrors []RequestScopedErrorRule `yaml:"request-scoped-errors,omitempty" json:"request-scoped-errors,omitempty"`


	Cloak *CloakConfig `yaml:"cloak,omitempty" json:"cloak,omitempty"`














	FingerprintProfile string `yaml:"fingerprint-profile,omitempty" json:"fingerprint-profile,omitempty"`



	ExperimentalCCHSigning bool `yaml:"experimental-cch-signing,omitempty" json:"experimental-cch-signing,omitempty"`
}

func (k ClaudeKey) GetAPIKey() string { return k.APIKey }

func (k ClaudeKey) GetBaseURL() string { return k.BaseURL }

func (k ClaudeKey) GetPrefix() string { return k.Prefix }

func (k ClaudeKey) GetProxyURL() string { return k.ProxyURL }


type ClaudeModel struct {

	Name string `yaml:"name" json:"name"`


	Alias string `yaml:"alias" json:"alias"`


	DisplayName string `yaml:"display-name,omitempty" json:"display-name,omitempty"`


	MaxContextLength int `yaml:"max-context-length,omitempty" json:"max-context-length,omitempty"`


	ForceMapping bool `yaml:"force-mapping,omitempty" json:"force-mapping,omitempty"`




	IsCompat bool `yaml:"is-compat,omitempty" json:"is-compat,omitempty"`


	Thinking *registry.ThinkingSupport `yaml:"thinking,omitempty" json:"thinking,omitempty"`
}

func (m ClaudeModel) GetName() string { return m.Name }

func (m ClaudeModel) GetAlias() string { return m.Alias }

func (m ClaudeModel) GetDisplayName() string   { return m.DisplayName }
func (m ClaudeModel) GetMaxContextLength() int { return m.MaxContextLength }
func (m ClaudeModel) GetForceMapping() bool    { return m.ForceMapping }
func (m ClaudeModel) GetIsCompat() bool        { return m.IsCompat }

func (m ClaudeModel) GetThinking() *registry.ThinkingSupport { return m.Thinking }



type CodexKey struct {

	APIKey string `yaml:"api-key" json:"api-key"`



	Priority int `yaml:"priority,omitempty" json:"priority,omitempty"`



	Weight *int `yaml:"weight,omitempty" json:"weight,omitempty"`


	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`



	BaseURL string `yaml:"base-url" json:"base-url"`


	Websockets bool `yaml:"websockets,omitempty" json:"websockets,omitempty"`


	AlphaSearch bool `yaml:"alpha-search,omitempty" json:"alpha-search,omitempty"`


	ProxyURL string `yaml:"proxy-url" json:"proxy-url"`




	ConnectionPool *ProxyConnectionPoolConfig `yaml:"connection-pool,omitempty" json:"connection-pool,omitempty"`


	Models []CodexModel `yaml:"models" json:"models"`


	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`


	ExcludedModels []string `yaml:"excluded-models,omitempty" json:"excluded-models,omitempty"`



	DisableCooling *bool `yaml:"disable-cooling,omitempty" json:"disable-cooling,omitempty"`



	DisableCodexCloaking *bool `yaml:"disable-codex-cloaking,omitempty" json:"disable-codex-cloaking,omitempty"`



	RequestRetry *int `yaml:"request-retry,omitempty" json:"request-retry,omitempty"`


	RequestScopedErrors []RequestScopedErrorRule `yaml:"request-scoped-errors,omitempty" json:"request-scoped-errors,omitempty"`
}

func (k CodexKey) GetAPIKey() string { return k.APIKey }

func (k CodexKey) GetBaseURL() string { return k.BaseURL }

func (k CodexKey) GetPrefix() string { return k.Prefix }

func (k CodexKey) GetProxyURL() string { return k.ProxyURL }


type CodexModel struct {

	Name string `yaml:"name" json:"name"`


	Alias string `yaml:"alias" json:"alias"`


	DisplayName string `yaml:"display-name,omitempty" json:"display-name,omitempty"`


	MaxContextLength int `yaml:"max-context-length,omitempty" json:"max-context-length,omitempty"`


	ForceMapping bool `yaml:"force-mapping,omitempty" json:"force-mapping,omitempty"`







	IsCompat bool `yaml:"is-compat,omitempty" json:"is-compat,omitempty"`



	SupportConfigurationUpdate bool `yaml:"support-configuration-update,omitempty" json:"support-configuration-update,omitempty"`


	Thinking *registry.ThinkingSupport `yaml:"thinking,omitempty" json:"thinking,omitempty"`
}

func (m CodexModel) GetName() string { return m.Name }

func (m CodexModel) GetAlias() string { return m.Alias }

func (m CodexModel) GetDisplayName() string   { return m.DisplayName }
func (m CodexModel) GetMaxContextLength() int { return m.MaxContextLength }
func (m CodexModel) GetForceMapping() bool    { return m.ForceMapping }
func (m CodexModel) GetIsCompat() bool        { return m.IsCompat }

func (m CodexModel) GetThinking() *registry.ThinkingSupport { return m.Thinking }


type XAIKey = CodexKey


type XAIModel = CodexModel


type MetaKey = CodexKey


type MetaModel = CodexModel



type GeminiKey struct {

	APIKey string `yaml:"api-key" json:"api-key"`



	Priority int `yaml:"priority,omitempty" json:"priority,omitempty"`



	Weight *int `yaml:"weight,omitempty" json:"weight,omitempty"`


	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`


	BaseURL string `yaml:"base-url,omitempty" json:"base-url,omitempty"`


	ProxyURL string `yaml:"proxy-url,omitempty" json:"proxy-url,omitempty"`




	ConnectionPool *ProxyConnectionPoolConfig `yaml:"connection-pool,omitempty" json:"connection-pool,omitempty"`


	Models []GeminiModel `yaml:"models,omitempty" json:"models,omitempty"`


	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`


	ExcludedModels []string `yaml:"excluded-models,omitempty" json:"excluded-models,omitempty"`



	DisableCooling *bool `yaml:"disable-cooling,omitempty" json:"disable-cooling,omitempty"`



	RequestRetry *int `yaml:"request-retry,omitempty" json:"request-retry,omitempty"`


	RequestScopedErrors []RequestScopedErrorRule `yaml:"request-scoped-errors,omitempty" json:"request-scoped-errors,omitempty"`
}

func (k GeminiKey) GetAPIKey() string { return k.APIKey }

func (k GeminiKey) GetBaseURL() string { return k.BaseURL }

func (k GeminiKey) GetPrefix() string { return k.Prefix }

func (k GeminiKey) GetProxyURL() string { return k.ProxyURL }


type GeminiModel struct {

	Name string `yaml:"name" json:"name"`


	Alias string `yaml:"alias" json:"alias"`


	DisplayName string `yaml:"display-name,omitempty" json:"display-name,omitempty"`


	MaxContextLength int `yaml:"max-context-length,omitempty" json:"max-context-length,omitempty"`


	ForceMapping bool `yaml:"force-mapping,omitempty" json:"force-mapping,omitempty"`



	IsCompat bool `yaml:"is-compat,omitempty" json:"is-compat,omitempty"`


	Thinking *registry.ThinkingSupport `yaml:"thinking,omitempty" json:"thinking,omitempty"`
}

func (m GeminiModel) GetName() string { return m.Name }

func (m GeminiModel) GetAlias() string { return m.Alias }

func (m GeminiModel) GetDisplayName() string   { return m.DisplayName }
func (m GeminiModel) GetMaxContextLength() int { return m.MaxContextLength }
func (m GeminiModel) GetForceMapping() bool    { return m.ForceMapping }
func (m GeminiModel) GetIsCompat() bool        { return m.IsCompat }

func (m GeminiModel) GetThinking() *registry.ThinkingSupport { return m.Thinking }



type OpenAICompatibility struct {

	Name string `yaml:"name" json:"name"`



	Priority int `yaml:"priority,omitempty" json:"priority,omitempty"`


	Disabled bool `yaml:"disabled,omitempty" json:"disabled,omitempty"`


	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`


	BaseURL string `yaml:"base-url" json:"base-url"`


	APIKeyEntries []OpenAICompatibilityAPIKey `yaml:"api-key-entries,omitempty" json:"api-key-entries,omitempty"`



	ConnectionPool *ProxyConnectionPoolConfig `yaml:"connection-pool,omitempty" json:"connection-pool,omitempty"`


	Models []OpenAICompatibilityModel `yaml:"models" json:"models"`


	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`


	SupportPromptCacheKey bool `yaml:"support-prompt-cache-key,omitempty" json:"support-prompt-cache-key,omitempty"`



	DisableCooling *bool `yaml:"disable-cooling,omitempty" json:"disable-cooling,omitempty"`



	RequestRetry *int `yaml:"request-retry,omitempty" json:"request-retry,omitempty"`


	RequestScopedErrors []RequestScopedErrorRule `yaml:"request-scoped-errors,omitempty" json:"request-scoped-errors,omitempty"`
}


type OpenAICompatibilityAPIKey struct {

	APIKey string `yaml:"api-key" json:"api-key"`



	Weight *int `yaml:"weight,omitempty" json:"weight,omitempty"`


	ProxyURL string `yaml:"proxy-url,omitempty" json:"proxy-url,omitempty"`




	ConnectionPool *ProxyConnectionPoolConfig `yaml:"connection-pool,omitempty" json:"connection-pool,omitempty"`
}



type OpenAICompatibilityModel struct {

	Name string `yaml:"name" json:"name"`


	Alias string `yaml:"alias" json:"alias"`


	DisplayName string `yaml:"display-name,omitempty" json:"display-name,omitempty"`


	MaxContextLength int `yaml:"max-context-length,omitempty" json:"max-context-length,omitempty"`


	ForceMapping bool `yaml:"force-mapping,omitempty" json:"force-mapping,omitempty"`


	Image bool `yaml:"image,omitempty" json:"image,omitempty"`



	InputModalities []string `yaml:"input-modalities,omitempty" json:"input-modalities,omitempty"`


	OutputModalities []string `yaml:"output-modalities,omitempty" json:"output-modalities,omitempty"`



	IsCompat bool `yaml:"is-compat,omitempty" json:"is-compat,omitempty"`



	UseMaxCompletionTokens bool `yaml:"use-max-completion-tokens,omitempty" json:"use-max-completion-tokens,omitempty"`



	Thinking *registry.ThinkingSupport `yaml:"thinking,omitempty" json:"thinking,omitempty"`
}

func (m OpenAICompatibilityModel) GetName() string { return m.Name }

func (m OpenAICompatibilityModel) GetAlias() string { return m.Alias }

func (m OpenAICompatibilityModel) GetDisplayName() string          { return m.DisplayName }
func (m OpenAICompatibilityModel) GetMaxContextLength() int        { return m.MaxContextLength }
func (m OpenAICompatibilityModel) GetForceMapping() bool           { return m.ForceMapping }
func (m OpenAICompatibilityModel) GetIsCompat() bool               { return m.IsCompat }
func (m OpenAICompatibilityModel) GetUseMaxCompletionTokens() bool { return m.UseMaxCompletionTokens }

func (m OpenAICompatibilityModel) GetThinking() *registry.ThinkingSupport { return m.Thinking }
