package media

import (
	"testing"
	"time"
)

func TestStillSeekForDuration(t *testing.T) {
	if StillSeekForDuration(0) != 30*time.Second {
		t.Fatalf("fallback: %v", StillSeekForDuration(0))
	}
	got := StillSeekForDuration(1000) // 100s -> clamp max 180? 10% of 1000 = 100s
	if got != 100*time.Second {
		t.Fatalf("got %v want 100s", got)
	}
	got = StillSeekForDuration(30) // 3s -> min 5s
	if got != 5*time.Second {
		t.Fatalf("got %v want 5s", got)
	}
	got = StillSeekForDuration(5000) // 500s -> clamp 180s
	if got != 180*time.Second {
		t.Fatalf("got %v want 180s", got)
	}
}
