package helps

import (
	"net/http"
	"reflect"
	"testing"
)

func TestPreserveClaudeNativeHeaders(t *testing.T) {
	src := http.Header{}
	src.Set("User-Agent", "claude-cli/3.0.0 (external, cli)")
	src.Set("X-Stainless-Package-Version", "1.2.3")
	src["X-New-Client-Feature"] = []string{"first", "second"}
	src.Set("Authorization", "local-secret")
	src.Set("X-Api-Key", "local-secret")
	src.Set("Cookie", "private-cookie")
	src.Set("Connection", "X-Hop-Only")
	src.Set("X-Hop-Only", "must-not-forward")
	src.Set("X-Forwarded-For", "private-address")
	src.Set("Anthropic-Beta", "caller-beta")
	src.Set("X-Claude-Code-Session-Id", "caller-session")
	dst := http.Header{}
	dst.Set("Authorization", "Bearer selected-account")
	dst.Set("User-Agent", "cached-old-version")
	dst.Set("Anthropic-Beta", "payload-beta")
	dst.Set("X-Claude-Code-Session-Id", "selected-session")
	PreserveClaudeNativeHeaders(dst, src)
	for _, name := range []string{"User-Agent", "X-Stainless-Package-Version", "X-New-Client-Feature"} {
		if !reflect.DeepEqual(dst.Values(name), src.Values(name)) {
			t.Errorf("%s was not preserved", name)
		}
	}
	for _, name := range []string{"X-Api-Key", "Cookie", "Connection", "X-Hop-Only", "X-Forwarded-For"} {
		if dst.Get(name) != "" {
			t.Errorf("%s leaked", name)
		}
	}
	if dst.Get("Authorization") != "Bearer selected-account" || dst.Get("Anthropic-Beta") != "payload-beta" || dst.Get("X-Claude-Code-Session-Id") != "selected-session" {
		t.Fatal("executor-owned headers overwritten")
	}
	dst["X-New-Client-Feature"][0] = "changed"
	if src.Values("X-New-Client-Feature")[0] != "first" {
		t.Fatal("caller header mutated")
	}
}
