package transcode

import (
	"strings"
	"testing"

	"github.com/alyshmahell/medora/internal/config"
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
	if got := selectInitialPipeline(true, "yuv420p10le"); got != PipelineVAAPIFull {
		t.Fatalf("10-bit initial: got %v want vaapi_full", got)
	}
	if got := selectInitialPipeline(true, "yuv420p"); got != PipelineVAAPIFull {
		t.Fatalf("8-bit: got %v", got)
	}
	if got := selectInitialPipeline(false, "yuv420p"); got != PipelineSoftware {
		t.Fatalf("no vaapi: got %v", got)
	}
}

func TestFallbackPipeline(t *testing.T) {
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
