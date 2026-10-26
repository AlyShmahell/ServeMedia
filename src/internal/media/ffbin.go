package media

import (
	"context"
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	mu          sync.RWMutex
	ffmpegPath  = "ffmpeg"
	ffprobePath = "ffprobe"
	// libvaCandidates are searched in order. The first ELF that opens decides the libc.
	libvaCandidates = []string{
		"/usr/lib64/libva.so.2",
		"/usr/lib/libva.so.2",
	}
)

// SetRoot points ffmpeg/ffprobe at {exeDir}/vendor/ffmpeg when those binaries exist.
// A host libva built for musl selects vendor/ffmpeg/musl when that pair is present.
// An explicit override still wins.
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
	muslBin := filepath.Join(dir, "musl", "ffmpeg")
	muslProbe := filepath.Join(dir, "musl", "ffprobe")
	if hostLibvaMusl() && fileExists(muslBin) && fileExists(muslProbe) {
		ffmpegPath = muslBin
		ffprobePath = muslProbe
		return
	}
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

// ffmpegCommand starts ffmpeg. A musl binary drops LD_LIBRARY_PATH and
// LD_PRELOAD so ld-musl does not load a bundled glibc.
func ffmpegCommand(ctx context.Context, args ...string) *exec.Cmd {
	return mediaCommand(ctx, ffmpeg(), args...)
}

// ffprobeCommand starts ffprobe with the same loader rule as ffmpegCommand.
func ffprobeCommand(ctx context.Context, args ...string) *exec.Cmd {
	return mediaCommand(ctx, ffprobe(), args...)
}

func mediaCommand(ctx context.Context, bin string, args ...string) *exec.Cmd {
	var cmd *exec.Cmd
	if ctx != nil {
		cmd = exec.CommandContext(ctx, bin, args...)
	} else {
		cmd = exec.Command(bin, args...)
	}
	if muslBinary(bin) {
		cmd.Env = envWithoutLoader(os.Environ())
	}
	return cmd
}

// muslBinary reports whether bin's DT_NEEDED entries include libc.musl.
// Those children drop LD_LIBRARY_PATH and LD_PRELOAD so ld-musl does not
// load a bundled glibc.
func muslBinary(bin string) bool {
	needed, err := elfNeeded(bin)
	if err != nil {
		return false
	}
	for _, name := range needed {
		if strings.Contains(name, "libc.musl") {
			return true
		}
	}
	return false
}

func envWithoutLoader(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "LD_LIBRARY_PATH=") || strings.HasPrefix(e, "LD_PRELOAD=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

func hostLibvaMusl() bool {
	for _, path := range libvaCandidates {
		needed, err := elfNeeded(path)
		if err != nil {
			continue
		}
		for _, name := range needed {
			if strings.Contains(name, "libc.musl") {
				return true
			}
		}
		return false
	}
	return false
}

func elfNeeded(path string) ([]string, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ImportedLibraries()
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
