package quota

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	WindowFiveHour = "five_hour"

	WindowSevenDay = "seven_day"

	WindowWeekly = "weekly"

	WindowFable = "fable"
)

const (
	fiveHourWindow = 5 * time.Hour
	sevenDayWindow = 7 * 24 * time.Hour
)

func defaultWindowLength(window string) time.Duration {
	switch window {
	case WindowFiveHour:
		return fiveHourWindow
	case WindowSevenDay, WindowWeekly, WindowFable:
		return sevenDayWindow
	default:
		return sevenDayWindow
	}
}

type Window struct {
	UsedPercent float64

	ResetAt time.Time

	WindowMinutes int

	LearnedAt time.Time

	Known bool

	HardLimited bool
}

func (w Window) length(window string) time.Duration {
	if w.WindowMinutes > 0 {
		return time.Duration(w.WindowMinutes) * time.Minute
	}
	return defaultWindowLength(window)
}

type Reading struct {
	AuthID string

	Provider string

	FileName string

	Plan string

	FetchedAt time.Time

	Windows map[string]Window
}

func (r Reading) clone() Reading {
	out := r
	if len(r.Windows) > 0 {
		out.Windows = make(map[string]Window, len(r.Windows))
		for key, window := range r.Windows {
			out.Windows[key] = window
		}
	}
	return out
}

func (r Reading) Window(id string) (Window, bool) {
	window, ok := r.Windows[id]
	return window, ok
}

type Store struct {
	mu      sync.RWMutex
	now     func() time.Time
	byAuth  map[string]*Reading
	ingests uint64
}

func NewStore(now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{now: now, byAuth: make(map[string]*Reading)}
}

var (
	defaultStoreOnce sync.Once
	defaultStore     *Store
)

func DefaultStore() *Store {
	defaultStoreOnce.Do(func() { defaultStore = NewStore(time.Now) })
	return defaultStore
}

type Identity struct {
	AuthID   string
	Provider string
	FileName string
	Plan     string
}

func (s *Store) Observe(id Identity, windows map[string]Window, learnedAt time.Time) bool {
	authID := strings.TrimSpace(id.AuthID)
	if s == nil || authID == "" {
		return false
	}
	if learnedAt.IsZero() {
		learnedAt = s.clock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.byAuth[authID]
	if entry == nil {
		entry = &Reading{AuthID: authID, Windows: make(map[string]Window)}
		s.byAuth[authID] = entry
	}
	changed := false
	if provider := strings.TrimSpace(id.Provider); provider != "" && entry.Provider != provider {
		entry.Provider = provider
		changed = true
	}
	if fileName := strings.TrimSpace(id.FileName); fileName != "" && entry.FileName != fileName {
		entry.FileName = fileName
		changed = true
	}
	if plan := strings.TrimSpace(id.Plan); plan != "" && entry.Plan != plan {
		entry.Plan = plan
		changed = true
	}
	for key, window := range windows {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if window.LearnedAt.IsZero() {
			window.LearnedAt = learnedAt
		}
		previous, exists := entry.Windows[key]
		if exists && previous.LearnedAt.After(window.LearnedAt) {
			continue
		}

		if exists && previous.HardLimited && !window.HardLimited {
			if previous.ResetAt.IsZero() || !window.LearnedAt.Before(previous.ResetAt) {
				previous.HardLimited = false
			}
			if previous.HardLimited {
				window.HardLimited = true
			}
		}
		if exists && window.WindowMinutes == 0 {
			window.WindowMinutes = previous.WindowMinutes
		}
		entry.Windows[key] = window
		if entry.FetchedAt.Before(window.LearnedAt) {
			entry.FetchedAt = window.LearnedAt
		}
		changed = true
	}
	if changed {
		s.ingests++
	}
	return changed
}

func (s *Store) MarkHardLimited(id Identity, exceeded bool, recoverAt time.Time) bool {
	authID := strings.TrimSpace(id.AuthID)
	if s == nil || authID == "" {
		return false
	}
	now := s.clock()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.byAuth[authID]
	if entry == nil {
		if !exceeded {
			return false
		}
		entry = &Reading{AuthID: authID, Windows: make(map[string]Window)}
		s.byAuth[authID] = entry
	}
	if provider := strings.TrimSpace(id.Provider); provider != "" {
		entry.Provider = provider
	}
	if fileName := strings.TrimSpace(id.FileName); fileName != "" {
		entry.FileName = fileName
	}
	if !exceeded {
		changed := false
		for key, window := range entry.Windows {
			if window.HardLimited {
				window.HardLimited = false
				entry.Windows[key] = window
				changed = true
			}
		}
		return changed
	}
	target := ""
	if !recoverAt.IsZero() {
		best := time.Duration(0)
		for key, window := range entry.Windows {
			if window.ResetAt.IsZero() {
				continue
			}
			delta := window.ResetAt.Sub(recoverAt)
			if delta < 0 {
				delta = -delta
			}
			if target == "" || delta < best {
				target, best = key, delta
			}
		}
	}
	changed := false
	if target != "" {
		window := entry.Windows[target]
		if !window.HardLimited {
			window.HardLimited = true
			entry.Windows[target] = window
			changed = true
		}
		return changed
	}
	if len(entry.Windows) == 0 {

		entry.Windows[WindowFiveHour] = Window{HardLimited: true, LearnedAt: now, ResetAt: recoverAt}
		if entry.FetchedAt.Before(now) {
			entry.FetchedAt = now
		}
		return true
	}
	for key, window := range entry.Windows {
		if !window.HardLimited {
			window.HardLimited = true
			entry.Windows[key] = window
			changed = true
		}
	}
	return changed
}

func (s *Store) Lookup(authID string) (Reading, bool) {
	authID = strings.TrimSpace(authID)
	if s == nil || authID == "" {
		return Reading{}, false
	}
	now := s.clock()
	s.mu.RLock()
	entry := s.byAuth[authID]
	s.mu.RUnlock()
	if entry == nil {
		return Reading{}, false
	}
	return normalizeReading(entry.clone(), now), true
}

func (s *Store) Snapshot() map[string]Reading {
	if s == nil {
		return map[string]Reading{}
	}
	now := s.clock()
	s.mu.RLock()
	out := make(map[string]Reading, len(s.byAuth))
	for _, entry := range s.byAuth {
		key := strings.TrimSpace(entry.FileName)
		if key == "" {
			key = entry.AuthID
		}
		out[key] = normalizeReading(entry.clone(), now)
	}
	s.mu.RUnlock()
	return out
}

func (s *Store) FetchedAt(authID string) time.Time {
	authID = strings.TrimSpace(authID)
	if s == nil || authID == "" {
		return time.Time{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if entry := s.byAuth[authID]; entry != nil {
		return entry.FetchedAt
	}
	return time.Time{}
}

func (s *Store) Forget(authID string) {
	authID = strings.TrimSpace(authID)
	if s == nil || authID == "" {
		return
	}
	s.mu.Lock()
	delete(s.byAuth, authID)
	s.mu.Unlock()
}

func (s *Store) Retain(authIDs []string) {
	if s == nil {
		return
	}
	keep := make(map[string]struct{}, len(authIDs))
	for _, id := range authIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			keep[trimmed] = struct{}{}
		}
	}
	s.mu.Lock()
	for id := range s.byAuth {
		if _, ok := keep[id]; !ok {
			delete(s.byAuth, id)
		}
	}
	s.mu.Unlock()
}

func (s *Store) Ingests() uint64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ingests
}

func (s *Store) AuthIDs() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	ids := make([]string, 0, len(s.byAuth))
	for id := range s.byAuth {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	sort.Strings(ids)
	return ids
}

func (s *Store) clock() time.Time {
	if s == nil || s.now == nil {
		return time.Now()
	}
	return s.now()
}

func normalizeReading(reading Reading, now time.Time) Reading {
	for key, window := range reading.Windows {
		reading.Windows[key] = normalizeWindow(key, window, now)
	}
	return reading
}

func normalizeWindow(id string, window Window, now time.Time) Window {
	if window.ResetAt.IsZero() || window.ResetAt.After(now) {
		return window
	}
	length := window.length(id)
	window.UsedPercent = 0
	window.HardLimited = false
	window.ResetAt = now.Add(length)
	return window
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
