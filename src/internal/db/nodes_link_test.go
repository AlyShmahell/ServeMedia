package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
)

func TestNodeEditLinks(t *testing.T) {
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
	showNode, err := d.UpsertNodeChain(ctx, lib.ID, "Show", "dir")
	if err != nil {
		t.Fatal(err)
	}
	seasonNode, err := d.UpsertNodeChain(ctx, lib.ID, "Show/Season 1", "dir")
	if err != nil {
		t.Fatal(err)
	}
	fileNode, err := d.UpsertNodeChain(ctx, lib.ID, "Show/Season 1/Pilot.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{LibraryID: lib.ID, Kind: "show", Title: "Show", Path: "/m/Show", Mtime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetMediaItemNode(ctx, showID, showNode); err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeFile(ctx, fileNode, showNode, "01", "1", "", "", ""); err != nil {
		t.Fatal(err)
	}
	gotShow, kind, _, _, err := d.ItemByNode(ctx, showNode)
	if err != nil || gotShow != showID || kind != "show" {
		t.Fatalf("item by node %d %s %v", gotShow, kind, err)
	}
	linkedShow, season, ok := d.SeasonEdit(ctx, seasonNode)
	if !ok || linkedShow != showID || season != 1 {
		t.Fatalf("season edit %d %d %v", linkedShow, season, ok)
	}
	title, plot := "Pilot", "A start"
	if err := d.SetNodeDisplay(ctx, fileNode, true, &title, &plot); err != nil {
		t.Fatal(err)
	}
	node, err := d.GetLibraryNode(ctx, fileNode)
	if err != nil || node == nil || node.FileTitle.String != "Pilot" || node.FilePlot.String != "A start" {
		t.Fatalf("file display %#v %v", node, err)
	}
	showTitle := "Renamed"
	if err := d.SetNodeDisplay(ctx, showNode, false, &showTitle, nil); err != nil {
		t.Fatal(err)
	}
	show, err := d.GetLibraryNode(ctx, showNode)
	if err != nil || show == nil || show.MMTitle.String != "Renamed" {
		t.Fatalf("show display %#v %v", show, err)
	}
}
