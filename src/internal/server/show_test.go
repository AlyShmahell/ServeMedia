package server

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/internal/config"
	"github.com/alyshmahell/servemedia/internal/db"
	"github.com/alyshmahell/servemedia/web"
	"github.com/go-chi/chi/v5"
)

func TestHandleShowOmitsEmptySeasons(t *testing.T) {
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
		LibraryID: lib.ID, Kind: "show", Title: "Parent Show", Path: "/media/Parent", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertSeason(ctx, showID, 0, "Ghost Season", "", ""); err != nil {
		t.Fatal(err)
	}
	s1, err := d.UpsertSeason(ctx, showID, 1, "Season 1", "", "season plot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: s1, ShowID: showID, EpisodeNumber: 1, Path: "/media/Parent/S1E1.mkv", Mtime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	childID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Nested Film", Path: "/media/Parent/Nested.mkv",
		Plot: sql.NullString{String: "secret plot", Valid: true}, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetMediaItemParent(ctx, childID, showID); err != nil {
		t.Fatal(err)
	}

	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Store.Path = store
	s := &Server{Cfg: cfg, DB: d, Templates: MustParseTemplates(tplFS)}

	rtr := chi.NewRouter()
	rtr.Get("/shows/{id}", s.handleShow)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/shows/%d", showID), nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, ">Titles<") {
		t.Fatal("expected Titles heading")
	}
	if strings.Contains(body, "Related") {
		t.Fatal("related section should be gone")
	}
	if strings.Contains(body, "Ghost Season") {
		t.Fatal("empty season should be omitted")
	}
	if !strings.Contains(body, "Season 1") {
		t.Fatal("season with episodes")
	}
	if !strings.Contains(body, "Nested Film") {
		t.Fatal("child in titles grid")
	}
	if strings.Contains(body, "secret plot") {
		t.Fatal("nested plot should be omitted")
	}
	if strings.Contains(body, "No synopsis") {
		t.Fatal("no synopsis placeholder")
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
