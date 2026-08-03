package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/db"
)

func TestListMediaMetaTargets_showTree(t *testing.T) {
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
	other, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Other", Path: "/media/a/Other", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	s1, err := d.UpsertSeason(ctx, showID, 1, "S1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertSeason(ctx, other, 1, "S1", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: s1, ShowID: showID, EpisodeNumber: 1, Path: "/media/a/Show/e1.mkv", Mtime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	targets, err := d.ListMediaMetaTargets(ctx, showID, false)
	if err != nil {
		t.Fatal(err)
	}
	scopes := map[string]int{}
	for _, t := range targets {
		scopes[t.Scope]++
	}
	if scopes["show"] != 1 || scopes["season"] != 1 || scopes["episode"] != 1 {
		t.Fatalf("targets %#v", scopes)
	}
}
