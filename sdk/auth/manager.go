package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	claudeauth "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/claude"
	xaiauth "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/xai"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
)


type Manager struct {
	authenticators map[string]Authenticator
	store          coreauth.Store
}



func NewManager(store coreauth.Store, authenticators ...Authenticator) *Manager {
	mgr := &Manager{
		authenticators: make(map[string]Authenticator),
		store:          store,
	}
	for i := range authenticators {
		mgr.Register(authenticators[i])
	}
	return mgr
}


func (m *Manager) Register(a Authenticator) {
	if a == nil {
		return
	}
	if m.authenticators == nil {
		m.authenticators = make(map[string]Authenticator)
	}
	m.authenticators[a.Provider()] = a
}


func (m *Manager) SetStore(store coreauth.Store) {
	m.store = store
}


func (m *Manager) Login(ctx context.Context, provider string, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, string, error) {
	auth, ok := m.authenticators[provider]
	if !ok {
		return nil, "", fmt.Errorf("cliproxy auth: authenticator %s not registered", provider)
	}

	loginProxyURL := ""
	if opts != nil {
		loginProxyURL = strings.TrimSpace(opts.ProxyURL)
	}
	if loginProxyURL != "" {
		if !proxyutil.ValidRequestProxy(loginProxyURL) {
			return nil, "", fmt.Errorf("cliproxy auth: invalid login proxy URL %q", proxyutil.Redact(loginProxyURL))
		}
		cfg = configWithLoginProxy(cfg, loginProxyURL)
	}

	record, err := auth.Login(ctx, cfg, opts)
	if err != nil {
		return nil, "", err
	}
	if record == nil {
		return nil, "", fmt.Errorf("cliproxy auth: authenticator %s returned nil record", provider)
	}

	if m.store == nil {
		return record, "", nil
	}

	if cfg != nil {
		if dirSetter, ok := m.store.(interface{ SetBaseDir(string) }); ok {
			dirSetter.SetBaseDir(cfg.AuthDir)
		}
		if strings.TrimSpace(cfg.AuthDir) != "" {
			targetFile := record.FileName
			if targetFile == "" {
				targetFile = record.ID
			}
			if targetFile != "" {
				fullPath := filepath.Join(cfg.AuthDir, targetFile)
				if raw, errRead := os.ReadFile(fullPath); errRead == nil && len(raw) > 0 {
					var existingMap map[string]any
					if errUnmarshal := json.Unmarshal(raw, &existingMap); errUnmarshal == nil && len(existingMap) > 0 {
						coreauth.MergeExistingAuthMetadata(record, existingMap)
					}
				}
			}
		}
	}
	legacyClaudeCredential, errLegacy := claudeauth.FindMatchingLegacyCredential(ctx, m.store, record)
	if errLegacy != nil {
		return record, "", errLegacy
	}
	if legacyClaudeCredential != nil {
		coreauth.MergeExistingAuthMetadata(record, legacyClaudeCredential.Metadata)
	}
	legacyXAICredential, errLegacyXAI := xaiauth.FindMatchingLegacyCredential(ctx, m.store, record)
	if errLegacyXAI != nil {
		return record, "", errLegacyXAI
	}
	adoptXAICredential := legacyXAICredential != nil && record != nil &&
		strings.EqualFold(strings.TrimSpace(record.Provider), "xai") &&
		metadataStringValue(record.Metadata, "email") == "" &&
		metadataStringValue(record.Metadata, "sub") != "" &&
		metadataStringValue(legacyXAICredential.Metadata, "email") != ""
	if adoptXAICredential {







		if _, isFileNameStore := m.store.(interface{ SetBaseDir(string) }); !isFileNameStore {
			adoptXAICredential = false
		} else if disabled, hasDisabled := record.Metadata["disabled"].(bool); hasDisabled && disabled {



			adoptXAICredential = false
		}
	}
	if legacyXAICredential != nil {
		coreauth.MergeExistingAuthMetadata(record, legacyXAICredential.Metadata)
	}
	if adoptXAICredential {
		adoptLegacyXAIFileName(record, legacyXAICredential)
	}
	applyLoginProxyToRecord(record, loginProxyURL)

	savedPath, err := m.store.Save(coreauth.WithAuthCreationIntent(ctx), record)
	if err != nil {
		return record, "", err
	}
	if legacyClaudeCredential != nil {
		if strings.TrimSpace(savedPath) == "" {
			return record, "", fmt.Errorf("cliproxy auth: canonical Claude credential was not persisted; legacy credential retained")
		}
		legacyID := strings.TrimSpace(legacyClaudeCredential.ID)
		if legacyID == "" {
			legacyID = strings.TrimSpace(legacyClaudeCredential.FileName)
		}
		if errDelete := m.store.Delete(ctx, legacyID); errDelete != nil {
			return record, savedPath, fmt.Errorf("cliproxy auth: canonical Claude credential saved but legacy credential cleanup failed: %w", errDelete)
		}
	}
	if legacyXAICredential != nil && !adoptXAICredential {
		if strings.TrimSpace(savedPath) == "" {
			return record, "", fmt.Errorf("cliproxy auth: canonical xAI credential was not persisted; legacy credential retained")
		}
		legacyID := strings.TrimSpace(legacyXAICredential.ID)
		if legacyID == "" {
			legacyID = strings.TrimSpace(legacyXAICredential.FileName)
		}
		if errDelete := m.store.Delete(ctx, legacyID); errDelete != nil {
			return record, savedPath, fmt.Errorf("cliproxy auth: canonical xAI credential saved but legacy credential cleanup failed: %w", errDelete)
		}
	}
	return record, savedPath, nil
}

func configWithLoginProxy(cfg *config.Config, proxyURL string) *config.Config {
	if cfg == nil {
		return &config.Config{SDKConfig: config.SDKConfig{ProxyURL: proxyURL}}
	}
	cloned := cfg.CloneForRuntime()
	if cloned == nil {
		copied := *cfg
		cloned = &copied
	}
	cloned.ProxyURL = proxyURL
	return cloned
}

func applyLoginProxyToRecord(record *coreauth.Auth, proxyURL string) {
	if record == nil || proxyURL == "" {
		return
	}
	record.ProxyURL = proxyURL
	if record.Metadata == nil {
		record.Metadata = make(map[string]any)
	}
	record.Metadata["proxy_url"] = proxyURL
	if setter, ok := record.Storage.(interface{ SetMetadata(map[string]any) }); ok {
		setter.SetMetadata(record.Metadata)
	}
}

func metadataStringValue(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}







func adoptLegacyXAIFileName(record, legacy *coreauth.Auth) {
	legacyFileName := strings.TrimSpace(legacy.FileName)
	if legacyFileName == "" {
		legacyFileName = strings.TrimSpace(legacy.ID)
	}
	if legacyFileName == "" {
		return
	}
	oldBaseName := filepath.Base(strings.TrimSpace(record.FileName))
	if oldBaseName == "" || oldBaseName == "." {
		oldBaseName = filepath.Base(strings.TrimSpace(record.ID))
	}
	record.ID = legacyFileName
	record.FileName = legacyFileName
	if storage, ok := record.Storage.(*xaiauth.TokenStorage); ok && storage != nil {
		if email := metadataStringValue(legacy.Metadata, "email"); email != "" {
			storage.Email = email
		}
	}
	for key, value := range record.Attributes {
		if strings.TrimSpace(value) != "" && strings.EqualFold(filepath.Base(strings.TrimSpace(value)), oldBaseName) {
			delete(record.Attributes, key)
		}
	}
}
