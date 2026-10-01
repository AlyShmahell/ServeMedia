package metadata_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

func TestTitleYearFromVideoPath_flatMoviesUsesFilename(t *testing.T) {
	dir := t.TempDir()
	movies := filepath.Join(dir, "movies")
	_ = os.MkdirAll(movies, 0o755)
	path := filepath.Join(movies, "Film A (2016).mp4")
	_ = os.WriteFile(path, []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(movies, "Film B (2011).mp4"), []byte("x"), 0o644)
	title, year := metadata.TitleYearFromVideoPath(path)
	if title != "Film A" || year != 2016 {
		t.Fatalf("got %q %d", title, year)
	}
}

func TestTitleYearFromVideoPath_folderMovieKeepsFolder(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "Film Title (2016)")
	_ = os.MkdirAll(folder, 0o755)
	path := filepath.Join(folder, "Film Title Wrong Stem.mkv")
	_ = os.WriteFile(path, []byte("x"), 0o644)
	title, year := metadata.TitleYearFromVideoPath(path)
	if title != "Film Title" || year != 2016 {
		t.Fatalf("got %q %d", title, year)
	}
}

func TestPreferBareMovieSidecar_flat(t *testing.T) {
	dir := t.TempDir()
	movies := filepath.Join(dir, "movies")
	_ = os.MkdirAll(movies, 0o755)
	_ = os.WriteFile(filepath.Join(movies, "Film A (2016).mp4"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(movies, "Film B (2011).mp4"), []byte("x"), 0o644)
	path := filepath.Join(movies, "Film A (2016).mp4")
	if metadata.PreferBareMovieSidecar(path) {
		t.Fatal("flat should not prefer bare")
	}
}

func TestPreferBareMovieSidecar_tvshowNFO(t *testing.T) {
	dir := t.TempDir()
	show := filepath.Join(dir, "KonoSuba")
	_ = os.MkdirAll(filepath.Join(show, "Season 1"), 0o755)
	movie := filepath.Join(show, "Legend of Crimson.mkv")
	_ = os.WriteFile(movie, []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(show, "tvshow.nfo"), []byte("<tvshow/>"), 0o644)
	if metadata.PreferBareMovieSidecar(movie) {
		t.Fatal("show folder must not use bare movie poster.jpg")
	}
	if !metadata.DirHasTVShowNFO(movie) {
		t.Fatal("tvshow.nfo")
	}
}

func TestUseFilenameMovieTitle_folder(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "Film Title (2016)")
	_ = os.MkdirAll(folder, 0o755)
	path := filepath.Join(folder, "Film.mkv")
	_ = os.WriteFile(path, []byte("x"), 0o644)
	if metadata.UseFilenameMovieTitle(path) {
		t.Fatal("single-video folder should use folder title")
	}
}
