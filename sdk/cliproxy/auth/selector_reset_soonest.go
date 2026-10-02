package auth

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/quota"
)

// ResetSoonestSelector picks the most urgent usable credential, where urgency
// is the share of a quota window still left divided by the hours until that
// window resets. The credential that should be burned first wins.
//
// It is the "reset-soonest" strategy (alias "expiring-first"). Selection runs
// after the usual availability filtering and inside the highest available
// priority tier, so cooldowns, disabled credentials, model support, the Codex
// websocket preference and credential priority all keep their existing meaning.
// Priority stays the manual override.
//
// Credentials with no quota reading rank after every credential with one and
// round-robin among themselves, so a brand new credential or a provider that
// reports nothing still gets traffic.
//
// Wrap it in a SessionAffinitySelector to keep an established thread on its
// credential; this selector then steers cold bindings, sessionless requests and
// post-failover rebinds.
type ResetSoonestSelector struct {
	// FiveHourGuardPercent demotes a credential at or above this much of its
	// five-hour window behind every credential below the guard. Zero uses
	// quota.DefaultFiveHourGuardPercent.
	FiveHourGuardPercent float64

	// Store supplies quota readings. Nil uses quota.DefaultStore().
	Store *quota.Store

	// Now is the clock, for tests. Nil uses time.Now.
	Now func() time.Time

	mu        sync.Mutex
	rotation  map[string]string
	pickCount atomic.Uint64
}

// NewResetSoonestSelector builds a selector against the shared readings store.
func NewResetSoonestSelector(fiveHourGuardPercent float64) *ResetSoonestSelector {
	return &ResetSoonestSelector{FiveHourGuardPercent: fiveHourGuardPercent}
}

// Picks reports how many picks this selector has served. It backs the cheap
// counter the design asks for alongside the per-pick debug log.
func (s *ResetSoonestSelector) Picks() uint64 {
	if s == nil {
		return 0
	}
	return s.pickCount.Load()
}

func (s *ResetSoonestSelector) store() *quota.Store {
	if s != nil && s.Store != nil {
		return s.Store
	}
	return quota.DefaultStore()
}

func (s *ResetSoonestSelector) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Pick selects the most urgent available auth for the request.
func (s *ResetSoonestSelector) Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	_ = opts
	now := s.now()
	store := s.store()

	// Ingest the passive header snapshot upstream already keeps on every
	// candidate, including candidates that availability filtering is about to
	// drop, so the readings store and the status endpoint see them. This is the
	// cheapest possible writer: no upstream request, no extra bookkeeping.
	IngestAuthQuota(store, auths, now)

	available, err := getSelectorAvailableAuths(ctx, auths, provider, model, now)
	if err != nil {
		return nil, err
	}
	if len(available) == 0 {
		return nil, &Error{Code: "auth_not_found", Message: "no auth candidates"}
	}
	if len(available) == 1 {
		s.pickCount.Add(1)
		return available[0], nil
	}

	byID := make(map[string]*Auth, len(available))
	candidates := make([]quota.Candidate, 0, len(available))
	for _, auth := range available {
		if auth == nil {
			continue
		}
		byID[auth.ID] = auth
		candidate := quota.Candidate{AuthID: auth.ID, Provider: canonicalQuotaProvider(auth)}
		if reading, ok := store.Lookup(auth.ID); ok {
			candidate.Reading = reading
		}
		candidates = append(candidates, candidate)
	}
	if len(candidates) == 0 {
		return nil, &Error{Code: "auth_not_found", Message: "no auth candidates"}
	}

	ranked := quota.Rank(candidates, quota.RankOptions{
		Model:                model,
		FiveHourGuardPercent: s.FiveHourGuardPercent,
		Now:                  now,
	})

	// Unusable candidates are kept by the ranker for the status surface. Here
	// they are dropped, unless every candidate is unusable: availability
	// filtering already judged them serviceable, so a reading that disagrees must
	// not empty the pool and fail the request.
	usable := make([]quota.Ranked, 0, len(ranked))
	for _, entry := range ranked {
		if entry.Usable {
			usable = append(usable, entry)
		}
	}
	if len(usable) == 0 {
		usable = ranked
	}

	chosen := usable[0]
	// Credentials with no reading tie on urgency, so rotate among them instead of
	// always handing the request to the lowest auth ID.
	if !chosen.Known {
		if rotated, ok := s.rotateUnknown(provider, model, usable); ok {
			chosen = rotated
		}
	}
	auth := byID[chosen.AuthID]
	if auth == nil {
		auth = available[0]
	}
	s.pickCount.Add(1)
	s.logPick(ctx, provider, model, chosen, len(usable))
	return auth, nil
}

// rotateUnknown round-robins across the leading block of no-data candidates.
func (s *ResetSoonestSelector) rotateUnknown(provider, model string, ordered []quota.Ranked) (quota.Ranked, bool) {
	unknown := make([]quota.Ranked, 0, len(ordered))
	for _, entry := range ordered {
		if entry.Known {
			break
		}
		unknown = append(unknown, entry)
	}
	if len(unknown) <= 1 {
		return quota.Ranked{}, false
	}
	key := strings.ToLower(strings.TrimSpace(provider)) + "::" + canonicalModelKey(model)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rotation == nil {
		s.rotation = make(map[string]string)
	}
	last := s.rotation[key]
	index := 0
	if last != "" {
		for i, entry := range unknown {
			if entry.AuthID == last {
				index = (i + 1) % len(unknown)
				break
			}
		}
	}
	chosen := unknown[index]
	s.rotation[key] = chosen.AuthID
	return chosen, true
}

// logPick records the selection at debug level, per the design.
func (s *ResetSoonestSelector) logPick(ctx context.Context, provider, model string, chosen quota.Ranked, candidates int) {
	entry := selectorLogEntry(ctx)
	if entry == nil {
		return
	}
	entry.Debugf("reset-soonest: picked auth=%s provider=%s model=%s urgency=%.3f window=%s reason=%s known=%t guarded=%t candidates=%d picks=%d",
		chosen.AuthID, provider, model, chosen.Urgency, chosen.RankingWindow, chosen.Reason, chosen.Known, chosen.Guarded, candidates, s.pickCount.Load())
}

// IngestAuthQuota copies the passive header snapshot and the cooldown state of
// each auth into a readings store.
//
// It is exported so the management status endpoint can refresh the store from
// the full credential list, not just the candidates of one request. A 429 that
// put a credential into quota cooldown becomes a hard limit on the window whose
// reset is closest to the recovery time, and is cleared once the cooldown ends.
func IngestAuthQuota(store *quota.Store, auths []*Auth, now time.Time) {
	if store == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		identity := quota.Identity{
			AuthID:   auth.ID,
			Provider: canonicalQuotaProvider(auth),
			FileName: strings.TrimSpace(auth.FileName),
			Plan:     quota.SignalPlan(auth.Provider, auth.Quota.Signals),
		}
		observedAt := auth.Quota.ObservedAt
		if observedAt.IsZero() {
			observedAt = now
		}
		if windows := quota.SignalWindows(auth.Provider, auth.Quota.Signals, observedAt); len(windows) > 0 {
			store.Observe(identity, windows, observedAt)
		} else if identity.FileName != "" || identity.Provider != "" {
			// Register the credential even with no reading so the status endpoint
			// lists it and the ranker can place it as no-data.
			store.Observe(identity, nil, observedAt)
		}
		exceeded := auth.Quota.Exceeded && auth.Quota.NextRecoverAt.After(now)
		store.MarkHardLimited(identity, exceeded, auth.Quota.NextRecoverAt)
	}
}

// canonicalQuotaProvider normalises a provider key for the readings store, so
// "anthropic" and "claude" share one identity.
func canonicalQuotaProvider(auth *Auth) string {
	if auth == nil {
		return ""
	}
	provider := strings.ToLower(strings.TrimSpace(auth.Provider))
	if provider == "anthropic" {
		return "claude"
	}
	return provider
}
