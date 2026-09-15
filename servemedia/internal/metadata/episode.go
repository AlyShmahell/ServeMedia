package metadata

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	epNxMRe        = regexp.MustCompile(`(?i)(?:^|[.\-_ ])(\d{1,2})x(\d{1,3})(?:\D|$)`)
	epUnderNumRe   = regexp.MustCompile(`(?i)_-_(\d{1,3})_`)
	epDashNumRe    = regexp.MustCompile(`(?i)(?:^|[.\-_ ])-\s*(\d{1,3})(?:\D|$)`)
	epWordNumRe    = regexp.MustCompile(`(?i)(?:^|[.\-_ \[\]()])(?:episode|ep|e)\s*(\d{1,3})(?:\D|$)`)
	epBracketNumRe = regexp.MustCompile(`\[(\d{1,3})\]`)
	epOVANumRe     = regexp.MustCompile(`(?i)\bovas?\s*(\d{1,3})\b`)
	// Season 01, Season 1 - Title, series 2, S01, S01 Something
	seasonFolderRe = regexp.MustCompile(`(?i)^(?:season|series)\s*(\d{1,2})\b|^s(\d{1,2})\b`)
	// Mid-name Season/Series N (e.g. "Show Name - Season 1")
	seasonMidNameRe = regexp.MustCompile(`(?i)(?:^|[^\w])(?:season|series)\s*(\d{1,2})\b`)
	// BluRay-style packs: Show.S01.1080p… (Sxx not only at start)
	seasonMidSxxRe = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])s(\d{1,2})(?:[^a-z0-9]|$)`)
	// "01. Title", "02 - Title", "3. Something"
	seasonNumPrefixRe = regexp.MustCompile(`(?i)^(\d{1,2})\s*[.\-–—]\s*\S`)
	specialsFolderExactRe = regexp.MustCompile(`(?i)^(?:ovas?|specials?)$`)
	specialsFolderWordRe  = regexp.MustCompile(`(?i)\b(?:ovas?|specials?)\b`)
	moviesFolderExactRe   = regexp.MustCompile(`(?i)^(movies?|films?)$`)
	moviesFolderPluralRe  = regexp.MustCompile(`(?i)\b(?:movies|films)\b`)
	ovaFileNameRe         = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])ovas?(?:[^a-z0-9]|$)`)
	extraNameRe           = regexp.MustCompile(`(?i)(?:^|[.\-_ \[\]()])(opening|ending|ncop|nced|\bop\b|\bed\b|trailer|\bpv\b)(?:$|[.\-_ \[\]()])`)
)

// IsSpecialsFolderName reports OVA / Specials-style directories (season 0),
// including release packs with those words as tokens.
func IsSpecialsFolderName(name string) bool {
	base := strings.TrimSpace(filepath.Base(name))
	if specialsFolderExactRe.MatchString(base) {
		return true
	}
	return specialsFolderWordRe.MatchString(base)
}

// IsMoviesFolderName reports a Movies/Films pack folder under a show.
// Exact Movie/Film/Movies/Films, or plural movies/films as a word token —
// not titles that only end with "The Movie".
func IsMoviesFolderName(name string) bool {
	base := strings.TrimSpace(filepath.Base(name))
	if moviesFolderExactRe.MatchString(base) {
		return true
	}
	return moviesFolderPluralRe.MatchString(base)
}

// IsSeasonFolderName reports season-like directory names (Season 2, S02, 01. Title, OVA,
// mid-name Season N, or BluRay packs containing S01 mid-name).
func IsSeasonFolderName(name string) bool {
	base := strings.TrimSpace(filepath.Base(name))
	if IsSpecialsFolderName(base) {
		return true
	}
	if IsMoviesFolderName(base) {
		return true
	}
	if seasonFolderRe.MatchString(base) {
		return true
	}
	if seasonMidNameRe.MatchString(base) {
		return true
	}
	if seasonNumPrefixRe.MatchString(base) {
		return true
	}
	return seasonMidSxxRe.MatchString(base)
}

// ParseSeasonFolder returns a season number for Season/Sxx/series/NN./OVA folder names.
// Specials/OVA and Movies packs map to season 0.
func ParseSeasonFolder(name string) (int, bool) {
	base := strings.TrimSpace(filepath.Base(name))
	if IsSpecialsFolderName(base) || IsMoviesFolderName(base) {
		return 0, true
	}
	if m := seasonFolderRe.FindStringSubmatch(base); len(m) == 3 {
		for _, g := range m[1:] {
			if g != "" {
				n, _ := strconv.Atoi(g)
				return n, n > 0
			}
		}
	}
	if m := seasonMidNameRe.FindStringSubmatch(base); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		return n, n > 0
	}
	if m := seasonNumPrefixRe.FindStringSubmatch(base); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		return n, n > 0
	}
	if m := seasonMidSxxRe.FindStringSubmatch(base); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		return n, n > 0
	}
	return 0, false
}

// IsOVAFileName reports filenames that look like OVAs (for root-level specials).
func IsOVAFileName(name string) bool {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	return ovaFileNameRe.MatchString(base)
}

// IsEpisodeExtra reports OP/ED/trailer-style extras that should not become episodes.
func IsEpisodeExtra(name string) bool {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	return extraNameRe.MatchString(base)
}

// ParseEpisodeNumber extracts an episode number without requiring SxxEyy.
func ParseEpisodeNumber(name string) (int, bool) {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	if m := epOVANumRe.FindStringSubmatch(base); len(m) == 2 {
		ep, _ := strconv.Atoi(m[1])
		return ep, ep > 0
	}
	if m := epNxMRe.FindStringSubmatch(base); len(m) == 3 {
		ep, _ := strconv.Atoi(m[2])
		return ep, ep > 0
	}
	if m := epUnderNumRe.FindStringSubmatch(base); len(m) == 2 {
		ep, _ := strconv.Atoi(m[1])
		return ep, ep > 0
	}
	if m := epDashNumRe.FindStringSubmatch(base); len(m) == 2 {
		ep, _ := strconv.Atoi(m[1])
		return ep, ep > 0
	}
	if m := epWordNumRe.FindStringSubmatch(base); len(m) == 2 {
		ep, _ := strconv.Atoi(m[1])
		return ep, ep > 0
	}
	if m := epBracketNumRe.FindStringSubmatch(base); len(m) == 2 {
		ep, _ := strconv.Atoi(m[1])
		return ep, ep > 0
	}
	return 0, false
}

func dirContainsVideo(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if IsVideo(e.Name()) {
			return true
		}
	}
	// One level deeper (e.g. Season/Extra nested) — still count the dir as a season bucket.
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		subs, err := os.ReadDir(sub)
		if err != nil {
			continue
		}
		for _, se := range subs {
			if !se.IsDir() && IsVideo(se.Name()) {
				return true
			}
		}
	}
	return false
}

// SeasonIndexForShow maps cleaned immediate child dirs that contain videos to season numbers.
// Explicit Season N / Sxx / OVA names win; remaining dirs get 1..N in sorted order, skipping used numbers.
func SeasonIndexForShow(showRoot string) map[string]int {
	showRoot = filepath.Clean(showRoot)
	out := map[string]int{}
	entries, err := os.ReadDir(showRoot)
	if err != nil {
		return out
	}
	var irregular []string
	used := map[int]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Clean(filepath.Join(showRoot, e.Name()))
		if !dirContainsVideo(p) {
			continue
		}
		if sn, ok := ParseSeasonFolder(e.Name()); ok {
			out[p] = sn
			if sn > 0 {
				used[sn] = true
			}
			continue
		}
		irregular = append(irregular, p)
	}
	sort.Strings(irregular)
	next := 1
	for _, p := range irregular {
		for used[next] {
			next++
		}
		out[p] = next
		used[next] = true
		next++
	}
	return out
}

// SeasonForPath picks season from the parent folder (specials, Season N, or show subdir index), or 1.
func SeasonForPath(videoPath, showRoot string) int {
	return SeasonForPathWithIndex(videoPath, showRoot, nil)
}

// SeasonForPathWithIndex is SeasonForPath with an optional precomputed SeasonIndexForShow map.
func SeasonForPathWithIndex(videoPath, showRoot string, index map[string]int) int {
	dir := filepath.Clean(filepath.Dir(videoPath))
	showRoot = filepath.Clean(showRoot)
	if dir == showRoot {
		if IsOVAFileName(videoPath) {
			return 0
		}
		return 1
	}
	if IsMoviesFolderName(filepath.Base(dir)) {
		return 0
	}
	if sn, ok := ParseSeasonFolder(filepath.Base(dir)); ok {
		return sn
	}
	if index == nil {
		index = SeasonIndexForShow(showRoot)
	}
	if sn, ok := index[dir]; ok {
		return sn
	}
	return 1
}

// InSeasonSubdir reports whether the video lives in an immediate child directory of the show root.
func InSeasonSubdir(videoPath, showRoot string) bool {
	dir := filepath.Clean(filepath.Dir(videoPath))
	showRoot = filepath.Clean(showRoot)
	if dir == showRoot {
		return false
	}
	return filepath.Dir(dir) == showRoot
}

// ResolveEpisodeLoose derives season/episode when ParseEpisode (SxxEyy) failed.
func ResolveEpisodeLoose(videoPath, showRoot string) (season, episode int, ok bool) {
	if IsEpisodeExtra(videoPath) {
		return 0, 0, false
	}
	base := filepath.Base(videoPath)
	season = SeasonForPath(videoPath, showRoot)
	if m := epNxMRe.FindStringSubmatch(base); len(m) == 3 {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		if e > 0 {
			if InSeasonSubdir(videoPath, showRoot) {
				return season, e, true
			}
			return s, e, true
		}
	}
	if e, eok := ParseEpisodeNumber(base); eok {
		return season, e, true
	}
	return 0, 0, false
}
