package helps

import (
	"net/http"
	"strings"
)

// PreserveClaudeCapabilityHeaders carries new gateway capabilities without a fixed
// allowlist. Headers already set by the credential executor keep their value.
func PreserveClaudeCapabilityHeaders(dst, src http.Header) {
	blocked := map[string]bool{}
	for _, value := range src.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			blocked[strings.ToLower(strings.TrimSpace(name))] = true
		}
	}
	for name, values := range src {
		lower := strings.ToLower(name)
		if blocked[lower] || (!strings.HasPrefix(lower, "anthropic-") && !strings.HasPrefix(lower, "x-claude-code-")) || dst.Get(name) != "" {
			continue
		}
		dst[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
	}
}
