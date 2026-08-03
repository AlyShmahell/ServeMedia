package media

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	stillSeekFallback = 30 * time.Second
	stillSeekMin      = 5 * time.Second
	stillSeekMax      = 180 * time.Second
)

// StillSeekForDuration returns a seek offset at ~10% of duration, clamped.
func StillSeekForDuration(durSec float64) time.Duration {
	if durSec <= 0 {
		return stillSeekFallback
	}
	seek := time.Duration(durSec * 0.1 * float64(time.Second))
	if seek < stillSeekMin {
		seek = stillSeekMin
	}
	if seek > stillSeekMax {
		seek = stillSeekMax
	}
	maxSeek := time.Duration(durSec * float64(time.Second))
	if maxSeek > stillSeekMin && seek >= maxSeek {
		seek = maxSeek / 10
		if seek < stillSeekMin {
			seek = stillSeekMin
		}
	}
	return seek
}

// ExtractStill writes a JPEG still from videoPath to outPath using ffmpeg.
func ExtractStill(videoPath, outPath string) error {
	if videoPath == "" || outPath == "" {
		return fmt.Errorf("empty video or output path")
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	seek := stillSeekFallback
	if p, err := Ffprobe(videoPath); err == nil {
		seek = StillSeekForDuration(p.DurationSeconds())
	}
	ss := fmt.Sprintf("%.3f", seek.Seconds())
	cmd := exec.Command("ffmpeg", "-y", "-ss", ss, "-i", videoPath,
		"-frames:v", "1", "-q:v", "2", outPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(out)
		if len(msg) > 400 {
			msg = msg[:400]
		}
		return fmt.Errorf("ffmpeg still: %w: %s", err, msg)
	}
	st, err := os.Stat(outPath)
	if err != nil || st.Size() == 0 {
		return fmt.Errorf("ffmpeg still: empty output")
	}
	return nil
}
