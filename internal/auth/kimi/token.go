package kimi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/misc"
	log "github.com/sirupsen/logrus"
)

type KimiTokenStorage struct {
	AccessToken string `json:"access_token"`

	RefreshToken string `json:"refresh_token"`

	TokenType string `json:"token_type"`

	Scope string `json:"scope,omitempty"`

	DeviceID string `json:"device_id,omitempty"`

	Expired string `json:"expired,omitempty"`

	Type string `json:"type"`

	Domain string `json:"domain,omitempty"`

	BaseURL string `json:"base_url,omitempty"`

	Metadata map[string]any `json:"-"`
}

func (ts *KimiTokenStorage) SetMetadata(meta map[string]any) {
	ts.Metadata = meta
}

type KimiTokenData struct {
	AccessToken string `json:"access_token"`

	RefreshToken string `json:"refresh_token"`

	TokenType string `json:"token_type"`

	ExpiresAt int64 `json:"expires_at"`

	Scope string `json:"scope"`
}

type KimiAuthBundle struct {
	TokenData *KimiTokenData

	DeviceID string
}

type DeviceCodeResponse struct {
	DeviceCode string `json:"device_code"`

	UserCode string `json:"user_code"`

	VerificationURI string `json:"verification_uri,omitempty"`

	VerificationURIComplete string `json:"verification_uri_complete"`

	ExpiresIn int `json:"expires_in"`

	Interval int `json:"interval"`
}

func (ts *KimiTokenStorage) SaveTokenToFile(authFilePath string) error {
	misc.LogSavingCredentials(authFilePath)
	if ts.Type == "" {
		if IsKimiAIDomain(ts.Domain) {
			ts.Type = "kimi-ai"
		} else {
			ts.Type = "kimi"
		}
	}
	if ts.Domain == "" {
		if IsKimiAIDomain(ts.Type) {
			ts.Domain = KimiAIDomain
		} else {
			ts.Domain = KimiDefaultDomain
		}
	}
	if ts.BaseURL == "" {
		ts.BaseURL = ResolveKimiAPIBaseURL(ts.Domain)
	}

	if err := os.MkdirAll(filepath.Dir(authFilePath), 0700); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	data, errMerge := misc.MergeMetadata(ts, ts.Metadata)
	if errMerge != nil {
		return fmt.Errorf("failed to merge metadata: %w", errMerge)
	}

	f, err := os.Create(authFilePath)
	if err != nil {
		return fmt.Errorf("failed to create token file: %w", err)
	}
	defer func() {
		if errClose := f.Close(); errClose != nil {
			log.Errorf("kimi token storage: close token file error: %v", errClose)
		}
	}()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(data); err != nil {
		return fmt.Errorf("failed to write token to file: %w", err)
	}
	return nil
}

func (ts *KimiTokenStorage) IsExpired() bool {
	if ts.Expired == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, ts.Expired)
	if err != nil {
		return true
	}

	return time.Now().Add(time.Duration(refreshThresholdSeconds) * time.Second).After(t)
}

func (ts *KimiTokenStorage) NeedsRefresh() bool {
	if ts.RefreshToken == "" {
		return false
	}
	return ts.IsExpired()
}
