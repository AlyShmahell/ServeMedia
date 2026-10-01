package watchdog

import (
	"strconv"
	"sync"
	"time"
)

const TabGrace = 30 * time.Second

type Tab struct {
	UserID   int64
	TabID    string
	Path     string
	Title    string
	LastSeen time.Time
	released bool
}

type Killer interface {
	ReleaseTab(tabID string)
	StopAll()
}

type Watchdog struct {
	mu           sync.Mutex
	tabs         map[string]*Tab
	ttl          time.Duration
	grace        time.Duration
	now          func() time.Time
	killer       Killer
	shutdown     func()
	everSeen     bool
	stopped      bool
	shutdownOnce sync.Once
}

func New(ttl time.Duration, killer Killer, shutdown func()) *Watchdog {
	return &Watchdog{
		tabs:     map[string]*Tab{},
		ttl:      ttl,
		grace:    TabGrace,
		now:      time.Now,
		killer:   killer,
		shutdown: shutdown,
	}
}

func (w *Watchdog) Run() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		if w.Tick() {
			return
		}
	}
}

func (w *Watchdog) Pulse(userID int64, tabID, path, title string) {
	if w == nil || tabID == "" || userID == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	w.everSeen = true
	k := strconv.FormatInt(userID, 10) + "/" + tabID
	tab := w.tabs[k]
	if tab == nil {
		tab = &Tab{UserID: userID, TabID: tabID}
		w.tabs[k] = tab
	}
	tab.Path = path
	tab.Title = title
	tab.LastSeen = w.now()
	tab.released = false
}

// Tick applies the transcode grace and idle shutdown.
// It returns true only on the tick that requests process shutdown.
func (w *Watchdog) Tick() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	if w.stopped || !w.everSeen {
		w.mu.Unlock()
		return false
	}
	now := w.now()
	var newest time.Time
	var release []string
	for _, tab := range w.tabs {
		if tab.LastSeen.After(newest) {
			newest = tab.LastSeen
		}
		if now.Sub(tab.LastSeen) >= w.grace && !tab.released {
			tab.released = true
			release = append(release, tab.TabID)
		}
	}
	doStop := w.ttl > 0 && !newest.IsZero() && now.Sub(newest) >= w.ttl
	if doStop {
		w.stopped = true
	}
	killer := w.killer
	shutdown := w.shutdown
	w.mu.Unlock()

	if killer != nil {
		for _, id := range release {
			killer.ReleaseTab(id)
		}
		if doStop {
			killer.StopAll()
		}
	}
	if doStop && shutdown != nil {
		w.shutdownOnce.Do(shutdown)
	}
	return doStop
}
