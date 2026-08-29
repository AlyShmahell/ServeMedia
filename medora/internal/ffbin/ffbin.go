package ffbin

import (
	"os"
	"path/filepath"
	"sync"
)

var (
	mu      sync.RWMutex
	ffmpeg  = "ffmpeg"
	ffprobe = "ffprobe"
)

// SetRoot points ffmpeg/ffprobe at {exeDir}/vendor/ffmpeg when those binaries exist.
func SetRoot(exeDir, override string) {
	mu.Lock()
	defer mu.Unlock()
	if override = trim(override); override != "" {
		ffmpeg = override
		dir := filepath.Dir(override)
		probe := filepath.Join(dir, "ffprobe")
		if fileExists(probe) {
			ffprobe = probe
		}
		return
	}
	dir := filepath.Join(exeDir, "vendor", "ffmpeg")
	bin := filepath.Join(dir, "ffmpeg")
	probe := filepath.Join(dir, "ffprobe")
	if fileExists(bin) {
		ffmpeg = bin
	}
	if fileExists(probe) {
		ffprobe = probe
	}
}

func FFmpeg() string {
	mu.RLock()
	defer mu.RUnlock()
	return ffmpeg
}

func FFprobe() string {
	mu.RLock()
	defer mu.RUnlock()
	return ffprobe
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
