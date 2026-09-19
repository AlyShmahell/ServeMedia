package prepare

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/internal/config"
)

func writeVendorJS(t *testing.T, dir string) config.Config {
	t.Helper()
	vdir := filepath.Join(dir, "vendor")
	if err := os.MkdirAll(filepath.Join(vdir, "video.js"), 0o755); err != nil {
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
	cfg.Vendor.FFmpegLicenseURL = "http://127.0.0.1:1/ffmpeg.lic"
	return cfg
}

func TestThirdPartyRequiresFFmpeg(t *testing.T) {
	t.Setenv("FLATPAK_ID", "")
	cfg := writeVendorJS(t, t.TempDir())
	err := ThirdParty(cfg)
	if err == nil {
		t.Fatal("expected missing ffmpeg")
	}
}

func TestThirdPartySkipsFFmpegWhenFlatpak(t *testing.T) {
	t.Setenv("FLATPAK_ID", "eu.alyshmahell.ServeMedia")
	cfg := writeVendorJS(t, t.TempDir())
	if err := ThirdParty(cfg); err != nil {
		t.Fatal(err)
	}
	ffmpeg := filepath.Join(cfg.VendorDir(), "ffmpeg", "ffmpeg")
	if _, err := os.Stat(ffmpeg); err == nil {
		t.Fatal("flatpak prepare should not require bundled ffmpeg")
	}
}
