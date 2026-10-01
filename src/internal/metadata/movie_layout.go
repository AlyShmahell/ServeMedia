package metadata

import (
	"os"
	"path/filepath"
)

func videoCountInDir(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if IsVideo(e.Name()) {
			n++
		}
	}
	return n
}

func sidecarDir(path string) string {
	if st, err := os.Stat(path); err == nil {
		if st.IsDir() {
			return path
		}
		return filepath.Dir(path)
	}
	return filepath.Dir(path)
}

// DirHasTVShowNFO reports when path (file or directory) lives next to a tvshow.nfo.
func DirHasTVShowNFO(path string) bool {
	_, err := os.Stat(filepath.Join(sidecarDir(path), "tvshow.nfo"))
	return err == nil
}

// UseFilenameMovieTitle reports when title/year should come from the filename
// (flat dirs with more than one video file).
func UseFilenameMovieTitle(videoPath string) bool {
	if st, err := os.Stat(videoPath); err == nil && st.IsDir() {
		return false
	}
	return videoCountInDir(filepath.Dir(videoPath)) > 1
}

// PreferBareMovieSidecar reports when bare movie.nfo / poster.jpg are safe
// (a directory that holds at most one video and is not a TV show folder).
func PreferBareMovieSidecar(path string) bool {
	dir := sidecarDir(path)
	if DirHasTVShowNFO(dir) {
		return false
	}
	return videoCountInDir(dir) <= 1
}

// QuarantineServeMediaRejected renames path to path.servemedia-rejected if it exists.
func QuarantineServeMediaRejected(path string) {
	if path == "" {
		return
	}
	if _, err := os.Stat(path); err != nil {
		return
	}
	dst := path + ".servemedia-rejected"
	_ = os.Rename(path, dst)
}
