package prepare

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alyshmahell/medora/internal/config"
)

// ThirdParty fetches htmx, video.js, and hls.js into {exeDir}/vendor when missing.
// ffmpeg/ffprobe come from the dist builder (static x264, dynamic VAAPI).
func ThirdParty(cfg config.Config) error {
	if strings.TrimSpace(cfg.Vendor.HTMXURL) == "" {
		return fmt.Errorf("vendor URLs missing from config")
	}
	vdir := cfg.VendorDir()
	if err := os.MkdirAll(filepath.Join(vdir, "video.js"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(vdir, "ffmpeg"), 0o755); err != nil {
		return err
	}
	gets := []struct {
		url, dest string
	}{
		{cfg.Vendor.HTMXURL, filepath.Join(vdir, "htmx.min.js")},
		{cfg.Vendor.HTMXLicenseURL, filepath.Join(vdir, "htmx.min.js.LICENSE")},
		{cfg.Vendor.VideoJSJSURL, filepath.Join(vdir, "video.js", "video.min.js")},
		{cfg.Vendor.VideoJSCSSURL, filepath.Join(vdir, "video.js", "video-js.min.css")},
		{cfg.Vendor.VideoJSLicenseURL, filepath.Join(vdir, "video.js", "LICENSE")},
		{cfg.Vendor.HLSURL, filepath.Join(vdir, "hls.min.js")},
		{cfg.Vendor.HLSLicenseURL, filepath.Join(vdir, "hls.min.js.LICENSE")},
		{cfg.Vendor.FFmpegLicenseURL, filepath.Join(vdir, "ffmpeg", "LICENSE")},
	}
	for _, g := range gets {
		if g.url == "" {
			continue
		}
		if fileOK(g.dest) {
			continue
		}
		if err := download(g.url, g.dest); err != nil {
			return fmt.Errorf("%s: %w", g.dest, err)
		}
	}
	ffmpeg := filepath.Join(vdir, "ffmpeg", "ffmpeg")
	ffprobe := filepath.Join(vdir, "ffmpeg", "ffprobe")
	if !fileOK(ffmpeg) || !fileOK(ffprobe) {
		return fmt.Errorf("vendor/ffmpeg missing; rebuild dist (builder compiles ffmpeg)")
	}
	return nil
}

func download(rawURL, dest string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "medora-prepare")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func fileOK(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Size() > 0
}
