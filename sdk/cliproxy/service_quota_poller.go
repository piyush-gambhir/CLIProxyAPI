package cliproxy

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/quota"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
	log "github.com/sirupsen/logrus"
)

const quotaPollerRequestTimeout = 30 * time.Second

var quotaPollerStart sync.Once

func (s *Service) startQuotaPoller(ctx context.Context) {
	if s == nil || s.coreManager == nil {
		return
	}
	quotaPollerStart.Do(func() {
		poller := quota.NewPoller(quota.PollerConfig{
			Store:   quota.DefaultStore(),
			Targets: s.quotaPollTargets,
			HTTPClientFor: func(target quota.Target) quota.Doer {
				return s.quotaPollClient(target)
			},
			ClaudeInterval: s.routingQuotaPollerDurations().claudeEvery,
			ClaudeMinGap:   s.routingQuotaPollerDurations().claudeGap,
			CodexInterval:  s.routingQuotaPollerDurations().codexEvery,
		})
		if poller == nil {
			return
		}
		if !s.quotaPollerEnabled() {
			log.Debug("quota poller disabled by routing configuration")
			return
		}
		log.Info("quota poller started for reset-soonest routing")
		poller.Start(ctx)
	})
}

type quotaPollerDurations struct {
	claudeEvery time.Duration
	claudeGap   time.Duration
	codexEvery  time.Duration
}

func (s *Service) routingQuotaPollerDurations() quotaPollerDurations {
	state := s.routingStateSnapshot()
	return quotaPollerDurations{
		claudeEvery: state.quotaPollerClaudeEvery,
		claudeGap:   state.quotaPollerClaudeGap,
		codexEvery:  state.quotaPollerCodexEvery,
	}
}

func (s *Service) quotaPollerEnabled() bool {
	return s.routingStateSnapshot().quotaPollerEnabled
}

func (s *Service) routingStateSnapshot() routingRuntimeState {
	if s == nil {
		return normalizedRoutingRuntimeState(nil)
	}
	s.cfgMu.RLock()
	cfg := s.cfg
	s.cfgMu.RUnlock()
	return normalizedRoutingRuntimeState(cfg)
}

func (s *Service) quotaPollTargets() []quota.Target {
	if s == nil || s.coreManager == nil {
		return nil
	}
	auths := s.coreManager.List()
	targets := make([]quota.Target, 0, len(auths))
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		provider := strings.ToLower(strings.TrimSpace(auth.Provider))
		if provider == "anthropic" {
			provider = "claude"
		}
		if provider != "claude" && provider != "codex" {
			continue
		}
		targets = append(targets, quota.Target{
			AuthID:      auth.ID,
			Provider:    provider,
			FileName:    strings.TrimSpace(auth.FileName),
			AccessToken: quotaPollAccessToken(auth),
			AccountID:   quotaPollAccountID(auth),
			Plan:        metadataString(auth.Metadata, "plan_type"),
			ProxyURL:    strings.TrimSpace(auth.ProxyURL),
			Disabled:    auth.Disabled || auth.Status == coreauth.StatusDisabled,
		})
	}
	return targets
}

func (s *Service) quotaPollClient(target quota.Target) quota.Doer {
	client := &http.Client{Timeout: quotaPollerRequestTimeout}
	proxyURL := strings.TrimSpace(target.ProxyURL)
	if proxyURL == "" {
		s.cfgMu.RLock()
		if s.cfg != nil {
			proxyURL = strings.TrimSpace(s.cfg.ProxyURL)
		}
		s.cfgMu.RUnlock()
	}
	if proxyURL == "" {
		return client
	}
	transport, _, errProxy := proxyutil.BuildHTTPTransport(proxyURL)
	if errProxy != nil || transport == nil {
		log.Debugf("quota poller: ignoring unusable proxy %s: %v", proxyutil.Redact(proxyURL), errProxy)
		return client
	}
	client.Transport = transport
	return client
}

func quotaPollAccessToken(auth *coreauth.Auth) string {
	if token := metadataString(auth.Metadata, "access_token"); token != "" {
		return token
	}
	return metadataString(auth.Metadata, "accessToken")
}

func quotaPollAccountID(auth *coreauth.Auth) string {
	if id := metadataString(auth.Metadata, "account_id"); id != "" {
		return id
	}
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	if claims, ok := auth.Metadata["id_token"].(map[string]any); ok {
		if id := metadataString(claims, "chatgpt_account_id"); id != "" {
			return id
		}
	}
	return ""
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	if value, ok := metadata[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}
