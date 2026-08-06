package watchdog

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRenderWakePage(t *testing.T) {
	cfg := Config{
		WakePoll:  750 * time.Millisecond,
		PulseHint: 30 * time.Second,
	}
	var buf bytes.Buffer
	if err := renderWakePage(&buf, cfg); err != nil {
		t.Fatalf("renderWakePage: %v", err)
	}
	body := buf.String()
	for _, want := range []string{
		"Medora is waking up",
		"wake-card",
		"wakePollMs",
		"750",
		"pulseMs",
		"30000",
		"Just a moment",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in wake page", want)
		}
	}
	for _, absent := range []string{
		"Pulse:",
		"Status:",
		"Starting Medora… keep this page open",
		"pulse-state",
		"medora-state",
	} {
		if strings.Contains(body, absent) {
			t.Fatalf("unexpected %q in wake page", absent)
		}
	}
}

func TestWakePollIntervalClamp(t *testing.T) {
	if got := wakePollInterval(Config{WakePoll: 50 * time.Millisecond}); got != minWakePoll {
		t.Fatalf("got %v want %v", got, minWakePoll)
	}
	if got := wakePollInterval(Config{}); got != 750*time.Millisecond {
		t.Fatalf("default got %v", got)
	}
}
