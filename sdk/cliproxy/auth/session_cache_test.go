package auth

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestSessionCache_CapacityEvictionOrder(t *testing.T) {
	t.Parallel()

	maxEntries := 5
	cache := NewSessionCacheWithCapacity(time.Hour, maxEntries)
	defer cache.Stop()


	for i := 1; i <= 5; i++ {
		cache.Set(fmt.Sprintf("sess-%d", i), fmt.Sprintf("auth-%d", i))
	}

	if cache.Len() != 5 {
		t.Fatalf("cache.Len() = %d, want 5", cache.Len())
	}


	cache.Set("sess-6", "auth-6")

	if cache.Len() > maxEntries {
		t.Fatalf("cache.Len() = %d, want <= %d", cache.Len(), maxEntries)
	}

	if _, ok := cache.Get("sess-1"); ok {
		t.Fatal("expected oldest entry sess-1 to be evicted")
	}

	for i := 2; i <= 6; i++ {
		if got, ok := cache.Get(fmt.Sprintf("sess-%d", i)); !ok || got != fmt.Sprintf("auth-%d", i) {
			t.Fatalf("Get(sess-%d) = %q, %v; want auth-%d, true", i, got, ok, i)
		}
	}
}

func TestSessionCache_MultiAliasGroupEviction(t *testing.T) {
	t.Parallel()

	maxEntries := 4
	cache := NewSessionCacheWithCapacity(time.Hour, maxEntries)
	defer cache.Stop()


	cache.SetAliases("auth-1", "s1-a", "s1-b")

	cache.SetAliases("auth-2", "s2-a", "s2-b")

	if cache.Len() != 4 {
		t.Fatalf("cache.Len() = %d, want 4", cache.Len())
	}



	cache.Set("s3", "auth-3")

	if cache.Len() > maxEntries {
		t.Fatalf("cache.Len() = %d, want <= %d", cache.Len(), maxEntries)
	}

	if _, ok := cache.Get("s1-a"); ok {
		t.Fatal("expected s1-a to be evicted")
	}
	if _, ok := cache.Get("s1-b"); ok {
		t.Fatal("expected s1-b to be evicted")
	}

	if got, ok := cache.Get("s2-a"); !ok || got != "auth-2" {
		t.Fatalf("Get(s2-a) = %q, %v", got, ok)
	}
	if got, ok := cache.Get("s3"); !ok || got != "auth-3" {
		t.Fatalf("Get(s3) = %q, %v", got, ok)
	}
}

func TestSessionCache_HighThroughputCapacitySaturated(t *testing.T) {
	t.Parallel()

	maxEntries := 1000
	cache := NewSessionCacheWithCapacity(time.Hour, maxEntries)
	defer cache.Stop()


	for i := 0; i < 10000; i++ {
		cache.Set(fmt.Sprintf("sess-%d", i), fmt.Sprintf("auth-%d", i%10))
	}

	if cache.Len() > maxEntries {
		t.Fatalf("cache.Len() = %d, want <= %d", cache.Len(), maxEntries)
	}


	for i := 9900; i < 10000; i++ {
		if got, ok := cache.Get(fmt.Sprintf("sess-%d", i)); !ok || got != fmt.Sprintf("auth-%d", i%10) {
			t.Fatalf("Get(sess-%d) = %q, %v", i, got, ok)
		}
	}
}

func TestSessionCache_ConcurrentSaturatedAccess(t *testing.T) {
	t.Parallel()

	maxEntries := 50
	cache := NewSessionCacheWithCapacity(time.Hour, maxEntries)
	defer cache.Stop()

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				key := fmt.Sprintf("worker-%d-sess-%d", w, i)
				cache.Set(key, fmt.Sprintf("auth-%d", w))
				cache.Get(key)
				if i%5 == 0 {
					cache.Touch(key, fmt.Sprintf("auth-%d", w))
				}
				if i%7 == 0 {
					cache.CompareAndDelete(key, fmt.Sprintf("auth-%d", w))
				}
			}
		}(worker)
	}
	wg.Wait()

	if cache.Len() > maxEntries {
		t.Fatalf("cache.Len() = %d, want <= %d", cache.Len(), maxEntries)
	}
}

func TestSessionCache_NilReceiverSafety(t *testing.T) {
	t.Parallel()

	var cache *SessionCache
	if got, ok := cache.Get("session-1"); ok || got != "" {
		t.Fatalf("nil.Get() = %q, %v; want '', false", got, ok)
	}
	if got, ok := cache.GetAndRefresh("session-1"); ok || got != "" {
		t.Fatalf("nil.GetAndRefresh() = %q, %v; want '', false", got, ok)
	}
	cache.Set("session-1", "auth-1")
	cache.SetAliases("auth-1", "s1", "s2")
	if ok := cache.Touch("session-1", "auth-1"); ok {
		t.Fatal("nil.Touch() unexpectedly succeeded")
	}
	if ok := cache.CompareAndDelete("session-1", "auth-1"); ok {
		t.Fatal("nil.CompareAndDelete() unexpectedly succeeded")
	}
	cache.Invalidate("session-1")
	cache.InvalidateAuth("auth-1")
	if n := cache.Len(); n != 0 {
		t.Fatalf("nil.Len() = %d, want 0", n)
	}
	cache.Stop()
}

func TestSessionCache_StopNilChannelNoPanic(t *testing.T) {
	t.Parallel()

	zeroCache := &SessionCache{}
	zeroCache.Stop()
}
