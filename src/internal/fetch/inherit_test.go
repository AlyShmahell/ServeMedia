package fetch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
)

func TestResolveManualFromChildMatch(t *testing.T) {
	ctx, _, media, d, lib, w := testWorker(t)
	root := filepath.Join(media, "KonoSuba")
	blessDir := filepath.Join(root, "Blessing")
	boomDir := filepath.Join(root, "Explosion")
	for _, dir := range []string{blessDir, boomDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	blessPoster := filepath.Join(blessDir, "poster.jpg")
	if err := os.WriteFile(blessPoster, []byte("bless"), 0o644); err != nil {
		t.Fatal(err)
	}
	parentID := mustShow(t, ctx, d, lib.ID, "KonoSuba", root)
	blessID := mustShow(t, ctx, d, lib.ID, "Blessing", blessDir)
	boomID := mustShow(t, ctx, d, lib.ID, "Explosion", boomDir)
	if err := d.SetMatchMediaMatch(ctx, blessID, "sess", "job-b", "matched", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateMediaItemMeta(ctx, blessID, "Blessing", 2016, "", blessPoster, "", "", 0, "tvmaze", "10772"); err != nil {
		t.Fatal(err)
	}
	parentSnap := EncodePickerSnapshot(matchmedia.Job{Candidates: []matchmedia.Candidate{
		{Provider: "tvmaze", ID: "67394", Title: "Explosion"},
		{Provider: "tvmaze", ID: "10772", Title: "Blessing"},
	}})
	if err := d.SetMatchMediaMatch(ctx, parentID, "sess", "job-p", "manual", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.SetMatchCandidates(ctx, parentID, parentSnap); err != nil {
		t.Fatal(err)
	}
	if err := d.SetMatchMediaMatch(ctx, boomID, "sess", "job-e", "manual", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.SetMatchCandidates(ctx, boomID, EncodePickerSnapshot(matchmedia.Job{Candidates: []matchmedia.Candidate{
		{Provider: "tvmaze", ID: "67394", Title: "Explosion"},
		{Provider: "tvmaze", ID: "10772", Title: "Blessing"},
	}})); err != nil {
		t.Fatal(err)
	}
	nodeID, err := d.UpsertNodeChain(ctx, lib.ID, "KonoSuba", "dir")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeGroup(ctx, nodeID, db.NodeGroup{Kind: "show", Title: "KonoSuba", Status: "manual", Candidates: parentSnap}); err != nil {
		t.Fatal(err)
	}

	if err := w.resolveManualFromChildren(ctx, lib); err != nil {
		t.Fatal(err)
	}
	parent, err := d.GetMediaItem(ctx, parentID)
	if err != nil || parent == nil {
		t.Fatal(err)
	}
	if parent.Title != "KonoSuba" {
		t.Fatalf("title %q", parent.Title)
	}
	if parent.MatchStatus.String != "matched" || parent.MetaProvider.String != "tvmaze" || parent.MetaID.String != "10772" {
		t.Fatalf("parent match %#v %#v %#v", parent.MatchStatus, parent.MetaProvider, parent.MetaID)
	}
	if parent.PosterPath.String != blessPoster {
		t.Fatalf("parent poster %q", parent.PosterPath.String)
	}
	boom, err := d.GetMediaItem(ctx, boomID)
	if err != nil || boom == nil || boom.MatchStatus.String != "manual" || boom.MetaID.Valid {
		t.Fatalf("explosion %#v %v", boom, err)
	}
	node, err := d.GetLibraryNode(ctx, nodeID)
	if err != nil || node == nil {
		t.Fatal(err)
	}
	if node.MMTitle.String != "KonoSuba" || node.MatchStatus.String != "matched" || node.MetaID.String != "10772" || node.PosterPath.String != blessPoster {
		t.Fatalf("node title %q status %q id %q poster %q", node.MMTitle.String, node.MatchStatus.String, node.MetaID.String, node.PosterPath.String)
	}
}

func TestResolveManualUsesLastIntersectingCandidate(t *testing.T) {
	ctx, _, media, d, lib, w := testWorker(t)
	root := filepath.Join(media, "Franchise")
	early := filepath.Join(root, "A Early")
	late := filepath.Join(root, "Z Late")
	other := filepath.Join(root, "M Other")
	for _, dir := range []string{early, late, other} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	earlyPoster := filepath.Join(early, "poster.jpg")
	latePoster := filepath.Join(late, "poster.jpg")
	if err := os.WriteFile(earlyPoster, []byte("early"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(latePoster, []byte("late"), 0o644); err != nil {
		t.Fatal(err)
	}
	parentID := mustShow(t, ctx, d, lib.ID, "Franchise", root)
	earlyID := mustShow(t, ctx, d, lib.ID, "Early", early)
	lateID := mustShow(t, ctx, d, lib.ID, "Late", late)
	otherID := mustShow(t, ctx, d, lib.ID, "Other", other)
	matchChild := func(id int64, title, poster, metaID string, year int) {
		t.Helper()
		if err := d.UpdateMediaItemMeta(ctx, id, title, year, "", poster, "", "", 0, "tvmaze", metaID); err != nil {
			t.Fatal(err)
		}
		if err := d.SetMatchMediaMatch(ctx, id, "", "", "matched", ""); err != nil {
			t.Fatal(err)
		}
	}
	matchChild(earlyID, "Early", earlyPoster, "2", 2001)
	matchChild(lateID, "Late", latePoster, "2", 2002)
	matchChild(otherID, "Other", filepath.Join(other, "missing.jpg"), "1", 2000)
	if err := d.SetMatchMediaMatch(ctx, parentID, "sess", "job", "manual", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.SetMatchCandidates(ctx, parentID, EncodePickerSnapshot(matchmedia.Job{Candidates: []matchmedia.Candidate{
		{Provider: "tvmaze", ID: "1", Title: "Other"},
		{Provider: "tvmaze", ID: "2", Title: "Show"},
	}})); err != nil {
		t.Fatal(err)
	}
	if err := w.resolveManualFromChildren(ctx, lib); err != nil {
		t.Fatal(err)
	}
	parent, err := d.GetMediaItem(ctx, parentID)
	if err != nil || parent == nil {
		t.Fatal(err)
	}
	if parent.MetaID.String != "2" || parent.PosterPath.String != latePoster {
		t.Fatalf("got id %q poster %q", parent.MetaID.String, parent.PosterPath.String)
	}
}

func mustShow(t *testing.T, ctx context.Context, d *db.DB, libraryID int64, title, path string) int64 {
	t.Helper()
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: libraryID, Kind: "show", Title: title, Path: path, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}
