package metadata

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// VideoSidecar is the NFO data read beside a video file.
type VideoSidecar struct {
	Title   string
	Plot    string
	Aired   string
	Season  int
	Episode int
	Runtime int
}

// ReadVideoSidecar reads the first readable NFO beside a video file:
// <base>.nfo, then episode.nfo, then movie.nfo. Unreadable or malformed
// files are skipped; an empty result means "no sidecar".
func ReadVideoSidecar(videoPath string) VideoSidecar {
	videoPath = strings.TrimSpace(videoPath)
	if videoPath == "" {
		return VideoSidecar{}
	}
	dir := filepath.Dir(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	for _, p := range []string{
		filepath.Join(dir, base+".nfo"),
		filepath.Join(dir, "episode.nfo"),
		filepath.Join(dir, "movie.nfo"),
	} {
		if side, ok := readVideoSidecarFile(p); ok {
			return side
		}
	}
	return VideoSidecar{}
}

// ReadVideoSidecarAt reads one specific NFO as a video sidecar. It returns an
// empty sidecar when the file is unreadable, malformed, or not an episode/movie NFO.
func ReadVideoSidecarAt(path string) VideoSidecar {
	side, _ := readVideoSidecarFile(path)
	return side
}

func readVideoSidecarFile(path string) (VideoSidecar, bool) {
	b, err := os.ReadFile(path)
	if err != nil || len(bytesTrim(b)) == 0 {
		return VideoSidecar{}, false
	}
	low := strings.ToLower(string(b))
	if strings.Contains(low, "<episodedetails") {
		n, err := ReadEpisodeNFO(path)
		if err != nil || n == nil {
			return VideoSidecar{}, false
		}
		return VideoSidecar{
			Title: strings.TrimSpace(n.Title), Plot: strings.TrimSpace(n.Plot),
			Aired: strings.TrimSpace(n.Aired), Season: n.Season, Episode: n.Episode, Runtime: n.Runtime,
		}, true
	}
	if strings.Contains(low, "<movie") {
		n, err := ReadMovieNFO(path)
		if err != nil || n == nil {
			return VideoSidecar{}, false
		}
		plot := n.Plot
		if strings.TrimSpace(plot) == "" {
			plot = n.Outline
		}
		return VideoSidecar{
			Title: strings.TrimSpace(n.Title), Plot: strings.TrimSpace(plot),
			Aired: strings.TrimSpace(n.Premiered), Runtime: n.Runtime,
		}, true
	}
	return VideoSidecar{}, false
}

// VideoTitle returns the sidecar title for a video file, or "" when none exists.
func VideoTitle(videoPath string) string {
	return ReadVideoSidecar(videoPath).Title
}

// NFOTitle reads the <title> from an NFO, dispatching on its root element.
// Unreadable, malformed, unknown-root, and blank titles return "".
func NFOTitle(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil || len(bytesTrim(b)) == 0 {
		return ""
	}
	low := strings.ToLower(string(b))
	switch {
	case strings.Contains(low, "<episodedetails"):
		if n, err := ReadEpisodeNFO(path); err == nil && n != nil {
			return strings.TrimSpace(n.Title)
		}
	case strings.Contains(low, "<tvshow"):
		if n, err := ReadTVShowNFO(path); err == nil && n != nil {
			return strings.TrimSpace(n.Title)
		}
	case strings.Contains(low, "<movie"):
		if n, err := ReadMovieNFO(path); err == nil && n != nil {
			return strings.TrimSpace(n.Title)
		}
	}
	return ""
}

// FolderTitle reads a folder title from tvshow.nfo, then movie.nfo, then the
// season NFO for seasonNum (parsed from the folder name when seasonNum <= 0).
func FolderTitle(dir string, seasonNum int) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	if t := NFOTitle(filepath.Join(dir, "tvshow.nfo")); t != "" {
		return t
	}
	if t := NFOTitle(filepath.Join(dir, "movie.nfo")); t != "" {
		return t
	}
	if seasonNum <= 0 {
		seasonNum = seasonNumberFromName(filepath.Base(dir))
	}
	if p := FindSeasonNFO(dir, seasonNum); p != "" {
		if n, err := ReadSeasonNFO(p); err == nil && n != nil {
			if t := strings.TrimSpace(n.Title); t != "" {
				return t
			}
		}
	}
	return ""
}

var (
	seasonFolderNameRe  = regexp.MustCompile(`(?i)^(?:season|series)[\s._-]*([0-9]{1,3})(?:\s*\([0-9]{4}\))?$`)
	shortSeasonFolderRe = regexp.MustCompile(`(?i)^s([0-9]{1,3})(?:\s*\([0-9]{4}\))?$`)
)

// ParseSeasonFolderName reads a season number from folder names such as
// "Season 2", "Series 5", and "S01". Names that are not a single season return false.
func ParseSeasonFolderName(name string) (int, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, false
	}
	m := seasonFolderNameRe.FindStringSubmatch(name)
	if m == nil {
		m = shortSeasonFolderRe.FindStringSubmatch(name)
	}
	if m == nil {
		return 0, false
	}
	season, err := strconv.Atoi(m[1])
	if err != nil || season < 1 || season > 99 {
		return 0, false
	}
	return season, true
}

func seasonNumberFromName(name string) int {
	n, ok := ParseSeasonFolderName(name)
	if !ok {
		return 0
	}
	return n
}

func bytesTrim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
