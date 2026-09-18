package server

import (
	"context"
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

func TestRecentHXTrigger(t *testing.T) {
	if got := recentHXTrigger(true, true); got != "load, every 2s" {
		t.Fatalf("scan initial: %q", got)
	}
	if got := recentHXTrigger(false, true); got != "load, every 30s" {
		t.Fatalf("idle initial: %q", got)
	}
	if got := recentHXTrigger(true, false); got != "every 2s" {
		t.Fatalf("scan swap: %q", got)
	}
	if got := recentHXTrigger(false, false); got != "every 30s" {
		t.Fatalf("idle swap: %q", got)
	}
}

func TestHandleHXRecentAndItemsLive(t *testing.T) {
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
	if _, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Alpha Film", Path: "/media/Alpha.mkv", Mtime: 1,
	}); err != nil {
		t.Fatal(err)
	}

	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Cfg: &config.Config{}, DB: d, Templates: MustParseTemplates(tplFS)}
	s.Cfg.Store.Path = store
	rtr := chi.NewRouter()
	rtr.Get("/hx/home/recent", s.handleHXRecent)
	rtr.Get("/hx/libraries/{id}/items", s.handleHXItems)

	get := func(path string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(context.WithValue(req.Context(), userKey, u))
		w := httptest.NewRecorder()
		rtr.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status %d body %s", path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}

	idleRecent := get("/hx/home/recent")
	if !strings.Contains(idleRecent, "Alpha Film") {
		t.Fatalf("recent title: %s", idleRecent)
	}
	if !strings.Contains(idleRecent, `hx-trigger="every 30s"`) || strings.Contains(idleRecent, "every 2s") {
		t.Fatalf("idle recent trigger: %s", idleRecent)
	}

	idleItems := get(fmt.Sprintf("/hx/libraries/%d/items", lib.ID))
	if !strings.Contains(idleItems, "Alpha Film") {
		t.Fatalf("items title: %s", idleItems)
	}
	if strings.Contains(idleItems, `hx-trigger="every 2s"`) {
		t.Fatalf("idle items must not poll: %s", idleItems)
	}
	if !strings.Contains(idleItems, "1 title") || !strings.Contains(idleItems, `hx-swap-oob="true"`) {
		t.Fatalf("idle count oob: %s", idleItems)
	}

	if _, err := d.CreateScanJob(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Bravo Show", Path: "/media/Bravo", Mtime: 2,
	}); err != nil {
		t.Fatal(err)
	}

	liveRecent := get("/hx/home/recent")
	if !strings.Contains(liveRecent, "Bravo Show") {
		t.Fatalf("recent after insert: %s", liveRecent)
	}
	if !strings.Contains(liveRecent, `hx-trigger="every 2s"`) {
		t.Fatalf("scan recent trigger: %s", liveRecent)
	}

	liveItems := get(fmt.Sprintf("/hx/libraries/%d/items", lib.ID))
	if !strings.Contains(liveItems, "Bravo Show") {
		t.Fatalf("items after insert: %s", liveItems)
	}
	if !strings.Contains(liveItems, `hx-trigger="every 2s"`) {
		t.Fatalf("scan items poll: %s", liveItems)
	}
	if !strings.Contains(liveItems, "2 titles") {
		t.Fatalf("live count: %s", liveItems)
	}
}
