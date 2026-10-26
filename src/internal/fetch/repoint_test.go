package fetch

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
)

func TestRepointLibraryMetadata(t *testing.T) {
	ctx := context.Background()
	store := t.TempDir()
	media := t.TempDir()
	d, err := db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", media)
	if err != nil {
		t.Fatal(err)
	}
	showDir := filepath.Join(media, "Show")
	seasonDir := filepath.Join(showDir, "Season 1")
	epPath := filepath.Join(seasonDir, "Show S01E01.mkv")
	touch(t, epPath)
	besideNFO := filepath.Join(seasonDir, "Show S01E01.nfo")
	if err := os.WriteFile(besideNFO, []byte("beside"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfoRel := "metadata/tv/Show/S01/Show S01E01.nfo"
	stillRel := "metadata/tv/Show/S01/Show S01E01-thumb.jpg"
	posterRel := "metadata/tv/Show/S01/poster.jpg"
	for _, rel := range []string{nfoRel, stillRel, posterRel} {
		if err := os.MkdirAll(filepath.Join(store, filepath.Dir(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(store, nfoRel), []byte("store-nfo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, stillRel), []byte("still"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, posterRel), []byte("season"), 0o644); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(store, "metadata", "tv", "Gone", "poster.jpg")
	if err := os.MkdirAll(filepath.Dir(orphan), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("orphan"), 0o644); err != nil {
		t.Fatal(err)
	}

	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Show", Path: showDir, Mtime: 1,
		NFOPath: sql.NullString{String: "metadata/tv/Show/tvshow.nfo", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(showDir, "tvshow.nfo"), []byte("show"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "metadata/tv/Show/tvshow.nfo"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	seasonID, err := d.UpsertSeason(ctx, showID, 1, "Season 1", posterRel, "")
	if err != nil {
		t.Fatal(err)
	}
	epID, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: seasonID, ShowID: showID, EpisodeNumber: 1,
		Title: sql.NullString{String: "Pilot", Valid: true}, Path: epPath, Mtime: 1,
		NFOPath:   sql.NullString{String: nfoRel, Valid: true},
		StillPath: sql.NullString{String: stillRel, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	rel := "Show/Season 1/Show S01E01.mkv"
	nodeID, err := d.UpsertNodeChain(ctx, lib.ID, rel, "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeStill(ctx, nodeID, stillRel); err != nil {
		t.Fatal(err)
	}
	seasonRel := "Show/Season 1"
	seasonNode, err := d.UpsertNodeChain(ctx, lib.ID, seasonRel, "dir")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodePoster(ctx, seasonNode, posterRel); err != nil {
		t.Fatal(err)
	}

	otherMedia := t.TempDir()
	other, err := d.CreateLibrary(ctx, u.ID, "Other", otherMedia)
	if err != nil {
		t.Fatal(err)
	}
	otherRel := "metadata/movies/Other/poster.jpg"
	if err := os.MkdirAll(filepath.Join(store, filepath.Dir(otherRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, otherRel), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: other.ID, Kind: "movie", Title: "Other",
		Path:      filepath.Join(otherMedia, "Other.mkv"), Mtime: 1,
		PosterPath: sql.NullString{String: otherRel, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	w := &Worker{DB: d, Store: store, MediaRoot: media}
	if err := w.RepointLibraryMetadata(ctx, lib); err != nil {
		t.Fatal(err)
	}

	show, err := d.GetMediaItem(ctx, showID)
	if err != nil || show == nil || show.NFOPath.String != filepath.Join(showDir, "tvshow.nfo") {
		t.Fatalf("show nfo %#v %v", show, err)
	}
	season, err := d.GetSeason(ctx, seasonID)
	if err != nil || season == nil || season.PosterPath.String != filepath.Join(seasonDir, "poster.jpg") {
		t.Fatalf("season poster %#v %v", season, err)
	}
	if b, err := os.ReadFile(season.PosterPath.String); err != nil || string(b) != "season" {
		t.Fatalf("copied poster %q %v", b, err)
	}
	ep, err := d.GetEpisode(ctx, epID)
	if err != nil || ep == nil {
		t.Fatal(err)
	}
	if ep.NFOPath.String != besideNFO {
		t.Fatalf("episode nfo %q", ep.NFOPath.String)
	}
	wantStill := filepath.Join(seasonDir, "Show S01E01-thumb.jpg")
	if ep.StillPath.String != wantStill {
		t.Fatalf("episode still %q", ep.StillPath.String)
	}
	if _, err := os.Stat(filepath.Join(store, stillRel)); !os.IsNotExist(err) {
		t.Fatalf("store still: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan: %v", err)
	}
	node, err := d.GetLibraryNode(ctx, nodeID)
	if err != nil || node == nil || node.StillPath.String != wantStill {
		t.Fatalf("node still %#v %v", node, err)
	}
	snode, err := d.GetLibraryNode(ctx, seasonNode)
	if err != nil || snode == nil || snode.PosterPath.String != filepath.Join(seasonDir, "poster.jpg") {
		t.Fatalf("season node %#v %v", snode, err)
	}
	if _, err := os.Stat(filepath.Join(store, otherRel)); err != nil {
		t.Fatalf("other library poster: %v", err)
	}
}
