package claude

type PKCECodes struct {
	CodeVerifier string `json:"code_verifier"`

	CodeChallenge string `json:"code_challenge"`
}

type ClaudeTokenData struct {
	AccessToken string `json:"access_token"`

	RefreshToken string `json:"refresh_token"`

	Email string `json:"email"`

	AccountUUID string `json:"account_uuid"`

	OrganizationUUID string `json:"organization_uuid"`

	OrganizationName string `json:"organization_name"`

	Expire string `json:"expired"`
}

type ClaudeAuthBundle struct {
	APIKey string `json:"api_key"`

	TokenData ClaudeTokenData `json:"token_data"`

	DeviceIDs []string `json:"claude_device_ids"`

	LastRefresh string `json:"last_refresh"`
}
