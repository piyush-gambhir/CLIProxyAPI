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

// Poller defaults from bandoyer #13 and #14.
const (
	// DefaultClaudeInterval is how often one idle Claude credential is polled.
	DefaultClaudeInterval = 30 * time.Minute
	// DefaultClaudeMinGap is the minimum spacing between any two Claude calls,
	// across every credential. Nobody knows whether Anthropic throttles the usage
	// endpoint per token, per account or per IP, so the gap is provider-wide.
	DefaultClaudeMinGap = 10 * time.Minute
	// DefaultClaudeBackoffStart is the first provider-wide pause after a 429.
	DefaultClaudeBackoffStart = 5 * time.Minute
	// DefaultClaudeBackoffMax caps the doubling backoff.
	DefaultClaudeBackoffMax = time.Hour
	// DefaultCodexInterval is how often one idle Codex credential is polled. The
	// official Codex TUI polls the same endpoint every 60 s, so this is generous.
	DefaultCodexInterval = 5 * time.Minute
	// DefaultTick is how often the poller looks for idle credentials.
	DefaultTick = time.Minute
	// defaultRequestTimeout bounds a single usage read. Credential acquisition is
	// the one place AGENTS.md allows a timeout, and a quota read is part of it.
	defaultRequestTimeout = 30 * time.Second
)

const (
	claudeUsageURL = "https://api.anthropic.com/api/oauth/usage"
	codexUsageURL  = "https://chatgpt.com/backend-api/wham/usage"
	// codexUserAgent matches what the Codex TUI sends; the endpoint rejects
	// unrecognised clients.
	codexUserAgent = "codex-tui/0.149.1"
)

// Target is one credential the poller may read usage for. It is a plain
// snapshot so this package never has to import the auth package.
type Target struct {
	// AuthID is the credential's auth manager ID.
	AuthID string
	// Provider is the upstream provider key.
	Provider string
	// FileName is the credential's auth file base name.
	FileName string
	// AccessToken is the credential's bearer token.
	AccessToken string
	// AccountID is the ChatGPT account id, Codex only.
	AccountID string
	// Plan is the plan slug already known for this credential, if any.
	Plan string
	// ProxyURL overrides the global proxy for this credential.
	ProxyURL string
	// Disabled credentials are never polled.
	Disabled bool
}

// Doer is the HTTP surface the poller needs, so tests can supply a stub.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// PollerConfig wires a poller to a store, a clock, a target supplier and an
// HTTP layer.
type PollerConfig struct {
	// Store receives every reading. Required.
	Store *Store
	// Now is the clock. Nil uses time.Now.
	Now func() time.Time
	// Targets supplies the current credential set on every pass. Required.
	Targets func() []Target
	// HTTPClientFor supplies the client for one target. Nil uses a shared
	// default client that honours no proxy.
	HTTPClientFor func(Target) Doer
	// ClaudeInterval, CodexInterval, ClaudeMinGap, ClaudeBackoffStart and
	// ClaudeBackoffMax override the defaults above when positive.
	ClaudeInterval     time.Duration
	CodexInterval      time.Duration
	ClaudeMinGap       time.Duration
	ClaudeBackoffStart time.Duration
	ClaudeBackoffMax   time.Duration
	// Tick is how often the poller wakes. Zero uses DefaultTick.
	Tick time.Duration
}

// Poller reads provider usage endpoints for idle credentials and writes the
// results into the shared store.
//
// It never polls just before a pick: that would add upstream latency to a
// request and spend the Claude throttle on the hot path. It never polls a
// credential whose reading is newer than its provider interval, because traffic
// response headers keep busy credentials fresh at no cost.
type Poller struct {
	cfg PollerConfig

	mu                 sync.Mutex
	lastClaudeCallAt   time.Time
	claudeBackoffUntil time.Time
	claudeBackoff      time.Duration
	started            bool
}

// NewPoller builds a poller. It returns nil when the store or target supplier
// is missing, so the caller can skip starting it.
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

// Start runs one pass immediately, then a pass on every tick until the context
// is cancelled. Readings are in memory only, so the startup pass is what makes
// the first pick after a restart informed.
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

// RunOnce performs a single pass over the current targets. It is exported so
// tests can drive the poller with a fake clock instead of waiting on a ticker.
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

// eligibleTargets filters the current target set down to idle, enabled
// credentials of a polled provider, in stable order so the spacing rules are
// deterministic.
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
		// Idle means no reading newer than the provider interval. Traffic headers
		// refresh busy credentials, so they are skipped here.
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

// providerInterval reports the poll interval for a provider, and false for a
// provider that is never polled. Paid xAI (the only check spends tokens) and
// Meta (the usage call mints a key) are deliberately absent.
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

// poll reads one credential's usage, honouring the Claude spacing rules.
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

// fetch performs the usage request for one credential.
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

// claimClaudeSlot enforces the provider-wide minimum gap and the shared backoff.
// It returns false when this pass must skip the credential.
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

// noteClaude429 starts or doubles the provider-wide Claude backoff.
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

// noteClaudeSuccess clears the backoff after a clean read.
func (p *Poller) noteClaudeSuccess() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.claudeBackoff = 0
	p.claudeBackoffUntil = time.Time{}
}

// ClaudeBackoff reports the current provider-wide Claude backoff, for tests and
// diagnostics.
func (p *Poller) ClaudeBackoff() (time.Duration, time.Time) {
	if p == nil {
		return 0, time.Time{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.claudeBackoff, p.claudeBackoffUntil
}
