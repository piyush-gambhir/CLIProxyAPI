package quota

import (
	"context"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)


const (

	DefaultClaudeInterval = 30 * time.Minute



	DefaultClaudeMinGap = 10 * time.Minute

	DefaultClaudeBackoffStart = 5 * time.Minute

	DefaultClaudeBackoffMax = time.Hour


	DefaultCodexInterval = 5 * time.Minute

	DefaultTick = time.Minute


	defaultRequestTimeout = 30 * time.Second
)

const (
	claudeUsageURL = "https://api.anthropic.com/api/oauth/usage"
	codexUsageURL  = "https://chatgpt.com/backend-api/wham/usage"


	codexUserAgent = "codex-tui/0.149.1"
)



type Target struct {

	AuthID string

	Provider string

	FileName string

	AccessToken string

	AccountID string

	Plan string

	ProxyURL string

	Disabled bool
}


type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}



type PollerConfig struct {

	Store *Store

	Now func() time.Time

	Targets func() []Target


	HTTPClientFor func(Target) Doer


	ClaudeInterval     time.Duration
	CodexInterval      time.Duration
	ClaudeMinGap       time.Duration
	ClaudeBackoffStart time.Duration
	ClaudeBackoffMax   time.Duration

	Tick time.Duration
}








type Poller struct {
	cfg PollerConfig

	mu                 sync.Mutex
	lastClaudeCallAt   time.Time
	claudeBackoffUntil time.Time
	claudeBackoff      time.Duration
	started            bool
}



func NewPoller(cfg PollerConfig) *Poller {
	if cfg.Store == nil || cfg.Targets == nil {
		return nil
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.ClaudeInterval <= 0 {
		cfg.ClaudeInterval = DefaultClaudeInterval
	}
	if cfg.CodexInterval <= 0 {
		cfg.CodexInterval = DefaultCodexInterval
	}
	if cfg.ClaudeMinGap <= 0 {
		cfg.ClaudeMinGap = DefaultClaudeMinGap
	}
	if cfg.ClaudeBackoffStart <= 0 {
		cfg.ClaudeBackoffStart = DefaultClaudeBackoffStart
	}
	if cfg.ClaudeBackoffMax <= 0 {
		cfg.ClaudeBackoffMax = DefaultClaudeBackoffMax
	}
	if cfg.Tick <= 0 {
		cfg.Tick = DefaultTick
	}
	return &Poller{cfg: cfg}
}




func (p *Poller) Start(ctx context.Context) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return
	}
	p.started = true
	p.mu.Unlock()
	go func() {
		p.RunOnce(ctx)
		ticker := time.NewTicker(p.cfg.Tick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.RunOnce(ctx)
			}
		}
	}()
}



func (p *Poller) RunOnce(ctx context.Context) {
	if p == nil || ctx == nil {
		return
	}
	targets := p.eligibleTargets()
	for _, target := range targets {
		if ctx.Err() != nil {
			return
		}
		p.poll(ctx, target)
	}
}




func (p *Poller) eligibleTargets() []Target {
	raw := p.cfg.Targets()
	now := p.cfg.Now()
	out := make([]Target, 0, len(raw))
	for _, target := range raw {
		if target.Disabled || strings.TrimSpace(target.AuthID) == "" {
			continue
		}
		interval, ok := p.providerInterval(target.Provider)
		if !ok {
			continue
		}
		if strings.TrimSpace(target.AccessToken) == "" {
			continue
		}


		if fetchedAt := p.cfg.Store.FetchedAt(target.AuthID); !fetchedAt.IsZero() && now.Sub(fetchedAt) < interval {
			continue
		}
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].AuthID < out[j].AuthID
	})
	return out
}




func (p *Poller) providerInterval(provider string) (time.Duration, bool) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude", "anthropic":
		return p.cfg.ClaudeInterval, true
	case "codex":
		return p.cfg.CodexInterval, true
	default:
		return 0, false
	}
}


func (p *Poller) poll(ctx context.Context, target Target) {
	isClaude := p.isClaude(target.Provider)
	if isClaude && !p.claimClaudeSlot() {
		return
	}
	now := p.cfg.Now()
	body, status, err := p.fetch(ctx, target)
	if err != nil {
		log.Debugf("quota poller: %s usage read failed for %s: %v", target.Provider, target.AuthID, err)
		return
	}
	if status == http.StatusTooManyRequests {
		if isClaude {
			p.noteClaude429()
		}
		log.Debugf("quota poller: %s usage read returned 429 for %s", target.Provider, target.AuthID)
		return
	}
	if status < 200 || status >= 300 {
		log.Debugf("quota poller: %s usage read returned %d for %s", target.Provider, status, target.AuthID)
		return
	}
	if isClaude {
		p.noteClaudeSuccess()
	}
	reading, errParse := ParseUsage(target.Provider, body, now)
	if errParse != nil {
		log.Debugf("quota poller: %s usage body unparsable for %s: %v", target.Provider, target.AuthID, errParse)
		return
	}
	if len(reading.Windows) == 0 {
		return
	}
	plan := reading.Plan
	if plan == "" {
		plan = target.Plan
	}
	p.cfg.Store.Observe(Identity{
		AuthID:   target.AuthID,
		Provider: strings.ToLower(strings.TrimSpace(target.Provider)),
		FileName: target.FileName,
		Plan:     plan,
	}, reading.Windows, now)
}


func (p *Poller) fetch(ctx context.Context, target Target) ([]byte, int, error) {
	endpoint := claudeUsageURL
	if !p.isClaude(target.Provider) {
		endpoint = codexUsageURL
	}
	req, errNew := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if errNew != nil {
		return nil, 0, errNew
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(target.AccessToken))
	req.Header.Set("Content-Type", "application/json")
	if p.isClaude(target.Provider) {
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	} else {
		req.Header.Set("User-Agent", codexUserAgent)
		if accountID := strings.TrimSpace(target.AccountID); accountID != "" {
			req.Header.Set("Chatgpt-Account-Id", accountID)
		}
	}
	client := p.clientFor(target)
	resp, errDo := client.Do(req)
	if errDo != nil {
		return nil, 0, errDo
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Debugf("quota poller: response body close error: %v", errClose)
		}
	}()
	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return nil, resp.StatusCode, errRead
	}
	return body, resp.StatusCode, nil
}

func (p *Poller) clientFor(target Target) Doer {
	if p.cfg.HTTPClientFor != nil {
		if client := p.cfg.HTTPClientFor(target); client != nil {
			return client
		}
	}
	return &http.Client{Timeout: defaultRequestTimeout}
}

func (p *Poller) isClaude(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude", "anthropic":
		return true
	default:
		return false
	}
}



func (p *Poller) claimClaudeSlot() bool {
	now := p.cfg.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.claudeBackoffUntil.IsZero() && now.Before(p.claudeBackoffUntil) {
		return false
	}
	if !p.lastClaudeCallAt.IsZero() && now.Sub(p.lastClaudeCallAt) < p.cfg.ClaudeMinGap {
		return false
	}
	p.lastClaudeCallAt = now
	return true
}


func (p *Poller) noteClaude429() {
	now := p.cfg.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.claudeBackoff <= 0 {
		p.claudeBackoff = p.cfg.ClaudeBackoffStart
	} else if p.claudeBackoff < p.cfg.ClaudeBackoffMax {
		p.claudeBackoff *= 2
	}
	if p.claudeBackoff > p.cfg.ClaudeBackoffMax {
		p.claudeBackoff = p.cfg.ClaudeBackoffMax
	}
	p.claudeBackoffUntil = now.Add(p.claudeBackoff)
}


func (p *Poller) noteClaudeSuccess() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.claudeBackoff = 0
	p.claudeBackoffUntil = time.Time{}
}



func (p *Poller) ClaudeBackoff() (time.Duration, time.Time) {
	if p == nil {
		return 0, time.Time{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.claudeBackoff, p.claudeBackoffUntil
}
