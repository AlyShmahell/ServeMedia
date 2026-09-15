package media

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/alyshmahell/medora/internal/ffbin"
)

// SubtitleVTTPath returns a cache path for a WebVTT extract of stream index.
func SubtitleVTTPath(cacheRoot, source string, streamIndex int) string {
	sum := sha256.Sum256([]byte(source))
	name := fmt.Sprintf("%s-%d.vtt", hex.EncodeToString(sum[:12]), streamIndex)
	return filepath.Join(cacheRoot, "subs", name)
}

// EnsureSubtitleVTT extracts streamIndex from source to WebVTT at outPath if missing.
func EnsureSubtitleVTT(source string, streamIndex int, outPath string) error {
	if st, err := os.Stat(outPath); err == nil && st.Size() > 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	tmp := outPath + ".tmp"
	_ = os.Remove(tmp)
	cmd := exec.Command(ffbin.FFmpeg(), "-y", "-i", source,
		"-map", fmt.Sprintf("0:%d", streamIndex),
		"-f", "webvtt", tmp)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(tmp)
		msg := strings.TrimSpace(string(out))
		if len(msg) > 500 {
			msg = msg[len(msg)-500:]
		}
		return fmt.Errorf("ffmpeg subtitle extract: %w: %s", err, msg)
	}
	if err := os.Rename(tmp, outPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
