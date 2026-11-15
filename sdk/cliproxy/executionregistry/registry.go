
package executionregistry

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

var (
	ErrRegistryNotAccepting          = errors.New("execution registry is not accepting dispatches")
	ErrRegistryClosed                = errors.New("execution registry is closed")
	ErrInvalidPendingDispatch        = errors.New("invalid pending dispatch")
	ErrInvalidExecutionResource      = errors.New("invalid execution resource")
	ErrExecutionResourceAlreadyBound = errors.New("execution resource is already bound")
)


type State uint32

const (
	StateAccepting State = iota
	StateDraining
	StateClosed
)


type Registry struct {
	state atomic.Uint32

	mu                     sync.Mutex
	next                   uint64
	snapshotRevision       int64
	observedBarrier        int64
	pendingBarrierSequence uint64
	publishedBarrier       int64
	pending                map[uint64]*PendingDispatch
	scopes                 map[uint64]*Scope
	releaseSequences       map[ReleaseGroup]int64
	releaseSink            ReleaseSink
	changed                chan struct{}

	closeMu      sync.Mutex
	closeStarted bool
	closeDone    chan struct{}
	closeErr     error
}


type PendingDispatch struct {
	id       uint64
	registry *Registry
	mu       sync.Mutex
	once     sync.Once
}


type ScopeSpec struct {
	RequestID    string
	CredentialID string
	Model        string
	Kind         string
	StartedAt    time.Time
	Accounted    bool
}


type ReleaseGroup struct {
	CredentialID string
	Model        string
}


type ReleaseTicket struct {
	Group    ReleaseGroup
	Sequence int64
	done     <-chan struct{}
}



func NewReleaseTicket(group ReleaseGroup, sequence int64, done <-chan struct{}) *ReleaseTicket {
	if sequence <= 0 || done == nil {
		return nil
	}
	return &ReleaseTicket{Group: group, Sequence: sequence, done: done}
}


func (t *ReleaseTicket) Wait(ctx context.Context) error {
	if t == nil || t.done == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-t.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}



type ReleaseSink func(ReleaseGroup, int64) *ReleaseTicket


type Scope struct {
	id       uint64
	registry *Registry
	spec     ScopeSpec

	mu            sync.Mutex
	closeFn       func() error
	closeDone     chan struct{}
	releaseTicket *ReleaseTicket
	active        bool
	ended         sync.Once
}


func New() *Registry {
	registry := &Registry{
		pending:          make(map[uint64]*PendingDispatch),
		scopes:           make(map[uint64]*Scope),
		releaseSequences: make(map[ReleaseGroup]int64),
		changed:          make(chan struct{}),
	}
	registry.state.Store(uint32(StateAccepting))
	return registry
}


func (r *Registry) BeginDispatch() (*PendingDispatch, error) {
	if r == nil || State(r.state.Load()) != StateAccepting {
		return nil, ErrRegistryNotAccepting
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if State(r.state.Load()) != StateAccepting {
		return nil, ErrRegistryNotAccepting
	}

	r.next++
	pending := &PendingDispatch{id: r.next, registry: r}
	r.pending[pending.id] = pending
	return pending, nil
}


func (r *Registry) WaitPending(ctx context.Context) error {
	if r == nil {
		return ErrRegistryClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r.mu.Lock()
	for len(r.pending) != 0 {
		changed := r.changed
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
		r.mu.Lock()
	}
	r.mu.Unlock()
	return nil
}


func (p *PendingDispatch) End() {
	if p == nil || p.registry == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.once.Do(func() {
		p.registry.mu.Lock()
		delete(p.registry.pending, p.id)
		p.registry.signalLocked()
		p.registry.mu.Unlock()
	})
}


func (r *Registry) Install(pending *PendingDispatch, spec ScopeSpec) (*Scope, error) {
	if r == nil || pending == nil || pending.registry != r {
		return nil, ErrInvalidPendingDispatch
	}

	pending.mu.Lock()
	defer pending.mu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()

	if State(r.state.Load()) != StateAccepting {
		pending.once.Do(func() {})
		delete(r.pending, pending.id)
		r.signalLocked()
		return nil, ErrRegistryNotAccepting
	}
	if _, exists := r.pending[pending.id]; !exists {
		return nil, ErrInvalidPendingDispatch
	}

	pending.once.Do(func() {})
	delete(r.pending, pending.id)
	scope := &Scope{id: pending.id, registry: r, spec: spec, active: true}
	r.scopes[scope.id] = scope
	r.signalLocked()
	return scope, nil
}



func (r *Registry) SetReleaseSink(rawSink any) {
	if r == nil {
		return
	}

	var sink ReleaseSink
	switch typed := rawSink.(type) {
	case nil:
	case ReleaseSink:
		sink = typed
	case func(ReleaseGroup, int64) *ReleaseTicket:
		sink = ReleaseSink(typed)
	case func(ReleaseGroup, int64):
		sink = func(group ReleaseGroup, sequence int64) *ReleaseTicket {
			typed(group, sequence)
			return nil
		}
	default:
		return
	}

	r.mu.Lock()
	r.releaseSink = sink
	sequences := make(map[ReleaseGroup]int64, len(r.releaseSequences))
	for group, sequence := range r.releaseSequences {
		sequences[group] = sequence
	}
	r.mu.Unlock()

	if sink == nil {
		return
	}
	for group, sequence := range sequences {
		if sequence > 0 {
			sink(group, sequence)
		}
	}
}


func (s *Scope) Bind(closeFn func() error) error {
	if s == nil || s.registry == nil || closeFn == nil {
		return ErrInvalidExecutionResource
	}

	s.registry.mu.Lock()
	defer s.registry.mu.Unlock()
	if State(s.registry.state.Load()) != StateAccepting || !s.active {
		return ErrRegistryNotAccepting
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closeFn != nil || s.closeDone != nil {
		return ErrExecutionResourceAlreadyBound
	}
	s.closeFn = closeFn
	return nil
}


func (s *Scope) End(reason string) {
	_ = s.EndWithRelease(reason)
}



func (s *Scope) EndWithRelease(_ string) *ReleaseTicket {
	if s == nil || s.registry == nil {
		return nil
	}

	var ticket *ReleaseTicket
	s.ended.Do(func() {
		s.registry.mu.Lock()
		s.mu.Lock()
		s.active = false
		s.mu.Unlock()
		s.registry.mu.Unlock()

		s.waitForBoundResourceClose()

		s.registry.mu.Lock()
		releaseSink, releaseGroup, releaseSequence := s.registry.markReleasedLocked(s)
		s.registry.mu.Unlock()

		if releaseSink != nil && releaseSequence > 0 {
			ticket = releaseSink(releaseGroup, releaseSequence)
		}

		s.mu.Lock()
		s.releaseTicket = ticket
		s.mu.Unlock()

		s.registry.mu.Lock()
		delete(s.registry.scopes, s.id)
		s.registry.signalLocked()
		s.registry.mu.Unlock()
	})

	s.mu.Lock()
	ticket = s.releaseTicket
	s.mu.Unlock()
	return ticket
}

func (r *Registry) markReleasedLocked(scope *Scope) (ReleaseSink, ReleaseGroup, int64) {
	if scope == nil || !scope.spec.Accounted {
		return nil, ReleaseGroup{}, 0
	}
	group := ReleaseGroup{CredentialID: scope.spec.CredentialID, Model: scope.spec.Model}
	r.releaseSequences[group]++
	return r.releaseSink, group, r.releaseSequences[group]
}

func (s *Scope) startBoundResourceClose() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closeDone != nil {
		return s.closeDone
	}
	closeFn := s.closeFn
	if closeFn == nil {
		return nil
	}
	closeDone := make(chan struct{})
	s.closeFn = nil
	s.closeDone = closeDone
	go func() {
		s.closeResource(closeFn)
		close(closeDone)
	}()
	return closeDone
}

func (s *Scope) waitForBoundResourceClose() {
	if closeDone := s.startBoundResourceClose(); closeDone != nil {
		<-closeDone
	}
}

func (s *Scope) closeResource(closeFn func() error) {
	if closeFn == nil {
		return
	}
	if errClose := closeFn(); errClose != nil {
		log.WithError(errClose).Warn("Home execution resource close failed")
	}
}


func (r *Registry) Drain(ctx context.Context) error {
	if r == nil {
		return ErrRegistryClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if !r.state.CompareAndSwap(uint32(StateAccepting), uint32(StateDraining)) && State(r.state.Load()) != StateDraining {
		return ErrRegistryClosed
	}

	r.mu.Lock()
	scopes := make([]*Scope, 0, len(r.scopes))
	for _, scope := range r.scopes {
		scopes = append(scopes, scope)
	}
	r.mu.Unlock()

	for _, scope := range scopes {
		scope.startBoundResourceClose()
	}

	r.mu.Lock()
	for len(r.pending) != 0 || len(r.scopes) != 0 {
		changed := r.changed
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
		r.mu.Lock()
	}
	r.state.Store(uint32(StateClosed))
	r.mu.Unlock()
	return nil
}


func (r *Registry) Close() error {
	if r == nil {
		return ErrRegistryClosed
	}

	r.closeMu.Lock()
	if r.closeStarted {
		closeDone := r.closeDone
		r.closeMu.Unlock()
		<-closeDone
		r.closeMu.Lock()
		errClose := r.closeErr
		r.closeMu.Unlock()
		return errClose
	}
	if State(r.state.Load()) == StateClosed {
		r.closeMu.Unlock()
		return nil
	}
	r.closeStarted = true
	r.closeDone = make(chan struct{})
	closeDone := r.closeDone
	r.closeMu.Unlock()

	for {
		state := State(r.state.Load())
		if state == StateClosed || r.state.CompareAndSwap(uint32(state), uint32(StateClosed)) {
			break
		}
	}

	r.mu.Lock()
	scopes := make([]*Scope, 0, len(r.scopes))
	for _, scope := range r.scopes {
		scopes = append(scopes, scope)
	}
	r.mu.Unlock()
	for _, scope := range scopes {
		scope.waitForBoundResourceClose()
	}

	r.closeMu.Lock()
	errClose := r.closeErr
	close(closeDone)
	r.closeMu.Unlock()
	return errClose
}

func (r *Registry) signalLocked() {
	close(r.changed)
	r.changed = make(chan struct{})
}
