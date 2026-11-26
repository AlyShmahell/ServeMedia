package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// SubtitleVTTPath returns a cache path for a WebVTT extract of stream index.
func SubtitleVTTPath(cacheRoot, source string, streamIndex int) string {
	sum := sha256.Sum256([]byte(source))
	name := fmt.Sprintf("%s-%d.vtt", hex.EncodeToString(sum[:12]), streamIndex)
	return filepath.Join(cacheRoot, "subs", name)
}

// EnsureSubtitleVTT extracts streamIndex from source to WebVTT at outPath if missing.
// Concurrent callers for the same outPath share one extraction. Cancelling ctx
// drops this caller; ffmpeg is cancelled only when nothing else is waiting.
func EnsureSubtitleVTT(ctx context.Context, source string, streamIndex int, outPath string) error {
	return ensureVTTFile(ctx, outPath, func(ctx context.Context, tmp string) error {
		cmd := ffmpegCommand(ctx, "-y", "-i", source,
			"-map", fmt.Sprintf("0:%d", streamIndex),
			"-f", "webvtt", tmp)
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if len(msg) > 500 {
				msg = msg[len(msg)-500:]
			}
			return fmt.Errorf("ffmpeg subtitle extract: %w: %s", err, msg)
		}
		return nil
	})
}
