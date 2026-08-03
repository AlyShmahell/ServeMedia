package metadata_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/metadata"
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

func TestIsMovieExtra(t *testing.T) {
	if !metadata.IsMovieExtra("/m/Movie-trailer.mkv") {
		t.Fatal("suffix trailer")
	}
	if !metadata.IsMovieExtraFolderName("trailers") {
		t.Fatal("trailers folder")
	}
	if metadata.IsMovieExtra("/m/Movie.mkv") {
		t.Fatal("main feature")
	}
}

func TestPickPrimaryMovie(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "Movie Title (2008)")
	_ = os.MkdirAll(folder, 0o755)
	a := filepath.Join(folder, "Movie Title (2008) - 1080p.mkv")
	b := filepath.Join(folder, "Movie Title (2008) - 2160p.mkv")
	_ = os.WriteFile(a, []byte("small"), 0o644)
	_ = os.WriteFile(b, make([]byte, 1000), 0o644)
	got := metadata.PickPrimaryMovie([]string{a, b})
	if got != b {
		t.Fatalf("want larger file, got %s", got)
	}
}

func TestFindMovieSidecar_skipsBareInFlat(t *testing.T) {
	dir := t.TempDir()
	movies := filepath.Join(dir, "movies")
	_ = os.MkdirAll(movies, 0o755)
	_ = os.WriteFile(filepath.Join(movies, "movie.nfo"), []byte(`<?xml version="1.0"?><movie><title>Wrong</title></movie>`), 0o644)
	_ = os.WriteFile(filepath.Join(movies, "Film A (2016).mp4"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(movies, "Film B (2011).mp4"), []byte("x"), 0o644)
	base := "Film A (2016)"
	path := filepath.Join(movies, base+".mp4")
	if metadata.PreferBareMovieSidecar(path) {
		t.Fatal("flat should not prefer bare")
	}
	if p := metadata.FindMovieSidecar(movies, base, false, "movie.nfo"); p != "" {
		t.Fatalf("should skip bare movie.nfo, got %s", p)
	}
}

func TestMovieNFOMatchesPath(t *testing.T) {
	if !metadata.MovieNFOMatchesPath("Some Film: The Movie", "Some Film") {
		t.Fatal("movie suffix variant")
	}
	if metadata.MovieNFOMatchesPath("Unrelated Sequel To the Movies", "Film A") {
		t.Fatal("unrelated")
	}
}

func TestIsSeasonFolderName_midSxx(t *testing.T) {
	if !metadata.IsSeasonFolderName("Show.Name.S01.1080p.BluRay.AV1-GROUP") {
		t.Fatal("mid S01")
	}
	sn, ok := metadata.ParseSeasonFolder("Show.Name.S02.1080p")
	if !ok || sn != 2 {
		t.Fatalf("got %d %v", sn, ok)
	}
}
