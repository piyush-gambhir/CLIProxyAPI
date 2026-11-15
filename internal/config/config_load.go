package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

func LoadConfig(configFile string) (*Config, error) {
	return LoadConfigOptional(configFile, false)
}

func ComputeSourceHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func PrepareConfigFileForUpload(configFile string) error {
	data, errRead := os.ReadFile(configFile)
	if errRead != nil {
		return errRead
	}
	prepared, errPrepare := prepareConfigForUpload(data)
	if errPrepare != nil {
		return errPrepare
	}
	if bytes.Equal(prepared, data) {
		return nil
	}
	if errWrite := os.WriteFile(configFile, prepared, 0600); errWrite != nil {
		return fmt.Errorf("failed to write prepared config file: %w", errWrite)
	}
	return nil
}

func prepareConfigForUpload(data []byte) ([]byte, error) {
	var cfg Config
	if errParse := yaml.Unmarshal(data, &cfg); errParse != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", errParse)
	}
	prepared := data
	if secret := cfg.RemoteManagement.SecretKey; secret != "" && !looksLikeBcrypt(secret) {
		hashed, errHash := hashSecret(secret)
		if errHash != nil {
			return nil, fmt.Errorf("failed to hash remote management key: %w", errHash)
		}
		updated, errUpdate := updateNestedScalarYAML(data, managementSecretKeyPath(data), hashed)
		if errUpdate != nil {
			return nil, fmt.Errorf("failed to update remote management key: %w", errUpdate)
		}
		prepared = updated
	}
	cleaned, _, errLayout := NormalizeConfigLayout(prepared, false)
	if errLayout != nil {
		return nil, errLayout
	}
	if plaintexts := plaintextManagementSecrets(data, cfg.RemoteManagement.SecretKey); len(plaintexts) > 0 {
		var doc yaml.Node
		if errParse := yaml.Unmarshal(cleaned, &doc); errParse != nil {
			return nil, fmt.Errorf("failed to parse prepared config: %w", errParse)
		}
		for _, plaintext := range plaintexts {
			if yamlScalarExists(&doc, plaintext) {
				return nil, errors.New("plaintext remote management key is still present elsewhere in the config file, for example through a YAML anchor or as another setting's value")
			}
		}
	}
	return cleaned, nil
}

func plaintextManagementSecrets(data []byte, effective string) []string {
	candidates := []string{effective}
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) == nil && len(doc.Content) > 0 {
		root := expandConfigAliases(doc.Content[0])
		for _, path := range []string{"remote-management.secret-key", "management.secret-key"} {
			if node := yamlPath(root, path); node != nil && node.Kind == yaml.ScalarNode && node.ShortTag() != "!!null" {
				candidates = append(candidates, node.Value)
			}
		}
	}
	var plaintexts []string
	for _, candidate := range candidates {
		if candidate != "" && !looksLikeBcrypt(candidate) {
			plaintexts = append(plaintexts, candidate)
		}
	}
	return plaintexts
}

func yamlScalarExists(node *yaml.Node, value string) bool {
	if node.Kind == yaml.ScalarNode {
		return node.ShortTag() != "!!null" && node.Value == value
	}
	first, step := 0, 1
	if node.Kind == yaml.MappingNode {
		first, step = 1, 2
	}
	for i := first; i < len(node.Content); i += step {
		if yamlScalarExists(node.Content[i], value) {
			return true
		}
	}
	return false
}

func managementSecretKeyPath(data []byte) []string {
	var source yaml.Node
	if yaml.Unmarshal(data, &source) == nil && len(source.Content) > 0 && yamlPath(expandConfigAliases(source.Content[0]), "management.secret-key") != nil {
		return []string{"management", "secret-key"}
	}
	return []string{"remote-management", "secret-key"}
}

func LoadConfigOptional(configFile string, optional bool) (*Config, error) {

	data, err := os.ReadFile(configFile)
	if err != nil {
		if optional {
			if os.IsNotExist(err) || errors.Is(err, syscall.EISDIR) {

				cfg := &Config{CredentialInFlight: DefaultCredentialInFlightConfig()}
				cfg.NormalizePluginsConfig()
				cfg.NormalizeClaudeConfig()
				return cfg, nil
			}
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if optional && len(bytes.TrimSpace(data)) == 0 {
		cfg := &Config{CredentialInFlight: DefaultCredentialInFlightConfig()}
		cfg.NormalizePluginsConfig()
		cfg.NormalizeClaudeConfig()
		return cfg, nil
	}

	if errValidate := validateCredentialWeightYAML(data); errValidate != nil {
		if optional {
			cfgOptional := &Config{CredentialInFlight: DefaultCredentialInFlightConfig()}
			cfgOptional.NormalizePluginsConfig()
			cfgOptional.NormalizeClaudeConfig()
			return cfgOptional, nil
		}
		return nil, errValidate
	}

	var cfg Config

	cfg.Host = ""
	cfg.LoggingToFile = false
	cfg.LogsMaxTotalSizeMB = 0
	cfg.ErrorLogsMaxFiles = 10
	cfg.UsageStatisticsEnabled = false
	cfg.RedisUsageQueueRetentionSeconds = 60
	cfg.DisableCooling = false
	cfg.SaveCooldownStatus = false
	cfg.TransientErrorCooldownSeconds = 0
	cfg.DisableImageGeneration = DisableImageGenerationOff
	cfg.WebsocketAuth = true
	cfg.Pprof.Enable = false
	cfg.Pprof.Addr = DefaultPprofAddr
	cfg.Discovery.Enabled = false
	cfg.Discovery.ServiceType = DefaultDiscoveryServiceType
	cfg.Discovery.Subtypes = []string{"_chat-completions", "_responses", "_messages", "_generate-content", "_interactions"}
	cfg.RemoteManagement.PanelGitHubRepository = DefaultPanelGitHubRepository
	cfg.CredentialInFlight = DefaultCredentialInFlightConfig()
	if err = yaml.Unmarshal(data, &cfg); err != nil {
		if optional {

			cfgOptional := &Config{CredentialInFlight: DefaultCredentialInFlightConfig()}
			cfgOptional.NormalizePluginsConfig()
			cfgOptional.NormalizeClaudeConfig()
			return cfgOptional, nil
		}
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	if errValidate := validateTrustedProxies(cfg.TrustedProxies); errValidate != nil {
		return nil, errValidate
	}

	cfg.CredentialConcurrency = cfg.CredentialConcurrency.WithDefaults()
	if errValidate := cfg.CredentialInFlight.Validate(); errValidate != nil {
		return nil, errValidate
	}
	if cfg.Discovery.ServiceType == "" {
		cfg.Discovery.ServiceType = DefaultDiscoveryServiceType
	}
	if len(cfg.Discovery.Subtypes) == 0 {
		cfg.Discovery.Subtypes = []string{"_chat-completions", "_responses", "_messages", "_generate-content", "_interactions"}
	}
	if errValidate := cfg.Codex.LiveMediaRelay.Validate(); errValidate != nil {
		return nil, errValidate
	}
	if errValidate := cfg.ValidateCredentialWeights(); errValidate != nil {
		return nil, errValidate
	}

	if cfg.RemoteManagement.SecretKey != "" && !looksLikeBcrypt(cfg.RemoteManagement.SecretKey) {
		hashed, errHash := hashSecret(cfg.RemoteManagement.SecretKey)
		if errHash != nil {
			return nil, fmt.Errorf("failed to hash remote management key: %w", errHash)
		}
		cfg.RemoteManagement.SecretKey = hashed

		_ = SaveConfigPreserveCommentsUpdateNestedScalar(configFile, managementSecretKeyPath(data), hashed)
	}

	cfg.RemoteManagement.PanelGitHubRepository = strings.TrimSpace(cfg.RemoteManagement.PanelGitHubRepository)
	if cfg.RemoteManagement.PanelGitHubRepository == "" {
		cfg.RemoteManagement.PanelGitHubRepository = DefaultPanelGitHubRepository
	}

	cfg.Pprof.Addr = strings.TrimSpace(cfg.Pprof.Addr)
	if cfg.Pprof.Addr == "" {
		cfg.Pprof.Addr = DefaultPprofAddr
	}

	cfg.KVStore.Path = strings.TrimSpace(cfg.KVStore.Path)

	if cfg.LogsMaxTotalSizeMB < 0 {
		cfg.LogsMaxTotalSizeMB = 0
	}

	if cfg.ErrorLogsMaxFiles < 0 {
		cfg.ErrorLogsMaxFiles = 10
	}

	if cfg.RedisUsageQueueRetentionSeconds <= 0 {
		cfg.RedisUsageQueueRetentionSeconds = 60
	} else if cfg.RedisUsageQueueRetentionSeconds > 3600 {
		log.WithField("value", cfg.RedisUsageQueueRetentionSeconds).Warn("redis-usage-queue-retention-seconds too large; clamping to 3600")
		cfg.RedisUsageQueueRetentionSeconds = 3600
	}

	if cfg.MaxRetryCredentials < 0 {
		cfg.MaxRetryCredentials = 0
	}

	cfg.NormalizePluginsConfig()
	cfg.NormalizeClaudeConfig()
	if errResolvePluginsDir := cfg.ResolvePluginsDir(); errResolvePluginsDir != nil && cfg.Plugins.Enabled {
		return nil, errResolvePluginsDir
	}

	cfg.SanitizeGeminiKeys()

	cfg.SanitizeInteractionsKeys()

	cfg.SanitizeVertexCompatKeys()

	cfg.SanitizeCodexKeys()

	cfg.SanitizeXAIKeys()

	cfg.SanitizeMetaKeys()

	cfg.SanitizeCodexHeaderDefaults()

	cfg.SanitizeClaudeHeaderDefaults()

	cfg.SanitizeClaudeKeys()

	cfg.SanitizeAntigravity()
	cfg.SanitizeDevin()

	cfg.SanitizeOpenAICompatibility()

	cfg.OAuthExcludedModels = NormalizeOAuthExcludedModels(cfg.OAuthExcludedModels)

	cfg.SanitizeOAuthModelAlias()

	cfg.SanitizeOAuthSettings()

	cfg.SanitizeOAuthRequestScopedErrors()

	cfg.SanitizePayloadRules()

	current, errRead := os.ReadFile(configFile)
	if errRead != nil {
		return nil, errRead
	}
	cleaned, changed, errLayout := NormalizeConfigLayout(current, false)
	if errLayout != nil {
		return nil, errLayout
	}
	if changed {
		if errWrite := os.WriteFile(configFile, cleaned, 0600); errWrite != nil {
			return nil, fmt.Errorf("clean conflicting config fields: %w", errWrite)
		}
	}

	cfg.SourceHash = ComputeSourceHash(data)

	return &cfg, nil
}
