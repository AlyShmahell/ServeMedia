package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/internal/db"
)

func TestListLibraryPostersStable(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", "/m")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := d.UpsertMediaItem(ctx, db.MediaItem{
			LibraryID:  lib.ID,
			Kind:       "movie",
			Title:      fmt.Sprintf("Title %d", i),
			Path:       fmt.Sprintf("/m/%d.mkv", i),
			PosterPath: sql.NullString{String: fmt.Sprintf("posters/%d.jpg", i), Valid: true},
			Mtime:      int64(i + 1),
		}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := d.ListLibraryPosters(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 3 {
		t.Fatalf("got %d posters, want 3: %#v", len(a), a)
	}
	b, err := d.ListLibraryPosters(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 3 {
		t.Fatalf("second call %d posters: %#v", len(b), b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("collage shuffled: first %#v second %#v", a, b)
		}
	}
}

func TestShowPosterFallsBackToSeason(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", "/m")
	if err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Spider", Path: "/m/Spider", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertSeason(ctx, showID, 0, "Specials", "metadata/tv/Spider/S00/poster.jpg", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertSeason(ctx, showID, 1, "Season 1", "metadata/tv/Spider/S01/poster.jpg", ""); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetMediaItem(ctx, showID)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if !got.PosterPath.Valid || got.PosterPath.String != "metadata/tv/Spider/S01/poster.jpg" {
		t.Fatalf("get poster %#v", got.PosterPath)
	}
	items, err := d.ListMediaItems(ctx, lib.ID, "", "")
	if err != nil || len(items) != 1 {
		t.Fatalf("list %#v %v", items, err)
	}
	if items[0].PosterPath.String != "metadata/tv/Spider/S01/poster.jpg" {
		t.Fatalf("list poster %q", items[0].PosterPath.String)
	}

	filmID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Film", Path: "/m/Film.mkv",
		PosterPath: sql.NullString{String: "metadata/movies/Film/poster.jpg", Valid: true}, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	film, err := d.GetMediaItem(ctx, filmID)
	if err != nil || film == nil || film.PosterPath.String != "metadata/movies/Film/poster.jpg" {
		t.Fatalf("movie poster %#v %v", film, err)
	}
}
