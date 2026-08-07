package ratelimit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Limiter enforces a per-second token bucket and an optional daily quota.
// Daily=0 means unlimited.
type Limiter struct {
	mu        sync.Mutex
	name      string
	perSecond float64
	daily     int
	tokens    float64
	last      time.Time
	day       string
	usedToday int
	path      string
}

type persisted struct {
	Day       string `json:"day"`
	UsedToday int    `json:"used_today"`
}

func New(name string, perSecond float64, daily int, stateDir string) *Limiter {
	if perSecond <= 0 {
		perSecond = 1
	}
	l := &Limiter{
		name:      name,
		perSecond: perSecond,
		daily:     daily,
		tokens:    perSecond,
		last:      time.Now(),
		day:       utcDay(time.Now()),
		path:      filepath.Join(stateDir, name+".json"),
	}
	l.load()
	return l
}

func utcDay(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

func (l *Limiter) load() {
	b, err := os.ReadFile(l.path)
	if err != nil {
		return
	}
	var p persisted
	if json.Unmarshal(b, &p) != nil {
		return
	}
	if p.Day == utcDay(time.Now()) {
		l.day = p.Day
		l.usedToday = p.UsedToday
	}
}

func (l *Limiter) save() {
	_ = os.MkdirAll(filepath.Dir(l.path), 0o755)
	b, err := json.Marshal(persisted{Day: l.day, UsedToday: l.usedToday})
	if err != nil {
		return
	}
	_ = os.WriteFile(l.path, b, 0o644)
}

func (l *Limiter) Acquire() error {
	for {
		l.mu.Lock()
		now := time.Now()
		day := utcDay(now)
		if day != l.day {
			l.day = day
			l.usedToday = 0
			l.save()
		}
		if l.daily > 0 && l.usedToday >= l.daily {
			l.mu.Unlock()
			return fmt.Errorf("%s daily limit reached (%d)", l.name, l.daily)
		}
		elapsed := now.Sub(l.last).Seconds()
		l.tokens += elapsed * l.perSecond
		if l.tokens > l.perSecond {
			l.tokens = l.perSecond
		}
		l.last = now
		if l.tokens >= 1 {
			l.tokens--
			l.usedToday++
			l.save()
			l.mu.Unlock()
			return nil
		}
		wait := time.Duration((1-l.tokens)/l.perSecond*1000) * time.Millisecond
		if wait < 10*time.Millisecond {
			wait = 10 * time.Millisecond
		}
		l.mu.Unlock()
		time.Sleep(wait)
	}
}
