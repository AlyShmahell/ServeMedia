package media

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var sidecarLangRe = regexp.MustCompile(`(?i)^(.+)\.([a-z]{2,3}(?:-[a-z]{2,4})?)\.(srt|vtt|ass|ssa)$`)
var sidecarWhisperRe = regexp.MustCompile(`(?i)^(.+)\.([a-z]{2,3}(?:-[a-z]{2,4})?)\.whisper-([a-z0-9._-]+)\.(srt|vtt|ass|ssa)$`)
var sidecarPlainRe = regexp.MustCompile(`(?i)^(.+)\.(srt|vtt|ass|ssa)$`)

// SidecarSubtitle is an external subtitle file next to a video.
type SidecarSubtitle struct {
	Path    string
	Lang    string
	Ext     string
	ID      string // stable key for /sub/{id}.vtt, e.g. sc-en or sc-en-whisper-tiny-q5_1
	Whisper string // model id or ""
}

// DiscoverSidecarSubtitles finds sidecars beside videoPath, including Whisper AI files.
func DiscoverSidecarSubtitles(videoPath string) []SidecarSubtitle {
	dir := filepath.Dir(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []SidecarSubtitle
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		full := filepath.Join(dir, name)
		if m := sidecarWhisperRe.FindStringSubmatch(name); len(m) == 5 {
			if !strings.EqualFold(m[1], base) {
				continue
			}
			lang := strings.ToLower(m[2])
			size := strings.ToLower(m[3])
			id := "sc-" + lang + "-whisper-" + size
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, SidecarSubtitle{
				Path: full, Lang: lang, Ext: strings.ToLower(m[4]), ID: id, Whisper: size,
			})
			continue
		}
		if m := sidecarLangRe.FindStringSubmatch(name); len(m) == 4 {
			if !strings.EqualFold(m[1], base) {
				continue
			}
			lang := strings.ToLower(m[2])
			id := "sc-" + lang
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, SidecarSubtitle{
				Path: full, Lang: lang, Ext: strings.ToLower(m[3]), ID: id,
			})
			continue
		}
		if m := sidecarPlainRe.FindStringSubmatch(name); len(m) == 3 {
			if !strings.EqualFold(m[1], base) {
				continue
			}
			id := "sc-und"
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, SidecarSubtitle{
				Path: full, Lang: "", Ext: strings.ToLower(m[2]), ID: id,
			})
		}
	}
	return out
}

// FindSidecarByID returns the sidecar matching id (e.g. sc-en), or nil.
func FindSidecarByID(videoPath, id string) *SidecarSubtitle {
	for _, sc := range DiscoverSidecarSubtitles(videoPath) {
		if sc.ID == id {
			cp := sc
			return &cp
		}
	}
	return nil
}

// SidecarTracks converts discovered sidecars into play-session Track values.
func SidecarTracks(sidecars []SidecarSubtitle, baseIndex int) []Track {
	var out []Track
	for i, sc := range sidecars {
		title := "Sidecar"
		switch {
		case sc.Whisper != "" && sc.Lang != "":
			title = fmt.Sprintf("AI whisper-%s (%s)", sc.Whisper, sc.Lang)
		case sc.Whisper != "":
			title = "AI whisper-" + sc.Whisper
		case sc.Lang != "":
			title = "Sidecar " + sc.Lang
		}
		out = append(out, Track{
			Index: baseIndex + i,
			ID:    sc.ID,
			Type:  "subtitle",
			Codec: sc.Ext,
			Lang:  sc.Lang,
			Title: title,
		})
	}
	return out
}

// SidecarVTTPath cache path for a converted sidecar.
func SidecarVTTPath(cacheRoot, sourcePath string) string {
	sum := sha256.Sum256([]byte(sourcePath))
	name := "sidecar-" + hex.EncodeToString(sum[:12]) + ".vtt"
	return filepath.Join(cacheRoot, "subs", name)
}

// EnsureSidecarVTT converts an external subtitle file to WebVTT at outPath.
func EnsureSidecarVTT(sourceSub, outPath string) error {
	if st, err := os.Stat(outPath); err == nil && st.Size() > 0 {
		return nil
	}
	if strings.EqualFold(filepath.Ext(sourceSub), ".vtt") {
		return CopyFileSimple(sourceSub, outPath)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	tmp := outPath + ".tmp"
	_ = os.Remove(tmp)
	cmd := exec.Command("ffmpeg", "-y", "-i", sourceSub, "-f", "webvtt", tmp)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(tmp)
		msg := strings.TrimSpace(string(out))
		if len(msg) > 500 {
			msg = msg[len(msg)-500:]
		}
		return fmt.Errorf("ffmpeg sidecar vtt: %w: %s", err, msg)
	}
	if err := os.Rename(tmp, outPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// CopyFileSimple copies src to dst creating parent dirs.
func CopyFileSimple(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0o644)
}
