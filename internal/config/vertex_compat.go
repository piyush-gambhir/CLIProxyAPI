package config

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)







type VertexCompatKey struct {


	APIKey string `yaml:"api-key" json:"api-key"`



	Priority int `yaml:"priority,omitempty" json:"priority,omitempty"`



	Weight *int `yaml:"weight,omitempty" json:"weight,omitempty"`


	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`







	BaseURL string `yaml:"base-url,omitempty" json:"base-url,omitempty"`


	ProxyURL string `yaml:"proxy-url,omitempty" json:"proxy-url,omitempty"`




	ConnectionPool *ProxyConnectionPoolConfig `yaml:"connection-pool,omitempty" json:"connection-pool,omitempty"`



	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`


	Models []VertexCompatModel `yaml:"models,omitempty" json:"models,omitempty"`


	ExcludedModels []string `yaml:"excluded-models,omitempty" json:"excluded-models,omitempty"`



	DisableCooling *bool `yaml:"disable-cooling,omitempty" json:"disable-cooling,omitempty"`



	RequestRetry *int `yaml:"request-retry,omitempty" json:"request-retry,omitempty"`


	Interactions *bool `yaml:"interactions,omitempty" json:"interactions,omitempty"`


	OpenAPI *bool `yaml:"openapi,omitempty" json:"openapi,omitempty"`


	OpenAPILocation string `yaml:"openapi-location,omitempty" json:"openapi-location,omitempty"`
}

func (k VertexCompatKey) GetAPIKey() string   { return k.APIKey }
func (k VertexCompatKey) GetBaseURL() string  { return k.BaseURL }
func (k VertexCompatKey) GetPrefix() string   { return k.Prefix }
func (k VertexCompatKey) GetProxyURL() string { return k.ProxyURL }



type VertexCompatModel struct {

	Name string `yaml:"name" json:"name"`


	Alias string `yaml:"alias" json:"alias"`


	DisplayName string `yaml:"display-name,omitempty" json:"display-name,omitempty"`


	ForceMapping bool `yaml:"force-mapping,omitempty" json:"force-mapping,omitempty"`


	OpenAPI *bool `yaml:"openapi,omitempty" json:"openapi,omitempty"`


	Thinking *registry.ThinkingSupport `yaml:"thinking,omitempty" json:"thinking,omitempty"`
}

func (m VertexCompatModel) GetOpenAPI() *bool { return m.OpenAPI }

func (m VertexCompatModel) GetName() string        { return m.Name }
func (m VertexCompatModel) GetAlias() string       { return m.Alias }
func (m VertexCompatModel) GetDisplayName() string { return m.DisplayName }
func (m VertexCompatModel) GetForceMapping() bool  { return m.ForceMapping }
func (m VertexCompatModel) GetThinking() *registry.ThinkingSupport {
	return m.Thinking
}


func (cfg *Config) SanitizeVertexCompatKeys() {
	if cfg == nil {
		return
	}

	seen := make(map[string]struct{}, len(cfg.VertexCompatAPIKey))
	out := cfg.VertexCompatAPIKey[:0]
	for i := range cfg.VertexCompatAPIKey {
		entry := cfg.VertexCompatAPIKey[i]
		entry.APIKey = strings.TrimSpace(entry.APIKey)
		if entry.APIKey == "" {
			continue
		}
		entry.Prefix = normalizeModelPrefix(entry.Prefix)
		entry.BaseURL = strings.TrimSpace(entry.BaseURL)
		entry.ProxyURL = strings.TrimSpace(entry.ProxyURL)
		entry.OpenAPILocation = strings.TrimSpace(entry.OpenAPILocation)
		entry.Headers = NormalizeHeaders(entry.Headers)
		entry.ExcludedModels = NormalizeExcludedModels(entry.ExcludedModels)


		sanitizedModels := make([]VertexCompatModel, 0, len(entry.Models))
		for _, model := range entry.Models {
			model.Alias = strings.TrimSpace(model.Alias)
			model.Name = strings.TrimSpace(model.Name)
			if model.Name != "" {
				sanitizedModels = append(sanitizedModels, model)
			}
		}
		entry.Models = sanitizedModels


		uniqueKey := entry.APIKey + "|" + entry.BaseURL + "\x00" + FormatConnectionPoolDedupID(entry.ConnectionPool)
		if _, exists := seen[uniqueKey]; exists {
			continue
		}
		seen[uniqueKey] = struct{}{}
		out = append(out, entry)
	}
	cfg.VertexCompatAPIKey = out
}
