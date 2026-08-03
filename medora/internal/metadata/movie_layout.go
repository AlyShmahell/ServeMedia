package metadata

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	genericLibraryFolderRe = regexp.MustCompile(`(?i)^(movies?|films?|tv|anime|media)$`)

	// Jellyfin-style extra subfolder names (exact basename match).
	movieExtraFolderRe = regexp.MustCompile(`(?i)^(trailers?|featurettes?|behind the scenes|deleted scenes|interviews?|scenes?|samples?|shorts?|clips?|extras?|other|theme-music|backdrops)$`)

	// Jellyfin-style extra filename suffixes / whole names.
	movieExtraSuffixRe = regexp.MustCompile(`(?i)(?:[-._ ](?:trailer|sample|scene|clip|interview|behindthescenes|deleted(?:scene)?|featurette|short|other|extra)|(?:^|[-._ ])(?:trailer|sample)$)`)
	movieExtraExactRe  = regexp.MustCompile(`(?i)^(trailer|sample|theme)$`)
)

// IsGenericLibraryFolderName reports library-root-style directory names that
// must not be used as a movie title (flat multi-movie layouts).
func IsGenericLibraryFolderName(name string) bool {
	base := strings.TrimSpace(filepath.Base(name))
	return genericLibraryFolderRe.MatchString(base)
}

// IsMovieExtraFolderName reports Jellyfin extra subfolders under a movie dir.
func IsMovieExtraFolderName(name string) bool {
	base := strings.TrimSpace(filepath.Base(name))
	return movieExtraFolderRe.MatchString(base)
}

// IsMovieExtra reports trailers/samples/featurettes by folder or filename
// (Jellyfin extras + existing show OP/ED-style extras).
func IsMovieExtra(path string) bool {
	base := filepath.Base(path)
	if IsEpisodeExtra(path) {
		return true
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if movieExtraExactRe.MatchString(stem) {
		return true
	}
	if movieExtraSuffixRe.MatchString(stem) {
		return true
	}
	parent := filepath.Base(filepath.Dir(path))
	return IsMovieExtraFolderName(parent)
}

// ListPrimaryVideosInDir returns immediate (non-recursive) primary video files
// in dir, excluding extras. Videos inside extra subfolders are ignored.
func ListPrimaryVideosInDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !IsVideo(name) {
			continue
		}
		p := filepath.Join(dir, name)
		if IsMovieExtra(p) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// CountPrimaryVideos returns how many primary videos sit directly in dir.
func CountPrimaryVideos(dir string) int {
	return len(ListPrimaryVideosInDir(dir))
}

// UseFilenameMovieTitle reports when title/year should come from the filename
// (flat multi-movie dirs under generic library folder parents).
func UseFilenameMovieTitle(videoPath string) bool {
	dir := filepath.Dir(videoPath)
	return IsGenericLibraryFolderName(filepath.Base(dir))
}

// PreferBareMovieSidecar reports when bare movie.nfo / poster.jpg are safe
// (single-primary movie folders). Flat multi-primary dirs must use base-* names.
func PreferBareMovieSidecar(videoPath string) bool {
	dir := filepath.Dir(videoPath)
	if IsGenericLibraryFolderName(filepath.Base(dir)) {
		return false
	}
	return CountPrimaryVideos(dir) <= 1
}

// PickPrimaryMovie chooses one video among versions in the same folder.
// Prefer the name closest to the folder title, then larger file size.
func PickPrimaryMovie(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	if len(paths) == 1 {
		return paths[0]
	}
	dir := filepath.Dir(paths[0])
	folderTitle, _ := ParseTitleYear(filepath.Base(dir))
	folderKey := aliasKey(folderTitle)

	best := paths[0]
	bestScore := -1
	var bestSize int64
	for _, p := range paths {
		stem, _ := ParseTitleYear(filepath.Base(p))
		score := titleCloseness(folderKey, aliasKey(stem))
		var size int64
		if fi, err := os.Stat(p); err == nil {
			size = fi.Size()
		}
		if score > bestScore || (score == bestScore && size > bestSize) {
			best, bestScore, bestSize = p, score, size
		}
	}
	return best
}

func titleCloseness(want, have string) int {
	if want == "" {
		return 0
	}
	if want == have {
		return 100
	}
	if strings.HasPrefix(have, want) || strings.HasPrefix(want, have) {
		return 80
	}
	if strings.Contains(have, want) || strings.Contains(want, have) {
		return 60
	}
	wt := strings.Fields(want)
	matched := 0
	for _, t := range wt {
		if len(t) > 1 && strings.Contains(have, t) {
			matched++
		}
	}
	if len(wt) == 0 {
		return 0
	}
	return (matched * 50) / len(wt)
}

// FindMovieSidecar finds NFO/poster sidecars with flat-layout safety:
// multi-primary dirs skip bare movie.nfo / poster.jpg.
func FindMovieSidecar(dir, base string, bareOK bool, names ...string) string {
	if bareOK {
		return FindSidecar(dir, base, names...)
	}
	// Prefer base-specific names first, then base.nfo style.
	for _, n := range names {
		p := filepath.Join(dir, base+"-"+n)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, n := range names {
		// Allow "Title (2016).nfo" when looking for movie.nfo equivalents.
		if n == "movie.nfo" {
			p := filepath.Join(dir, base+".nfo")
			if _, err := os.Stat(p); err == nil {
				return p
			}
			continue
		}
		if strings.HasPrefix(n, "poster.") || strings.HasPrefix(n, "folder.") {
			continue
		}
		p := filepath.Join(dir, base+"-"+n)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// QuarantineMedoraRejected renames path to path.medora-rejected if it exists.
func QuarantineMedoraRejected(path string) {
	if path == "" {
		return
	}
	if _, err := os.Stat(path); err != nil {
		return
	}
	dst := path + ".medora-rejected"
	_ = os.Rename(path, dst)
}

// MovieNFOMatchesPath reports whether an NFO title is compatible with the
// path-derived lookup title (aliasKey equality or strip-movie-suffix).
func MovieNFOMatchesPath(nfoTitle, pathTitle string) bool {
	nk := aliasKey(nfoTitle)
	pk := aliasKey(pathTitle)
	if nk == "" || pk == "" {
		return false
	}
	if nk == pk {
		return true
	}
	if aliasKey(StripMovieTitleSuffix(nfoTitle)) == pk {
		return true
	}
	if nk == aliasKey(StripMovieTitleSuffix(pathTitle)) {
		return true
	}
	// Path title tokens are a subset of NFO (or vice versa) for "The Movie" variants.
	if strings.HasPrefix(nk, pk) || strings.HasPrefix(pk, nk) {
		return true
	}
	return false
}
