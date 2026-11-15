package config

type ServerResponseCompression = ResponseCompressionConfig

type Config struct {
	ServerResponseCompression ResponseCompressionConfig `yaml:"-" json:"response-compression"`

	Models ModelCatalogs `yaml:"models" json:"models"`

	SDKConfig `yaml:",inline"`

	Host string `yaml:"host" json:"-"`

	Port int `yaml:"port" json:"-"`

	GitHubToken string `yaml:"github-token" json:"-"`

	TrustedProxies []string `yaml:"trusted-proxies" json:"trusted-proxies"`

	TLS TLSConfig `yaml:"tls" json:"tls"`

	Home HomeConfig `yaml:"-" json:"-"`

	SourceHash string `yaml:"-" json:"-"`

	CredentialConcurrency CredentialConcurrencyConfig `yaml:"credential-concurrency" json:"credential-concurrency"`

	CredentialInFlight CredentialInFlightConfig `yaml:"credential-in-flight" json:"credential-in-flight"`

	RemoteManagement RemoteManagement `yaml:"remote-management" json:"-"`

	Plugins PluginsConfig `yaml:"plugins" json:"plugins"`

	AuthDir string `yaml:"auth-dir" json:"-"`

	Debug bool `yaml:"debug" json:"debug"`

	Pprof PprofConfig `yaml:"pprof" json:"pprof"`

	Discovery DiscoveryConfig `yaml:"discovery" json:"discovery"`

	KVStore KVStoreConfig `yaml:"kv-store" json:"kv-store"`

	CommercialMode bool `yaml:"commercial-mode" json:"commercial-mode"`

	LoggingToFile bool `yaml:"logging-to-file" json:"logging-to-file"`

	LogsMaxTotalSizeMB int `yaml:"logs-max-total-size-mb" json:"logs-max-total-size-mb"`

	ErrorLogsMaxFiles int `yaml:"error-logs-max-files" json:"error-logs-max-files"`

	UsageStatisticsEnabled bool `yaml:"usage-statistics-enabled" json:"usage-statistics-enabled"`

	RedisUsageQueueRetentionSeconds int `yaml:"redis-usage-queue-retention-seconds" json:"redis-usage-queue-retention-seconds"`

	DisableCooling bool `yaml:"disable-cooling" json:"disable-cooling"`

	SaveCooldownStatus bool `yaml:"save-cooldown-status" json:"save-cooldown-status"`

	TransientErrorCooldownSeconds int `yaml:"transient-error-cooldown-seconds" json:"transient-error-cooldown-seconds"`

	AuthAutoRefreshWorkers int `yaml:"auth-auto-refresh-workers" json:"auth-auto-refresh-workers"`

	RequestRetry int `yaml:"request-retry" json:"request-retry"`

	MaxRetryCredentials int `yaml:"max-retry-credentials" json:"max-retry-credentials"`

	MaxRetryInterval int `yaml:"max-retry-interval" json:"max-retry-interval"`

	QuotaExceeded QuotaExceeded `yaml:"quota-exceeded" json:"quota-exceeded"`

	Routing RoutingConfig `yaml:"routing" json:"routing"`

	WebsocketAuth bool `yaml:"ws-auth" json:"ws-auth"`

	AntigravitySignatureCacheEnabled *bool `yaml:"antigravity-signature-cache-enabled,omitempty" json:"antigravity-signature-cache-enabled,omitempty"`

	AntigravitySignatureBypassStrict *bool `yaml:"antigravity-signature-bypass-strict,omitempty" json:"antigravity-signature-bypass-strict,omitempty"`

	ProxyPoolPublisher uint64 `yaml:"-" json:"-"`

	ProxyPoolGeneration uint64 `yaml:"-" json:"-"`

	Antigravity AntigravityConfig `yaml:"antigravity" json:"antigravity"`

	Devin DevinConfig `yaml:"devin" json:"devin"`

	GeminiKey []GeminiKey `yaml:"gemini-api-key" json:"gemini-api-key"`

	InteractionsKey []GeminiKey `yaml:"interactions-api-key" json:"interactions-api-key"`

	CodexKey []CodexKey `yaml:"codex-api-key" json:"codex-api-key"`

	XAIKey []XAIKey `yaml:"xai-api-key" json:"xai-api-key"`

	MetaKey []MetaKey `yaml:"meta-api-key" json:"meta-api-key"`

	XAI XAIConfig `yaml:"xai" json:"xai"`

	Codex CodexConfig `yaml:"codex" json:"codex"`

	CodexHeaderDefaults CodexHeaderDefaults `yaml:"codex-header-defaults" json:"codex-header-defaults"`

	Claude ClaudeConfig `yaml:"claude" json:"claude"`

	ClaudeKey []ClaudeKey `yaml:"claude-api-key" json:"claude-api-key"`

	ClaudeHeaderDefaults ClaudeHeaderDefaults `yaml:"claude-header-defaults" json:"claude-header-defaults"`

	DisableClaudeCloakMode bool `yaml:"disable-claude-cloak-mode" json:"disable-claude-cloak-mode"`

	OpenAICompatibility []OpenAICompatibility `yaml:"openai-compatibility" json:"openai-compatibility"`

	VertexCompatAPIKey []VertexCompatKey `yaml:"vertex-api-key" json:"vertex-api-key"`

	OAuthExcludedModels map[string][]string `yaml:"oauth-excluded-models,omitempty" json:"oauth-excluded-models,omitempty"`

	OAuthModelAlias map[string][]OAuthModelAlias `yaml:"oauth-model-alias,omitempty" json:"oauth-model-alias,omitempty"`

	OAuthRequestScopedErrors map[string][]RequestScopedErrorRule `yaml:"oauth-request-scoped-errors,omitempty" json:"oauth-request-scoped-errors,omitempty"`

	OAuthSettings map[string][]OAuthModelSetting `yaml:"oauth-settings,omitempty" json:"oauth-settings,omitempty"`

	Payload PayloadConfig `yaml:"payload" json:"payload"`
}
