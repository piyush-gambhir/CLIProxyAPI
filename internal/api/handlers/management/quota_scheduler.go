package management

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/quota"
)

// quotaWindowPayload is one quota window in the scheduler status contract.
// Field names and the RFC3339 reset are fixed: an external client decodes this
// shape, and renaming a field breaks it.
type quotaWindowPayload struct {
	UsedPercent float64 `json:"used_percent"`
	ResetAt     string  `json:"reset_at,omitempty"`
	Known       bool    `json:"known"`
	HardLimited bool    `json:"hard_limited"`
}

// quotaAccountPayload is one credential in the scheduler status contract,
// keyed in the accounts map by auth file name.
type quotaAccountPayload struct {
	Provider  string              `json:"provider"`
	Plan      string              `json:"plan,omitempty"`
	FetchedAt string              `json:"fetched_at,omitempty"`
	FiveHour  *quotaWindowPayload `json:"five_hour,omitempty"`
	SevenDay  *quotaWindowPayload `json:"seven_day,omitempty"`
	Weekly    *quotaWindowPayload `json:"weekly,omitempty"`
	Fable     *quotaWindowPayload `json:"fable,omitempty"`
}

// quotaRankingPayload is one entry of the extra ranking view. The contract's
// decoder ignores unknown top-level keys, so this is additive.
type quotaRankingPayload struct {
	Auth          string  `json:"auth"`
	Provider      string  `json:"provider"`
	Urgency       float64 `json:"urgency"`
	Usable        bool    `json:"usable"`
	Reason        string  `json:"reason"`
	RankingWindow string  `json:"ranking_window,omitempty"`
}

// quotaSchedulerStatusPayload is the full response body.
type quotaSchedulerStatusPayload struct {
	Accounts    map[string]quotaAccountPayload `json:"accounts"`
	Strategy    string                         `json:"strategy"`
	GeneratedAt string                         `json:"generated_at"`
	Ranking     []quotaRankingPayload          `json:"ranking"`
}

// quotaStoreForStatus resolves the readings store this endpoint reads. It is a
// variable so a test can substitute an isolated store instead of the
// process-wide one.
var quotaStoreForStatus = quota.DefaultStore

// GetQuotaSchedulerStatus reports the quota readings routing is working from,
// plus the order an unprefixed request would be served in.
//
// Endpoint:
//
//	GET /v8/management/quota-scheduler/status
//
// The accounts map is keyed by auth file name and carries one entry per window
// the proxy has a reading for. An empty accounts map means no credential has
// reported quota yet, which is the normal state right after a restart until the
// usage poller's first pass completes.
func (h *Handler) GetQuotaSchedulerStatus(c *gin.Context) {
	now := time.Now().UTC()
	store := quotaStoreForStatus()

	var auths []*coreauth.Auth
	if h != nil && h.authManager != nil {
		auths = h.authManager.List()
	}
	// Refresh from the passive header snapshot so the status reflects every
	// credential, not only the ones a recent pick considered.
	coreauth.IngestAuthQuota(store, auths, now)

	payload := quotaSchedulerStatusPayload{
		Accounts:    make(map[string]quotaAccountPayload),
		Strategy:    h.routingStrategyName(),
		GeneratedAt: now.Format(time.RFC3339),
		Ranking:     h.quotaRanking(store, auths, now),
	}
	for name, reading := range store.Snapshot() {
		payload.Accounts[name] = quotaAccountPayload{
			Provider:  reading.Provider,
			Plan:      reading.Plan,
			FetchedAt: formatQuotaTime(reading.FetchedAt),
			FiveHour:  quotaWindowOf(reading, quota.WindowFiveHour),
			SevenDay:  quotaWindowOf(reading, quota.WindowSevenDay),
			Weekly:    quotaWindowOf(reading, quota.WindowWeekly),
			Fable:     quotaWindowOf(reading, quota.WindowFable),
		}
	}
	c.JSON(http.StatusOK, payload)
}

// quotaRanking builds the pick order per provider family, as reset-soonest
// would rank an unprefixed request.
func (h *Handler) quotaRanking(store *quota.Store, auths []*coreauth.Auth, now time.Time) []quotaRankingPayload {
	guard := h.fiveHourGuardPercent()
	byProvider := make(map[string][]quota.Candidate)
	for _, auth := range auths {
		if auth == nil || auth.Disabled {
			continue
		}
		provider := strings.ToLower(strings.TrimSpace(auth.Provider))
		if provider == "anthropic" {
			provider = "claude"
		}
		if provider == "" {
			continue
		}
		candidate := quota.Candidate{AuthID: auth.ID, Provider: provider}
		if reading, ok := store.Lookup(auth.ID); ok {
			candidate.Reading = reading
		}
		byProvider[provider] = append(byProvider[provider], candidate)
	}
	providers := make([]string, 0, len(byProvider))
	for provider := range byProvider {
		providers = append(providers, provider)
	}
	sort.Strings(providers)

	out := make([]quotaRankingPayload, 0, len(auths))
	for _, provider := range providers {
		ranked := quota.Rank(byProvider[provider], quota.RankOptions{FiveHourGuardPercent: guard, Now: now})
		for _, entry := range ranked {
			out = append(out, quotaRankingPayload{
				Auth:          entry.AuthID,
				Provider:      entry.Provider,
				Urgency:       entry.Urgency,
				Usable:        entry.Usable,
				Reason:        entry.Reason,
				RankingWindow: entry.RankingWindow,
			})
		}
	}
	return out
}

// routingStrategyName reports the effective strategy name.
func (h *Handler) routingStrategyName() string {
	if h == nil || h.cfg == nil {
		return "round-robin"
	}
	if normalized, ok := normalizeRoutingStrategy(h.cfg.Routing.Strategy); ok {
		return normalized
	}
	return strings.TrimSpace(h.cfg.Routing.Strategy)
}

// fiveHourGuardPercent reports the configured guard, or the default.
func (h *Handler) fiveHourGuardPercent() float64 {
	if h == nil || h.cfg == nil {
		return quota.DefaultFiveHourGuardPercent
	}
	if guard := h.cfg.Routing.ResetSoonest.FiveHourGuardPercent; guard != nil && *guard >= 0 && *guard <= 100 {
		return *guard
	}
	return quota.DefaultFiveHourGuardPercent
}

// quotaWindowOf projects one window, or nil when there is no reading for it.
func quotaWindowOf(reading quota.Reading, id string) *quotaWindowPayload {
	window, ok := reading.Window(id)
	if !ok {
		return nil
	}
	return &quotaWindowPayload{
		UsedPercent: window.UsedPercent,
		ResetAt:     formatQuotaTime(window.ResetAt),
		Known:       window.Known,
		HardLimited: window.HardLimited,
	}
}

func formatQuotaTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
