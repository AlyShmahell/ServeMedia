package fetch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
)

func TestExtractMovieStillDoesNotClobberShowPoster(t *testing.T) {
	ctx, d, lib, store, mediaRoot := scanFixLib(t)
	showDir := filepath.Join(mediaRoot, "KonoSuba")
	epPath := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	moviePath := filepath.Join(showDir, "Legend of Crimson.mkv")
	showPoster := filepath.Join(showDir, "poster.jpg")
	if err := os.MkdirAll(filepath.Dir(epPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(moviePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(showDir, "tvshow.nfo"), []byte("<tvshow><title>KonoSuba</title></tvshow>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(showPoster, []byte("show-art"), 0o644); err != nil {
		t.Fatal(err)
	}

	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{LibraryID: lib.ID, Kind: "show", Title: "KonoSuba", Path: showDir, Mtime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertMediaItem(ctx, db.MediaItem{LibraryID: lib.ID, Kind: "movie", Title: "Legend of Crimson", Path: moviePath, Mtime: 1}); err != nil {
		t.Fatal(err)
	}
	s1, err := d.UpsertSeason(ctx, showID, 1, "Season 1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, db.Episode{SeasonID: s1, ShowID: showID, EpisodeNumber: 1, Path: epPath, Mtime: 1}); err != nil {
		t.Fatal(err)
	}

	old := extractStill
	extractStill = func(src, dst string) error {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, []byte("still-jpeg"), 0o644)
	}
	t.Cleanup(func() { extractStill = old })

	w := &Worker{DB: d, Store: store}
	if err := w.extractMissingStills(ctx, lib, nil, Opts{Persist: true}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(showPoster)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "show-art" {
		t.Fatalf("show poster overwritten: %q", got)
	}
	named := filepath.Join(showDir, "Legend of Crimson-poster.jpg")
	if _, err := os.Stat(named); err != nil {
		t.Fatalf("named movie sidecar: %v", err)
	}
	film, err := d.GetMediaItemByPath(ctx, lib.ID, moviePath)
	if err != nil || film == nil || !film.PosterPath.Valid || film.PosterPath.String == "" {
		t.Fatalf("movie poster %#v %v", film, err)
	}
}

func TestExtractMovieStillSkipsExistingStorePoster(t *testing.T) {
	ctx, d, lib, store, mediaRoot := scanFixLib(t)
	filmPath := filepath.Join(mediaRoot, "Film", "Film.mkv")
	if err := os.MkdirAll(filepath.Dir(filmPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filmPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{LibraryID: lib.ID, Kind: "movie", Title: "Film", Path: filmPath, Mtime: 1})
	if err != nil {
		t.Fatal(err)
	}
	storePoster := filepath.Join(store, "metadata", "movies", "Film", "poster.jpg")
	if err := os.MkdirAll(filepath.Dir(storePoster), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storePoster, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	called := 0
	old := extractStill
	extractStill = func(src, dst string) error {
		called++
		return os.WriteFile(dst, []byte("new"), 0o644)
	}
	t.Cleanup(func() { extractStill = old })

	w := &Worker{DB: d, Store: store}
	if err := w.extractMissingStills(ctx, lib, nil, Opts{}); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatalf("extracted over existing store poster (%d)", called)
	}
	got, err := os.ReadFile(storePoster)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep" {
		t.Fatalf("store poster %q", got)
	}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil || it == nil || it.PosterPath.String == "" {
		t.Fatalf("db poster %#v %v", it, err)
	}
}
