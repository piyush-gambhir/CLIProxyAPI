package helps

import (
	"net/http"
	"testing"
)

func TestPreserveClaudeCapabilityHeaders(t *testing.T) {
	src := http.Header{"Anthropic-Future-Capability": {"yes"}, "X-Claude-Code-Agent-Id": {"agent"}, "Anthropic-Beta": {"incoming"}, "Authorization": {"never"}, "Connection": {"Anthropic-Hop"}, "Anthropic-Hop": {"blocked"}}
	dst := http.Header{"Anthropic-Beta": {"managed"}}
	PreserveClaudeCapabilityHeaders(dst, src)
	if dst.Get("Anthropic-Future-Capability") != "yes" || dst.Get("X-Claude-Code-Agent-Id") != "agent" || dst.Get("Anthropic-Beta") != "managed" || dst.Get("Authorization") != "" || dst.Get("Anthropic-Hop") != "" {
		t.Fatalf("headers: %v", dst)
	}
}
