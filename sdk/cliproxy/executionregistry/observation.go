package executionregistry

import "time"

type Observation struct {
	RequestID    string
	CredentialID string
	Model        string
	RequestKind  string
	StartedAt    time.Time
	Accounted    bool
}

type Freeze struct {
	Revision        int64
	BarrierRevision int64
	Executions      []Observation
}

func (r *Registry) ObserveBarrier(revision int64) {
	if r == nil || revision <= 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if revision > r.observedBarrier {
		r.observedBarrier = revision
		r.pendingBarrierSequence = r.next
	}
}

func (r *Registry) FreezeInFlight(_ time.Time) Freeze {
	if r == nil {
		return Freeze{}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.observedBarrier > r.publishedBarrier {
		blocked := false
		for sequence := range r.pending {
			if sequence <= r.pendingBarrierSequence {
				blocked = true
				break
			}
		}
		if !blocked {
			r.publishedBarrier = r.observedBarrier
		}
	}

	r.snapshotRevision++
	freeze := Freeze{
		Revision:        r.snapshotRevision,
		BarrierRevision: r.publishedBarrier,
		Executions:      make([]Observation, 0, len(r.scopes)),
	}
	for _, scope := range r.scopes {
		freeze.Executions = append(freeze.Executions, Observation{
			RequestID:    scope.spec.RequestID,
			CredentialID: scope.spec.CredentialID,
			Model:        scope.spec.Model,
			RequestKind:  scope.spec.Kind,
			StartedAt:    scope.spec.StartedAt,
			Accounted:    scope.spec.Accounted,
		})
	}
	return freeze
}
