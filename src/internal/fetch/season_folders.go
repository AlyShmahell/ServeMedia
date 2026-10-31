package fetch

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

var (
	sxxexxRe         = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])s([0-9]{1,2})e([0-9]{1,3})(?:[^a-z0-9]|$)`)
	nxnnRe           = regexp.MustCompile(`(?i)(?:^|[^0-9a-z])([0-9]{1,2})x([0-9]{1,3})(?:[^0-9a-z]|$)`)
	absoluteDigitsRe = regexp.MustCompile(`[0-9]{3,4}`)
)

func fillSeasonNumbers(showPath string, files []matchmedia.JobFile) []matchmedia.JobFile {
	out := make([]matchmedia.JobFile, len(files))
	copy(out, files)
	showPath = cleanMediaPath(showPath)
	if showPath == "" {
		return out
	}
	for i := range out {
		f := &out[i]
		if strings.TrimSpace(f.Season) != "" || strings.TrimSpace(f.Episode) != "" {
			continue
		}
		path := cleanMediaPath(f.Path)
		if path == "" || !metadata.IsVideo(filepath.Base(path)) {
			continue
		}
		if !pathWithin(showPath, path) {
			continue
		}
		base := filepath.Base(path)
		if isExtraVideoName(base) {
			continue
		}
		dirSeason, hasDirSeason := seasonFromAncestors(path, showPath)

		if strings.TrimSpace(f.NFOPath) != "" {
			side := metadata.ReadVideoSidecarAt(f.NFOPath)
			if side.Season >= 1 && side.Episode >= 1 {
				if hasDirSeason && dirSeason != side.Season {
					continue
				}
				setEpisodeNumbers(f, side.Season, side.Episode)
				continue
			}
		}

		if season, episode, ok := parseEpisodeMarker(base); ok {
			if hasDirSeason && dirSeason != season {
				continue
			}
			setEpisodeNumbers(f, season, episode)
			continue
		}

		if hasDirSeason {
			if episode, ok := trailingAbsoluteEpisode(base, dirSeason); ok {
				setEpisodeNumbers(f, dirSeason, episode)
			}
		}
	}
	return out
}

func setEpisodeNumbers(f *matchmedia.JobFile, season, episode int) {
	f.Season = strconv.Itoa(season)
	f.Episode = strconv.Itoa(episode)
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func seasonFromAncestors(path, showPath string) (int, bool) {
	showPath = filepath.Clean(showPath)
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if dir == showPath {
			return 0, false
		}
		if season, ok := parseSeasonDirName(filepath.Base(dir)); ok {
			return season, true
		}
		if parent := filepath.Dir(dir); parent == dir {
			return 0, false
		}
	}
}

func parseSeasonDirName(name string) (int, bool) {
	return metadata.ParseSeasonFolderName(name)
}

func parseEpisodeMarker(name string) (int, int, bool) {
	cleaned := cleanedEpisodeToken(name)
	if m := sxxexxRe.FindStringSubmatch(cleaned); m != nil {
		season, err1 := strconv.Atoi(m[1])
		episode, err2 := strconv.Atoi(m[2])
		if err1 == nil && err2 == nil && season >= 1 && season <= 99 && episode >= 1 && episode <= 999 {
			return season, episode, true
		}
	}
	if m := nxnnRe.FindStringSubmatch(cleaned); m != nil {
		season, err1 := strconv.Atoi(m[1])
		episode, err2 := strconv.Atoi(m[2])
		if err1 == nil && err2 == nil && season >= 1 && season <= 99 && episode >= 1 && episode <= 999 {
			return season, episode, true
		}
	}
	return 0, 0, false
}

func trailingAbsoluteEpisode(name string, folderSeason int) (int, bool) {
	if folderSeason < 1 {
		return 0, false
	}
	cleaned := cleanedEpisodeToken(name)
	matches := absoluteDigitsRe.FindAllStringIndex(cleaned, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		start, end := matches[i][0], matches[i][1]
		if start > 0 && isASCIIDigit(cleaned[start-1]) {
			continue
		}
		if end < len(cleaned) && isASCIIDigit(cleaned[end]) {
			continue
		}
		digits := cleaned[start:end]
		if end < len(cleaned) {
			switch cleaned[end] {
			case 'p', 'P', 'i', 'I':
				continue
			}
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			continue
		}
		if len(digits) == 4 && n >= 1900 && n <= 2099 {
			continue
		}
		if n/100 != folderSeason {
			continue
		}
		episode := n % 100
		if episode < 1 || episode > 99 {
			continue
		}
		return episode, true
	}
	return 0, false
}

func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isExtraVideoName(base string) bool {
	base = strings.TrimSuffix(base, filepath.Ext(base))
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	words := " " + strings.Join(strings.Fields(b.String()), " ") + " "
	for _, w := range []string{"sample", "trailer", "trailers", "featurette", "featurettes", "extra", "extras", "interviews"} {
		if strings.Contains(words, " "+w+" ") {
			return true
		}
	}
	for _, phrase := range []string{"behind the scenes", "deleted scenes"} {
		if strings.Contains(words, " "+phrase+" ") {
			return true
		}
	}
	return false
}

func cleanedEpisodeToken(base string) string {
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func countFilledNumbers(before, after []matchmedia.JobFile) int {
	n := 0
	for i := range before {
		if i >= len(after) {
			break
		}
		if strings.TrimSpace(before[i].Season) != "" || strings.TrimSpace(before[i].Episode) != "" {
			continue
		}
		if strings.TrimSpace(after[i].Season) != "" && strings.TrimSpace(after[i].Episode) != "" {
			n++
		}
	}
	return n
}
