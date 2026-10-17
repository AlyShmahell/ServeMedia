package server

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/config"
	"github.com/alyshmahell/servemedia/src/internal/db"
)

func TestCatalogBrowseRoutesGone(t *testing.T) {
	ctx := context.Background()
	store := t.TempDir()
	d, err := db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	s := &Server{Cfg: &config.Config{}, DB: d, Templates: template.Must(template.New("x").Parse(""))}
	rtr := s.Router()
	for _, path := range []string{"/shows/1", "/movies/1", "/shows/1/seasons/1"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		rtr.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s status %d body %s", path, w.Code, w.Body.String())
		}
	}
}

func TestNodeHrefAndShowFolderBack(t *testing.T) {
	ctx := context.Background()
	store := t.TempDir()
	d, err := db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", "/media")
	if err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Dark Matter", Path: "/media/Dark Matter", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	showNode, err := d.UpsertNodeChain(ctx, lib.ID, "Dark Matter", "dir")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetMediaItemNode(ctx, showID, showNode); err != nil {
		t.Fatal(err)
	}
	s1, err := d.UpsertSeason(ctx, showID, 1, "Season 1", "", "season plot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: s1, ShowID: showID, EpisodeNumber: 1, Path: "/media/Dark Matter/Season 1/E01.mkv", Mtime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	seasonNode, err := d.UpsertNodeChain(ctx, lib.ID, "Dark Matter/Season 1", "dir")
	if err != nil {
		t.Fatal(err)
	}
	fileNode, err := d.UpsertNodeChain(ctx, lib.ID, "Dark Matter/Season 1/E01.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeFile(ctx, fileNode, showNode, "1", "1", "Pilot", "", ""); err != nil {
		t.Fatal(err)
	}
	_ = seasonNode
	filmID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Film", Path: "/media/Film.mkv", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	filmNode, err := d.UpsertNodeChain(ctx, lib.ID, "Film.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetMediaItemNode(ctx, filmID, filmNode); err != nil {
		t.Fatal(err)
	}
	packID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Packed", Path: "/media/Pack/Packed.mkv", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	packNode, err := d.UpsertNodeChain(ctx, lib.ID, "Pack/Packed.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetMediaItemNode(ctx, packID, packNode); err != nil {
		t.Fatal(err)
	}

	s := &Server{Cfg: &config.Config{}, DB: d}
	if href := s.nodeHref(ctx, lib, db.LibraryNode{ID: showNode, NodeKind: "dir", RelPath: "Dark Matter", LibraryID: lib.ID}); href != fmt.Sprintf("/libraries/%d/tree?path=%s", lib.ID, urlQueryEscape("Dark Matter")) {
		t.Fatalf("show href %s", href)
	}
	if href := s.nodeHref(ctx, lib, db.LibraryNode{ID: seasonNode, NodeKind: "dir", RelPath: "Dark Matter/Season 1", LibraryID: lib.ID}); href != fmt.Sprintf("/libraries/%d/tree?path=%s", lib.ID, urlQueryEscape("Dark Matter/Season 1")) {
		t.Fatalf("season href %s", href)
	}
	if href := s.nodeHref(ctx, lib, db.LibraryNode{ID: filmNode, NodeKind: "file", RelPath: "Film.mkv", LibraryID: lib.ID}); href != fmt.Sprintf("/play/file/%d", filmNode) {
		t.Fatalf("movie href %s", href)
	}

	show, err := d.GetMediaItem(ctx, showID)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.showFolderBack(ctx, show); got != fmt.Sprintf("/libraries/%d/tree?path=%s", lib.ID, urlQueryEscape("Dark Matter")) {
		t.Fatalf("show folder %s", got)
	}
	film, err := d.GetMediaItem(ctx, filmID)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.showFolderBack(ctx, film); got != fmt.Sprintf("/libraries/%d", lib.ID) {
		t.Fatalf("film folder %s", got)
	}
	pack, err := d.GetMediaItem(ctx, packID)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.showFolderBack(ctx, pack); got != fmt.Sprintf("/libraries/%d/tree?path=Pack", lib.ID) {
		t.Fatalf("pack folder %s", got)
	}

	cards := s.mediaItemCards(ctx, u.ID, []db.MediaItem{*show, *film, *pack})
	if len(cards) != 3 || cards[0].Href != s.showFolderBack(ctx, show) || cards[1].Href != s.showFolderBack(ctx, film) || cards[2].Href != s.showFolderBack(ctx, pack) {
		t.Fatalf("cards %#v", cards)
	}
}

func TestEntryScanProgressData(t *testing.T) {
	job := &db.ScanJob{ID: 1, Status: "done", ProgressPct: 100}
	got := entryScanProgressData(job, false, 9, true, "manual")
	if got["NeedPick"] != true || got["Reload"] != false {
		t.Fatalf("manual pick %#v", got)
	}
	got = entryScanProgressData(job, false, 9, true, "unmatched")
	if got["NeedPick"] != true || got["Reload"] != false {
		t.Fatalf("unmatched pick %#v", got)
	}
	got = entryScanProgressData(job, false, 9, true, "matched")
	if got["NeedPick"] != false || got["Reload"] != true {
		t.Fatalf("matched reload %#v", got)
	}
	got = entryScanProgressData(job, false, 9, false, "manual")
	if got["NeedPick"] != false || got["Reload"] != true {
		t.Fatalf("local reload %#v", got)
	}
	running := entryScanProgressData(&db.ScanJob{ID: 1, Status: "running"}, true, 9, true, "")
	if running["NeedPick"] != false || running["Reload"] != false {
		t.Fatalf("running %#v", running)
	}
}
