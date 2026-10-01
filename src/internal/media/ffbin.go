package media

import (
	"os"
	"path/filepath"
	"sync"
)

var (
	mu          sync.RWMutex
	ffmpegPath  = "ffmpeg"
	ffprobePath = "ffprobe"
)

// SetRoot points ffmpeg/ffprobe at {exeDir}/vendor/ffmpeg when those binaries exist.
func SetRoot(exeDir, override string) {
	mu.Lock()
	defer mu.Unlock()
	if override = trim(override); override != "" {
		ffmpegPath = override
		dir := filepath.Dir(override)
		probe := filepath.Join(dir, "ffprobe")
		if fileExists(probe) {
			ffprobePath = probe
		}
		return
	}
	dir := filepath.Join(exeDir, "vendor", "ffmpeg")
	bin := filepath.Join(dir, "ffmpeg")
	probe := filepath.Join(dir, "ffprobe")
	if fileExists(bin) {
		ffmpegPath = bin
	}
	if fileExists(probe) {
		ffprobePath = probe
	}
}

func ffmpeg() string {
	mu.RLock()
	defer mu.RUnlock()
	return ffmpegPath
}

func ffprobe() string {
	mu.RLock()
	defer mu.RUnlock()
	return ffprobePath
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
