package xai

import "time"

const (
	DefaultAPIBaseURL = "https://api.x.ai/v1"

	CLIChatProxyBaseURL = "https://cli-chat-proxy.grok.com/v1"

	Issuer = "https://auth.x.ai"

	DiscoveryURL = Issuer + "/.well-known/openid-configuration"

	ClientID = "b1a00492-073a-47ea-816f-4c329264a828"

	Scope = "openid profile email offline_access grok-cli:access api:access"

	DeviceCodeGrantType = "urn:ietf:params:oauth:grant-type:device_code"

	defaultPollInterval = 5 * time.Second

	httpClientTimeout = 30 * time.Second

	MaxPollDuration = 30 * time.Minute
)

var refreshLead = 5 * time.Minute

func RefreshLead() time.Duration {
	return refreshLead
}

type Discovery struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
	TokenEndpoint           string `json:"-"`
}

type TokenData struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	Expire       string `json:"expired,omitempty"`
	Email        string `json:"email,omitempty"`
	Subject      string `json:"sub,omitempty"`
}

type AuthBundle struct {
	TokenData     TokenData
	LastRefresh   string
	BaseURL       string
	RedirectURI   string
	TokenEndpoint string
}
