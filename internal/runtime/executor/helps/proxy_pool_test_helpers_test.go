package helps

import (
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func poolEnabled() *cliproxyauth.ProxyConnectionPoolOverride {
	enabled := true
	return &cliproxyauth.ProxyConnectionPoolOverride{Enabled: &enabled}
}

func poolOptInAuth(id string) *cliproxyauth.Auth {
	return &cliproxyauth.Auth{ID: id, ProxyConnectionPool: poolEnabled()}
}

func poolAuth(id string, override *cliproxyauth.ProxyConnectionPoolOverride) *cliproxyauth.Auth {
	return &cliproxyauth.Auth{ID: id, ProxyConnectionPool: override}
}

func poolBlock() *cliproxyauth.ProxyConnectionPoolOverride {
	return &cliproxyauth.ProxyConnectionPoolOverride{}
}

func testConfig() *config.Config { return &config.Config{} }
