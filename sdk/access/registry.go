package access

import (
	"context"
	"net/http"
	"strings"
	"sync"
)

type Provider interface {
	Identifier() string
	Authenticate(ctx context.Context, r *http.Request) (*Result, *AuthError)
}

type Result struct {
	Provider  string
	Principal string
	Metadata  map[string]string
}

var (
	registryMu        sync.RWMutex
	registry          = make(map[string]Provider)
	order             []string
	exclusiveProvider string
)

func RegisterProvider(typ string, provider Provider) {
	normalizedType := strings.TrimSpace(typ)
	if normalizedType == "" || provider == nil {
		return
	}

	registryMu.Lock()
	if _, exists := registry[normalizedType]; !exists {
		order = append(order, normalizedType)
	}
	registry[normalizedType] = provider
	registryMu.Unlock()
}

func UnregisterProvider(typ string) {
	normalizedType := strings.TrimSpace(typ)
	if normalizedType == "" {
		return
	}
	registryMu.Lock()
	if _, exists := registry[normalizedType]; !exists {
		registryMu.Unlock()
		return
	}
	delete(registry, normalizedType)
	for index := range order {
		if order[index] != normalizedType {
			continue
		}
		order = append(order[:index], order[index+1:]...)
		break
	}
	registryMu.Unlock()
}

func SetExclusiveProvider(typ string) {
	normalizedType := strings.TrimSpace(typ)
	registryMu.Lock()
	exclusiveProvider = normalizedType
	registryMu.Unlock()
}

func ClearExclusiveProvider() {
	registryMu.Lock()
	exclusiveProvider = ""
	registryMu.Unlock()
}

func RegisteredProviders() []Provider {
	registryMu.RLock()
	if len(order) == 0 {
		registryMu.RUnlock()
		return nil
	}
	if exclusiveProvider != "" {
		if provider, exists := registry[exclusiveProvider]; exists && provider != nil {
			registryMu.RUnlock()
			return []Provider{provider}
		}
	}
	providers := make([]Provider, 0, len(order))
	for _, providerType := range order {
		provider, exists := registry[providerType]
		if !exists || provider == nil {
			continue
		}
		providers = append(providers, provider)
	}
	registryMu.RUnlock()
	return providers
}
