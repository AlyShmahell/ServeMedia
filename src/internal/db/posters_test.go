package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
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

func TestListLibraryPostersSkipsStills(t *testing.T) {
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
	if _, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Real", Path: "/m/real.mkv",
		PosterPath: sql.NullString{String: "posters/real.jpg", Valid: true}, Mtime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Old still", Path: "/m/old.mkv",
		PosterPath: sql.NullString{String: "stills/a.jpg", Valid: true}, Mtime: 2,
	}); err != nil {
		t.Fatal(err)
	}
	for i, still := range []string{"stills/a.jpg", "stills/b.jpg", "stills/c.jpg"} {
		id, err := d.UpsertNodeChain(ctx, lib.ID, fmt.Sprintf("clip-%d.mkv", i), "file")
		if err != nil {
			t.Fatal(err)
		}
		if err := d.SetNodeStill(ctx, id, still); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.ListLibraryPosters(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "posters/real.jpg" {
		t.Fatalf("posters %#v", got)
	}
}

func TestListLibraryPostersHighestRated(t *testing.T) {
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
	type row struct {
		title, poster, added string
		rating               float64
	}
	for _, it := range []row{
		{title: "Newest low", poster: "posters/low.jpg", added: "2026-10-17T18:00:00Z", rating: 1.2},
		{title: "Mid", poster: "posters/mid.jpg", added: "2020-01-01T00:00:00Z", rating: 7.4},
		{title: "High", poster: "posters/high.jpg", added: "2018-01-01T00:00:00Z", rating: 9.1},
		{title: "Second", poster: "posters/second.jpg", added: "2019-01-01T00:00:00Z", rating: 8.6},
	} {
		id, err := d.UpsertMediaItem(ctx, db.MediaItem{
			LibraryID:  lib.ID,
			Kind:       "movie",
			Title:      it.title,
			Path:       "/m/" + it.title + ".mkv",
			PosterPath: sql.NullString{String: it.poster, Valid: true},
			Rating:     sql.NullFloat64{Float64: it.rating, Valid: true},
			Mtime:      1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.SQL.ExecContext(ctx, `UPDATE media_items SET date_added=? WHERE id=?`, it.added, id); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.ListLibraryPosters(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"posters/high.jpg", "posters/second.jpg", "posters/mid.jpg"}
	if len(got) != len(want) {
		t.Fatalf("posters %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("posters %#v want %#v", got, want)
		}
	}
}

func TestListLibraryPostersUniqueTitles(t *testing.T) {
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
	type row struct {
		title, poster, provider, id string
		rating                      float64
		rated                       bool
	}
	for _, it := range []row{
		{title: "Steins;Gate", poster: "posters/gate.jpg", provider: "tvmaze", id: "2051", rating: 9.2, rated: true},
		{title: "Steins Gate", poster: "posters/gate-copy.jpg", provider: "tvmaze", id: "2051", rating: 8.0, rated: true},
		{title: "Other", poster: "posters/other.jpg", provider: "tvmaze", id: "1", rating: 7.0, rated: true},
		{title: "Fresh", poster: "posters/fresh.jpg"},
	} {
		item := db.MediaItem{
			LibraryID: lib.ID, Kind: "show", Title: it.title, Path: "/m/" + it.title,
			PosterPath: sql.NullString{String: it.poster, Valid: true}, Mtime: 1,
		}
		if it.rated {
			item.Rating = sql.NullFloat64{Float64: it.rating, Valid: true}
		}
		id, err := d.UpsertMediaItem(ctx, item)
		if err != nil {
			t.Fatal(err)
		}
		if it.provider != "" {
			if _, err := d.SQL.ExecContext(ctx, `UPDATE media_items SET meta_provider=?, meta_id=? WHERE id=?`, it.provider, it.id, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	got, err := d.ListLibraryPosters(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"posters/gate.jpg", "posters/other.jpg", "posters/fresh.jpg"}
	if len(got) != len(want) {
		t.Fatalf("posters %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("posters %#v want %#v", got, want)
		}
	}
}

func TestListLibraryPostersSeasonFallbackAndUnratedFill(t *testing.T) {
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
		LibraryID: lib.ID, Kind: "show", Title: "Show", Path: "/m/Show",
		Rating: sql.NullFloat64{Float64: 9.4, Valid: true}, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertSeason(ctx, showID, 1, "Season 1", "metadata/tv/Show/S01/poster.jpg", ""); err != nil {
		t.Fatal(err)
	}
	filmID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Film", Path: "/m/Film.mkv",
		PosterPath: sql.NullString{String: "metadata/movies/Film/poster.jpg", Valid: true},
		Rating:     sql.NullFloat64{Float64: 3.1, Valid: true}, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	oldID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Old", Path: "/m/Old.mkv",
		PosterPath: sql.NullString{String: "metadata/movies/Old/poster.jpg", Valid: true}, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	newID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "New", Path: "/m/New.mkv",
		PosterPath: sql.NullString{String: "metadata/movies/New/poster.jpg", Valid: true}, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		id    int64
		added string
	}{
		{showID, "2010-01-01T00:00:00Z"},
		{filmID, "2011-01-01T00:00:00Z"},
		{oldID, "2012-01-01T00:00:00Z"},
		{newID, "2026-01-01T00:00:00Z"},
	} {
		if _, err := d.SQL.ExecContext(ctx, `UPDATE media_items SET date_added=? WHERE id=?`, pair.added, pair.id); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.ListLibraryPosters(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"metadata/tv/Show/S01/poster.jpg", "metadata/movies/Film/poster.jpg", "metadata/movies/New/poster.jpg"}
	if len(got) != len(want) {
		t.Fatalf("posters %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("posters %#v want %#v", got, want)
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
