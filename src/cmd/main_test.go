package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/config"
)

func TestPublicURL(t *testing.T) {
	if got := publicURL(":7676"); got != "http://127.0.0.1:7676" {
		t.Fatalf("bare host: %q", got)
	}
	if got := publicURL("0.0.0.0:7676"); got != "http://127.0.0.1:7676" {
		t.Fatalf("wildcard: %q", got)
	}
	if got := publicURL("127.0.0.1:9000"); got != "http://127.0.0.1:9000" {
		t.Fatalf("explicit: %q", got)
	}
}

func TestIsAddrInUse(t *testing.T) {
	if !isAddrInUse(syscall.EADDRINUSE) {
		t.Fatal("EADDRINUSE")
	}
	if isAddrInUse(syscall.ECONNREFUSED) {
		t.Fatal("ECONNREFUSED should not match")
	}
}

func writeVendorJS(t *testing.T, dir string) config.Config {
	t.Helper()
	vdir := filepath.Join(dir, "vendor")
	if err := os.MkdirAll(filepath.Join(vdir, "video.js"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vdir, "codemirror"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{
		filepath.Join(vdir, "htmx.min.js"),
		filepath.Join(vdir, "htmx.min.js.LICENSE"),
		filepath.Join(vdir, "video.js", "video.min.js"),
		filepath.Join(vdir, "video.js", "video-js.min.css"),
		filepath.Join(vdir, "video.js", "LICENSE"),
		filepath.Join(vdir, "hls.min.js"),
		filepath.Join(vdir, "hls.min.js.LICENSE"),
		filepath.Join(vdir, "codemirror", "codemirror.min.js"),
		filepath.Join(vdir, "codemirror", "codemirror.min.css"),
		filepath.Join(vdir, "codemirror", "yaml.min.js"),
		filepath.Join(vdir, "codemirror", "LICENSE"),
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("ok"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Defaults()
	cfg.ExeDir = dir
	cfg.Vendor.HTMXURL = "http://127.0.0.1:1/htmx"
	cfg.Vendor.HTMXLicenseURL = "http://127.0.0.1:1/htmx.lic"
	cfg.Vendor.VideoJSJSURL = "http://127.0.0.1:1/video.js"
	cfg.Vendor.VideoJSCSSURL = "http://127.0.0.1:1/video.css"
	cfg.Vendor.VideoJSLicenseURL = "http://127.0.0.1:1/video.lic"
	cfg.Vendor.HLSURL = "http://127.0.0.1:1/hls"
	cfg.Vendor.HLSLicenseURL = "http://127.0.0.1:1/hls.lic"
	cfg.Vendor.CodeMirrorJSURL = "http://127.0.0.1:1/cm.js"
	cfg.Vendor.CodeMirrorCSSURL = "http://127.0.0.1:1/cm.css"
	cfg.Vendor.CodeMirrorYAMLURL = "http://127.0.0.1:1/yaml.js"
	cfg.Vendor.CodeMirrorLicenseURL = "http://127.0.0.1:1/cm.lic"
	cfg.Vendor.FFmpegLicenseURL = "http://127.0.0.1:1/ffmpeg.lic"
	return cfg
}

func TestThirdPartyRequiresFFmpeg(t *testing.T) {
	t.Setenv("FLATPAK_ID", "")
	t.Setenv("APPIMAGE", "")
	cfg := writeVendorJS(t, t.TempDir())
	err := thirdParty(cfg)
	if err == nil {
		t.Fatal("expected missing ffmpeg")
	}
}

func TestThirdPartySkipsFFmpegWhenAppImage(t *testing.T) {
	t.Setenv("FLATPAK_ID", "")
	t.Setenv("APPIMAGE", "/opt/ServeMedia.AppImage")
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("SERVEMEDIA_HOME", "")
	t.Setenv("HOME", t.TempDir())
	cfg := writeVendorJS(t, t.TempDir())
	if err := thirdParty(cfg); err != nil {
		t.Fatal(err)
	}
	ffmpeg := filepath.Join(cfg.VendorDir(), "ffmpeg", "ffmpeg")
	if _, err := os.Stat(ffmpeg); err == nil {
		t.Fatal("appimage prepare should not require bundled ffmpeg")
	}
}

func TestThirdPartySkipsFFmpegWhenFlatpak(t *testing.T) {
	t.Setenv("FLATPAK_ID", "eu.alyshmahell.ServeMedia")
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("SERVEMEDIA_HOME", "")
	t.Setenv("HOME", t.TempDir())
	cfg := writeVendorJS(t, t.TempDir())
	if err := thirdParty(cfg); err != nil {
		t.Fatal(err)
	}
	ffmpeg := filepath.Join(cfg.VendorDir(), "ffmpeg", "ffmpeg")
	if _, err := os.Stat(ffmpeg); err == nil {
		t.Fatal("flatpak prepare should not require bundled ffmpeg")
	}
}
