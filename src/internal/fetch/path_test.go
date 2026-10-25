package fetch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
)

func TestRelWithinKeepsTrailingSpace(t *testing.T) {
	ctx, d, lib, _, media := scanFixLib(t)
	show := filepath.Join(media, "Show ")
	ep := filepath.Join(show, "Season 1", "ep.mkv")
	if err := os.MkdirAll(filepath.Dir(ep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	showRel, err := relWithin(media, show)
	if err != nil {
		t.Fatal(err)
	}
	epRel, err := relWithin(media, ep)
	if err != nil {
		t.Fatal(err)
	}
	if showRel != "Show " {
		t.Fatalf("show rel %q", showRel)
	}
	if epRel != "Show /Season 1/ep.mkv" {
		t.Fatalf("episode rel %q", epRel)
	}

	j := matchmedia.Job{
		ID: "job-space", Status: "unmatched", Kind: "show", Title: "Show", Path: show,
		Files: []matchmedia.JobFile{{Path: ep, Season: "1", Episode: "1"}},
	}
	w := &Worker{DB: d}
	if err := w.applyJobs(ctx, lib, []matchmedia.Job{j}, nil, Opts{}, ""); err != nil {
		t.Fatal(err)
	}
	it, err := d.GetMediaItemByPath(ctx, lib.ID, show)
	if err != nil {
		t.Fatal(err)
	}
	if it == nil || it.Path != show {
		t.Fatalf("media path %#v", it)
	}
	nodeID, err := d.NodeIDByRel(ctx, lib.ID, showRel)
	if err != nil || nodeID == 0 {
		t.Fatalf("show node %d %v", nodeID, err)
	}
	epID, err := d.NodeIDByRel(ctx, lib.ID, epRel)
	if err != nil || epID == 0 {
		t.Fatalf("episode node %d %v", epID, err)
	}
	linked, _, _, _, err := d.ItemByNode(ctx, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if linked != it.ID {
		t.Fatalf("linked %d want %d", linked, it.ID)
	}
}

func TestFilelessShowFolderGetsMediaItem(t *testing.T) {
	ctx, d, lib, _, media := scanFixLib(t)
	show := filepath.Join(media, "Wrapped")
	if err := os.MkdirAll(show, 0o755); err != nil {
		t.Fatal(err)
	}
	j := matchmedia.Job{
		ID: "parent", Status: "unmatched", Kind: "show", Title: "Wrapped", Path: show, Role: "title",
	}
	w := &Worker{DB: d}
	if err := w.applyJobs(ctx, lib, []matchmedia.Job{j}, nil, Opts{}, ""); err != nil {
		t.Fatal(err)
	}
	it, err := d.GetMediaItemByPath(ctx, lib.ID, show)
	if err != nil {
		t.Fatal(err)
	}
	if it == nil {
		t.Fatal("file-less show has no media item")
	}
	nodeID, err := d.NodeIDByRel(ctx, lib.ID, "Wrapped")
	if err != nil || nodeID == 0 {
		t.Fatalf("folder node %d %v", nodeID, err)
	}
	linked, _, _, _, err := d.ItemByNode(ctx, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if linked != it.ID {
		t.Fatalf("linked %d want %d", linked, it.ID)
	}

	rootJob := matchmedia.Job{
		ID: "root", Status: "unmatched", Kind: "show", Title: "Lib", Path: media,
	}
	if err := w.applyJobs(ctx, lib, []matchmedia.Job{rootJob}, nil, Opts{}, ""); err != nil {
		t.Fatal(err)
	}
	rootItem, err := d.GetMediaItemByPath(ctx, lib.ID, media)
	if err != nil {
		t.Fatal(err)
	}
	if rootItem != nil {
		t.Fatalf("library root became a show: %#v", rootItem)
	}
}
