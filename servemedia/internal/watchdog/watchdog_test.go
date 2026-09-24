package watchdog

import (
	"sync"
	"testing"
	"time"
)

type fakeKiller struct {
	mu       sync.Mutex
	released []string
	stops    int
}

func (f *fakeKiller) ReleaseTab(tabID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, tabID)
}

func (f *fakeKiller) StopAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
}

func TestTickReleasesSilentTabOnly(t *testing.T) {
	k := &fakeKiller{}
	var shut int
	w := New(60*time.Second, k, func() { shut++ })
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return now }
	w.Pulse(1, "tab-a", "/play/movie/1", "Movie")
	w.Pulse(1, "tab-b", "/", "Home")
	now = now.Add(TabGrace)
	w.Pulse(1, "tab-b", "/", "Home")
	if w.Tick() {
		t.Fatal("shutdown before ttl")
	}
	k.mu.Lock()
	got := append([]string(nil), k.released...)
	stops := k.stops
	k.mu.Unlock()
	if len(got) != 1 || got[0] != "tab-a" || stops != 0 || shut != 0 {
		t.Fatalf("released %v stops %d shut %d", got, stops, shut)
	}
}

func TestSharedLiveTabBlocksShutdown(t *testing.T) {
	k := &fakeKiller{}
	var shut int
	w := New(60*time.Second, k, func() { shut++ })
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return now }
	w.Pulse(1, "tab-a", "/play/movie/1", "Movie")
	w.Pulse(2, "tab-b", "/", "Home")
	now = now.Add(30 * time.Second)
	w.Pulse(2, "tab-b", "/", "Home")
	now = now.Add(20 * time.Second)
	if w.Tick() {
		t.Fatal("live tab should block shutdown")
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.released) != 1 || k.released[0] != "tab-a" || k.stops != 0 || shut != 0 {
		t.Fatalf("released %v stops %d shut %d", k.released, k.stops, shut)
	}
}

func TestIdleShutdownOnce(t *testing.T) {
	k := &fakeKiller{}
	var shut int
	w := New(60*time.Second, k, func() { shut++ })
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return now }
	w.Pulse(1, "tab-a", "/", "Home")
	now = now.Add(60 * time.Second)
	if !w.Tick() {
		t.Fatal("expected shutdown")
	}
	if w.Tick() {
		t.Fatal("second tick should stay stopped without repeating work")
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.stops != 1 || shut != 1 {
		t.Fatalf("stops %d shut %d", k.stops, shut)
	}
}

func TestTTLZeroNeverShutsDown(t *testing.T) {
	k := &fakeKiller{}
	var shut int
	w := New(0, k, func() { shut++ })
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	w.now = func() time.Time { return now }
	w.Pulse(1, "tab-a", "/", "Home")
	now = now.Add(time.Hour)
	if w.Tick() {
		t.Fatal("ttl 0 should not shut down")
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.released) != 1 || k.stops != 0 || shut != 0 {
		t.Fatalf("released %v stops %d shut %d", k.released, k.stops, shut)
	}
}

func TestNoTabsYetDoesNotShutDown(t *testing.T) {
	k := &fakeKiller{}
	var shut int
	w := New(60*time.Second, k, func() { shut++ })
	if w.Tick() || shut != 0 || k.stops != 0 {
		t.Fatal("shutdown before any tab")
	}
}
