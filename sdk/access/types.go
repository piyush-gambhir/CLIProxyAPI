package access


type AccessConfig struct {

	Providers []AccessProvider `yaml:"providers,omitempty" json:"providers,omitempty"`
}


type AccessProvider struct {

	Name string `yaml:"name" json:"name"`


	Type string `yaml:"type" json:"type"`


	SDK string `yaml:"sdk,omitempty" json:"sdk,omitempty"`


	APIKeys []string `yaml:"api-keys,omitempty" json:"api-keys,omitempty"`


	Config map[string]any `yaml:"config,omitempty" json:"config,omitempty"`
}

const (

	AccessProviderTypeConfigAPIKey = "config-api-key"


	DefaultAccessProviderName = "config-inline"
)



func MakeInlineAPIKeyProvider(keys []string) *AccessProvider {
	if len(keys) == 0 {
		return nil
	}
	provider := &AccessProvider{
		Name:    DefaultAccessProviderName,
		Type:    AccessProviderTypeConfigAPIKey,
		APIKeys: append([]string(nil), keys...),
	}
	return provider
}
