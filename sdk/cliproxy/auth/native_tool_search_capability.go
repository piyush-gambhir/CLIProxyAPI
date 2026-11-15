package auth

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)




type NativeToolSearchSupport interface {
	SupportsNativeToolSearch(auth *Auth, upstreamModel string) bool
}





func (m *Manager) SupportsNativeToolSearchForModel(model string) bool {
	model = strings.TrimSpace(model)
	if m == nil || model == "" {
		return false
	}
	auths := m.List()
	serving := nativeToolSearchServingAuths(auths, model)
	if len(serving) == 0 {
		if slash := strings.Index(model, "/"); slash >= 0 {
			model = strings.TrimSpace(model[slash+1:])
			serving = nativeToolSearchServingAuths(auths, model)
		}
	}
	if len(serving) == 0 {
		return false
	}
	for _, auth := range serving {
		executor, okExecutor := m.Executor(auth.Provider)
		if !okExecutor {
			return false
		}
		support, okSupport := executor.(NativeToolSearchSupport)
		if !okSupport || !support.SupportsNativeToolSearch(auth, m.ResolveExecutionModel(auth, model)) {
			return false
		}
	}
	return true
}

func nativeToolSearchServingAuths(auths []*Auth, model string) []*Auth {
	if model == "" {
		return nil
	}
	modelRegistry := registry.GetGlobalRegistry()
	var serving []*Auth
	for _, auth := range auths {
		if auth == nil || auth.Disabled || auth.Status == StatusDisabled {
			continue
		}
		if modelRegistry.ClientSupportsModel(auth.ID, model) {
			serving = append(serving, auth)
		}
	}
	return serving
}
