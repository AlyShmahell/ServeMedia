package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
)

func TestContinueWatchingFileTitleAndPoster(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "admin", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Movies", "/media/m")
	if err != nil {
		t.Fatal(err)
	}
	folderID, err := d.UpsertNodeChain(ctx, lib.ID, "Film", "dir")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeGroup(ctx, folderID, db.NodeGroup{Title: "The Film", Poster: "metadata/movies/Film/poster.jpg"}); err != nil {
		t.Fatal(err)
	}
	filmID, err := d.UpsertNodeChain(ctx, lib.ID, "Film/Film.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeFile(ctx, filmID, folderID, "", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertWatchNode(ctx, u.ID, filmID, 30, 100, false); err != nil {
		t.Fatal(err)
	}
	pilotID, err := d.UpsertNodeChain(ctx, lib.ID, "Show/Pilot.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeFile(ctx, pilotID, 0, "1", "1", "Pilot.mkv", "", "metadata/tv/Show/Pilot-thumb.jpg"); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertWatchNode(ctx, u.ID, pilotID, 40, 100, false); err != nil {
		t.Fatal(err)
	}

	rows, err := d.ContinueWatching(ctx, u.ID, 12)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, row := range rows {
		got[row.PosterPath] = row.Title
	}
	if got["metadata/movies/Film/poster.jpg"] != "The Film" {
		t.Fatalf("film card: %#v", got)
	}
	if got["metadata/tv/Show/Pilot-thumb.jpg"] != "Pilot" {
		t.Fatalf("pilot card: %#v", got)
	}
}
