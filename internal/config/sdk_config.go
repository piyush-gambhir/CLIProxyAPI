package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type SDKConfig struct {
	MaxRequestBodyBytes int64 `yaml:"max-request-body-bytes" json:"max-request-body-bytes"`

	ResponseCompression ResponseCompressionConfig `yaml:"response-compression" json:"-"`

	Client ClientConfig `yaml:"client" json:"client"`

	OAuthOnlyFields map[string]bool `yaml:"-" json:"-"`

	CodexResponseSteering bool `yaml:"-" json:"-"`

	ProxyURL string `yaml:"proxy-url" json:"proxy-url"`

	TLSHandshakeTimeoutSeconds int `yaml:"tls-handshake-timeout-seconds" json:"tls-handshake-timeout-seconds"`

	DisableImageGeneration DisableImageGenerationMode `yaml:"disable-image-generation" json:"disable-image-generation"`

	GPTImage2BaseModel string `yaml:"gpt-image-2-base-model,omitempty" json:"gpt-image-2-base-model,omitempty"`

	VideoResultAuthCacheTTL string `yaml:"video-result-auth-cache-ttl,omitempty" json:"video-result-auth-cache-ttl,omitempty"`

	ForceModelPrefix bool `yaml:"force-model-prefix" json:"force-model-prefix"`

	RequestLog bool `yaml:"request-log" json:"request-log"`

	CodexOrphanDelegationCompatibility bool `yaml:"-" json:"-"`

	ClaudeCode ClaudeCodeConfig `yaml:"claude-code" json:"claude-code"`

	APIKeys []string `yaml:"api-keys" json:"api-keys"`

	PassthroughHeaders bool `yaml:"passthrough-headers" json:"passthrough-headers"`

	Streaming StreamingConfig `yaml:"streaming" json:"streaming"`

	NonStreamKeepAliveInterval int `yaml:"nonstream-keepalive-interval,omitempty" json:"nonstream-keepalive-interval,omitempty"`
}

func (c *SDKConfig) TLSHandshakeTimeout() time.Duration {
	if c == nil || c.TLSHandshakeTimeoutSeconds <= 0 {
		return 0
	}
	const maxTLSHandshakeTimeoutSeconds = math.MaxInt64 / int64(time.Second)
	if int64(c.TLSHandshakeTimeoutSeconds) > maxTLSHandshakeTimeoutSeconds {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(c.TLSHandshakeTimeoutSeconds) * time.Second
}

type ClientConfig struct {
	Codex  CodexClientConfig  `yaml:"codex" json:"codex"`
	Claude ClaudeClientConfig `yaml:"claude" json:"claude"`
}

type ClaudeClientConfig struct {
	ShowCodexCommentary bool `yaml:"show-codex-commentary" json:"show-codex-commentary"`
}

type CodexClientConfig struct {
	OptimizeMultiAgentV2 bool `yaml:"optimize-multi-agent-v2" json:"optimize-multi-agent-v2"`

	EnableApplyPatch bool `yaml:"enable-apply-patch" json:"enable-apply-patch"`

	WebSearchResultCache CodexWebSearchResultCacheConfig `yaml:"web-search-result-cache" json:"web-search-result-cache"`
}

const (
	DefaultCodexWebSearchResultCacheTTL = 24 * time.Hour

	DefaultCodexWebSearchResultCacheLimit = 1024

	maxCodexWebSearchResultCacheTTLSeconds = int64(math.MaxInt64 / time.Second)
)

type CodexWebSearchResultCacheConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`

	TTL string `yaml:"ttl" json:"ttl"`

	Limit int `yaml:"limit" json:"limit"`
}

func (c CodexWebSearchResultCacheConfig) Validate() error {
	if _, err := parseCodexWebSearchResultCacheTTL(c.TTL); err != nil {
		return err
	}
	if c.Limit < 0 {
		return fmt.Errorf("client.codex.web-search-result-cache.limit must not be negative")
	}
	return nil
}

func (c CodexWebSearchResultCacheConfig) EffectiveTTL() time.Duration {
	d, err := parseCodexWebSearchResultCacheTTL(c.TTL)
	if err != nil || d <= 0 {
		return DefaultCodexWebSearchResultCacheTTL
	}
	return d
}

func (c CodexWebSearchResultCacheConfig) EffectiveLimit() int {
	if c.Limit > 0 {
		return c.Limit
	}
	return DefaultCodexWebSearchResultCacheLimit
}

func (c *CodexWebSearchResultCacheConfig) UnmarshalYAML(value *yaml.Node) error {
	if c == nil {
		return nil
	}
	if value == nil || value.Kind == 0 || value.Tag == "!!null" {
		*c = CodexWebSearchResultCacheConfig{}
		return nil
	}
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("client.codex.web-search-result-cache must be a mapping")
	}
	var next CodexWebSearchResultCacheConfig
	seen := make(map[string]bool, len(value.Content)/2)
	for i := 0; i+1 < len(value.Content); i += 2 {
		keyNode := value.Content[i]
		valNode := value.Content[i+1]
		if keyNode == nil || valNode == nil {
			continue
		}
		if seen[keyNode.Value] {
			return fmt.Errorf("client.codex.web-search-result-cache: duplicate field %q", keyNode.Value)
		}
		seen[keyNode.Value] = true
		switch keyNode.Value {
		case "enabled":
			if valNode.Tag == "!!null" {
				next.Enabled = false
				continue
			}
			if valNode.Kind != yaml.ScalarNode || valNode.Tag != "!!bool" {
				return fmt.Errorf("client.codex.web-search-result-cache.enabled must be a boolean")
			}
			var enabled bool
			if errDecode := valNode.Decode(&enabled); errDecode != nil {
				return fmt.Errorf("client.codex.web-search-result-cache.enabled: %w", errDecode)
			}
			next.Enabled = enabled
		case "ttl":
			if valNode.Tag == "!!null" {
				next.TTL = ""
				continue
			}
			if valNode.Kind != yaml.ScalarNode || (valNode.Tag != "!!str" && valNode.Tag != "!!int") {
				return fmt.Errorf("client.codex.web-search-result-cache.ttl must be a duration string or integer seconds")
			}
			next.TTL = strings.TrimSpace(valNode.Value)
		case "limit":
			if valNode.Tag == "!!null" {
				next.Limit = 0
				continue
			}
			if valNode.Kind != yaml.ScalarNode || valNode.Tag != "!!int" {
				return fmt.Errorf("client.codex.web-search-result-cache.limit must be an integer")
			}
			var limit int
			if errDecode := valNode.Decode(&limit); errDecode != nil {
				return fmt.Errorf("client.codex.web-search-result-cache.limit: %w", errDecode)
			}
			next.Limit = limit
		default:
			return fmt.Errorf("client.codex.web-search-result-cache: unknown field %q", keyNode.Value)
		}
	}
	if errValidate := next.Validate(); errValidate != nil {
		return errValidate
	}
	*c = next
	return nil
}

func (c *CodexWebSearchResultCacheConfig) UnmarshalJSON(data []byte) error {
	if c == nil {
		return nil
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*c = CodexWebSearchResultCacheConfig{}
		return nil
	}
	var raw struct {
		Enabled bool            `json:"enabled"`
		TTL     json.RawMessage `json:"ttl"`
		Limit   int             `json:"limit"`
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if errDecode := decoder.Decode(&raw); errDecode != nil {
		return errDecode
	}
	ttl, errTTL := parseCodexWebSearchResultCacheTTLJSON(raw.TTL)
	if errTTL != nil {
		return errTTL
	}
	next := CodexWebSearchResultCacheConfig{
		Enabled: raw.Enabled,
		TTL:     ttl,
		Limit:   raw.Limit,
	}
	if errValidate := next.Validate(); errValidate != nil {
		return errValidate
	}
	*c = next
	return nil
}

func parseCodexWebSearchResultCacheTTLJSON(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	if trimmed[0] == '"' {
		var s string
		if errUnmarshal := json.Unmarshal(trimmed, &s); errUnmarshal != nil {
			return "", fmt.Errorf("client.codex.web-search-result-cache.ttl: %w", errUnmarshal)
		}
		return strings.TrimSpace(s), nil
	}
	var n int64
	if errUnmarshal := json.Unmarshal(trimmed, &n); errUnmarshal != nil {
		return "", fmt.Errorf("client.codex.web-search-result-cache.ttl must be a duration string or integer seconds")
	}
	return strconv.FormatInt(n, 10), nil
}

func parseCodexWebSearchResultCacheTTL(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" {
		return DefaultCodexWebSearchResultCacheTTL, nil
	}
	if secs, errParseInt := strconv.ParseInt(raw, 10, 64); errParseInt == nil {
		if secs < 0 {
			return 0, fmt.Errorf("client.codex.web-search-result-cache.ttl must not be negative")
		}
		if secs > maxCodexWebSearchResultCacheTTLSeconds {
			return 0, fmt.Errorf("client.codex.web-search-result-cache.ttl overflows duration")
		}
		return time.Duration(secs) * time.Second, nil
	}
	d, errParseDuration := time.ParseDuration(raw)
	if errParseDuration != nil {
		return 0, fmt.Errorf("client.codex.web-search-result-cache.ttl %q is invalid: %w", raw, errParseDuration)
	}
	if d < 0 {
		return 0, fmt.Errorf("client.codex.web-search-result-cache.ttl must not be negative")
	}
	if d == 0 {
		return DefaultCodexWebSearchResultCacheTTL, nil
	}
	return d, nil
}

type ClaudeCodeConfig struct {
	DisableCloakingModelList bool `yaml:"disable-cloaking-model-list" json:"disable-cloaking-model-list"`
}

type StreamingConfig struct {
	KeepAliveSeconds int `yaml:"keepalive-seconds,omitempty" json:"keepalive-seconds,omitempty"`

	BootstrapRetries int `yaml:"bootstrap-retries,omitempty" json:"bootstrap-retries,omitempty"`

	SendKeepAliveBeforeFT bool `yaml:"send-keepalive-before-ft,omitempty" json:"send-keepalive-before-ft,omitempty"`
}
