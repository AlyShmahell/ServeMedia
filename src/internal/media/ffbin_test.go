package media

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
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
	if got := ffmpeg(); got != filepath.Join(bin, "ffmpeg") {
		t.Fatalf("ffmpeg %q", got)
	}
	if got := ffprobe(); got != filepath.Join(bin, "ffprobe") {
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
	if got := ffmpeg(); got != filepath.Join(bin, "ffmpeg") {
		t.Fatalf("ffmpeg %q", got)
	}
	if got := ffprobe(); got != filepath.Join(bin, "ffprobe") {
		t.Fatalf("ffprobe %q", got)
	}
}

func TestSetRootSelectsLibvaLibc(t *testing.T) {
	prevBin, prevProbe := ffmpeg(), ffprobe()
	prevVA := append([]string(nil), libvaCandidates...)
	t.Cleanup(func() {
		mu.Lock()
		ffmpegPath = prevBin
		ffprobePath = prevProbe
		mu.Unlock()
		libvaCandidates = prevVA
	})
	t.Setenv("LD_LIBRARY_PATH", "/bundled/lib")
	t.Setenv("LD_PRELOAD", "/bundled/preload.so")

	root := t.TempDir()
	dir := filepath.Join(root, "vendor", "ffmpeg")
	musl := filepath.Join(dir, "musl")
	if err := os.MkdirAll(musl, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(dir, "ffmpeg"),
		filepath.Join(dir, "ffprobe"),
	} {
		if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeNeededELF(t, filepath.Join(musl, "ffmpeg"), "libc.musl-x86_64.so.1")
	writeNeededELF(t, filepath.Join(musl, "ffprobe"), "libc.musl-x86_64.so.1")
	glibcBin := filepath.Join(dir, "ffmpeg")
	muslBin := filepath.Join(musl, "ffmpeg")

	libvaCandidates = []string{elfWithNeeded(t, "libc.musl-x86_64.so.1")}
	SetRoot(root, "")
	if got := ffmpeg(); got != muslBin {
		t.Fatalf("musl ffmpeg %q", got)
	}
	if got := ffprobe(); got != filepath.Join(musl, "ffprobe") {
		t.Fatalf("musl ffprobe %q", got)
	}
	muslCmd := ffmpegCommand(nil, "-version")
	if !strings.HasSuffix(muslCmd.Path, "/musl/ffmpeg") {
		t.Fatalf("musl command path %q", muslCmd.Path)
	}
	sawPath := false
	for _, e := range muslCmd.Env {
		if strings.HasPrefix(e, "LD_LIBRARY_PATH=") || strings.HasPrefix(e, "LD_PRELOAD=") {
			t.Fatalf("musl command kept %s", e)
		}
		if strings.HasPrefix(e, "PATH=") {
			sawPath = true
		}
	}
	if !sawPath {
		t.Fatal("musl command dropped the rest of the environment")
	}

	libvaCandidates = []string{elfWithNeeded(t, "libc.so.6")}
	SetRoot(root, "")
	if got := ffmpeg(); got != glibcBin {
		t.Fatalf("glibc ffmpeg %q", got)
	}
	glibcCmd := ffmpegCommand(nil, "-version")
	if glibcCmd.Env != nil {
		t.Fatalf("glibc command replaced env, dropping host LD_LIBRARY_PATH: %v", glibcCmd.Env)
	}

	libvaCandidates = []string{filepath.Join(t.TempDir(), "missing-libva.so")}
	SetRoot(root, "")
	if got := ffmpeg(); got != glibcBin {
		t.Fatalf("missing libva ffmpeg %q", got)
	}

	libvaCandidates = []string{elfWithNeeded(t, "libc.musl-x86_64.so.1")}
	SetRoot(root, "/usr/bin/ffmpeg")
	if got := ffmpeg(); got != "/usr/bin/ffmpeg" {
		t.Fatalf("override ffmpeg %q", got)
	}
}

func TestMuslVendorFFmpegStripsLoaderEnv(t *testing.T) {
	prevBin, prevProbe := ffmpeg(), ffprobe()
	t.Cleanup(func() {
		mu.Lock()
		ffmpegPath = prevBin
		ffprobePath = prevProbe
		mu.Unlock()
	})
	t.Setenv("LD_LIBRARY_PATH", "/bundled/lib")
	t.Setenv("LD_PRELOAD", "/bundled/preload.so")

	root := t.TempDir()
	dir := filepath.Join(root, "vendor", "ffmpeg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeNeededELF(t, filepath.Join(dir, "ffmpeg"), "libc.musl-x86_64.so.1")
	writeNeededELF(t, filepath.Join(dir, "ffprobe"), "libc.musl-x86_64.so.1")
	SetRoot(root, "")
	cmd := ffmpegCommand(nil, "-version")
	if cmd.Path != filepath.Join(dir, "ffmpeg") {
		t.Fatalf("ffmpeg %q", cmd.Path)
	}
	sawPath := false
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "LD_LIBRARY_PATH=") || strings.HasPrefix(e, "LD_PRELOAD=") {
			t.Fatalf("musl command kept %s", e)
		}
		if strings.HasPrefix(e, "PATH=") {
			sawPath = true
		}
	}
	if !sawPath {
		t.Fatal("musl command dropped the rest of the environment")
	}
}

// writeNeededELF writes a small ELF64 at path whose DT_NEEDED entry is name.
func writeNeededELF(t *testing.T, path, name string) {
	t.Helper()
	src := elfWithNeeded(t, name)
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

// elfWithNeeded writes a small ELF64 whose DT_NEEDED entry is name.
func elfWithNeeded(t *testing.T, name string) string {
	t.Helper()
	dynstr := append(append([]byte{0}, name...), 0)
	dyn := make([]byte, 32)
	binary.LittleEndian.PutUint64(dyn[0:8], 1)
	binary.LittleEndian.PutUint64(dyn[8:16], 1)

	const (
		dynOff    = 64
		shentsize = 64
		shnum     = 3
	)
	strOff := dynOff + len(dyn)
	shOff := strOff + len(dynstr)
	buf := make([]byte, shOff+shnum*shentsize)
	copy(buf[:7], []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(buf[16:18], 3)
	binary.LittleEndian.PutUint16(buf[18:20], 62)
	binary.LittleEndian.PutUint32(buf[20:24], 1)
	binary.LittleEndian.PutUint64(buf[40:48], uint64(shOff))
	binary.LittleEndian.PutUint16(buf[52:54], 64)
	binary.LittleEndian.PutUint16(buf[58:60], shentsize)
	binary.LittleEndian.PutUint16(buf[60:62], shnum)
	copy(buf[dynOff:], dyn)
	copy(buf[strOff:], dynstr)

	writeSh := func(index int, typ uint32, off, size uint64, link uint32) {
		b := buf[shOff+index*shentsize:]
		binary.LittleEndian.PutUint32(b[4:8], typ)
		binary.LittleEndian.PutUint64(b[24:32], off)
		binary.LittleEndian.PutUint64(b[32:40], size)
		binary.LittleEndian.PutUint32(b[40:44], link)
	}
	writeSh(1, 6, dynOff, uint64(len(dyn)), 2)
	writeSh(2, 3, uint64(strOff), uint64(len(dynstr)), 0)

	path := filepath.Join(t.TempDir(), "libva.so.2")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
