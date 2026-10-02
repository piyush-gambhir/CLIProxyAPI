package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// readSavedToken saves a storage to a temp file and returns the written JSON.
func readSavedToken(t *testing.T, ts *CodexTokenStorage) map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex-test.json")
	if err := ts.SaveTokenToFile(path); err != nil {
		t.Fatalf("SaveTokenToFile() error = %v", err)
	}
	data, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatalf("ReadFile() error = %v", errRead)
	}
	var saved map[string]any
	if errUnmarshal := json.Unmarshal(data, &saved); errUnmarshal != nil {
		t.Fatalf("Unmarshal() error = %v", errUnmarshal)
	}
	return saved
}

// TestSaveTokenDefaultsWebsocketsOnWhenAbsent covers a fresh Codex login: the
// credential opts into the Responses over WebSocket transport by default.
func TestSaveTokenDefaultsWebsocketsOnWhenAbsent(t *testing.T) {
	t.Parallel()

	saved := readSavedToken(t, &CodexTokenStorage{
		AccessToken: "test-access-token", RefreshToken: "test-refresh-token", Email: "pat@example.com",
	})
	if saved["websockets"] != true {
		t.Fatalf("websockets = %v, want true", saved["websockets"])
	}
	if saved["type"] != "codex" {
		t.Fatalf("type = %v, want codex", saved["type"])
	}
}

// TestSaveTokenKeepsAnExplicitWebsocketsChoice checks both explicit values
// survive, so an operator who turned the transport off is not overridden.
func TestSaveTokenKeepsAnExplicitWebsocketsChoice(t *testing.T) {
	t.Parallel()

	for _, want := range []bool{false, true} {
		ts := &CodexTokenStorage{AccessToken: "test-access-token"}
		ts.SetMetadata(map[string]any{"websockets": want})
		saved := readSavedToken(t, ts)
		if saved["websockets"] != want {
			t.Fatalf("websockets = %v, want %v", saved["websockets"], want)
		}
	}
}

// TestSaveTokenPreservesOtherMetadata checks the default does not disturb the
// rest of the injected metadata.
func TestSaveTokenPreservesOtherMetadata(t *testing.T) {
	t.Parallel()

	ts := &CodexTokenStorage{AccessToken: "test-access-token"}
	ts.SetMetadata(map[string]any{"priority": 5, "note": "pool member"})
	saved := readSavedToken(t, ts)
	if saved["websockets"] != true {
		t.Fatalf("websockets = %v, want true", saved["websockets"])
	}
	if saved["note"] != "pool member" {
		t.Fatalf("note = %v, want pool member", saved["note"])
	}
	if priority, _ := saved["priority"].(float64); priority != 5 {
		t.Fatalf("priority = %v, want 5", saved["priority"])
	}
}
