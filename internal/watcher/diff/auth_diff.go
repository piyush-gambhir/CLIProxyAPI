package diff

import (
	"fmt"
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func BuildAuthChangeDetails(oldAuth, newAuth *coreauth.Auth) []string {
	changes := make([]string, 0, 3)

	if oldAuth == nil {
		oldAuth = &coreauth.Auth{}
	}
	if newAuth == nil {
		return changes
	}

	oldPrefix := strings.TrimSpace(oldAuth.Prefix)
	newPrefix := strings.TrimSpace(newAuth.Prefix)
	if oldPrefix != newPrefix {
		changes = append(changes, fmt.Sprintf("prefix: %s -> %s", oldPrefix, newPrefix))
	}

	oldProxy := strings.TrimSpace(oldAuth.ProxyURL)
	newProxy := strings.TrimSpace(newAuth.ProxyURL)
	if oldProxy != newProxy {
		changes = append(changes, fmt.Sprintf("proxy_url: %s -> %s", formatProxyURL(oldProxy), formatProxyURL(newProxy)))
	}

	if oldAuth.Disabled != newAuth.Disabled {
		changes = append(changes, fmt.Sprintf("disabled: %t -> %t", oldAuth.Disabled, newAuth.Disabled))
	}

	return changes
}
