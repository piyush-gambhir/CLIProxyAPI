package auth

import (
	"context"
	"strings"
	"sync"
	"time"

	homekv "github.com/router-for-me/CLIProxyAPI/v8/internal/home"
)

type antigravityUseCreditsContextKey struct{}



func WithAntigravityCredits(ctx context.Context) context.Context {
	return context.WithValue(ctx, antigravityUseCreditsContextKey{}, true)
}


func AntigravityCreditsRequested(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(antigravityUseCreditsContextKey{}).(bool)
	return v
}



const AntigravityCreditsHintRefreshInterval = 10 * time.Minute


type AntigravityCreditsHint struct {
	Known           bool
	Available       bool
	CreditAmount    float64
	MinCreditAmount float64
	PaidTierID      string
	UpdatedAt       time.Time
}



func IsAntigravityCreditsHintFresh(hint AntigravityCreditsHint, now time.Time) bool {
	return hint.Known && !hint.UpdatedAt.IsZero() && now.Sub(hint.UpdatedAt) < AntigravityCreditsHintRefreshInterval
}

var antigravityCreditsHintByAuth sync.Map


func SetAntigravityCreditsHint(authID string, hint AntigravityCreditsHint) {
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return
	}
	if hint.UpdatedAt.IsZero() {
		hint.UpdatedAt = time.Now()
	}
	if _, homeMode, _ := homekv.CurrentKVClient(); homeMode {
		homekv.KVSetJSONBestEffort(context.Background(), antigravityCreditsHintKey(authID), hint, 30*time.Minute)
		return
	}
	antigravityCreditsHintByAuth.Store(authID, hint)
}


func GetAntigravityCreditsHint(authID string) (AntigravityCreditsHint, bool) {
	hint, ok, err := GetAntigravityCreditsHintRequired(context.Background(), authID)
	if err == nil {
		return hint, ok
	}
	return AntigravityCreditsHint{}, false
}


func GetAntigravityCreditsHintRequired(ctx context.Context, authID string) (AntigravityCreditsHint, bool, error) {
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return AntigravityCreditsHint{}, false, nil
	}
	var homeHint AntigravityCreditsHint
	homeMode, found, errGet := homekv.KVGetJSONRequired(ctx, antigravityCreditsHintKey(authID), &homeHint)
	if homeMode {
		return homeHint, found, errGet
	}
	value, ok := antigravityCreditsHintByAuth.Load(authID)
	if !ok {
		return AntigravityCreditsHint{}, false, nil
	}
	hint, ok := value.(AntigravityCreditsHint)
	if !ok {
		antigravityCreditsHintByAuth.Delete(authID)
		return AntigravityCreditsHint{}, false, nil
	}
	return hint, true, nil
}


func HasKnownAntigravityCreditsHint(authID string) bool {
	hint, ok := GetAntigravityCreditsHint(authID)
	return ok && hint.Known
}

func antigravityCreditsHintKey(authID string) string {
	return "cpa:antigravity:credits-hint:" + strings.TrimSpace(authID)
}

func antigravityCreditsAvailableForModel(auth *Auth, model string) bool {
	if auth == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(auth.Provider), "antigravity") {
		return false
	}
	if !strings.Contains(strings.ToLower(strings.TrimSpace(model)), "claude") {
		return false
	}
	hint, ok := GetAntigravityCreditsHint(auth.ID)
	if !ok || !hint.Known {
		return false
	}
	return hint.Available
}
