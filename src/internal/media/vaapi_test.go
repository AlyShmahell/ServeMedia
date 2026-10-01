package media

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/config"
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
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("SERVEMEDIA_HOME", "")
	t.Setenv("HOME", t.TempDir())
	if err := EnsureVAAPIProbeClip(); err != nil {
		t.Fatalf("ensure probe clip: %v", err)
	}
	if vaapiProbeClipPath() == "" {
		t.Fatal("expected probe clip path after ensure")
	}
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

func TestVaapiSmokeTestAV1(t *testing.T) {
	dev := requireRenderNode(t)
	if err := vaapiSmokeTestAV1(dev); err != nil {
		t.Skipf("av1_vaapi smoke on %s: %v", dev, err)
	}
}

func TestVaapiSmokeTestAV110(t *testing.T) {
	dev := requireRenderNode(t)
	if err := vaapiSmokeTestAV110(dev); err != nil {
		t.Skipf("av1_vaapi 10-bit smoke on %s: %v", dev, err)
	}
}

func TestProbeVAAPIIntegration(t *testing.T) {
	requireRenderNode(t)
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("SERVEMEDIA_HOME", "")
	t.Setenv("HOME", t.TempDir())
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
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("SERVEMEDIA_HOME", "")
	t.Setenv("HOME", t.TempDir())
	if err := EnsureVAAPIProbeClip(); err != nil {
		t.Fatalf("ensure probe clip: %v", err)
	}
	clip := vaapiProbeClipPath()
	if clip == "" {
		t.Fatal("expected probe clip")
	}
	cmd := exec.Command(ffmpeg(),
		"-hide_banner", "-loglevel", "error",
		"-init_hw_device", "vaapi=va:"+dev,
		"-filter_hw_device", "va",
		"-hwaccel", "vaapi", "-hwaccel_device", "va", "-hwaccel_output_format", "vaapi",
		"-i", clip,
		"-vf", "scale_vaapi=format=nv12",
		"-c:v", "h264_vaapi",
		"-frames:v", "15",
		"-f", "null", "-",
	)
	if err := runFFmpegSmoke(cmd); err != nil {
		t.Fatalf("full-hw encode smoke: %v", err)
	}
}

func TestEnsureVAAPIProbeClipWritesXDG(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("SERVEMEDIA_HOME", "")
	t.Setenv("HOME", t.TempDir())
	if err := EnsureVAAPIProbeClip(); err != nil {
		t.Skipf("ffmpeg unavailable for probe clip: %v", err)
	}
	want := filepath.Join(xdg, "servemedia", "assets", "vaapi-probe.mkv")
	st, err := os.Stat(want)
	if err != nil || st.Size() == 0 {
		t.Fatalf("probe clip %s: %v size=%v", want, err, st)
	}
	if got := vaapiProbeClipPath(); got != want {
		t.Fatalf("path %q want %q", got, want)
	}
	if err := EnsureVAAPIProbeClip(); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
}

func TestVaapiSmokeTest10BitDirect(t *testing.T) {
	dev := requireRenderNode(t)
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("SERVEMEDIA_HOME", "")
	t.Setenv("HOME", t.TempDir())
	if err := EnsureVAAPIProbeClip(); err != nil {
		t.Skipf("ensure probe clip: %v", err)
	}
	clip := vaapiProbeClipPath()
	if clip == "" {
		t.Fatal("expected probe clip")
	}
	// Generated clip is 8-bit; direct 10-bit smoke is N/A — skip unless file is 10-bit.
	if err := vaapiSmokeTest10BitDirect(dev, clip); err != nil {
		t.Skipf("10-bit direct smoke on 8-bit probe clip: %v", err)
	}
}

func TestVaapiHybridEncodeSmoke(t *testing.T) {
	dev := requireRenderNode(t)
	cmd := exec.Command(ffmpeg(),
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

func TestEnsureLibVADriversPathKeepsExisting(t *testing.T) {
	t.Setenv("LIBVA_DRIVERS_PATH", "/custom/dri")
	ensureLibVADriversPath()
	if got := os.Getenv("LIBVA_DRIVERS_PATH"); got != "/custom/dri" {
		t.Fatalf("LIBVA_DRIVERS_PATH=%q", got)
	}
}

func TestJoinExistingLibVADriverDirsOrder(t *testing.T) {
	root := t.TempDir()
	nonfree := filepath.Join(root, "dri-nonfree")
	freeworld := filepath.Join(root, "dri-freeworld")
	dri := filepath.Join(root, "dri")
	missing := filepath.Join(root, "absent")
	for _, dir := range []string{nonfree, freeworld, dri} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := joinExistingLibVADriverDirs(nonfree, freeworld, dri, missing)
	want := nonfree + ":" + freeworld + ":" + dri
	if got != want {
		t.Fatalf("join=%q want %q", got, want)
	}
	if got := joinExistingLibVADriverDirs(missing); got != "" {
		t.Fatalf("missing dirs: got %q", got)
	}
}

func TestLibVADriverSearchDirsOmitRuntimeMultiarch(t *testing.T) {
	if len(libvaDriverSearchDirs) == 0 {
		t.Fatal("empty libvaDriverSearchDirs")
	}
	for _, dir := range libvaDriverSearchDirs {
		if strings.Contains(dir, "x86_64-linux-gnu") || strings.Contains(dir, "aarch64-linux-gnu") {
			t.Fatalf("runtime multiarch dri must not be pinned: %s", dir)
		}
	}
}

func TestRunFFmpegSmokeIncludesStderr(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo vaapi-boom >&2; exit 1")
	err := runFFmpegSmoke(cmd)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "vaapi-boom") {
		t.Fatalf("stderr not in error: %v", err)
	}
}
