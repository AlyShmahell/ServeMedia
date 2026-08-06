package watchdog

import (
	"testing"
	"time"
)

func TestSessionStoreCreateValidate(t *testing.T) {
	store := NewSessionStore(time.Hour)
	now := time.Now()
	resp := store.Create(now)
	if resp.Token == "" {
		t.Fatal("expected token")
	}
	if !store.Validate(resp.Token, now.Add(time.Minute)) {
		t.Fatal("token should be valid")
	}
	if store.Validate(resp.Token, now.Add(2*time.Hour)) {
		t.Fatal("token should expire")
	}
	if store.Validate("missing", now) {
		t.Fatal("unknown token should fail")
	}
}
