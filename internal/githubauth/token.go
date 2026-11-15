
package githubauth

import (
	"net/url"
	"os"
	"strings"
	"sync/atomic"
)

var configuredToken atomic.Pointer[string]


func SetToken(token string) {
	token = strings.TrimSpace(token)
	configuredToken.Store(&token)
}


func ResolveToken() string {
	if token := configuredToken.Load(); token != nil && *token != "" {
		return *token
	}
	for _, name := range []string{"GITHUB_TOKEN", "github_token"} {
		if token := strings.TrimSpace(os.Getenv(name)); token != "" {
			return token
		}
	}
	gitURL := strings.ToLower(strings.TrimSpace(os.Getenv("GITSTORE_GIT_URL")))
	if !strings.Contains(gitURL, "github.com") {
		return ""
	}
	return strings.TrimSpace(os.Getenv("GITSTORE_GIT_TOKEN"))
}


func TokenForURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Hostname(), "api.github.com") || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return ""
	}
	return ResolveToken()
}
