package helps

import (
	"net/http"
	"strings"
)

// PreserveClaudeNativeHeaders forwards caller-owned end-to-end metadata for a
// confirmed native client. Credential, session, payload and transport headers
// remain owned by the executor because it selects a different upstream account
// and may normalize the payload. Never copy proxy credentials or cookies.
func PreserveClaudeNativeHeaders(dst, src http.Header) {
	blocked := map[string]bool{
		"authorization": true, "x-api-key": true, "x-management-key": true,
		"cookie": true, "cookie2": true, "proxy-authorization": true,
		"connection": true, "proxy-connection": true, "keep-alive": true,
		"te": true, "trailer": true, "transfer-encoding": true, "upgrade": true,
		"host": true, "content-length": true, "content-type": true, "content-encoding": true,
		"accept": true, "accept-encoding": true, "anthropic-beta": true,
		"x-claude-code-session-id": true, "x-cliproxy-console": true,
		"forwarded": true, "via": true, "x-real-ip": true,
	}
	for _, value := range src.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			blocked[strings.ToLower(strings.TrimSpace(name))] = true
		}
	}
	for name, values := range src {
		lower := strings.ToLower(name)
		if blocked[lower] || strings.HasPrefix(lower, "x-forwarded-") {
			continue
		}
		dst[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
	}
}
