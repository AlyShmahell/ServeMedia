package media

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// ExtractStillJPEG grabs one JPEG frame from src into dst.
// It probes duration, then retries a few seek offsets.
func ExtractStillJPEG(src, dst string) error {
	if !toolsAvailable() {
		return fmt.Errorf("ffmpeg/ffprobe missing")
	}
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)
	if src == "" || dst == "" {
		return fmt.Errorf("missing still paths")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	dur := 0.0
	if p, err := Ffprobe(src); err == nil && p != nil {
		dur, _ = strconv.ParseFloat(p.Format.Duration, 64)
	}
	var last error
	for _, t := range stillSeekTimes(dur) {
		last = grabJPEGFrame(src, dst, t)
		if last == nil {
			st, err := os.Stat(dst)
			if err == nil && !st.IsDir() && st.Size() > 0 {
				return nil
			}
			last = fmt.Errorf("empty still")
		}
	}
	if last == nil {
		last = fmt.Errorf("still extract failed")
	}
	return last
}

// ExtractStillJPEGAt grabs one JPEG frame at the given timestamp.
func ExtractStillJPEGAt(src, dst string, at float64) error {
	if !toolsAvailable() {
		return fmt.Errorf("ffmpeg/ffprobe missing")
	}
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)
	if src == "" || dst == "" {
		return fmt.Errorf("missing still paths")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if at < 0 {
		at = 0
	}
	if err := grabJPEGFrame(src, dst, at); err != nil {
		return err
	}
	st, err := os.Stat(dst)
	if err != nil || st.IsDir() || st.Size() == 0 {
		return fmt.Errorf("empty still")
	}
	return nil
}

// VideoDuration returns the ffprobe duration in seconds.
func VideoDuration(src string) (float64, error) {
	p, err := Ffprobe(src)
	if err != nil || p == nil {
		return 0, err
	}
	dur, err := strconv.ParseFloat(p.Format.Duration, 64)
	if err != nil {
		return 0, err
	}
	if dur < 0 {
		return 0, nil
	}
	return dur, nil
}

// StillOffsetRange is the half-open window used for random still seeks.
func StillOffsetRange(dur float64) (lo, hi float64) {
	if dur <= 2 {
		return 0, dur
	}
	lo = dur * 0.05
	hi = dur - 1
	if hi <= lo {
		hi = dur * 0.95
	}
	if lo < 0 {
		lo = 0
	}
	return lo, hi
}

// RandomStillOffset picks a timestamp inside StillOffsetRange.
func RandomStillOffset(dur float64) float64 {
	lo, hi := StillOffsetRange(dur)
	if hi <= lo {
		return lo
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return lo + (hi-lo)/2
	}
	return lo + (float64(n.Int64())/1_000_000.0)*(hi-lo)
}

func stillSeekTimes(dur float64) []float64 {
	out := make([]float64, 0, 4)
	add := func(t float64) {
		if t < 0 {
			return
		}
		if dur > 0 && t >= dur {
			return
		}
		for _, x := range out {
			if x == t {
				return
			}
		}
		out = append(out, t)
	}
	if dur > 2 {
		t := dur * 0.1
		if t > 30 {
			t = 30
		}
		if t < 1 {
			t = 1
		}
		add(t)
	}
	add(5)
	add(1)
	add(0)
	if len(out) == 0 {
		return []float64{0}
	}
	return out
}

func grabJPEGFrame(src, dst string, at float64) error {
	cmd := exec.Command(ffmpeg(),
		"-hide_banner", "-loglevel", "error",
		"-ss", fmt.Sprintf("%.3f", at),
		"-i", src,
		"-frames:v", "1",
		"-q:v", "2",
		"-y", dst,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		if len(out) > 0 {
			return fmt.Errorf("ffmpeg still: %w: %s", err, string(out))
		}
		return fmt.Errorf("ffmpeg still: %w", err)
	}
	return nil
}

func toolsAvailable() bool {
	return binOK(ffmpeg()) && binOK(ffprobe())
}

func binOK(p string) bool {
	if p == "" {
		return false
	}
	if filepath.IsAbs(p) {
		return fileExists(p)
	}
	_, err := exec.LookPath(p)
	return err == nil
}
