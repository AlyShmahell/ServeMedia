package metadata

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SubtitleSidecarName returns Kodi-style {base}.{lang}.srt next to a video.
func SubtitleSidecarName(videoPath, lang string) string {
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	lang = strings.TrimSpace(strings.ToLower(lang))
	lang = strings.ReplaceAll(lang, "_", "-")
	if lang == "" {
		return base + ".srt"
	}
	return fmt.Sprintf("%s.%s.srt", base, lang)
}

// WhisperSidecarName returns {base}.{lang}.whisper-{modelID}.srt.
func WhisperSidecarName(videoPath, lang, modelID string) string {
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	lang = strings.TrimSpace(strings.ToLower(lang))
	lang = strings.ReplaceAll(lang, "_", "-")
	modelID = strings.TrimSpace(strings.ToLower(modelID))
	if modelID == "" {
		modelID = "tiny-q5_1"
	}
	if lang == "" {
		return fmt.Sprintf("%s.whisper-%s.srt", base, modelID)
	}
	return fmt.Sprintf("%s.%s.whisper-%s.srt", base, lang, modelID)
}

// SubtitleSidecarPath is the full path for a language sidecar beside the video.
func SubtitleSidecarPath(videoPath, lang string) string {
	return filepath.Join(filepath.Dir(videoPath), SubtitleSidecarName(videoPath, lang))
}

// WhisperSidecarPath is the full path for a Whisper-generated sidecar.
func WhisperSidecarPath(videoPath, lang, modelID string) string {
	return filepath.Join(filepath.Dir(videoPath), WhisperSidecarName(videoPath, lang, modelID))
}

// WriteBesideDir writes filename into dir from r (creates dir if needed).
func WriteBesideDir(dir, filename string, r io.Reader) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, filepath.Base(filename))
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	return dst, nil
}

// WriteBesideVideo writes filename next to the video file.
func WriteBesideVideo(videoPath, filename string, r io.Reader) (string, error) {
	return WriteBesideDir(filepath.Dir(videoPath), filename, r)
}

// WriteBytesBesideDir writes data into dir/filename.
func WriteBytesBesideDir(dir, filename string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, filepath.Base(filename))
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

// WriteBytesBesideVideo writes data next to the video file.
func WriteBytesBesideVideo(videoPath, filename string, data []byte) (string, error) {
	return WriteBytesBesideDir(filepath.Dir(videoPath), filename, data)
}

func WriteSeasonNFO(path string, n SeasonNFO) error {
	b, err := xml.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), b...), 0o644)
}

func WriteEpisodeNFO(path string, n EpisodeNFO) error {
	b, err := xml.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), b...), 0o644)
}
