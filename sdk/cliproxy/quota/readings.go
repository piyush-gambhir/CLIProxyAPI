// Package quota holds per-credential quota readings and turns them into a
// routing order for the reset-soonest selection strategy.
//
// The package is a leaf: it never imports the auth package, so the selector,
// the usage poller and the management status handler can all depend on it
// without an import cycle. Credentials are identified by their auth ID, and a
// reading carries the auth file name so the management surface can key
// accounts the way the control panel expects.
//
// Readings are in memory only. A restart loses them and the poller's startup
// pass refills them.
package quota

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// Canonical quota window identifiers. These are the ids the management status
// contract uses, so they must not be renamed.
const (
	// WindowFiveHour is the short rolling session window shared by Claude and Codex.
	WindowFiveHour = "five_hour"
	// WindowSevenDay is Claude's long window.
	WindowSevenDay = "seven_day"
	// WindowWeekly is Codex's long window.
	WindowWeekly = "weekly"
	// WindowFable is Claude's per-model 7-day window for Fable models.
	WindowFable = "fable"
)

const (
	fiveHourWindow = 5 * time.Hour
	sevenDayWindow = 7 * 24 * time.Hour
)

// defaultWindowLength returns the nominal length of a canonical window. It is
// used to project the next reset once a known reset time has passed.
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

// Window is one quota window reading for one credential.
type Window struct {
	// UsedPercent is how much of the window has been consumed, 0 to 100.
	UsedPercent float64
	// ResetAt is when the window refills. Zero when the source did not say.
	ResetAt time.Time
	// WindowMinutes is the window length reported by the source, 0 when absent.
	WindowMinutes int
	// LearnedAt is when this reading was observed, from a header or a poll.
	LearnedAt time.Time
	// Known is false for a placeholder created only to carry a hard limit.
	Known bool
	// HardLimited records that the credential was refused on this window.
	HardLimited bool
}

// length returns the window length to use when projecting a passed reset.
func (w Window) length(window string) time.Duration {
	if w.WindowMinutes > 0 {
		return time.Duration(w.WindowMinutes) * time.Minute
	}
	return defaultWindowLength(window)
}

// Reading is everything known about one credential's quota.
type Reading struct {
	// AuthID is the credential's auth manager ID.
	AuthID string
	// Provider is the upstream provider key, for example "claude" or "codex".
	Provider string
	// FileName is the credential's auth file base name, used as the status key.
	FileName string
	// Plan is the provider's plan slug when it reports one.
	Plan string
	// FetchedAt is the newest LearnedAt across the windows.
	FetchedAt time.Time
	// Windows holds the readings keyed by canonical window id.
	Windows map[string]Window
}

// clone returns an independent copy so callers cannot mutate stored state.
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

// Window returns one window reading.
func (r Reading) Window(id string) (Window, bool) {
	window, ok := r.Windows[id]
	return window, ok
}

// Store keeps the newest reading per credential and window.
//
// Every writer goes through Observe (or a helper that calls it), so a polled
// reading and a header reading are indistinguishable to readers. A newer
// reading of a window replaces an older one; an older one is dropped.
type Store struct {
	mu      sync.RWMutex
	now     func() time.Time
	byAuth  map[string]*Reading
	ingests uint64
}

// NewStore builds an empty store. A nil clock falls back to time.Now.
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

// DefaultStore returns the process-wide store shared by the selector, the
// usage poller and the management status endpoint.
func DefaultStore() *Store {
	defaultStoreOnce.Do(func() { defaultStore = NewStore(time.Now) })
	return defaultStore
}

// Identity carries the non-quota fields a writer knows about a credential.
type Identity struct {
	AuthID   string
	Provider string
	FileName string
	Plan     string
}

// Observe records windows for a credential. Only windows whose LearnedAt is
// newer than the stored reading are written, so an out-of-order writer cannot
// resurrect a stale watermark. Observe returns true when anything changed.
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
		// A reading that arrives after the previous window's reset clears a hard
		// limit: the refusal belonged to a window that has since refilled.
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

// MarkHardLimited records a provider refusal. The window whose reset is closest
// to recoverAt takes the flag; with no usable hint every known window does, per
// the design's "or all windows when unknown".
//
// exceeded false clears the flags, which is how a credential leaving cooldown
// becomes rankable again before its next reading lands.
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
		// Nothing is known about this credential yet. Record the refusal on the
		// short window so availability filtering and the status surface agree.
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

// Lookup returns the normalized reading for a credential. A window whose reset
// has passed is reported as full again, with the next reset projected from the
// window length, per the design's "a passed reset means the window is full".
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

// Snapshot returns every reading keyed by auth file name, falling back to the
// auth ID when the file name is unknown. Readings are normalized as in Lookup.
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

// FetchedAt reports when the newest window reading for a credential landed.
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

// Forget drops a credential's readings, for example after it is removed.
func (s *Store) Forget(authID string) {
	authID = strings.TrimSpace(authID)
	if s == nil || authID == "" {
		return
	}
	s.mu.Lock()
	delete(s.byAuth, authID)
	s.mu.Unlock()
}

// Retain drops every credential outside the supplied set.
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

// Ingests counts accepted writes. It backs the cheap per-pick debug counter.
func (s *Store) Ingests() uint64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ingests
}

// AuthIDs returns the stored credential IDs in sorted order.
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

// normalizeReading applies the passed-reset rule to every window.
func normalizeReading(reading Reading, now time.Time) Reading {
	for key, window := range reading.Windows {
		reading.Windows[key] = normalizeWindow(key, window, now)
	}
	return reading
}

// normalizeWindow treats a window whose reset has passed as full again. The
// hard limit is dropped with it: the refusal belonged to the spent window.
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

// clampPercent keeps a reported percentage inside 0 to 100.
func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
