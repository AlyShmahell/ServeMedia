package server

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/config"
	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/fetch"
	"github.com/alyshmahell/servemedia/src/web"
	"github.com/go-chi/chi/v5"
)

func tinyPNG() []byte {
	return []byte("\x89PNG\r\n\x1a\n" + "0123456789ab")
}

func editTestServer(t *testing.T) (ctx context.Context, store, media string, d *db.DB, owner, other *db.User, lib *db.Library, s *Server) {
	t.Helper()
	ctx = context.Background()
	store = t.TempDir()
	media = t.TempDir()
	var err error
	d, err = db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	owner, err = d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	other, err = d.CreateUser(ctx, "bob", "x", db.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	lib, err = d.CreateLibrary(ctx, owner.ID, "Lib", media)
	if err != nil {
		t.Fatal(err)
	}
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Store.Path = store
	s = &Server{
		Cfg: cfg, DB: d, Templates: MustParseTemplates(tplFS),
		Fetch: &fetch.Worker{DB: d, Store: store},
	}
	return ctx, store, media, d, owner, other, lib, s
}

func TestHandleMediaMetaUnauthorized404(t *testing.T) {
	ctx, _, media, d, owner, other, lib, s := editTestServer(t)
	filmDir := filepath.Join(media, "Film")
	filmPath := filepath.Join(filmDir, "Film.mkv")
	if err := os.MkdirAll(filmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filmPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Film", Path: filmPath, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = owner
	rtr := chi.NewRouter()
	rtr.Post("/hx/media/{id}/meta", s.handleMediaMeta)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/hx/media/%d/meta", id), strings.NewReader("title=Nope"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), userKey, other))
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	rtr.Delete("/hx/media/{id}/poster", s.handleMediaPosterDelete)
	dreq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/hx/media/%d/poster", id), nil)
	dreq = dreq.WithContext(context.WithValue(dreq.Context(), userKey, other))
	dw := httptest.NewRecorder()
	rtr.ServeHTTP(dw, dreq)
	if dw.Code != http.StatusNotFound {
		t.Fatalf("delete %d %s", dw.Code, dw.Body.String())
	}
}

func TestHandleMediaMetaAndPoster(t *testing.T) {
	ctx, store, media, d, owner, _, lib, s := editTestServer(t)
	filmDir := filepath.Join(media, "Film")
	filmPath := filepath.Join(filmDir, "Film.mkv")
	if err := os.MkdirAll(filmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filmPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Film", Path: filmPath, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	rtr := chi.NewRouter()
	rtr.Post("/hx/media/{id}/meta", s.handleMediaMeta)
	rtr.Post("/hx/media/{id}/poster", s.handleMediaPoster)

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/hx/media/%d/meta", id), strings.NewReader("title=Edited&plot="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), userKey, owner))
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("meta %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"title":"Edited"`) {
		t.Fatalf("body %s", w.Body.String())
	}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Edited" || it.Plot.String != "" {
		t.Fatalf("db %#v plot %#v", it.Title, it.Plot)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "p.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(tinyPNG()); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	preq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/hx/media/%d/poster", id), &buf)
	preq.Header.Set("Content-Type", mw.FormDataContentType())
	preq = preq.WithContext(context.WithValue(preq.Context(), userKey, owner))
	pw := httptest.NewRecorder()
	rtr.ServeHTTP(pw, preq)
	if pw.Code != http.StatusOK {
		t.Fatalf("poster %d %s", pw.Code, pw.Body.String())
	}
	if !strings.Contains(pw.Body.String(), `/metadata/`) {
		t.Fatalf("src %s", pw.Body.String())
	}
	it, _ = d.GetMediaItem(ctx, id)
	if _, err := os.Stat(filepath.Join(store, it.PosterPath.String)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filmDir, "poster.png")); err != nil {
		t.Fatal(err)
	}

	rtr.Delete("/hx/media/{id}/poster", s.handleMediaPosterDelete)
	dreq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/hx/media/%d/poster", id), nil)
	dreq = dreq.WithContext(context.WithValue(dreq.Context(), userKey, owner))
	dw := httptest.NewRecorder()
	rtr.ServeHTTP(dw, dreq)
	if dw.Code != http.StatusOK {
		t.Fatalf("delete %d %s", dw.Code, dw.Body.String())
	}
	it, _ = d.GetMediaItem(ctx, id)
	if it.PosterPath.Valid && it.PosterPath.String != "" {
		t.Fatalf("poster still set %#v", it.PosterPath)
	}
	if _, err := os.Stat(filepath.Join(filmDir, "poster.png")); !os.IsNotExist(err) {
		t.Fatalf("beside poster remains: %v", err)
	}
}

func TestHandleSeasonEpisodeMetaUnauthorized404(t *testing.T) {
	ctx, _, media, d, _, other, lib, s := editTestServer(t)
	showDir := filepath.Join(media, "Show")
	epPath := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(epPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Show", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonID, err := d.UpsertSeason(ctx, showID, 1, "Season 1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	epID, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: seasonID, ShowID: showID, EpisodeNumber: 1, Path: epPath, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	rtr := chi.NewRouter()
	rtr.Post("/hx/shows/{id}/seasons/{n}/meta", s.handleSeasonMeta)
	rtr.Post("/hx/episodes/{id}/meta", s.handleEpisodeMeta)

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/hx/shows/%d/seasons/1/meta", showID), strings.NewReader("title=Nope"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), userKey, other))
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("season %d %s", w.Code, w.Body.String())
	}

	ereq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/hx/episodes/%d/meta", epID), strings.NewReader("title=Nope"))
	ereq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ereq = ereq.WithContext(context.WithValue(ereq.Context(), userKey, other))
	ew := httptest.NewRecorder()
	rtr.ServeHTTP(ew, ereq)
	if ew.Code != http.StatusNotFound {
		t.Fatalf("episode %d %s", ew.Code, ew.Body.String())
	}
}