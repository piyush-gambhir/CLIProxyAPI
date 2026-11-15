package auth

type ApplyPatchSupport interface {
	SupportsApplyPatch() bool
}

func (m *Manager) SupportsApplyPatchForProviders(providers []string) bool {
	if m == nil || len(providers) == 0 {
		return false
	}
	for _, provider := range providers {
		executor, okExecutor := m.Executor(provider)
		if !okExecutor {
			return false
		}
		support, okSupport := executor.(ApplyPatchSupport)
		if !okSupport || !support.SupportsApplyPatch() {
			return false
		}
	}
	return true
}
