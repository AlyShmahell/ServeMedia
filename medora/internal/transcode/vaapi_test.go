package transcode

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/config"
	"github.com/alyshmahell/medora/internal/ffbin"
)

func requireRenderNode(t *testing.T) string {
	t.Helper()
	matches, err := filepath.Glob("/dev/dri/renderD*")
	if err != nil || len(matches) == 0 {
		t.Skip("no /dev/dri/renderD* nodes")
	}
	for _, dev := range matches {
		if _, err := os.Stat(dev); err == nil {
			return dev
		}
	}
	t.Skip("render nodes present but not accessible")
	return ""
}

func TestVaapiSmokeTestFull(t *testing.T) {
	dev := requireRenderNode(t)
	if err := vaapiSmokeTestFull(dev); err != nil {
		t.Fatalf("full smoke on %s: %v", dev, err)
	}
}

func TestVaapiSmokeTestFullHW(t *testing.T) {
	dev := requireRenderNode(t)
	if err := vaapiSmokeTestFullHW(dev); err != nil {
		t.Fatalf("full-hw smoke on %s: %v", dev, err)
	}
}

func TestVaapiSmokeTestMinimal(t *testing.T) {
	dev := requireRenderNode(t)
	if err := vaapiSmokeTestMinimal(dev); err != nil {
		t.Fatalf("minimal smoke on %s: %v", dev, err)
	}
}

func TestProbeVAAPIIntegration(t *testing.T) {
	requireRenderNode(t)
	m := NewManager(config.Defaults())
	if !m.useVAAPI {
		t.Fatal("expected useVAAPI true when render node accessible")
	}
	if m.vaapiDev == "" {
		t.Fatal("expected vaapiDev set")
	}
	if st := m.HWAccelStatus(); st == "software" {
		t.Fatalf("HWAccelStatus = %q", st)
	}
}

func TestVaapiFullHWEncodeSmoke(t *testing.T) {
	dev := requireRenderNode(t)
	cmd := exec.Command(ffbin.FFmpeg(),
		"-hide_banner", "-loglevel", "error",
		"-init_hw_device", "vaapi=va:"+dev,
		"-filter_hw_device", "va",
		"-hwaccel", "vaapi", "-hwaccel_device", "va", "-hwaccel_output_format", "vaapi",
		"-f", "lavfi", "-i", "color=c=black:s=320x240:d=0.5",
		"-vf", "scale_vaapi=format=nv12",
		"-c:v", "h264_vaapi",
		"-frames:v", "15",
		"-f", "null", "-",
	)
	if err := runFFmpegSmoke(cmd); err != nil {
		t.Fatalf("full-hw encode smoke: %v", err)
	}
}

func TestVaapiSmokeTest10BitDirect(t *testing.T) {
	dev := requireRenderNode(t)
	clip := vaapiProbeClipPath()
	if clip == "" {
		t.Skip("no /usr/share/medora/vaapi-probe.mkv")
	}
	if err := vaapiSmokeTest10BitDirect(dev, clip); err != nil {
		t.Fatalf("10-bit direct smoke on %s: %v", dev, err)
	}
}

func TestVaapiHybridEncodeSmoke(t *testing.T) {
	dev := requireRenderNode(t)
	cmd := exec.Command(ffbin.FFmpeg(),
		"-hide_banner", "-loglevel", "error",
		"-init_hw_device", "vaapi=va:"+dev,
		"-filter_hw_device", "va",
		"-f", "lavfi", "-i", "color=c=black:s=320x240:d=0.5",
		"-vf", "format=nv12,hwupload,scale_vaapi=format=nv12",
		"-c:v", "h264_vaapi",
		"-frames:v", "15",
		"-f", "null", "-",
	)
	if err := runFFmpegSmoke(cmd); err != nil {
		t.Fatalf("hybrid encode smoke: %v", err)
	}
}

func TestIs10BitPixFmt(t *testing.T) {
	if !is10BitPixFmt("yuv420p10le") {
		t.Fatal("expected 10-bit")
	}
	if is10BitPixFmt("yuv420p") {
		t.Fatal("expected 8-bit")
	}
}
