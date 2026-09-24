package transcode

import (
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/internal/config"
)

func TestFFmpegArgsVAAPIFull(t *testing.T) {
	m := &Manager{
		Cfg:      config.Defaults(),
		vaapiDev: "/dev/dri/renderD128",
	}
	args := strings.Join(m.ffmpegArgs("/media/x.mkv", "/tmp/out", PipelineVAAPIFull, -1, 720, 100), " ")
	if !strings.Contains(args, "h264_vaapi") {
		t.Fatalf("expected h264_vaapi in %q", args)
	}
	if !strings.Contains(args, "-hwaccel vaapi") {
		t.Fatalf("expected input hwaccel in %q", args)
	}
	if !strings.Contains(args, "-hwaccel_output_format vaapi") {
		t.Fatalf("expected hwaccel_output_format in %q", args)
	}
	if strings.Contains(args, "hwupload") {
		t.Fatalf("full-hw should not hwupload: %q", args)
	}
	if !strings.Contains(args, "-ss 100") {
		t.Fatalf("expected seek before input in %q", args)
	}
}

func TestFFmpegArgsVAAPIFull10Bit(t *testing.T) {
	m := &Manager{
		Cfg:      config.Defaults(),
		vaapiDev: "/dev/dri/renderD128",
	}
	args := strings.Join(m.ffmpegArgs("/media/x.mkv", "/tmp/out", PipelineVAAPIFull10Bit, -1, 0, 0), " ")
	if !strings.Contains(args, "-hwaccel vaapi") {
		t.Fatalf("expected input hwaccel in %q", args)
	}
	if !strings.Contains(args, "hwdownload,format=p010le,format=nv12,hwupload") {
		t.Fatalf("expected 10-bit conversion chain in %q", args)
	}
}

func TestFFmpegArgsVAAPIHybrid(t *testing.T) {
	m := &Manager{
		Cfg:      config.Defaults(),
		vaapiDev: "/dev/dri/renderD128",
	}
	args := strings.Join(m.ffmpegArgs("/media/x.mkv", "/tmp/out", PipelineVAAPIHybrid, -1, 720, 100), " ")
	if !strings.Contains(args, "h264_vaapi") {
		t.Fatalf("expected h264_vaapi in %q", args)
	}
	if !strings.Contains(args, "hwupload") {
		t.Fatalf("expected hwupload in %q", args)
	}
	if strings.Contains(args, "-hwaccel vaapi") {
		t.Fatalf("hybrid should not use input hwaccel: %q", args)
	}
}

func TestFFmpegArgsVAAPIAV1(t *testing.T) {
	m := &Manager{
		Cfg:        config.Defaults(),
		vaapiDev:   "/dev/dri/renderD128",
		vaapiCodec: "av1",
	}
	args := strings.Join(m.ffmpegArgs("/media/x.mkv", "/tmp/out", PipelineVAAPIHybrid, -1, 0, 0), " ")
	if !strings.Contains(args, "av1_vaapi") {
		t.Fatalf("expected av1_vaapi in %q", args)
	}
	if strings.Contains(args, "h264_vaapi") {
		t.Fatalf("unexpected h264 in %q", args)
	}
	if !strings.Contains(args, "fmp4") {
		t.Fatalf("expected fmp4 HLS for av1 in %q", args)
	}
	if strings.Contains(args, ".ts") {
		t.Fatalf("av1 HLS should not use mpegts: %q", args)
	}
}

func TestFFmpegArgsVAAPIFullAV110(t *testing.T) {
	m := &Manager{
		Cfg:        config.Defaults(),
		vaapiDev:   "/dev/dri/renderD128",
		vaapiCodec: "av1",
	}
	args := strings.Join(m.ffmpegArgs("/media/x.mkv", "/tmp/out", PipelineVAAPIFullAV110, -1, 720, 0), " ")
	if !strings.Contains(args, "av1_vaapi") {
		t.Fatalf("expected av1_vaapi in %q", args)
	}
	if !strings.Contains(args, "scale_vaapi=w=-2:h=720:format=p010") {
		t.Fatalf("expected p010 scale in %q", args)
	}
	if strings.Contains(args, "format=nv12") {
		t.Fatalf("10-bit AV1 should not force nv12: %q", args)
	}
	if !strings.Contains(args, "-hwaccel vaapi") {
		t.Fatalf("expected input hwaccel in %q", args)
	}
	if !strings.Contains(args, "fmp4") {
		t.Fatalf("expected fmp4 HLS for av1 in %q", args)
	}
}

func TestFFmpegArgsSoftware(t *testing.T) {
	m := &Manager{Cfg: config.Defaults()}
	args := strings.Join(m.ffmpegArgs("/media/x.mkv", "/tmp/out", PipelineSoftware, 0, 0, 0), " ")
	if !strings.Contains(args, "libx264") {
		t.Fatalf("expected libx264 in %q", args)
	}
	if strings.Contains(args, "h264_vaapi") {
		t.Fatalf("unexpected vaapi in software args: %q", args)
	}
}

func TestSelectInitialPipeline(t *testing.T) {
	if got := selectInitialPipeline(true, "yuv420p10le", "av1", true); got != PipelineVAAPIFullAV110 {
		t.Fatalf("10-bit av1 allow: got %v want vaapi_full_av1_10", got)
	}
	if got := selectInitialPipeline(true, "yuv420p10le", "av1", false); got != PipelineVAAPIFull {
		t.Fatalf("10-bit av1 force8: got %v want vaapi_full", got)
	}
	if got := selectInitialPipeline(true, "yuv420p", "av1", true); got != PipelineVAAPIFull {
		t.Fatalf("8-bit av1: got %v want vaapi_full", got)
	}
	if got := selectInitialPipeline(true, "yuv420p10le", "h264", true); got != PipelineVAAPIFull {
		t.Fatalf("10-bit h264: got %v want vaapi_full", got)
	}
	if got := selectInitialPipeline(true, "yuv420p10le", "", false); got != PipelineVAAPIFull {
		t.Fatalf("10-bit initial: got %v want vaapi_full", got)
	}
	if got := selectInitialPipeline(true, "yuv420p", "", false); got != PipelineVAAPIFull {
		t.Fatalf("8-bit: got %v", got)
	}
	if got := selectInitialPipeline(false, "yuv420p", "", false); got != PipelineSoftware {
		t.Fatalf("no vaapi: got %v", got)
	}
}

func TestHWAccelStatusAV110(t *testing.T) {
	m := &Manager{useVAAPI: true, vaapiDev: "/dev/dri/renderD128", vaapiCodec: "av1", vaapiAV110: true}
	if got := m.HWAccelStatus(); got != "vaapi /dev/dri/renderD128 av1 av1_10" {
		t.Fatalf("got %q", got)
	}
}

func TestReleaseTabSharedJob(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{
		jobs:        map[string]*Job{},
		activeByKey: map[string]string{},
	}
	cancelled := 0
	j := &Job{
		ID:        "job",
		OwnerKey:  "tab-b",
		Tabs:      map[string]struct{}{"tab-a": {}, "tab-b": {}},
		OutputDir: dir,
		cancel:    func() { cancelled++ },
	}
	m.jobs["job"] = j
	m.activeByKey["tab-a"] = "job"
	m.activeByKey["tab-b"] = "job"

	m.ReleaseTab("tab-a")
	if _, ok := m.jobs["job"]; !ok {
		t.Fatal("shared job removed while another tab remains")
	}
	if _, ok := j.Tabs["tab-a"]; ok {
		t.Fatal("released tab still attached")
	}
	if cancelled != 0 {
		t.Fatal("ffmpeg cancelled while a viewer remains")
	}

	m.ReleaseTab("tab-b")
	if _, ok := m.jobs["job"]; ok {
		t.Fatal("job still present after last viewer left")
	}
	if cancelled != 1 {
		t.Fatalf("cancel calls %d", cancelled)
	}
	if len(m.activeByKey) != 0 {
		t.Fatalf("active keys left: %#v", m.activeByKey)
	}
}

func TestFallbackPipeline(t *testing.T) {
	if got := fallbackPipeline(PipelineVAAPIFullAV110, "yuv420p10le"); got != PipelineVAAPIFull {
		t.Fatalf("av1 10-bit fallback: got %v want vaapi_full", got)
	}
	if got := fallbackPipeline(PipelineVAAPIFull, "yuv420p10le"); got != PipelineVAAPIFull10Bit {
		t.Fatalf("10-bit full fallback: got %v", got)
	}
	if got := fallbackPipeline(PipelineVAAPIFull, "yuv420p"); got != PipelineVAAPIHybrid {
		t.Fatalf("8-bit full fallback: got %v", got)
	}
	if got := fallbackPipeline(PipelineVAAPIFull10Bit, "yuv420p10le"); got != PipelineVAAPIHybrid {
		t.Fatalf("got %v", got)
	}
	if got := fallbackPipeline(PipelineVAAPIHybrid, ""); got != PipelineSoftware {
		t.Fatalf("got %v", got)
	}
}
