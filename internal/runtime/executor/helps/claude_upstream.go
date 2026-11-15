package helps

import (
	"net/url"
	"strings"
)

func IsAnthropicUpstreamURL(u *url.URL) bool {
	if u == nil || u.User != nil || !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Hostname(), "api.anthropic.com") {
		return false
	}
	port := u.Port()
	return port == "" || port == "443"
}
