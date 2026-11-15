package helps

import (
	"errors"
	"sync"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)






func TestApplyClaudeCredentialMetadataConcurrentSharedAuth(t *testing.T) {
	auth := &cliproxyauth.Auth{
		ID:       "shared-credential",
		Metadata: map[string]any{"account_uuid": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
	}
	payload := []byte(`{"model":"claude-opus-4-6","messages":[{"role":"user","content":"hi"}]}`)

	const goroutines = 32
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	start := make(chan struct{})

	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			sessionID := "session-" + string(rune('a'+i%26))
			if _, _, err := ApplyClaudeCredentialMetadata(payload, auth, sessionID); err != nil {
				errs <- err
				return
			}

			_ = ClaudeCredentialAccountUUID(auth)
		}(i)
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("ApplyClaudeCredentialMetadata on shared auth: %v", err)
	}

	if auth.Metadata == nil {
		t.Fatal("expected metadata to be initialized")
	}
}



func TestEnsureClaudeCredentialDevicePoolConcurrentSharedAuth(t *testing.T) {
	auth := &cliproxyauth.Auth{ID: "shared-credential"}

	const goroutines = 32
	var wg sync.WaitGroup
	results := make(chan string, goroutines)
	errs := make(chan error, goroutines)
	start := make(chan struct{})

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			deviceIDs, err := EnsureClaudeCredentialDevicePoolRequired(t.Context(), auth)
			if err != nil {
				errs <- err
				return
			}
			if len(deviceIDs) == 0 {
				errs <- errEmptyPool
				return
			}
			results <- deviceIDs[0]
		}()
	}

	close(start)
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Fatalf("EnsureClaudeCredentialDevicePoolRequired on shared auth: %v", err)
	}



	seen := make(map[string]struct{})
	for deviceID := range results {
		seen[deviceID] = struct{}{}
	}
	if len(seen) != 1 {
		t.Fatalf("device pool bootstrap was not stable: got %d distinct device IDs, want 1", len(seen))
	}
}

var errEmptyPool = errors.New("device pool is empty")
