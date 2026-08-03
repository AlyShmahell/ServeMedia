package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/db"
)

func TestUpsertEpisode_reassignsSeasonByPath(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "anime", "/media/a")
	if err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Show", Path: "/media/a/Show", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	s4, err := d.UpsertSeason(ctx, showID, 4, "S4", "", "")
	if err != nil {
		t.Fatal(err)
	}
	s1, err := d.UpsertSeason(ctx, showID, 1, "S1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	path := "/media/a/Show/Cour One/ep.mkv"
	id4, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: s4, ShowID: showID, EpisodeNumber: 1, Path: path, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	id1, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: s1, ShowID: showID, EpisodeNumber: 1, Path: path, Mtime: 2,
	})
	if err != nil {
		t.Fatalf("reassign by path: %v", err)
	}
	if id1 != id4 {
		t.Fatalf("expected same episode row, got %d then %d", id4, id1)
	}
	got, err := d.GetEpisode(ctx, id1)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.SeasonID != s1 || got.EpisodeNumber != 1 || got.Path != path {
		t.Fatalf("got season=%d ep=%d path=%q", got.SeasonID, got.EpisodeNumber, got.Path)
	}
	eps, err := d.ListEpisodesByShow(ctx, showID)
	if err != nil || len(eps) != 1 {
		t.Fatalf("expected 1 episode, got %#v err=%v", eps, err)
	}
}

func TestUpsertEpisode_pathWinsOccupiedSlot(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "anime", "/media/a")
	if err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Show", Path: "/media/a/Show", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	s1, err := d.UpsertSeason(ctx, showID, 1, "S1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	s4, err := d.UpsertSeason(ctx, showID, 4, "S4", "", "")
	if err != nil {
		t.Fatal(err)
	}
	oldPath := "/media/a/Show/old.mkv"
	newPath := "/media/a/Show/new.mkv"
	if _, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: s1, ShowID: showID, EpisodeNumber: 1, Path: oldPath, Mtime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	movedID, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: s4, ShowID: showID, EpisodeNumber: 1, Path: newPath, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Move newPath into S1E01, displacing oldPath.
	id, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: s1, ShowID: showID, EpisodeNumber: 1, Path: newPath, Mtime: 2,
	})
	if err != nil {
		t.Fatalf("path into occupied slot: %v", err)
	}
	if id != movedID {
		t.Fatalf("expected path row %d, got %d", movedID, id)
	}
	got, err := d.GetEpisode(ctx, id)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.SeasonID != s1 || got.Path != newPath {
		t.Fatalf("got %+v", got)
	}
}
