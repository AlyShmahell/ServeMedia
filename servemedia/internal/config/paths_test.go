package config

import (
	"path/filepath"
	"testing"
)

func TestResolveMediaPaths(t *testing.T) {
	c := Defaults()
	c.ExeDir = "/opt/servemedia"
	c.Media.Path = "rel, /abs/tv, "
	c.resolvePaths()
	if c.Media.Path != "/opt/servemedia/rel,/abs/tv" {
		t.Fatalf("got %q", c.Media.Path)
	}
}

func TestExeDirHonorsServeMediaRoot(t *testing.T) {
	t.Setenv("SERVEMEDIA_ROOT", "/app")
	root, err := ExeDir()
	if err != nil {
		t.Fatal(err)
	}
	if root != "/app" {
		t.Fatalf("got %q", root)
	}
}

func TestFlatpakDataRoot(t *testing.T) {
	t.Setenv("FLATPAK_ID", "eu.alyshmahell.ServeMedia")
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("SERVEMEDIA_ROOT", "/app")
	root, err := ExeDir()
	if err != nil {
		t.Fatal(err)
	}
	if root != "/app" {
		t.Fatalf("exe dir %q", root)
	}
	c := Defaults()
	c.ExeDir = root
	c.resolvePaths()
	wantStore := filepath.Join(xdg, "servemedia", "data", "store")
	if c.Store.Path != wantStore {
		t.Fatalf("store %q want %q", c.Store.Path, wantStore)
	}
	wantOverlay := filepath.Join(xdg, "servemedia", "data", "config.yaml")
	if overlayPath(root) != wantOverlay {
		t.Fatalf("overlay %q want %q", overlayPath(root), wantOverlay)
	}
	wantMatch := filepath.Join(xdg, "servemedia", "data", "matchmedia")
	if c.MatchMediaDataDir() != wantMatch {
		t.Fatalf("matchmedia %q want %q", c.MatchMediaDataDir(), wantMatch)
	}
}

func TestLoadFlatpakUsesXDGData(t *testing.T) {
	t.Setenv("SERVEMEDIA_ROOT", "/app")
	t.Setenv("FLATPAK_ID", "eu.alyshmahell.ServeMedia")
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ExeDir != "/app" {
		t.Fatalf("exe dir %q", cfg.ExeDir)
	}
	wantStore := filepath.Join(xdg, "servemedia", "data", "store")
	if cfg.Store.Path != wantStore {
		t.Fatalf("store %q want %q", cfg.Store.Path, wantStore)
	}
	wantOverlay := filepath.Join(xdg, "servemedia", "data", "config.yaml")
	if cfg.OverlayPath != wantOverlay {
		t.Fatalf("overlay %q want %q", cfg.OverlayPath, wantOverlay)
	}
}

func TestDataRootHostUnchanged(t *testing.T) {
	t.Setenv("FLATPAK_ID", "")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-should-not-apply")
	if got := DataRoot("/opt/servemedia"); got != "/opt/servemedia" {
		t.Fatalf("got %q", got)
	}
}

