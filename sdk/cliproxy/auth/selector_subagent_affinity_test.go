package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestSessionAffinityAntigravitySubagentInheritsParentByDefault(t *testing.T) {
	t.Parallel()

	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: &RoundRobinSelector{},
		TTL:      time.Minute,
	})
	defer selector.Stop()

	auths := []*Auth{
		{ID: "auth-ag-1", Provider: "antigravity"},
		{ID: "auth-ag-2", Provider: "antigravity"},
	}

	parentOpts := cliproxyexecutor.Options{
		Headers:         http.Header{"X-Claude-Code-Session-Id": []string{"claude-root-100"}},
		OriginalRequest: []byte(`{"messages":[{"role":"user","content":"parent task"}]}`),
		Metadata:        map[string]any{},
	}
	parentAuth, errParent := selector.Pick(context.Background(), "antigravity", "gemini-3.7-flash-high", parentOpts, auths)
	if errParent != nil {
		t.Fatalf("parent Pick() error = %v", errParent)
	}
	if parentAuth.ID != "auth-ag-1" {
		t.Fatalf("parent auth = %q, want auth-ag-1", parentAuth.ID)
	}

	subagentOpts := cliproxyexecutor.Options{
		Headers: http.Header{
			"X-Claude-Code-Session-Id": []string{"claude-root-100"},
			"X-Claude-Code-Agent-Id":   []string{"subagent-001"},
		},
		OriginalRequest: []byte(`{"messages":[{"role":"user","content":"subagent task"}]}`),
		Metadata:        map[string]any{},
	}
	subagentAuth, errSub := selector.Pick(context.Background(), "antigravity", "gemini-3.7-flash-high", subagentOpts, auths)
	if errSub != nil {
		t.Fatalf("subagent Pick() error = %v", errSub)
	}
	if subagentAuth.ID != parentAuth.ID {
		t.Fatalf("subagent did not inherit parent auth %q, got %q", parentAuth.ID, subagentAuth.ID)
	}

	subagentTurn2Auth, errSub2 := selector.Pick(context.Background(), "antigravity", "gemini-3.7-flash-high", subagentOpts, auths)
	if errSub2 != nil {
		t.Fatalf("subagent turn 2 Pick() error = %v", errSub2)
	}
	if subagentTurn2Auth.ID != "auth-ag-1" {
		t.Fatalf("subagent turn 2 did not stay sticky: got %q, want auth-ag-1", subagentTurn2Auth.ID)
	}
}

func TestSessionAffinityGeminiSubagentInheritsParentWhenExplicitlyTrue(t *testing.T) {
	t.Parallel()

	enabled := true
	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback:         &RoundRobinSelector{},
		TTL:              time.Minute,
		SubagentAffinity: &enabled,
	})
	defer selector.Stop()

	auths := []*Auth{
		{ID: "auth-gem-1", Provider: "gemini"},
		{ID: "auth-gem-2", Provider: "gemini"},
	}

	parentOpts := cliproxyexecutor.Options{
		Headers:         http.Header{"X-Claude-Code-Session-Id": []string{"gem-root-200"}},
		OriginalRequest: []byte(`{"messages":[{"role":"user","content":"parent gemini task"}]}`),
		Metadata:        map[string]any{},
	}
	parentAuth, errParent := selector.Pick(context.Background(), "gemini", "gemini-2.5-pro", parentOpts, auths)
	if errParent != nil {
		t.Fatalf("parent Pick() error = %v", errParent)
	}
	if parentAuth.ID != "auth-gem-1" {
		t.Fatalf("parent auth = %q, want auth-gem-1", parentAuth.ID)
	}

	subagentOpts := cliproxyexecutor.Options{
		Headers: http.Header{
			"X-Claude-Code-Session-Id": []string{"gem-root-200"},
			"X-Claude-Code-Agent-Id":   []string{"gem-sub-001"},
		},
		OriginalRequest: []byte(`{"messages":[{"role":"user","content":"subagent gemini task"}]}`),
		Metadata:        map[string]any{},
	}
	subagentAuth, errSub := selector.Pick(context.Background(), "gemini", "gemini-2.5-pro", subagentOpts, auths)
	if errSub != nil {
		t.Fatalf("subagent Pick() error = %v", errSub)
	}
	if subagentAuth.ID != parentAuth.ID {
		t.Fatalf("subagent did not inherit parent auth %q, got %q", parentAuth.ID, subagentAuth.ID)
	}
}

func TestSessionAffinitySubagentIsolatesWhenExplicitlyFalse(t *testing.T) {
	t.Parallel()

	disabled := false
	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback:         &RoundRobinSelector{},
		TTL:              time.Minute,
		SubagentAffinity: &disabled,
	})
	defer selector.Stop()

	auths := []*Auth{
		{ID: "auth-1", Provider: "claude"},
		{ID: "auth-2", Provider: "claude"},
	}

	parentOpts := cliproxyexecutor.Options{
		Headers:         http.Header{"X-Claude-Code-Session-Id": []string{"sess-iso-300"}},
		OriginalRequest: []byte(`{"messages":[{"role":"user","content":"parent task"}]}`),
		Metadata:        map[string]any{},
	}
	parentAuth, errParent := selector.Pick(context.Background(), "claude", "claude-3-7-sonnet", parentOpts, auths)
	if errParent != nil {
		t.Fatalf("parent Pick() error = %v", errParent)
	}
	if parentAuth.ID != "auth-1" {
		t.Fatalf("parent auth = %q, want auth-1", parentAuth.ID)
	}

	subagentOpts := cliproxyexecutor.Options{
		Headers: http.Header{
			"X-Claude-Code-Session-Id": []string{"sess-iso-300"},
			"X-Claude-Code-Agent-Id":   []string{"sub-iso-001"},
		},
		OriginalRequest: []byte(`{"messages":[{"role":"user","content":"subagent task"}]}`),
		Metadata:        map[string]any{},
	}
	subagentAuth, errSub := selector.Pick(context.Background(), "claude", "claude-3-7-sonnet", subagentOpts, auths)
	if errSub != nil {
		t.Fatalf("subagent Pick() error = %v", errSub)
	}
	if subagentAuth.ID == parentAuth.ID {
		t.Fatalf("subagent incorrectly inherited parent auth %q when SubagentAffinity is false", parentAuth.ID)
	}
	if subagentAuth.ID != "auth-2" {
		t.Fatalf("subagent auth = %q, want auth-2", subagentAuth.ID)
	}
}

func TestSessionAffinitySubagentFailureIsolation(t *testing.T) {
	t.Parallel()

	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: &RoundRobinSelector{},
		TTL:      time.Minute,
	})
	defer selector.Stop()

	auths := []*Auth{
		{ID: "auth-1", Provider: "antigravity"},
		{ID: "auth-2", Provider: "antigravity"},
	}

	parentOpts := cliproxyexecutor.Options{
		Headers:         http.Header{"X-Claude-Code-Session-Id": []string{"sess-fail-400"}},
		OriginalRequest: []byte(`{"messages":[{"role":"user","content":"parent task"}]}`),
		Metadata:        map[string]any{},
	}
	parentAuth, errParent := selector.Pick(context.Background(), "antigravity", "gemini-3.7-flash-high", parentOpts, auths)
	if errParent != nil {
		t.Fatalf("parent Pick() error = %v", errParent)
	}
	if parentAuth.ID != "auth-1" {
		t.Fatalf("parent auth = %q, want auth-1", parentAuth.ID)
	}

	parentCacheKey := "antigravity::claude:sess-fail-400::gemini-3.7-flash-high"
	if bound, ok := selector.cache.Get(parentCacheKey); !ok || bound != "auth-1" {
		t.Fatalf("parent cache binding = (%q, %v), want (auth-1, true)", bound, ok)
	}

	subagentOpts := cliproxyexecutor.Options{
		Headers: http.Header{
			"X-Claude-Code-Session-Id": []string{"sess-fail-400"},
			"X-Claude-Code-Agent-Id":   []string{"sub-fail-001"},
		},
		OriginalRequest: []byte(`{"messages":[{"role":"user","content":"subagent task"}]}`),
		Metadata:        map[string]any{},
	}
	subagentAuth, errSub := selector.Pick(context.Background(), "antigravity", "gemini-3.7-flash-high", subagentOpts, auths)
	if errSub != nil {
		t.Fatalf("subagent Pick() error = %v", errSub)
	}
	if subagentAuth.ID != "auth-1" {
		t.Fatalf("subagent auth = %q, want auth-1", subagentAuth.ID)
	}

	subagentCacheKey := "antigravity::claude:sess-fail-400:agent:sub-fail-001::gemini-3.7-flash-high"
	if bound, ok := selector.cache.Get(subagentCacheKey); !ok || bound != "auth-1" {
		t.Fatalf("subagent cache binding = (%q, %v), want (auth-1, true)", bound, ok)
	}

	selector.OnResult(Result{
		AuthID:  "auth-1",
		Success: false,
		Options: subagentOpts,
	})

	if bound, ok := selector.cache.Get(subagentCacheKey); ok {
		t.Fatalf("subagent cache binding was not cleared after failure: got %q", bound)
	}

	if bound, ok := selector.cache.Get(parentCacheKey); !ok || bound != "auth-1" {
		t.Fatalf("parent binding was damaged by subagent failure: got (%q, %v), want (auth-1, true)", bound, ok)
	}

	parentTurn2Auth, errParent2 := selector.Pick(context.Background(), "antigravity", "gemini-3.7-flash-high", parentOpts, auths)
	if errParent2 != nil {
		t.Fatalf("parent turn 2 Pick() error = %v", errParent2)
	}
	if parentTurn2Auth.ID != "auth-1" {
		t.Fatalf("parent turn 2 auth = %q, want auth-1", parentTurn2Auth.ID)
	}
}

func TestSessionAffinitySubagentAliasIsolation(t *testing.T) {
	t.Parallel()

	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: &RoundRobinSelector{},
		TTL:      time.Minute,
	})
	defer selector.Stop()

	auths := []*Auth{
		{ID: "auth-1", Provider: "antigravity"},
		{ID: "auth-2", Provider: "antigravity"},
	}

	parentOpts := cliproxyexecutor.Options{
		Headers:         http.Header{"X-Claude-Code-Session-Id": []string{"sess-alias-500"}},
		OriginalRequest: []byte(`{"messages":[{"role":"user","content":"parent task"}]}`),
		Metadata:        map[string]any{},
	}
	_, errParent := selector.Pick(context.Background(), "antigravity", "gemini-3.7-flash-high", parentOpts, auths)
	if errParent != nil {
		t.Fatalf("parent Pick() error = %v", errParent)
	}

	subagentOpts := cliproxyexecutor.Options{
		Headers: http.Header{
			"X-Claude-Code-Session-Id": []string{"sess-alias-500"},
			"X-Claude-Code-Agent-Id":   []string{"sub-alias-001"},
		},
		OriginalRequest: []byte(`{"messages":[{"role":"user","content":"sub task"}]}`),
		Metadata:        map[string]any{},
	}
	_, errSub := selector.Pick(context.Background(), "antigravity", "gemini-3.7-flash-high", subagentOpts, auths)
	if errSub != nil {
		t.Fatalf("subagent Pick() error = %v", errSub)
	}

	selector.OnResult(Result{
		AuthID:  "auth-1",
		Success: false,
		Options: subagentOpts,
	})

	parentCacheKey := "antigravity::claude:sess-alias-500::gemini-3.7-flash-high"
	val, ok := selector.cache.Get(parentCacheKey)
	if !ok || val != "auth-1" {
		t.Fatalf("parent cache key %s = (%q, %v), want (auth-1, true)", parentCacheKey, val, ok)
	}
}
