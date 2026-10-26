package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
)

func TestRecentlyAddedRequiresShowOrMovieNFO(t *testing.T) {
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
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", "/media")
	if err != nil {
		t.Fatal(err)
	}
	nfo := func(path string) sql.NullString {
		return sql.NullString{String: path, Valid: true}
	}
	insert := func(it db.MediaItem) int64 {
		t.Helper()
		id, err := d.UpsertMediaItem(ctx, it)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	insert(db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Alpha Film", Path: "/media/Alpha.mkv", Mtime: 1,
		NFOPath: nfo("/media/Alpha/movie.nfo"),
	})
	folder := insert(db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Franchise", Path: "/media/Franchise", Mtime: 2,
	})
	child := insert(db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Nested Show", Path: "/media/Franchise/Nested", Mtime: 3,
		NFOPath: nfo("/media/Franchise/Nested/tvshow.nfo"),
	})
	if err := d.SetMediaItemParent(ctx, child, folder); err != nil {
		t.Fatal(err)
	}
	insert(db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Loose File", Path: "/media/Loose.mkv", Mtime: 4,
	})
	insert(db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Episode File", Path: "/media/Show/E01.mkv", Mtime: 5,
		NFOPath: nfo("/media/Show/Show E01.nfo"),
	})
	insert(db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Bare Movie", Path: "/media/Bare.mkv", Mtime: 6,
		NFOPath: nfo("movie.nfo"),
	})

	got, err := d.RecentlyAdded(ctx, u.ID, 24)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"Alpha Film": true, "Nested Show": true, "Bare Movie": true}
	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d", len(got), len(want))
	}
	for _, it := range got {
		if !want[it.Title] {
			t.Fatalf("unexpected %q", it.Title)
		}
	}
}
