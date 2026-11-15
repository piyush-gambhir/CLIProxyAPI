package util

import "github.com/router-for-me/CLIProxyAPI/v8/internal/githubauth"






func ResolveGitHubToken() string {
	return githubauth.ResolveToken()
}
