package kimi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/buildinfo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"
)

const (
	kimiClientID = "17e5f671-d194-4dfb-9706-5516cb48c098"

	KimiDefaultDomain = "kimi.com"

	KimiAIDomain = "kimi.ai"

	KimiOAuthHost = "https://auth.kimi.com"

	KimiAIOAuthHost = "https://auth.kimi.ai"

	kimiOAuthHost = KimiOAuthHost

	kimiDeviceCodeURL = kimiOAuthHost + "/api/oauth/device_authorization"

	kimiTokenURL = kimiOAuthHost + "/api/oauth/token"

	KimiAPIBaseURL = "https://api.kimi.com/coding"

	KimiAIAPIBaseURL = "https://api.kimi.ai/coding"

	defaultPollInterval = 5 * time.Second

	maxPollDuration = 15 * time.Minute

	refreshThresholdSeconds = 300
)

var kimiRefreshGroup singleflight.Group

func IsKimiAIDomain(domain string) bool {
	d := strings.ToLower(strings.TrimSpace(domain))
	return d == "kimi.ai" || d == "ai" || d == "kimi-ai" || strings.HasSuffix(d, ".kimi.ai")
}

func IsKimiComDomain(domain string) bool {
	d := strings.ToLower(strings.TrimSpace(domain))
	return d == "kimi.com" || d == "com" || d == "kimi" || strings.HasSuffix(d, ".kimi.com")
}

func isKimiAIHost(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	return host == "kimi.ai" || host == "api.kimi.ai" || host == "auth.kimi.ai" || strings.HasSuffix(host, ".kimi.ai")
}

func isKimiComHost(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	return host == "kimi.com" || host == "api.kimi.com" || host == "auth.kimi.com" || strings.HasSuffix(host, ".kimi.com")
}

func NormalizeKimiDomain(domain string) string {
	if IsKimiAIDomain(domain) {
		return KimiAIDomain
	}
	return KimiDefaultDomain
}

func ResolveKimiOAuthHost(domain string) string {
	if IsKimiAIDomain(domain) {
		return KimiAIOAuthHost
	}
	return KimiOAuthHost
}

func ResolveKimiAPIBaseURL(domain string) string {
	if IsKimiAIDomain(domain) {
		return KimiAIAPIBaseURL
	}
	return KimiAPIBaseURL
}

func ResolveKimiDomainFromAuth(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return KimiDefaultDomain
	}

	if auth.Attributes != nil {
		if dom := auth.Attributes["domain"]; dom != "" {
			if IsKimiAIDomain(dom) {
				return KimiAIDomain
			}
			if IsKimiComDomain(dom) {
				return KimiDefaultDomain
			}
		}
		if bu := auth.Attributes["base_url"]; bu != "" {
			if isKimiAIHost(bu) {
				return KimiAIDomain
			}
			if isKimiComHost(bu) {
				return KimiDefaultDomain
			}
		}
	}

	if auth.Metadata != nil {
		if dom, ok := auth.Metadata["domain"].(string); ok && strings.TrimSpace(dom) != "" {
			if IsKimiAIDomain(dom) {
				return KimiAIDomain
			}
			if IsKimiComDomain(dom) {
				return KimiDefaultDomain
			}
		}
		if bu, ok := auth.Metadata["base_url"].(string); ok && strings.TrimSpace(bu) != "" {
			if isKimiAIHost(bu) {
				return KimiAIDomain
			}
			if isKimiComHost(bu) {
				return KimiDefaultDomain
			}
		}
		if t, ok := auth.Metadata["type"].(string); ok && strings.TrimSpace(t) != "" {
			if IsKimiAIDomain(t) {
				return KimiAIDomain
			}
			if IsKimiComDomain(t) {
				return KimiDefaultDomain
			}
		}
	}

	if storage, ok := auth.Storage.(*KimiTokenStorage); ok && storage != nil {
		if storage.Domain != "" {
			if IsKimiAIDomain(storage.Domain) {
				return KimiAIDomain
			}
			if IsKimiComDomain(storage.Domain) {
				return KimiDefaultDomain
			}
		}
		if storage.BaseURL != "" {
			if isKimiAIHost(storage.BaseURL) {
				return KimiAIDomain
			}
			if isKimiComHost(storage.BaseURL) {
				return KimiDefaultDomain
			}
		}
		if storage.Type != "" {
			if IsKimiAIDomain(storage.Type) {
				return KimiAIDomain
			}
			if IsKimiComDomain(storage.Type) {
				return KimiDefaultDomain
			}
		}
	}

	if auth.Provider != "" {
		if IsKimiAIDomain(auth.Provider) {
			return KimiAIDomain
		}
		if IsKimiComDomain(auth.Provider) {
			return KimiDefaultDomain
		}
	}

	id := strings.ToLower(auth.ID)
	fileName := strings.ToLower(auth.FileName)
	if strings.Contains(id, "kimi-ai") || strings.Contains(id, "kimi.ai") ||
		strings.Contains(fileName, "kimi-ai") || strings.Contains(fileName, "kimi.ai") {
		return KimiAIDomain
	}
	return KimiDefaultDomain
}

func IsKimiAIAuth(auth *cliproxyauth.Auth) bool {
	return IsKimiAIDomain(ResolveKimiDomainFromAuth(auth))
}

type KimiAuth struct {
	deviceClient *DeviceFlowClient
	cfg          *config.Config
	domain       string
}

func NewKimiAuth(cfg *config.Config) *KimiAuth {
	return NewKimiAuthWithDomain(cfg, KimiDefaultDomain)
}

func NewKimiAIAuth(cfg *config.Config) *KimiAuth {
	return NewKimiAuthWithDomain(cfg, KimiAIDomain)
}

func NewKimiAuthWithDomain(cfg *config.Config, domain string) *KimiAuth {
	normDomain := NormalizeKimiDomain(domain)
	return &KimiAuth{
		deviceClient: NewDeviceFlowClientWithDomain(cfg, normDomain),
		cfg:          cfg,
		domain:       normDomain,
	}
}

func (k *KimiAuth) StartDeviceFlow(ctx context.Context) (*DeviceCodeResponse, error) {
	return k.deviceClient.RequestDeviceCode(ctx)
}

func (k *KimiAuth) WaitForAuthorization(ctx context.Context, deviceCode *DeviceCodeResponse) (*KimiAuthBundle, error) {
	tokenData, err := k.deviceClient.PollForToken(ctx, deviceCode)
	if err != nil {
		return nil, err
	}

	return &KimiAuthBundle{
		TokenData: tokenData,
		DeviceID:  k.deviceClient.deviceID,
	}, nil
}

func (k *KimiAuth) CreateTokenStorage(bundle *KimiAuthBundle) *KimiTokenStorage {
	expired := ""
	if bundle.TokenData.ExpiresAt > 0 {
		expired = time.Unix(bundle.TokenData.ExpiresAt, 0).UTC().Format(time.RFC3339)
	}
	tType := "kimi"
	domain := KimiDefaultDomain
	if k != nil && IsKimiAIDomain(k.domain) {
		tType = "kimi-ai"
		domain = KimiAIDomain
	}
	return &KimiTokenStorage{
		AccessToken:  bundle.TokenData.AccessToken,
		RefreshToken: bundle.TokenData.RefreshToken,
		TokenType:    bundle.TokenData.TokenType,
		Scope:        bundle.TokenData.Scope,
		DeviceID:     strings.TrimSpace(bundle.DeviceID),
		Expired:      expired,
		Type:         tType,
		Domain:       domain,
		BaseURL:      ResolveKimiAPIBaseURL(domain),
	}
}

type DeviceFlowClient struct {
	httpClient *http.Client
	cfg        *config.Config
	deviceID   string
	domain     string
	oauthHost  string
}

func NewDeviceFlowClient(cfg *config.Config) *DeviceFlowClient {
	return NewDeviceFlowClientWithDomain(cfg, KimiDefaultDomain)
}

func NewDeviceFlowClientWithDomain(cfg *config.Config, domain string) *DeviceFlowClient {
	return NewDeviceFlowClientWithDomainDeviceIDAndProxyURL(cfg, domain, "", "")
}

func NewDeviceFlowClientWithDeviceID(cfg *config.Config, deviceID string) *DeviceFlowClient {
	return NewDeviceFlowClientWithDomainDeviceIDAndProxyURL(cfg, KimiDefaultDomain, deviceID, "")
}

func NewDeviceFlowClientWithDeviceIDAndProxyURL(cfg *config.Config, deviceID string, proxyURL string) *DeviceFlowClient {
	return NewDeviceFlowClientWithDomainDeviceIDAndProxyURL(cfg, KimiDefaultDomain, deviceID, proxyURL)
}

func NewDeviceFlowClientWithDomainDeviceIDAndProxyURL(cfg *config.Config, domain string, deviceID string, proxyURL string) *DeviceFlowClient {
	client := &http.Client{Timeout: 30 * time.Second}
	effectiveProxyURL := strings.TrimSpace(proxyURL)
	var sdkCfg config.SDKConfig
	if cfg != nil {
		sdkCfg = cfg.SDKConfig
		if effectiveProxyURL == "" {
			effectiveProxyURL = strings.TrimSpace(cfg.ProxyURL)
		}
	}
	sdkCfg.ProxyURL = effectiveProxyURL
	client = util.SetProxy(&sdkCfg, client)

	resolvedDeviceID := strings.TrimSpace(deviceID)
	if resolvedDeviceID == "" {
		resolvedDeviceID = getOrCreateDeviceID()
	}
	normDomain := NormalizeKimiDomain(domain)
	oauthHost := ResolveKimiOAuthHost(normDomain)
	return &DeviceFlowClient{
		httpClient: client,
		cfg:        cfg,
		deviceID:   resolvedDeviceID,
		domain:     normDomain,
		oauthHost:  oauthHost,
	}
}

func (c *DeviceFlowClient) deviceCodeURL() string {
	if c.oauthHost != "" {
		return c.oauthHost + "/api/oauth/device_authorization"
	}
	return ResolveKimiOAuthHost(c.domain) + "/api/oauth/device_authorization"
}

func (c *DeviceFlowClient) tokenURL() string {
	if c.oauthHost != "" {
		return c.oauthHost + "/api/oauth/token"
	}
	return ResolveKimiOAuthHost(c.domain) + "/api/oauth/token"
}

func (c *DeviceFlowClient) SetHTTPClient(client *http.Client) {
	if client != nil {
		c.httpClient = client
	}
}

func getOrCreateDeviceID() string {
	return uuid.New().String()
}

func getDeviceModel() string {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	switch osName {
	case "darwin":
		return fmt.Sprintf("macOS %s", arch)
	case "windows":
		return fmt.Sprintf("Windows %s", arch)
	case "linux":
		return fmt.Sprintf("Linux %s", arch)
	default:
		return fmt.Sprintf("%s %s", osName, arch)
	}
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

func (c *DeviceFlowClient) commonHeaders() map[string]string {
	return map[string]string{
		"X-Msh-Platform":     "CLIProxyAPI",
		"X-Msh-Version":      buildinfo.Version,
		"X-Msh-Device-Name":  getHostname(),
		"X-Msh-Device-Model": getDeviceModel(),
		"X-Msh-Device-Id":    c.deviceID,
	}
}

func (c *DeviceFlowClient) RequestDeviceCode(ctx context.Context) (*DeviceCodeResponse, error) {
	data := url.Values{}
	data.Set("client_id", kimiClientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.deviceCodeURL(), strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("kimi: failed to create device code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	for k, v := range c.commonHeaders() {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kimi: device code request failed: %w", err)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("kimi device code: close body error: %v", errClose)
		}
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kimi: failed to read device code response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kimi: device code request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var deviceCode DeviceCodeResponse
	if err = json.Unmarshal(bodyBytes, &deviceCode); err != nil {
		return nil, fmt.Errorf("kimi: failed to parse device code response: %w", err)
	}

	return &deviceCode, nil
}

func (c *DeviceFlowClient) PollForToken(ctx context.Context, deviceCode *DeviceCodeResponse) (*KimiTokenData, error) {
	if deviceCode == nil {
		return nil, fmt.Errorf("kimi: device code is nil")
	}

	interval := time.Duration(deviceCode.Interval) * time.Second
	if interval < defaultPollInterval {
		interval = defaultPollInterval
	}

	deadline := time.Now().Add(maxPollDuration)
	if deviceCode.ExpiresIn > 0 {
		codeDeadline := time.Now().Add(time.Duration(deviceCode.ExpiresIn) * time.Second)
		if codeDeadline.Before(deadline) {
			deadline = codeDeadline
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("kimi: context cancelled: %w", ctx.Err())
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("kimi: device code expired")
			}

			token, pollErr, shouldContinue := c.exchangeDeviceCode(ctx, deviceCode.DeviceCode)
			if token != nil {
				return token, nil
			}
			if !shouldContinue {
				return nil, pollErr
			}

		}
	}
}

func (c *DeviceFlowClient) exchangeDeviceCode(ctx context.Context, deviceCode string) (*KimiTokenData, error, bool) {
	data := url.Values{}
	data.Set("client_id", kimiClientID)
	data.Set("device_code", deviceCode)
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL(), strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("kimi: failed to create token request: %w", err), false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	for k, v := range c.commonHeaders() {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kimi: token request failed: %w", err), false
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("kimi token exchange: close body error: %v", errClose)
		}
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kimi: failed to read token response: %w", err), false
	}

	var oauthResp struct {
		Error            string  `json:"error"`
		ErrorDescription string  `json:"error_description"`
		AccessToken      string  `json:"access_token"`
		RefreshToken     string  `json:"refresh_token"`
		TokenType        string  `json:"token_type"`
		ExpiresIn        float64 `json:"expires_in"`
		Scope            string  `json:"scope"`
	}

	if err = json.Unmarshal(bodyBytes, &oauthResp); err != nil {
		return nil, fmt.Errorf("kimi: failed to parse token response: %w", err), false
	}

	if oauthResp.Error != "" {
		switch oauthResp.Error {
		case "authorization_pending":
			return nil, nil, true
		case "slow_down":
			return nil, nil, true
		case "expired_token":
			return nil, fmt.Errorf("kimi: device code expired"), false
		case "access_denied":
			return nil, fmt.Errorf("kimi: access denied by user"), false
		default:
			return nil, fmt.Errorf("kimi: OAuth error: %s - %s", oauthResp.Error, oauthResp.ErrorDescription), false
		}
	}

	if oauthResp.AccessToken == "" {
		return nil, fmt.Errorf("kimi: empty access token in response"), false
	}

	var expiresAt int64
	if oauthResp.ExpiresIn > 0 {
		expiresAt = time.Now().Unix() + int64(oauthResp.ExpiresIn)
	}

	return &KimiTokenData{
		AccessToken:  oauthResp.AccessToken,
		RefreshToken: oauthResp.RefreshToken,
		TokenType:    oauthResp.TokenType,
		ExpiresAt:    expiresAt,
		Scope:        oauthResp.Scope,
	}, nil, false
}

func (c *DeviceFlowClient) RefreshToken(ctx context.Context, refreshToken string) (*KimiTokenData, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return nil, fmt.Errorf("kimi: refresh token is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	refreshToken = strings.TrimSpace(refreshToken)
	flightKey := c.tokenURL() + ":" + refreshToken

	result, err, _ := kimiRefreshGroup.Do(flightKey, func() (interface{}, error) {
		return c.refreshTokenSingleFlight(context.WithoutCancel(ctx), refreshToken)
	})
	if err != nil {
		return nil, err
	}
	tokenData, ok := result.(*KimiTokenData)
	if !ok || tokenData == nil {
		return nil, fmt.Errorf("kimi: refresh token failed: invalid single-flight result")
	}
	return tokenData, nil
}

func (c *DeviceFlowClient) refreshTokenSingleFlight(ctx context.Context, refreshToken string) (*KimiTokenData, error) {
	data := url.Values{}
	data.Set("client_id", kimiClientID)
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL(), strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("kimi: failed to create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	for k, v := range c.commonHeaders() {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kimi: refresh request failed: %w", err)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("kimi refresh token: close body error: %v", errClose)
		}
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kimi: failed to read refresh response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("kimi: refresh token rejected (status %d)", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kimi: refresh failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var tokenResp struct {
		AccessToken  string  `json:"access_token"`
		RefreshToken string  `json:"refresh_token"`
		TokenType    string  `json:"token_type"`
		ExpiresIn    float64 `json:"expires_in"`
		Scope        string  `json:"scope"`
	}

	if err = json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return nil, fmt.Errorf("kimi: failed to parse refresh response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("kimi: empty access token in refresh response")
	}

	var expiresAt int64
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Unix() + int64(tokenResp.ExpiresIn)
	}

	return &KimiTokenData{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresAt:    expiresAt,
		Scope:        tokenResp.Scope,
	}, nil
}
