package helps

import (
	"context"
	"sync"
	"time"

	homekv "github.com/router-for-me/CLIProxyAPI/v8/internal/home"
)

type CodexCache struct {
	ID     string
	Expire time.Time
}

var (
	codexCacheMap = make(map[string]CodexCache)
	codexCacheMu  sync.RWMutex
)

const codexCacheCleanupInterval = 15 * time.Minute

var codexCacheCleanupOnce sync.Once

func startCodexCacheCleanup() {
	go func() {
		ticker := time.NewTicker(codexCacheCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			purgeExpiredCodexCache()
		}
	}()
}

func purgeExpiredCodexCache() {
	now := time.Now()
	codexCacheMu.Lock()
	defer codexCacheMu.Unlock()
	for key, cache := range codexCacheMap {
		if cache.Expire.Before(now) {
			delete(codexCacheMap, key)
		}
	}
}

func GetCodexCache(key string) (CodexCache, bool) {
	cache, ok, err := GetCodexCacheRequired(context.Background(), key)
	if err == nil {
		return cache, ok
	}
	return CodexCache{}, false
}

func GetCodexCacheRequired(ctx context.Context, key string) (CodexCache, bool, error) {
	var homeCache CodexCache
	homeMode, found, errGet := homekv.KVGetJSONRequired(ctx, key, &homeCache)
	if homeMode {
		if errGet != nil || !found {
			return CodexCache{}, false, errGet
		}
		if homeCache.Expire.Before(time.Now()) {
			_, _, _ = homekv.KVDelRequired(ctx, key)
			return CodexCache{}, false, nil
		}
		return homeCache, true, nil
	}

	codexCacheCleanupOnce.Do(startCodexCacheCleanup)
	codexCacheMu.RLock()
	cache, ok := codexCacheMap[key]
	codexCacheMu.RUnlock()
	if !ok || cache.Expire.Before(time.Now()) {
		return CodexCache{}, false, nil
	}
	return cache, true, nil
}

func SetCodexCache(key string, cache CodexCache) {
	SetCodexCacheBestEffort(context.Background(), key, cache)
}

func SetCodexCacheRequired(ctx context.Context, key string, cache CodexCache) error {
	ttl := time.Until(cache.Expire)
	if ttl <= 0 {
		return nil
	}
	if _, homeMode, _ := homekv.CurrentKVClient(); homeMode {
		_, errSet := homekv.KVSetJSONRequired(ctx, key, cache, ttl)
		return errSet
	}
	codexCacheCleanupOnce.Do(startCodexCacheCleanup)
	codexCacheMu.Lock()
	codexCacheMap[key] = cache
	codexCacheMu.Unlock()
	return nil
}

func SetCodexCacheBestEffort(ctx context.Context, key string, cache CodexCache) bool {
	ttl := time.Until(cache.Expire)
	if ttl <= 0 {
		return false
	}
	if _, homeMode, _ := homekv.CurrentKVClient(); homeMode {
		return homekv.KVSetJSONBestEffort(ctx, key, cache, ttl)
	}
	codexCacheCleanupOnce.Do(startCodexCacheCleanup)
	codexCacheMu.Lock()
	codexCacheMap[key] = cache
	codexCacheMu.Unlock()
	return true
}

func CodexPromptCacheKey(modelName string, userScope string) string {
	return "cpa:codex:prompt-cache:" + homekv.HashKeyPart(modelName) + ":" + homekv.HashKeyPart(userScope)
}
