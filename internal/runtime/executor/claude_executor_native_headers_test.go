package executor

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"net/http"
	"testing"
)

func TestApplyClaudeHeadersPreservesOptedInNativeMetadata(t *testing.T) {
	for _, confirmed := range []bool{true, false} {
		incoming := http.Header{}
		incoming.Set("User-Agent", "claude-cli/3.0.0 (external, cli)")
		incoming.Set("X-Stainless-Package-Version", "1.2.3")
		incoming.Set("X-New-Client-Feature", "new-feature")
		incoming.Set("Authorization", "local-proxy-secret")
		incoming.Set("Cookie", "private-cookie")
		cfg := &config.Config{ClaudeHeaderDefaults: config.ClaudeHeaderDefaults{PreserveNativeHeaders: true}}
		req := newClaudeHeaderTestRequest(t, nil)
		err := applyClaudeHeaders(req, claudeOAuthAuthForBetaPolicy(), claudeRaceProbeOAuthKey, false, nil, []byte(`{"model":"claude-opus-5"}`), cfg, incoming, confirmed)
		if err != nil {
			t.Fatal(err)
		}
		if confirmed {
			for _, name := range []string{"User-Agent", "X-Stainless-Package-Version", "X-New-Client-Feature"} {
				if req.Header.Get(name) != incoming.Get(name) {
					t.Errorf("%s not forwarded", name)
				}
			}
		} else if req.Header.Get("X-New-Client-Feature") != "" {
			t.Fatal("unconfirmed client received native forwarding")
		}
		if req.Header.Get("Authorization") != "Bearer "+claudeRaceProbeOAuthKey || req.Header.Get("Cookie") != "" {
			t.Fatal("caller credentials forwarded")
		}
	}
}
