package quota

import (
	"testing"
	"time"
)

// fakeClock is a controllable clock. AGENTS.md forbids wall-clock sleeps in
// ordering and expiry tests, so every time-dependent test drives this instead.
type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

var storeBase = time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

func newTestStore() (*Store, *fakeClock) {
	clock := &fakeClock{now: storeBase}
	return NewStore(clock.Now), clock
}

func TestStoreNewestReadingPerWindowWins(t *testing.T) {
	t.Parallel()

	store, clock := newTestStore()
	identity := Identity{AuthID: "a", Provider: "claude", FileName: "claude-a.json"}

	store.Observe(identity, map[string]Window{
		WindowSevenDay: {UsedPercent: 20, ResetAt: storeBase.Add(72 * time.Hour), Known: true},
	}, clock.Now())

	clock.Advance(time.Minute)
	store.Observe(identity, map[string]Window{
		WindowSevenDay: {UsedPercent: 35, ResetAt: storeBase.Add(72 * time.Hour), Known: true},
	}, clock.Now())

	reading, ok := store.Lookup("a")
	if !ok {
		t.Fatal("Lookup() missing")
	}
	if window, _ := reading.Window(WindowSevenDay); window.UsedPercent != 35 {
		t.Fatalf("used percent = %.1f, want 35", window.UsedPercent)
	}

	// An out-of-order writer must not resurrect the older watermark.
	store.Observe(identity, map[string]Window{
		WindowSevenDay: {UsedPercent: 20, ResetAt: storeBase.Add(72 * time.Hour), Known: true, LearnedAt: storeBase},
	}, storeBase)
	reading, _ = store.Lookup("a")
	if window, _ := reading.Window(WindowSevenDay); window.UsedPercent != 35 {
		t.Fatalf("used percent after stale write = %.1f, want 35", window.UsedPercent)
	}
}

func TestStorePassedResetMeansTheWindowIsFull(t *testing.T) {
	t.Parallel()

	store, clock := newTestStore()
	identity := Identity{AuthID: "a", Provider: "claude", FileName: "claude-a.json"}
	store.Observe(identity, map[string]Window{
		WindowFiveHour: {UsedPercent: 100, HardLimited: true, ResetAt: storeBase.Add(time.Hour), Known: true},
		WindowSevenDay: {UsedPercent: 90, ResetAt: storeBase.Add(48 * time.Hour), Known: true},
	}, clock.Now())

	// Before the reset the reading stands as observed.
	reading, _ := store.Lookup("a")
	if window, _ := reading.Window(WindowFiveHour); window.UsedPercent != 100 || !window.HardLimited {
		t.Fatalf("five hour before reset = %+v, want spent and hard limited", window)
	}

	clock.Advance(90 * time.Minute)
	reading, _ = store.Lookup("a")
	short, _ := reading.Window(WindowFiveHour)
	if short.UsedPercent != 0 {
		t.Fatalf("used percent after reset = %.1f, want 0", short.UsedPercent)
	}
	if short.HardLimited {
		t.Fatal("a hard limit must not outlive the window it was observed on")
	}
	wantReset := clock.Now().Add(fiveHourWindow)
	if !short.ResetAt.Equal(wantReset) {
		t.Fatalf("projected reset = %s, want %s", short.ResetAt, wantReset)
	}
	// The long window has not reset, so it is untouched.
	if long, _ := reading.Window(WindowSevenDay); long.UsedPercent != 90 {
		t.Fatalf("seven day used percent = %.1f, want 90", long.UsedPercent)
	}
}

func TestStoreMarkHardLimitedTargetsTheWindowNearestTheRecovery(t *testing.T) {
	t.Parallel()

	store, clock := newTestStore()
	identity := Identity{AuthID: "a", Provider: "claude", FileName: "claude-a.json"}
	store.Observe(identity, map[string]Window{
		WindowFiveHour: {UsedPercent: 70, ResetAt: storeBase.Add(2 * time.Hour), Known: true},
		WindowSevenDay: {UsedPercent: 70, ResetAt: storeBase.Add(96 * time.Hour), Known: true},
	}, clock.Now())

	store.MarkHardLimited(identity, true, storeBase.Add(2*time.Hour+time.Minute))
	reading, _ := store.Lookup("a")
	short, _ := reading.Window(WindowFiveHour)
	long, _ := reading.Window(WindowSevenDay)
	if !short.HardLimited {
		t.Fatal("the five hour window should carry the hard limit")
	}
	if long.HardLimited {
		t.Fatal("the seven day window should be untouched")
	}

	// Leaving cooldown clears the flag even before a new reading lands.
	store.MarkHardLimited(identity, false, time.Time{})
	reading, _ = store.Lookup("a")
	if short, _ = reading.Window(WindowFiveHour); short.HardLimited {
		t.Fatal("clearing the cooldown should clear the hard limit")
	}
}

func TestStoreMarkHardLimitedWithNoHintFlagsEveryWindow(t *testing.T) {
	t.Parallel()

	store, clock := newTestStore()
	identity := Identity{AuthID: "a", Provider: "codex", FileName: "codex-a.json"}
	store.Observe(identity, map[string]Window{
		WindowFiveHour: {UsedPercent: 10, Known: true},
		WindowWeekly:   {UsedPercent: 10, Known: true},
	}, clock.Now())

	store.MarkHardLimited(identity, true, time.Time{})
	reading, _ := store.Lookup("a")
	for _, id := range []string{WindowFiveHour, WindowWeekly} {
		if window, _ := reading.Window(id); !window.HardLimited {
			t.Fatalf("window %q should be hard limited", id)
		}
	}
}

func TestStoreMarkHardLimitedOnAnUnknownCredential(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore()
	identity := Identity{AuthID: "fresh", Provider: "claude", FileName: "claude-fresh.json"}
	store.MarkHardLimited(identity, true, storeBase.Add(time.Hour))

	reading, ok := store.Lookup("fresh")
	if !ok {
		t.Fatal("a refusal on an unseen credential should still register it")
	}
	if window, _ := reading.Window(WindowFiveHour); !window.HardLimited {
		t.Fatalf("five hour = %+v, want hard limited", window)
	}
	// Nothing to clear when the credential was never refused.
	store.Forget("fresh")
	if store.MarkHardLimited(Identity{AuthID: "fresh"}, false, time.Time{}) {
		t.Fatal("clearing a hard limit on an unknown credential should be a no-op")
	}
}

func TestStoreAReadingAfterTheResetClearsAHardLimit(t *testing.T) {
	t.Parallel()

	store, clock := newTestStore()
	identity := Identity{AuthID: "a", Provider: "claude", FileName: "claude-a.json"}
	store.Observe(identity, map[string]Window{
		WindowSevenDay: {UsedPercent: 100, HardLimited: true, ResetAt: storeBase.Add(time.Hour), Known: true},
	}, clock.Now())

	// A fresh reading learned after the old reset drops the flag.
	clock.Advance(2 * time.Hour)
	store.Observe(identity, map[string]Window{
		WindowSevenDay: {UsedPercent: 5, ResetAt: clock.Now().Add(sevenDayWindow), Known: true},
	}, clock.Now())
	reading, _ := store.Lookup("a")
	window, _ := reading.Window(WindowSevenDay)
	if window.HardLimited {
		t.Fatal("a reading after the reset should clear the hard limit")
	}
	if window.UsedPercent != 5 {
		t.Fatalf("used percent = %.1f, want 5", window.UsedPercent)
	}
}

func TestStoreAReadingBeforeTheResetKeepsAHardLimit(t *testing.T) {
	t.Parallel()

	store, clock := newTestStore()
	identity := Identity{AuthID: "a", Provider: "claude", FileName: "claude-a.json"}
	store.Observe(identity, map[string]Window{
		WindowSevenDay: {UsedPercent: 100, HardLimited: true, ResetAt: storeBase.Add(4 * time.Hour), Known: true},
	}, clock.Now())

	clock.Advance(time.Hour)
	store.Observe(identity, map[string]Window{
		WindowSevenDay: {UsedPercent: 98, ResetAt: storeBase.Add(4 * time.Hour), Known: true},
	}, clock.Now())
	reading, _ := store.Lookup("a")
	if window, _ := reading.Window(WindowSevenDay); !window.HardLimited {
		t.Fatal("a reading inside the refused window should keep the hard limit")
	}
}

func TestStoreSnapshotIsKeyedByAuthFileName(t *testing.T) {
	t.Parallel()

	store, clock := newTestStore()
	store.Observe(Identity{AuthID: "id-1", Provider: "claude", FileName: "claude-a@example.com.json", Plan: "max"},
		map[string]Window{WindowSevenDay: {UsedPercent: 1, Known: true}}, clock.Now())
	store.Observe(Identity{AuthID: "id-2", Provider: "codex"},
		map[string]Window{WindowWeekly: {UsedPercent: 2, Known: true}}, clock.Now())

	snapshot := store.Snapshot()
	if _, ok := snapshot["claude-a@example.com.json"]; !ok {
		t.Fatalf("snapshot keys = %v, want the auth file name", keysOf(snapshot))
	}
	// With no file name the auth ID is the key, so nothing is silently dropped.
	if _, ok := snapshot["id-2"]; !ok {
		t.Fatalf("snapshot keys = %v, want the auth ID fallback", keysOf(snapshot))
	}
	if snapshot["claude-a@example.com.json"].Plan != "max" {
		t.Fatal("plan should round-trip")
	}
	// The snapshot is a copy: mutating it must not reach the store.
	snapshot["claude-a@example.com.json"].Windows[WindowSevenDay] = Window{UsedPercent: 99, Known: true}
	reading, _ := store.Lookup("id-1")
	if window, _ := reading.Window(WindowSevenDay); window.UsedPercent != 1 {
		t.Fatalf("store used percent = %.1f, want 1", window.UsedPercent)
	}
}

func TestStoreRetainAndForget(t *testing.T) {
	t.Parallel()

	store, clock := newTestStore()
	for _, id := range []string{"a", "b", "c"} {
		store.Observe(Identity{AuthID: id, Provider: "claude"},
			map[string]Window{WindowSevenDay: {UsedPercent: 1, Known: true}}, clock.Now())
	}
	store.Retain([]string{"a", "c"})
	if got := store.AuthIDs(); len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("AuthIDs() = %v, want [a c]", got)
	}
	store.Forget("a")
	if got := store.AuthIDs(); len(got) != 1 || got[0] != "c" {
		t.Fatalf("AuthIDs() = %v, want [c]", got)
	}
}

func TestStoreFetchedAtTracksTheNewestWindow(t *testing.T) {
	t.Parallel()

	store, clock := newTestStore()
	identity := Identity{AuthID: "a", Provider: "claude"}
	store.Observe(identity, map[string]Window{WindowFiveHour: {UsedPercent: 1, Known: true}}, clock.Now())
	first := store.FetchedAt("a")
	clock.Advance(5 * time.Minute)
	store.Observe(identity, map[string]Window{WindowSevenDay: {UsedPercent: 1, Known: true}}, clock.Now())
	if !store.FetchedAt("a").After(first) {
		t.Fatal("FetchedAt should advance with the newest window")
	}
	if store.FetchedAt("missing").IsZero() != true {
		t.Fatal("FetchedAt on an unknown credential should be zero")
	}
}

func TestStoreNilReceiverIsSafe(t *testing.T) {
	t.Parallel()

	var store *Store
	if store.Observe(Identity{AuthID: "a"}, nil, time.Time{}) {
		t.Fatal("Observe on a nil store should report no change")
	}
	if _, ok := store.Lookup("a"); ok {
		t.Fatal("Lookup on a nil store should miss")
	}
	if len(store.Snapshot()) != 0 {
		t.Fatal("Snapshot on a nil store should be empty")
	}
	store.Forget("a")
	store.Retain(nil)
}

func keysOf(m map[string]Reading) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}
