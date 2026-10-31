package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/config"
)

func TestDefaultsBooleansOff(t *testing.T) {
	c := config.Defaults()
	if c.Backup.Enabled || c.Scan.OnStartup {
		t.Fatalf("enabled %v startup %v", c.Backup.Enabled, c.Scan.OnStartup)
	}
	if c.Watchdog.TTLSeconds != 60 || c.Transcode.HWAccel != "auto" {
		t.Fatalf("defaults %+v", c)
	}
}

func TestHeightsFromLegacyMax(t *testing.T) {
	got := config.HeightsFromLegacyMax(720)
	want := []int{720, 480, 360, 240, 144}
	if len(got) != len(want) {
		t.Fatalf("720: %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("720: %#v", got)
		}
	}
	got = config.HeightsFromLegacyMax(1000)
	want = []int{1000, 720, 480, 360, 240, 144}
	if len(got) != len(want) {
		t.Fatalf("1000: %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("1000: %#v", got)
		}
	}
}

func TestLoadOverlayMigratesMaxHeight(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SERVEMEDIA_ROOT", root)
	t.Setenv("SERVEMEDIA_HOME", root)
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	overlay := "transcode:\n  max_height: 720\n  crf: 23\n"
	if err := os.WriteFile(filepath.Join(root, "config", "overlay.yaml"), []byte(overlay), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	want := []int{720, 480, 360, 240, 144}
	if len(c.Transcode.Heights) != len(want) {
		t.Fatalf("heights %#v", c.Transcode.Heights)
	}
	for i := range want {
		if c.Transcode.Heights[i] != want[i] {
			t.Fatalf("heights %#v", c.Transcode.Heights)
		}
	}
}

func TestSaveHeightsOmitsMaxHeight(t *testing.T) {
	c := config.Defaults()
	c.Transcode.Heights = []int{720, 480}
	path := filepath.Join(t.TempDir(), "overlay.yaml")
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "max_height") || !strings.Contains(text, "heights:") {
		t.Fatalf("overlay:\n%s", text)
	}
}

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
	c.Media.Path = ""
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
