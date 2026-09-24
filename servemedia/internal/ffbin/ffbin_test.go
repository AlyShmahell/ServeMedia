package ffbin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetRootUsesVendorFFmpeg(t *testing.T) {
	t.Setenv("APPIMAGE", "")
	dir := t.TempDir()
	bin := filepath.Join(dir, "vendor", "ffmpeg")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "ffmpeg"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "ffprobe"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	SetRoot(dir, "")
	if got := FFmpeg(); got != filepath.Join(bin, "ffmpeg") {
		t.Fatalf("ffmpeg %q", got)
	}
	if got := FFprobe(); got != filepath.Join(bin, "ffprobe") {
		t.Fatalf("ffprobe %q", got)
	}
}

func TestSetRootAppImageUsesVendorFFmpeg(t *testing.T) {
	t.Setenv("APPIMAGE", "/opt/ServeMedia.AppImage")
	dir := t.TempDir()
	bin := filepath.Join(dir, "vendor", "ffmpeg")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "ffmpeg"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "ffprobe"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	SetRoot(dir, "")
	if got := FFmpeg(); got != filepath.Join(bin, "ffmpeg") {
		t.Fatalf("ffmpeg %q", got)
	}
	if got := FFprobe(); got != filepath.Join(bin, "ffprobe") {
		t.Fatalf("ffprobe %q", got)
	}
}
