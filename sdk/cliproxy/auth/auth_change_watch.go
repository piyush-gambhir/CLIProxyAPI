package auth






func (m *Manager) WatchAuthChanges(authID string) (<-chan struct{}, func()) {
	changes := make(chan struct{}, 1)
	if m == nil || authID == "" {
		close(changes)
		return changes, func() {}
	}
	m.mu.Lock()
	if m.authChangeWatchers == nil {
		m.authChangeWatchers = make(map[string]map[chan struct{}]struct{})
	}
	if m.authChangeWatchers[authID] == nil {
		m.authChangeWatchers[authID] = make(map[chan struct{}]struct{})
	}
	m.authChangeWatchers[authID][changes] = struct{}{}
	m.mu.Unlock()
	return changes, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		watchers := m.authChangeWatchers[authID]
		if _, exists := watchers[changes]; !exists {
			return
		}
		delete(watchers, changes)
		if len(watchers) == 0 {
			delete(m.authChangeWatchers, authID)
		}
		close(changes)
	}
}


func (m *Manager) notifyAuthChangeLocked(authID string) {
	for changes := range m.authChangeWatchers[authID] {
		select {
		case changes <- struct{}{}:
		default:
		}
	}
}
