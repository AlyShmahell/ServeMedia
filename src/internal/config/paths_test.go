package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveMediaPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("SERVEMEDIA_ROOT", "")
	t.Setenv("SERVEMEDIA_HOME", "")
	c := Defaults()
	c.Media.Path = "rel, /abs/tv, "
	if err := c.resolvePaths(); err != nil {
		t.Fatal(err)
	}
	wantRel := filepath.Join(home, "share", "servemedia", "rel")
	if c.Media.Path != wantRel+",/abs/tv" {
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

func TestExeDirUsesXDGWhenRootUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SERVEMEDIA_ROOT", "")
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "share"))
	root, err := ExeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "share", "servemedia")
	if root != want {
		t.Fatalf("got %q want %q", root, want)
	}
}

func TestDefaultConfigPath(t *testing.T) {
	t.Setenv("SERVEMEDIA_ROOT", "/app")
	path, err := DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != "/app/config/default.yaml" {
		t.Fatalf("got %q", path)
	}
}

func TestFlatpakDataRoot(t *testing.T) {
	t.Setenv("FLATPAK_ID", "eu.alyshmahell.ServeMedia")
	xdg := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("SERVEMEDIA_ROOT", "/app")
	t.Setenv("SERVEMEDIA_HOME", "")
	root, err := ExeDir()
	if err != nil {
		t.Fatal(err)
	}
	if root != "/app" {
		t.Fatalf("exe dir %q", root)
	}
	c := Defaults()
	c.ExeDir = root
	if err := c.resolvePaths(); err != nil {
		t.Fatal(err)
	}
	wantStore := filepath.Join(xdg, "servemedia", "store")
	if c.Store.Path != wantStore {
		t.Fatalf("store %q want %q", c.Store.Path, wantStore)
	}
	wantOverlay := filepath.Join(xdg, "servemedia", "config", "overlay.yaml")
	gotOverlay, err := overlayPath()
	if err != nil {
		t.Fatal(err)
	}
	if gotOverlay != wantOverlay {
		t.Fatalf("overlay %q want %q", gotOverlay, wantOverlay)
	}
	wantMatch := filepath.Join(xdg, "servemedia", "matchmedia")
	if c.MatchMediaDataDir() != wantMatch {
		t.Fatalf("matchmedia %q want %q", c.MatchMediaDataDir(), wantMatch)
	}
	wantCache := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "servemedia", "transcode")
	if c.Transcode.Path != wantCache {
		t.Fatalf("transcode %q want %q", c.Transcode.Path, wantCache)
	}
}

func TestLoadFlatpakUsesXDGData(t *testing.T) {
	t.Setenv("SERVEMEDIA_ROOT", "/app")
	t.Setenv("FLATPAK_ID", "eu.alyshmahell.ServeMedia")
	xdg := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("SERVEMEDIA_HOME", "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ExeDir != "/app" {
		t.Fatalf("exe dir %q", cfg.ExeDir)
	}
	wantStore := filepath.Join(xdg, "servemedia", "store")
	if cfg.Store.Path != wantStore {
		t.Fatalf("store %q want %q", cfg.Store.Path, wantStore)
	}
	wantOverlay := filepath.Join(xdg, "servemedia", "config", "overlay.yaml")
	if cfg.OverlayPath != wantOverlay {
		t.Fatalf("overlay %q want %q", cfg.OverlayPath, wantOverlay)
	}
}

func TestDataRootHostUsesXDG(t *testing.T) {
	t.Setenv("FLATPAK_ID", "")
	t.Setenv("APPIMAGE", "")
	t.Setenv("SERVEMEDIA_ROOT", "")
	t.Setenv("SERVEMEDIA_HOME", "")
	xdg := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", xdg)
	got, err := DataRoot()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(xdg, "servemedia")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestAppImageUsesSameXDG(t *testing.T) {
	t.Setenv("FLATPAK_ID", "")
	t.Setenv("APPIMAGE", "/opt/ServeMedia.AppImage")
	t.Setenv("SERVEMEDIA_ROOT", "/opt/payload")
	t.Setenv("SERVEMEDIA_HOME", "")
	xdg := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	root, err := ExeDir()
	if err != nil {
		t.Fatal(err)
	}
	if root != "/opt/payload" {
		t.Fatalf("payload %q", root)
	}
	got, err := DataRoot()
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(xdg, "servemedia") {
		t.Fatalf("data %q", got)
	}
}

func TestServeMediaHomeOverridesDataRoot(t *testing.T) {
	t.Setenv("SERVEMEDIA_HOME", "/opt/sm-data")
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	got, err := DataRoot()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/opt/sm-data" {
		t.Fatalf("got %q", got)
	}
}

func TestMatchMediaPathsFlatpak(t *testing.T) {
	t.Setenv("FLATPAK_ID", "eu.alyshmahell.ServeMedia")
	c := Config{}
	if got := c.MatchMediaBin(); got != "/app/tools/matchmedia/matchmedia" {
		t.Fatalf("bin %q", got)
	}
	if got := c.MatchMediaShare(); got != "/app/tools/matchmedia" {
		t.Fatalf("share %q", got)
	}
	if got := c.MatchMediaSeed(); got != "/app/tools/matchmedia/config/default.yaml" {
		t.Fatalf("seed %q", got)
	}
}

func TestMatchMediaPathsSibling(t *testing.T) {
	t.Setenv("FLATPAK_ID", "")
	t.Setenv("SERVEMEDIA_ROOT", "")
	c := Config{}
	bin := c.MatchMediaBin()
	if filepath.Base(bin) != "matchmedia" {
		t.Fatalf("bin base %q", bin)
	}
	share := c.MatchMediaShare()
	if filepath.Base(share) != "matchmedia" {
		t.Fatalf("share base %q", share)
	}
	if !strings.HasSuffix(filepath.ToSlash(share), "/share/matchmedia") {
		t.Fatalf("share should end with /share/matchmedia, got %q", share)
	}
}

func TestMatchMediaShareFromServeMediaRoot(t *testing.T) {
	t.Setenv("FLATPAK_ID", "")
	t.Setenv("APPIMAGE", "/opt/ServeMedia.AppImage")
	t.Setenv("SERVEMEDIA_ROOT", "/payload/share/servemedia")
	c := Config{}
	if got := c.MatchMediaShare(); got != "/payload/share/matchmedia" {
		t.Fatalf("share %q", got)
	}
	if got := c.MatchMediaSeed(); got != "/payload/share/matchmedia/config/default.yaml" {
		t.Fatalf("seed %q", got)
	}
}

func TestExpandMediaXDGTokens(t *testing.T) {
	home := t.TempDir()
	cfgHome := filepath.Join(home, ".config")
	if err := os.MkdirAll(cfgHome, 0o755); err != nil {
		t.Fatal(err)
	}
	userDirs := "XDG_VIDEOS_DIR=\"$HOME/Films\"\nXDG_MUSIC_DIR=\"$HOME/Tunes\"\n"
	if err := os.WriteFile(filepath.Join(cfgHome, "user-dirs.dirs"), []byte(userDirs), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("SERVEMEDIA_ROOT", "")
	t.Setenv("SERVEMEDIA_HOME", "")
	c := Defaults()
	if err := c.resolvePaths(); err != nil {
		t.Fatal(err)
	}
	want := []string{"/mnt", "/media", filepath.Join(home, "Films"), filepath.Join(home, "Tunes")}
	got := c.MediaRoots()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestLoadIgnoresInternalPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("SERVEMEDIA_ROOT", "")
	t.Setenv("SERVEMEDIA_HOME", home)
	t.Setenv("SERVEMEDIA_STORE_PATH", "")
	path := filepath.Join(t.TempDir(), "default.yaml")
	body := "version: \"1.2.3\"\nstore:\n  path: /tmp/nope\ntranscode:\n  path: /tmp/tc\nbackup:\n  dir: /tmp/bak\n  enabled: true\nmedia:\n  path: /library\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(home, "config", "overlay.yaml")
	if err := os.MkdirAll(filepath.Dir(overlay), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlay, []byte("store:\n  path: /tmp/overlay-store\nmedia:\n  path: /from-overlay\nscan:\n  on_startup: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store.Path != filepath.Join(home, "store") {
		t.Fatalf("store %q", cfg.Store.Path)
	}
	if cfg.Transcode.Path != filepath.Join(home, "cache", "servemedia", "transcode") {
		t.Fatalf("transcode %q", cfg.Transcode.Path)
	}
	if cfg.Backup.Dir != filepath.Join(home, "backups") {
		t.Fatalf("backup %q", cfg.Backup.Dir)
	}
	if cfg.Media.Path != "/from-overlay" {
		t.Fatalf("media %q", cfg.Media.Path)
	}
	if !cfg.Backup.Enabled || !cfg.Scan.OnStartup {
		t.Fatalf("settings %#v %#v", cfg.Backup.Enabled, cfg.Scan.OnStartup)
	}
	if cfg.Version != "1.2.3" {
		t.Fatalf("version %q", cfg.Version)
	}
}

func TestSaveSettingsKeepsMediaPath(t *testing.T) {
	cfg := Defaults()
	cfg.Store.Path = "/resolved/store"
	cfg.Transcode.Path = "/resolved/transcode"
	cfg.Backup.Dir = "/resolved/backups"
	cfg.Transcode.CRF = 21
	path := filepath.Join(t.TempDir(), "overlay.yaml")
	if err := os.WriteFile(path, []byte("media:\n  path: /keep/media\nstore:\n  path: /old/store\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "/old/store") || strings.Contains(text, "/resolved/") {
		t.Fatalf("internal paths written:\n%s", text)
	}
	if !strings.Contains(text, "/keep/media") || !strings.Contains(text, "crf:") {
		t.Fatalf("settings missing:\n%s", text)
	}
}
