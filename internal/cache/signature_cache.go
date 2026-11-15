package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	homekv "github.com/router-for-me/CLIProxyAPI/v8/internal/home"
	log "github.com/sirupsen/logrus"
)

type SignatureEntry struct {
	Signature string
	Timestamp time.Time
}

const (
	SignatureCacheTTL = 3 * time.Hour

	SignatureTextHashLen = 16

	MinValidSignatureLen = 50

	CacheCleanupInterval = 10 * time.Minute
)

var signatureCache sync.Map

var cacheCleanupOnce sync.Once

type signatureKVClient interface {
	KVGet(ctx context.Context, key string) ([]byte, bool, error)
	KVSet(ctx context.Context, key string, value []byte, opts homekv.KVSetOptions) (bool, error)
	KVDel(ctx context.Context, keys ...string) (int64, error)
	KVExpire(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

var currentSignatureKVClient = func() (signatureKVClient, bool, error) {
	return homekv.CurrentKVClient()
}

type groupCache struct {
	mu      sync.RWMutex
	entries map[string]SignatureEntry
}

func hashText(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])[:SignatureTextHashLen]
}

func getOrCreateGroupCache(groupKey string) *groupCache {

	cacheCleanupOnce.Do(startCacheCleanup)

	if val, ok := signatureCache.Load(groupKey); ok {
		return val.(*groupCache)
	}
	sc := &groupCache{entries: make(map[string]SignatureEntry)}
	actual, _ := signatureCache.LoadOrStore(groupKey, sc)
	return actual.(*groupCache)
}

func startCacheCleanup() {
	go func() {
		ticker := time.NewTicker(CacheCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			purgeExpiredCaches()
		}
	}()
}

func purgeExpiredCaches() {
	now := time.Now()
	signatureCache.Range(func(key, value any) bool {
		sc := value.(*groupCache)
		sc.mu.Lock()

		for k, entry := range sc.entries {
			if now.Sub(entry.Timestamp) > SignatureCacheTTL {
				delete(sc.entries, k)
			}
		}
		isEmpty := len(sc.entries) == 0
		sc.mu.Unlock()

		if isEmpty {
			signatureCache.Delete(key)
		}
		return true
	})
	purgeExpiredCodexReasoningReplayCache(now)
	purgeExpiredXAIReasoningReplayCache(now)
	purgeExpiredAntigravityReasoningReplayCache(now)
	purgeExpiredKimiThinkingReplayCache(now)
	purgeExpiredClaudeThinkingReplayCache(now)
	purgeExpiredClaudeWebSearchResultCache(now)
}

func CacheSignature(modelName, text, signature string) {
	CacheSignatureBestEffort(context.Background(), modelName, text, signature)
}

func CacheSignatureBestEffort(ctx context.Context, modelName, text, signature string) bool {
	if text == "" || signature == "" {
		return false
	}
	if len(signature) < MinValidSignatureLen {
		return false
	}

	if client, homeMode, errClient := currentSignatureKVClient(); homeMode {
		if errClient != nil {
			log.Errorf("home kv best-effort signature set failed prefix=cpa:signature:*: %v", errClient)
			return false
		}
		written, errSet := client.KVSet(ctx, signatureKVKey(modelName, text), []byte(signature), homekv.KVSetOptions{EX: SignatureCacheTTL})
		if errSet != nil {
			log.Errorf("home kv best-effort signature set failed prefix=cpa:signature:*: %v", errSet)
			return false
		}
		return written
	}

	groupKey := GetModelGroup(modelName)
	textHash := hashText(text)
	sc := getOrCreateGroupCache(groupKey)
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.entries[textHash] = SignatureEntry{
		Signature: signature,
		Timestamp: time.Now(),
	}
	return true
}

func GetCachedSignature(modelName, text string) string {
	signature, errSignature := GetCachedSignatureRequired(context.Background(), modelName, text)
	if errSignature != nil {
		return ""
	}
	return signature
}

func GetCachedSignatureRequired(ctx context.Context, modelName, text string) (string, error) {
	groupKey := GetModelGroup(modelName)

	if text == "" {
		if groupKey == "gemini" {
			return "skip_thought_signature_validator", nil
		}
		return "", nil
	}

	if client, homeMode, errClient := currentSignatureKVClient(); homeMode {
		if errClient != nil {
			return "", errClient
		}
		key := signatureKVKey(modelName, text)
		raw, found, errGet := client.KVGet(ctx, key)
		if errGet != nil {
			return "", errGet
		}
		if !found {
			if groupKey == "gemini" {
				return "skip_thought_signature_validator", nil
			}
			return "", nil
		}
		if _, errExpire := client.KVExpire(ctx, key, SignatureCacheTTL); errExpire != nil {
			return "", errExpire
		}
		return string(raw), nil
	}

	val, ok := signatureCache.Load(groupKey)
	if !ok {
		if groupKey == "gemini" {
			return "skip_thought_signature_validator", nil
		}
		return "", nil
	}
	sc := val.(*groupCache)

	textHash := hashText(text)

	now := time.Now()

	sc.mu.Lock()
	entry, exists := sc.entries[textHash]
	if !exists {
		sc.mu.Unlock()
		if groupKey == "gemini" {
			return "skip_thought_signature_validator", nil
		}
		return "", nil
	}
	if now.Sub(entry.Timestamp) > SignatureCacheTTL {
		delete(sc.entries, textHash)
		sc.mu.Unlock()
		if groupKey == "gemini" {
			return "skip_thought_signature_validator", nil
		}
		return "", nil
	}

	entry.Timestamp = now
	sc.entries[textHash] = entry
	sc.mu.Unlock()

	return entry.Signature, nil
}

func ClearSignatureCache(modelName string) {
	if modelName == "" {
		signatureCache.Range(func(key, _ any) bool {
			signatureCache.Delete(key)
			return true
		})
		return
	}
	groupKey := GetModelGroup(modelName)
	signatureCache.Delete(groupKey)
}

func DeleteCachedSignatureRequired(ctx context.Context, modelName, text string) error {
	if text == "" {
		return nil
	}
	if client, homeMode, errClient := currentSignatureKVClient(); homeMode {
		if errClient != nil {
			return errClient
		}
		_, errDel := client.KVDel(ctx, signatureKVKey(modelName, text))
		return errDel
	}
	groupKey := GetModelGroup(modelName)
	textHash := hashText(text)
	val, ok := signatureCache.Load(groupKey)
	if !ok {
		return nil
	}
	sc := val.(*groupCache)
	sc.mu.Lock()
	delete(sc.entries, textHash)
	isEmpty := len(sc.entries) == 0
	sc.mu.Unlock()
	if isEmpty {
		signatureCache.Delete(groupKey)
	}
	return nil
}

func HasValidSignature(modelName, signature string) bool {
	return (signature != "" && len(signature) >= MinValidSignatureLen) || (signature == "skip_thought_signature_validator" && GetModelGroup(modelName) == "gemini")
}

func GetModelGroup(modelName string) string {
	if strings.Contains(modelName, "gpt") {
		return "gpt"
	} else if strings.Contains(modelName, "claude") {
		return "claude"
	} else if strings.Contains(modelName, "gemini") {
		return "gemini"
	}
	return modelName
}

func signatureKVKey(modelName, text string) string {
	return fmt.Sprintf("cpa:signature:%s:%s", GetModelGroup(modelName), homekv.HashKeyPart(text))
}

var signatureCacheEnabled atomic.Bool
var signatureBypassStrictMode atomic.Bool

func init() {
	signatureCacheEnabled.Store(true)
	signatureBypassStrictMode.Store(false)
}

func SetSignatureCacheEnabled(enabled bool) {
	previous := signatureCacheEnabled.Swap(enabled)
	if previous == enabled {
		return
	}
	if !enabled {
		log.Info("antigravity signature cache DISABLED - bypass mode active, cached signatures will not be used for request translation")
	}
}

func SignatureCacheEnabled() bool {
	return signatureCacheEnabled.Load()
}

func SetSignatureBypassStrictMode(strict bool) {
	previous := signatureBypassStrictMode.Swap(strict)
	if previous == strict {
		return
	}
	if strict {
		log.Debug("antigravity bypass signature validation: strict mode (protobuf tree)")
	} else {
		log.Debug("antigravity bypass signature validation: basic mode (R/E + 0x12)")
	}
}

func SignatureBypassStrictMode() bool {
	return signatureBypassStrictMode.Load()
}
