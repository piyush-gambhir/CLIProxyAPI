package executor

import (
	"fmt"
	"strings"
	"sync"

	"github.com/MoeForge/moego/ojson"
	claudeauth "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/claude"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

const (
	claudeFingerprintProfileDefault       = config.ClaudeFingerprintProfileDefault
	claudeFingerprintProfileClaudeCodeCLI = config.ClaudeFingerprintProfileClaudeCodeCLI
	claudeFingerprintProfileAttr          = "fingerprint_profile"
)






var claudeFingerprintProfileWarned sync.Map















type claudeFingerprintPolicy struct {
	AuthIsOAuthToken     bool
	ProfileClaudeCodeCLI bool
	UseOAuthBetas        bool
	ApplyCLIIdentity     bool
	SynthesizeIdentity   bool
	MCPAlias             bool
	InjectDiagnostics    bool
	OAuthCancellation    bool
}

func normalizeClaudeFingerprintProfile(raw string) string {
	profile, ok := config.NormalizeClaudeFingerprintProfile(raw)
	if !ok {
		if _, warned := claudeFingerprintProfileWarned.LoadOrStore(strings.TrimSpace(raw), struct{}{}); !warned {
			log.Warnf("unrecognized claude fingerprint-profile %q (supported: %q); falling back to default", raw, claudeFingerprintProfileClaudeCodeCLI)
		}
	}
	return profile
}

func claudeFingerprintProfileFromAuth(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return claudeFingerprintProfileDefault
	}
	if auth.Attributes != nil {
		if raw, ok := auth.Attributes[claudeFingerprintProfileAttr]; ok && strings.TrimSpace(raw) != "" {
			return normalizeClaudeFingerprintProfile(raw)
		}
	}
	for _, key := range []string{claudeFingerprintProfileAttr, "fingerprint-profile"} {
		raw := claudeauth.ReadMetadataString(&auth.Metadata, key)
		if strings.TrimSpace(raw) != "" {
			return normalizeClaudeFingerprintProfile(raw)
		}
	}
	return claudeFingerprintProfileDefault
}

func claudeFingerprintProfileFromConfig(cfg *config.Config, auth *cliproxyauth.Auth) string {
	if profile := claudeFingerprintProfileFromAuth(auth); profile != claudeFingerprintProfileDefault {
		return profile
	}
	entry := resolveClaudeKeyConfig(cfg, auth)
	if entry == nil {
		return claudeFingerprintProfileDefault
	}
	return normalizeClaudeFingerprintProfile(entry.FingerprintProfile)
}





func resolveClaudeFingerprintPolicy(cfg *config.Config, auth *cliproxyauth.Auth, apiKey string) claudeFingerprintPolicy {


	authIsOAuth := isClaudeOAuthToken(apiKey)
	profile := claudeFingerprintProfileFromConfig(cfg, auth)
	profileClaudeCodeCLI := authIsOAuth || profile == claudeFingerprintProfileClaudeCodeCLI


	injectDiagnostics := profileClaudeCodeCLI
	if cfg != nil && cfg.ClaudeHeaderDefaults.InjectDiagnostics != nil && !*cfg.ClaudeHeaderDefaults.InjectDiagnostics {
		injectDiagnostics = false
	}

	return claudeFingerprintPolicy{
		AuthIsOAuthToken:     authIsOAuth,
		ProfileClaudeCodeCLI: profileClaudeCodeCLI,
		UseOAuthBetas:        profileClaudeCodeCLI,
		ApplyCLIIdentity:     profileClaudeCodeCLI,
		SynthesizeIdentity:   profileClaudeCodeCLI && !authIsOAuth,
		MCPAlias:             profileClaudeCodeCLI,
		InjectDiagnostics:    injectDiagnostics,
		OAuthCancellation:    authIsOAuth,
	}
}













func applyClaudeCLIIdentity(body []byte, auth *cliproxyauth.Auth, apiKey, upstreamURL, sessionID string, synthesize bool) ([]byte, error) {
	doc := ojson.NewDoc(body)
	if errApply := applyClaudeCLIIdentityDoc(doc, auth, apiKey, upstreamURL, sessionID, synthesize); errApply != nil {
		return nil, errApply
	}
	return doc.Bytes(), nil
}

func applyClaudeCLIIdentityDoc(doc *ojson.Doc, auth *cliproxyauth.Auth, apiKey, upstreamURL, sessionID string, synthesize bool) error {
	identitySeed := apiKey
	if isKimiMessagesUpstream(auth, upstreamURL) {
		identitySeed = helps.ClaudeCLIAuthIdentitySeed(auth)
	}
	identityAuth, errIdentity := helps.PrepareClaudeCLIFingerprintAuth(auth, identitySeed, synthesize)
	if errIdentity != nil {
		return fmt.Errorf("ensure Claude CLI fingerprint identity: %w", errIdentity)
	}
	if _, errApply := helps.ApplyClaudeCredentialMetadataDoc(doc, identityAuth, sessionID); errApply != nil {
		return fmt.Errorf("apply Claude credential metadata: %w", errApply)
	}
	return nil
}
