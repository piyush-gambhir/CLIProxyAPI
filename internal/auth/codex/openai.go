package codex

type PKCECodes struct {
	CodeVerifier string `json:"code_verifier"`

	CodeChallenge string `json:"code_challenge"`
}

type CodexTokenData struct {
	IDToken string `json:"id_token"`

	AccessToken string `json:"access_token"`

	RefreshToken string `json:"refresh_token"`

	AccountID string `json:"account_id"`

	Email string `json:"email"`

	Expire string `json:"expired"`

	PlanType string `json:"plan_type,omitempty"`
}

type CodexSIWCTokenData struct {
	AccessToken       string   `json:"access_token"`
	RefreshToken      string   `json:"refresh_token"`
	IDToken           string   `json:"id_token,omitempty"`
	ClientID          string   `json:"client_id"`
	Scope             string   `json:"scope,omitempty"`
	Scopes            []string `json:"scopes,omitempty"`
	Expire            string   `json:"expired"`
	ExpiresAt         string   `json:"expires_at,omitempty"`
	EarliestRefreshAt any      `json:"earliest_refresh_at,omitempty"`
	LastRefresh       string   `json:"last_refresh"`
	DeviceID          string   `json:"device_id,omitempty"`
}

func (d *CodexSIWCTokenData) ToMap() map[string]any {
	if d == nil {
		return nil
	}
	out := map[string]any{
		"access_token":  d.AccessToken,
		"refresh_token": d.RefreshToken,
		"client_id":     d.ClientID,
		"expired":       d.Expire,
		"last_refresh":  d.LastRefresh,
	}
	if d.ExpiresAt != "" {
		out["expires_at"] = d.ExpiresAt
	}
	if d.EarliestRefreshAt != nil {
		out["earliest_refresh_at"] = d.EarliestRefreshAt
	}
	if d.IDToken != "" {
		out["id_token"] = d.IDToken
	}
	if d.Scope != "" {
		out["scope"] = d.Scope
	}
	if len(d.Scopes) > 0 {
		scopesCopy := make([]string, len(d.Scopes))
		copy(scopesCopy, d.Scopes)
		out["scopes"] = scopesCopy
	}
	if d.DeviceID != "" {
		out["device_id"] = d.DeviceID
	}
	return out
}

type CodexAuthBundle struct {
	APIKey string `json:"api_key"`

	TokenData CodexTokenData `json:"token_data"`

	LastRefresh string `json:"last_refresh"`
}
