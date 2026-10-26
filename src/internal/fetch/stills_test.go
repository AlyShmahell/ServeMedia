package fetch

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
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
	if _, err := os.Stat(filepath.Join(showDir, "poster.jpg")); err != nil {
		t.Fatal(err)
	}
	film, err := d.GetMediaItemByPath(ctx, lib.ID, moviePath)
	if err != nil || film == nil || (film.PosterPath.Valid && film.PosterPath.String != "") {
		t.Fatalf("extracted frame stored as title poster %#v %v", film, err)
	}
	still := filepath.Join(filepath.Dir(moviePath), "Legend of Crimson-thumb.jpg")
	if _, err := os.Stat(still); err != nil {
		t.Fatalf("movie still: %v", err)
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
	got, err := os.ReadFile(storePoster)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep" {
		t.Fatalf("store poster %q", got)
	}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil || it == nil || (it.PosterPath.Valid && it.PosterPath.String != "") {
		t.Fatalf("extracted frame stored as title poster %#v %v", it, err)
	}
}

func TestStillTasksMovieWithPosterEnqueuesStill(t *testing.T) {
	ctx, d, lib, store, mediaRoot := scanFixLib(t)
	filmPath := filepath.Join(mediaRoot, "Film", "Film.mkv")
	if err := os.MkdirAll(filepath.Dir(filmPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filmPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID:  lib.ID,
		Kind:       "movie",
		Title:      "Film",
		Path:       filmPath,
		Mtime:      1,
		PosterPath: sql.NullString{String: "metadata/movies/Film/poster.jpg", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	w := &Worker{DB: d, Store: store}
	tasks, err := w.stillTasks(ctx, lib, it, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("want still task with poster, got %d", len(tasks))
	}
}

func TestStillsProgressKeepsFetchLine(t *testing.T) {
	ctx, d, lib, store, mediaRoot := scanFixLib(t)
	filmPath := filepath.Join(mediaRoot, "Film", "Film.mkv")
	if err := os.MkdirAll(filepath.Dir(filmPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filmPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertMediaItem(ctx, db.MediaItem{LibraryID: lib.ID, Kind: "movie", Title: "Film", Path: filmPath, Mtime: 1}); err != nil {
		t.Fatal(err)
	}
	scanID, err := d.CreateScanJob(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	prev := "match 1/1 matched:1 manual:0 unmatched:0 error:0 Film\nfetch 1/1 nfo:1 poster:1 Film"
	if err := d.UpdateScanJob(ctx, scanID, "running", 70, prev); err != nil {
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
	if err := w.extractMissingStills(ctx, lib, nil, Opts{ScanJobID: scanID}); err != nil {
		t.Fatal(err)
	}
	job, err := d.GetScanJob(ctx, scanID)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	msg := job.Message.String
	if !strings.Contains(msg, "fetch 1/1") {
		t.Fatalf("lost fetch line: %q", msg)
	}
	if !strings.Contains(msg, "stills ") {
		t.Fatalf("missing stills line: %q", msg)
	}
}

func TestMergeStillsLine(t *testing.T) {
	got := mergeStillsLine("match 1/1 Film\nfetch 1 nfo:1 Film\nstills 0/1 old", "stills 1/1 Film")
	if got != "match 1/1 Film\nfetch 1 nfo:1 Film\nstills 1/1 Film" {
		t.Fatalf("%q", got)
	}
}
