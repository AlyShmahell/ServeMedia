package config_test

import (
	"testing"

	"github.com/alyshmahell/medora/internal/config"
)

func TestEnsureIntegrationDefaults(t *testing.T) {
	cfg := config.Defaults()
	if !cfg.EnsureIntegrationDefaults() {
		t.Fatal("expected defaults to be generated")
	}
	if cfg.Integrations.Webhooks.ServerID == "" {
		t.Fatal("missing server id")
	}
	if cfg.EnsureIntegrationDefaults() {
		t.Fatal("second call should not change")
	}
}

func TestSplitMediaPaths(t *testing.T) {
	got := config.SplitMediaPaths(" /mnt/a, /mnt/b , ")
	if len(got) != 2 || got[0] != "/mnt/a" || got[1] != "/mnt/b" {
		t.Fatalf("got %#v", got)
	}
	if n := config.SplitMediaPaths(""); len(n) != 0 {
		t.Fatalf("empty: %#v", n)
	}
}

func TestMediaRoots(t *testing.T) {
	c := config.Defaults()
	if got := c.MediaRoots(); len(got) != 1 || got[0] != "/media" {
		t.Fatalf("empty path fallback: %#v", got)
	}
	c.Media.Path = "/mnt/Movies, /mnt/TV,/mnt/Anime"
	got := c.MediaRoots()
	want := []string{"/mnt/Movies", "/mnt/TV", "/mnt/Anime"}
	if len(got) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v want %#v", got, want)
		}
	}
	if c.PrimaryMediaRoot() != "/mnt/Movies" {
		t.Fatalf("primary %q", c.PrimaryMediaRoot())
	}
}
