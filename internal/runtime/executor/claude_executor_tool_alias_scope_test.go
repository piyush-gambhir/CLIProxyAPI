package executor

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)





func TestClaudeOAuthToolAliasStoreIsSharedAcrossForAPIKeyCopies(t *testing.T) {
	createBody := []byte(`{"thread":{"type":"create"}}`)
	continuationBody := []byte(`{"thread":{"type":"continue","previous_message_id":"msg-shared"}}`)
	cases := map[string]cliproxyauth.APIKeyConfigExecutor{
		"claude": NewClaudeExecutor(&config.Config{}),
		"kimi":   NewKimiExecutor(&config.Config{}),
	}
	for name, registered := range cases {
		t.Run(name, func(t *testing.T) {
			first := claudeExecutorForAliasTest(t, registered.ForAPIKey())
			second := claudeExecutorForAliasTest(t, registered.ForAPIKey())
			if first == second {
				t.Fatal("ForAPIKey returned the same executor; test needs two scoped copies")
			}
			first.rememberClaudeOAuthToolAliases(createBody, map[string]string{"alias": "Read"}, "msg-shared")
			_, aliases, err := second.prepareClaudeOAuthToolNamesForRequest(continuationBody, claudeMCPAliasOptions{secret: "shared-store"})
			if err != nil {
				t.Fatalf("continuation on another scoped copy: %v", err)
			}
			if aliases["alias"] != "Read" {
				t.Fatalf("aliases = %#v, want alias -> Read", aliases)
			}
		})
	}
}

func claudeExecutorForAliasTest(t *testing.T, scoped cliproxyauth.ProviderExecutor) *ClaudeExecutor {
	t.Helper()
	switch executor := scoped.(type) {
	case *ClaudeExecutor:
		return executor
	case *KimiExecutor:
		return &executor.ClaudeExecutor
	default:
		t.Fatalf("unexpected scoped executor %T", scoped)
		return nil
	}
}
