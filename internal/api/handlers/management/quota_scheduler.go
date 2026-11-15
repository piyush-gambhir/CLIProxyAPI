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

type quotaWindowPayload struct {
	UsedPercent float64 `json:"used_percent"`
	ResetAt     string  `json:"reset_at,omitempty"`
	Known       bool    `json:"known"`
	HardLimited bool    `json:"hard_limited"`
}

type quotaAccountPayload struct {
	Provider  string              `json:"provider"`
	Plan      string              `json:"plan,omitempty"`
	FetchedAt string              `json:"fetched_at,omitempty"`
	FiveHour  *quotaWindowPayload `json:"five_hour,omitempty"`
	SevenDay  *quotaWindowPayload `json:"seven_day,omitempty"`
	Weekly    *quotaWindowPayload `json:"weekly,omitempty"`
	Fable     *quotaWindowPayload `json:"fable,omitempty"`
}

type quotaRankingPayload struct {
	Auth          string  `json:"auth"`
	Provider      string  `json:"provider"`
	Urgency       float64 `json:"urgency"`
	Usable        bool    `json:"usable"`
	Reason        string  `json:"reason"`
	RankingWindow string  `json:"ranking_window,omitempty"`
}

type quotaSchedulerStatusPayload struct {
	Accounts    map[string]quotaAccountPayload `json:"accounts"`
	Strategy    string                         `json:"strategy"`
	GeneratedAt string                         `json:"generated_at"`
	Ranking     []quotaRankingPayload          `json:"ranking"`
}

var quotaStoreForStatus = quota.DefaultStore

func (h *Handler) GetQuotaSchedulerStatus(c *gin.Context) {
	now := time.Now().UTC()
	store := quotaStoreForStatus()

	var auths []*coreauth.Auth
	if h != nil && h.authManager != nil {
		auths = h.authManager.List()
	}

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

func (h *Handler) routingStrategyName() string {
	if h == nil || h.cfg == nil {
		return "round-robin"
	}
	if normalized, ok := normalizeRoutingStrategy(h.cfg.Routing.Strategy); ok {
		return normalized
	}
	return strings.TrimSpace(h.cfg.Routing.Strategy)
}

func (h *Handler) fiveHourGuardPercent() float64 {
	if h == nil || h.cfg == nil {
		return quota.DefaultFiveHourGuardPercent
	}
	if guard := h.cfg.Routing.ResetSoonest.FiveHourGuardPercent; guard != nil && *guard >= 0 && *guard <= 100 {
		return *guard
	}
	return quota.DefaultFiveHourGuardPercent
}

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
