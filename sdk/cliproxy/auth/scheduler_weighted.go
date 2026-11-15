package auth

func (s *authScheduler) pickWeightedPoolLocked(route string, priority int, transportClass string, entries []*scheduledAuth, predicate func(*scheduledAuth) bool, pinnedAuthID string, nonWebsocketOnly bool) (*Auth, error) {
	if s.weightedSelector == nil {
		return nil, &Error{Code: "auth_unavailable", Message: "weighted selector is not configured"}
	}
	auths := make([]*Auth, 0, len(entries))
	ids := make([]string, 0, len(entries))
	weights := make([]int64, 0, len(entries))
	for _, entry := range entries {
		if entry == nil || entry.auth == nil || entry.meta == nil || entry.meta.weight <= 0 {
			continue
		}
		if predicate != nil && !predicate(entry) {
			continue
		}
		if nonWebsocketOnly && entry.meta.websocketEnabled {
			continue
		}
		auths = append(auths, entry.auth)
		ids = append(ids, entry.auth.ID)
		weights = append(weights, entry.meta.weight)
	}
	picked, reservation := s.weightedSelector.reservePoolScoped(route, priority, transportClass, auths, weights, ids, pinnedAuthID)
	if picked == nil {
		return nil, &Error{Code: "auth_unavailable", Message: "no auth available with positive weight"}
	}
	reservation.finish(true)
	return picked, nil
}
