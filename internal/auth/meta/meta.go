package meta

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	log "github.com/sirupsen/logrus"
)

const (
	DefaultAPIBaseURL = "https://api.meta.ai/v1"

	AuthHost = "https://auth.meta.com"

	DeviceAuthorizationEndpoint = AuthHost + "/oidc/device/authorization/"

	TokenEndpoint = AuthHost + "/oidc/device/token/"

	ClientID = "1031625952748946"

	DeviceCodeGrantType = "urn:ietf:params:oauth:grant-type:device_code"

	DefaultPollInterval = 5 * time.Second

	MaxPollDuration = 15 * time.Minute

	httpClientTimeout = 30 * time.Second
)

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
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in,omitempty"`
	ExpiresAt        int64  `json:"expires_at,omitempty"`
	Error            string `json:"error,omitempty"`
	ErrorDescription string `json:"error_description,omitempty"`
}

type MintedKeyResponse struct {
	APIKey           string `json:"api_key"`
	BaseURL          string `json:"base_url"`
	UserEmail        string `json:"user_email"`
	UserFullName     string `json:"user_full_name"`
	SubsTierName     string `json:"subs_tier_name"`
	SubsTierID       string `json:"subs_tier_id"`
	IsSubsActive     bool   `json:"is_subs_active"`
	HasPaymentMethod bool   `json:"has_payment_method"`
	RequirePayment   bool   `json:"require_payment"`
	CanSubscribe     bool   `json:"can_subscribe"`
}

type MetaAuthBundle struct {
	TokenData *TokenData
	MintedKey *MintedKeyResponse
	Email     string
	Name      string
}

type MetaTokenStorage struct {
	Type         string `json:"type"`
	AuthKind     string `json:"auth_kind"`
	AccessToken  string `json:"access_token"`
	DCAToken     string `json:"dca_token,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	Expired      string `json:"expired,omitempty"`
	DCAExpired   string `json:"dca_expired,omitempty"`
	DCAExpiresAt int64  `json:"dca_expires_at,omitempty"`
	LastRefresh  string `json:"last_refresh,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
	Email        string `json:"email,omitempty"`
	Name         string `json:"name,omitempty"`

	Metadata map[string]any `json:"-"`
}

func (ts *MetaTokenStorage) SetMetadata(meta map[string]any) {
	ts.Metadata = meta
}

var metaCredentialFields = map[string]struct{}{
	"type":           {},
	"auth_kind":      {},
	"access_token":   {},
	"token_type":     {},
	"dca_token":      {},
	"api_key":        {},
	"expires_in":     {},
	"expired":        {},
	"dca_expired":    {},
	"dca_expires_at": {},
	"last_refresh":   {},
}

func (ts *MetaTokenStorage) SaveTokenToFile(authFilePath string) error {
	ts.Type = "meta"
	ts.AuthKind = "oauth"
	if errMkdirAll := os.MkdirAll(filepath.Dir(authFilePath), 0o700); errMkdirAll != nil {
		return fmt.Errorf("meta token storage: create directory: %w", errMkdirAll)
	}

	data := map[string]any{
		"type":         "meta",
		"auth_kind":    "oauth",
		"access_token": ts.AccessToken,
	}
	if ts.DCAToken != "" {
		data["dca_token"] = ts.DCAToken
	}
	if ts.APIKey != "" {
		data["api_key"] = ts.APIKey
	}
	if ts.TokenType != "" {
		data["token_type"] = ts.TokenType
	}
	if ts.ExpiresIn > 0 {
		data["expires_in"] = ts.ExpiresIn
	}
	if ts.Expired != "" {
		data["expired"] = ts.Expired
	}
	if ts.DCAExpired != "" {
		data["dca_expired"] = ts.DCAExpired
	}
	if ts.DCAExpiresAt > 0 {
		data["dca_expires_at"] = ts.DCAExpiresAt
	}
	if ts.LastRefresh != "" {
		data["last_refresh"] = ts.LastRefresh
	}
	if ts.BaseURL != "" {
		data["base_url"] = ts.BaseURL
	}
	if ts.Email != "" {
		data["email"] = ts.Email
	}
	if ts.Name != "" {
		data["name"] = ts.Name
	}

	metadata := ts.Metadata
	if metadata == nil {
		if raw, errRead := os.ReadFile(authFilePath); errRead == nil {
			_ = json.Unmarshal(raw, &metadata)
		}
	}
	for k, v := range metadata {
		if _, isCred := metaCredentialFields[k]; isCred {
			continue
		}

		if _, exists := data[k]; !exists || ts.Metadata != nil {
			data[k] = v
		}
	}

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("meta token storage: encode token file: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(authFilePath), ".meta-token-*")
	if err != nil {
		return fmt.Errorf("meta token storage: create token file: %w", err)
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, errWrite := file.Write(append(raw, '\n')); errWrite != nil {
		_ = file.Close()
		return fmt.Errorf("meta token storage: write token file: %w", errWrite)
	}
	if errClose := file.Close(); errClose != nil {
		return fmt.Errorf("meta token storage: close token file: %w", errClose)
	}
	if errRename := os.Rename(file.Name(), authFilePath); errRename != nil {
		return fmt.Errorf("meta token storage: replace token file: %w", errRename)
	}

	return nil
}

type MetaAuth struct {
	cfg        *config.Config
	httpClient *http.Client
	proxyURL   string
	mintURL    string
}

func (a *MetaAuth) SetMintURL(u string) {
	a.mintURL = strings.TrimSpace(u)
}

func NewMetaAuth(cfg *config.Config) *MetaAuth {
	return NewMetaAuthWithProxyURL(cfg, "")
}

func NewMetaAuthWithProxyURL(cfg *config.Config, proxyURL string) *MetaAuth {
	effectiveProxyURL := strings.TrimSpace(proxyURL)
	var sdkCfg config.SDKConfig
	if cfg != nil {
		sdkCfg = cfg.SDKConfig
		if effectiveProxyURL == "" {
			effectiveProxyURL = strings.TrimSpace(cfg.ProxyURL)
		}
	}
	sdkCfg.ProxyURL = effectiveProxyURL
	return &MetaAuth{
		cfg:        cfg,
		httpClient: util.SetProxy(&sdkCfg, &http.Client{Timeout: httpClientTimeout}),
		proxyURL:   proxyURL,
	}
}

func (a *MetaAuth) StartDeviceFlow(ctx context.Context) (*DeviceCodeResponse, error) {
	return a.StartDeviceFlowWithEndpoint(ctx, DeviceAuthorizationEndpoint)
}

func (a *MetaAuth) StartDeviceFlowWithEndpoint(ctx context.Context, endpoint string) (*DeviceCodeResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = DeviceAuthorizationEndpoint
	}

	form := url.Values{
		"client_id": {ClientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("meta device flow: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "muse-code/1.0.2")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("meta device flow: request failed: %w", err)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("meta device flow: close response body error: %v", errClose)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("meta device flow: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("meta device flow failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var dcr DeviceCodeResponse
	if err := json.Unmarshal(body, &dcr); err != nil {
		return nil, fmt.Errorf("meta device flow: parse response: %w", err)
	}
	if strings.TrimSpace(dcr.DeviceCode) == "" || strings.TrimSpace(dcr.UserCode) == "" {
		return nil, fmt.Errorf("meta device flow: response missing required device_code or user_code")
	}
	dcr.TokenEndpoint = TokenEndpoint
	return &dcr, nil
}

func (a *MetaAuth) WaitForAuthorization(ctx context.Context, dcr *DeviceCodeResponse) (*MetaAuthBundle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if dcr == nil || dcr.DeviceCode == "" {
		return nil, fmt.Errorf("meta auth: missing device code response")
	}

	tokenEndpoint := dcr.TokenEndpoint
	if tokenEndpoint == "" {
		tokenEndpoint = TokenEndpoint
	}

	interval := time.Duration(dcr.Interval) * time.Second
	if interval <= 0 {
		interval = DefaultPollInterval
	}

	maxDuration := MaxPollDuration
	if dcr.ExpiresIn > 0 {
		expiresDuration := time.Duration(dcr.ExpiresIn) * time.Second
		if expiresDuration < maxDuration {
			maxDuration = expiresDuration
		}
	}

	ctx, cancel := context.WithTimeout(ctx, maxDuration)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("meta auth: authorization timed out or canceled: %w", ctx.Err())
		case <-ticker.C:
			form := url.Values{
				"grant_type":  {DeviceCodeGrantType},
				"device_code": {dcr.DeviceCode},
				"client_id":   {ClientID},
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
			if err != nil {
				return nil, fmt.Errorf("meta auth: create token request: %w", err)
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Accept", "application/json")
			req.Header.Set("User-Agent", "muse-code/1.0.2")

			resp, err := a.httpClient.Do(req)
			if err != nil {
				log.Warnf("meta auth: poll request error: %v (retrying)", err)
				continue
			}

			body, errRead := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if errRead != nil {
				log.Warnf("meta auth: read token response error: %v (retrying)", errRead)
				continue
			}

			if resp.StatusCode == http.StatusOK {
				var tokenData TokenData
				if err := json.Unmarshal(body, &tokenData); err != nil {
					return nil, fmt.Errorf("meta auth: parse token response: %w", err)
				}
				if tokenData.AccessToken == "" {
					return nil, fmt.Errorf("meta auth: response missing access_token")
				}
				if tokenData.ExpiresIn > 0 {
					tokenData.ExpiresAt = time.Now().Add(time.Duration(tokenData.ExpiresIn) * time.Second).Unix()
				}
				bundle := &MetaAuthBundle{
					TokenData: &tokenData,
				}
				minted, errMint := a.MintAPIKey(ctx, tokenData.AccessToken)
				if errMint != nil {
					log.Warnf("meta auth: could not mint api_key from dca_token: %v", errMint)
				} else if minted != nil {
					bundle.MintedKey = minted
					if minted.UserEmail != "" {
						bundle.Email = minted.UserEmail
					}
					if minted.UserFullName != "" {
						bundle.Name = minted.UserFullName
					}
				}
				return bundle, nil
			}

			var errResp TokenData
			_ = json.Unmarshal(body, &errResp)
			switch errResp.Error {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 5 * time.Second
				ticker.Reset(interval)
				continue
			case "access_denied":
				return nil, fmt.Errorf("meta auth: access was denied by user")
			case "expired_token":
				return nil, fmt.Errorf("meta auth: device code has expired")
			default:
				if errResp.Error != "" {
					return nil, fmt.Errorf("meta auth: error from authorization server: %s: %s", errResp.Error, errResp.ErrorDescription)
				}
				log.Warnf("meta auth: unexpected response %d: %s", resp.StatusCode, string(body))
			}
		}
	}
}

func (a *MetaAuth) MintAPIKey(ctx context.Context, dcaToken string) (*MintedKeyResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	dcaToken = strings.TrimSpace(dcaToken)
	if dcaToken == "" {
		return nil, fmt.Errorf("meta auth: missing dca token")
	}

	reqBody, _ := json.Marshal(map[string]string{
		"dca_token": dcaToken,
	})

	mintURL := a.mintURL
	if mintURL == "" {
		if envMint := strings.TrimSpace(os.Getenv("META_MINT_URL")); envMint != "" {
			mintURL = envMint
		} else {
			mintURL = "https://api.meta.ai/muse-code/key"
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mintURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("meta auth: create mint request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+dcaToken)
	req.Header.Set("User-Agent", "muse-code/1.0.2")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("meta auth: mint request failed: %w", err)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("meta auth: close mint response body error: %v", errClose)
		}
	}()

	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return nil, fmt.Errorf("meta auth: read mint response: %w", errRead)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("meta auth: mint key failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var minted MintedKeyResponse
	if err := json.Unmarshal(body, &minted); err != nil {
		return nil, fmt.Errorf("meta auth: parse mint response: %w", err)
	}
	if strings.TrimSpace(minted.APIKey) == "" {
		return nil, fmt.Errorf("meta auth: mint response missing api_key")
	}
	return &minted, nil
}

func (a *MetaAuth) CreateTokenStorage(bundle *MetaAuthBundle) *MetaTokenStorage {
	if bundle == nil || bundle.TokenData == nil {
		return nil
	}
	dcaExpired := ""
	if bundle.TokenData.ExpiresAt > 0 {
		dcaExpired = time.Unix(bundle.TokenData.ExpiresAt, 0).UTC().Format(time.RFC3339)
	}
	apiKey := ""
	baseURL := DefaultAPIBaseURL
	email := bundle.Email
	name := bundle.Name
	if bundle.MintedKey != nil {
		apiKey = bundle.MintedKey.APIKey
		if mintedURL := strings.TrimSpace(bundle.MintedKey.BaseURL); mintedURL != "" {
			baseURL = mintedURL
		}
		if bundle.MintedKey.UserEmail != "" {
			email = bundle.MintedKey.UserEmail
		}
		if bundle.MintedKey.UserFullName != "" {
			name = bundle.MintedKey.UserFullName
		}
	}

	accessToken := bundle.TokenData.AccessToken
	expired := ""
	if apiKey != "" {
		accessToken = apiKey

	} else {

		expired = dcaExpired
	}

	return &MetaTokenStorage{
		Type:         "meta",
		AuthKind:     "oauth",
		AccessToken:  accessToken,
		DCAToken:     bundle.TokenData.AccessToken,
		APIKey:       apiKey,
		TokenType:    bundle.TokenData.TokenType,
		ExpiresIn:    bundle.TokenData.ExpiresIn,
		Expired:      expired,
		DCAExpired:   dcaExpired,
		DCAExpiresAt: bundle.TokenData.ExpiresAt,
		LastRefresh:  time.Now().UTC().Format(time.RFC3339),
		BaseURL:      baseURL,
		Email:        email,
		Name:         name,
	}
}

func CredentialFileName(email, sub string) string {
	clean := strings.TrimSpace(email)
	if clean != "" {
		sanitized := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
				return r
			}
			return '_'
		}, clean)

		if len(sanitized) > 120 {
			sanitized = sanitized[:120]
		}
		hash := sha256.Sum256([]byte(clean))
		return fmt.Sprintf("meta-%s-%s.json", sanitized, hex.EncodeToString(hash[:8]))
	}
	cleanSub := strings.TrimSpace(sub)
	if cleanSub != "" {
		hash := sha256.Sum256([]byte(cleanSub))
		return fmt.Sprintf("meta-%s.json", hex.EncodeToString(hash[:8]))
	}
	return "meta-oauth.json"
}
