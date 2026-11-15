package cmd

import (
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
)







func newAuthManager() *sdkAuth.Manager {
	store := sdkAuth.GetTokenStore()
	manager := sdkAuth.NewManager(store,
		sdkAuth.NewCodexAuthenticator(),
		sdkAuth.NewClaudeAuthenticator(),
		sdkAuth.NewAntigravityAuthenticator(),
		sdkAuth.NewKimiAuthenticator(),
		sdkAuth.NewKimiAIAuthenticator(),
		sdkAuth.NewKimiAIDotAuthenticator(),
		sdkAuth.NewXAIAuthenticator(),
		sdkAuth.NewDevinAuthenticator(),
		sdkAuth.NewMetaAuthenticator(),
	)
	return manager
}
