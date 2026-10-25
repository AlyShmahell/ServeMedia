package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/config"
	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/fetch"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
	"github.com/alyshmahell/servemedia/src/web"
	"github.com/go-chi/chi/v5"
)

func TestPickerCandidatesSeedsMatch(t *testing.T) {
	got := pickerCandidates(matchmedia.Job{
		Match: &matchmedia.Candidate{Provider: "tmdb", ID: "11", Title: "Hit", Score: 0.99},
	})
	if len(got) != 1 || got[0].ID != "11" || got[0].Provider != "tmdb" {
		t.Fatalf("%#v", got)
	}

	got = pickerCandidates(matchmedia.Job{
		Match: &matchmedia.Candidate{Provider: "tmdb", ID: "11", Title: "Hit"},
		Candidates: []matchmedia.Candidate{
			{Provider: "tmdb", ID: "11", Title: "Hit", Score: 0.99},
			{Provider: "tvmaze", ID: "22", Title: "Other", Score: 0.4},
		},
	})
	if len(got) != 2 {
		t.Fatalf("dup match %#v", got)
	}

	got = pickerCandidates(matchmedia.Job{})
	if len(got) != 0 {
		t.Fatalf("empty %#v", got)
	}
}

func TestHandleMatchPostSkip(t *testing.T) {
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
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Film", Path: "/media/Film.mkv", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	nodeID, err := d.UpsertNodeChain(ctx, lib.ID, "Film.mkv", "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetMediaItemNode(ctx, id, nodeID); err != nil {
		t.Fatal(err)
	}
	if err := d.SetNodeMatchStatus(ctx, nodeID, "manual", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.SetMatchMediaMatch(ctx, id, "sess", "job", "manual", ""); err != nil {
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
	rtr.Post("/hx/media/{id}/match", s.handleMatchPost)
	form := url.Values{"skip": {"1"}}
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/hx/media/%d/match", id), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	got, err := d.GetMediaItem(ctx, id)
	if err != nil || got == nil || !got.MatchSkipped() {
		t.Fatalf("skip %#v %v", got, err)
	}
	if got.MatchMediaJobID.Valid {
		t.Fatalf("job still set %#v", got.MatchMediaJobID)
	}
	n, err := d.GetLibraryNode(ctx, nodeID)
	if err != nil || n == nil || n.MatchStatus.String != db.MatchStatusSkipped {
		t.Fatalf("node skip %#v %v", n, err)
	}
}

func TestHandleMatchGetRendersCandidates(t *testing.T) {
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
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "/media")
	if err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Film Title", Path: "/media/Anime/Film Title (2016)/Film Title (2016).mp4", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	const sess = "sess-film"
	if err := d.SetMatchMediaMatch(ctx, id, sess, "job-ingest", "manual", ""); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, "session", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": "job-ingest", "status": "manual",
			"candidates": []map[string]any{
				{"provider": "omdb", "id": "ttFilm2016", "title": "Film Title.", "year": "2016", "score": 0.9},
				{"provider": "omdb", "id": "ttFilmOther", "title": "Film Title Longer Variant", "year": "2020", "score": 0.4},
			},
		}})
	})
	stub := httptest.NewServer(mux)
	defer stub.Close()
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Store.Path = store
	s := &Server{
		Cfg: cfg, DB: d, Templates: MustParseTemplates(tplFS),
		Meta: &matchmedia.Client{Base: stub.URL, HTTP: stub.Client()},
	}
	rtr := chi.NewRouter()
	rtr.Get("/hx/media/{id}/match", s.handleMatchGet)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/hx/media/%d/match", id), nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	html := w.Body.String()
	if !strings.Contains(html, `class="cand"`) {
		t.Fatalf("missing cand buttons: %s", html)
	}
	if !strings.Contains(html, "2016") {
		t.Fatalf("missing 2016: %s", html)
	}
}

func TestHandleMatchGetUsesSnapshotWhenLiveJobsEmpty(t *testing.T) {
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
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "/media")
	if err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Frieren", Path: "/media/Frieren", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	const sess = "sess-snap"
	if err := d.SetMatchMediaMatch(ctx, id, sess, "job-show", "manual", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.SetMatchCandidates(ctx, id, `{"candidates":[{"provider":"tvmaze","id":"99","title":"Frieren","year":"2023","score":0.91}]}`); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": "job-show", "status": "manual",
		}})
	})
	stub := httptest.NewServer(mux)
	defer stub.Close()
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Store.Path = store
	s := &Server{
		Cfg: cfg, DB: d, Templates: MustParseTemplates(tplFS),
		Meta: &matchmedia.Client{Base: stub.URL, HTTP: stub.Client()},
	}
	rtr := chi.NewRouter()
	rtr.Get("/hx/media/{id}/match", s.handleMatchGet)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/hx/media/%d/match", id), nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	html := w.Body.String()
	if !strings.Contains(html, `class="cand"`) {
		t.Fatalf("missing cand buttons: %s", html)
	}
	if !strings.Contains(html, "Frieren") {
		t.Fatalf("missing snapshot title: %s", html)
	}
}

func TestHandleMatchGetSearchesWhenSnapshotEmpty(t *testing.T) {
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
	lib, err := d.CreateLibrary(ctx, u.ID, "Anime", "/media")
	if err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Erased", Path: "/media/Erased", Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"healthy":true}`))
	})
	mux.HandleFunc("/v1/ingest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"session":"sess-search","files":1,"jobs":1}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"running":false,"files":1,"done":1}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": "job-search", "status": "manual",
			"candidates": []map[string]any{{
				"provider": "tvmaze", "id": "42", "title": "Erased", "year": "2016", "score": 0.9,
			}},
		}})
	})
	stub := httptest.NewServer(mux)
	defer stub.Close()
	client := &matchmedia.Client{Base: stub.URL, HTTP: stub.Client()}
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Store.Path = store
	s := &Server{
		Cfg: cfg, DB: d, Templates: MustParseTemplates(tplFS),
		Meta:  client,
		Fetch: &fetch.Worker{DB: d, Store: store, Meta: client},
	}
	rtr := chi.NewRouter()
	rtr.Get("/hx/media/{id}/match", s.handleMatchGet)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/hx/media/%d/match", id), nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	html := w.Body.String()
	if !strings.Contains(html, `class="cand"`) || !strings.Contains(html, "Erased") {
		t.Fatalf("missing searched candidate: %s", html)
	}
}

func TestFolderActionsWithoutNodeLink(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	showDir := filepath.Join(media, "Show")
	seasonDir := filepath.Join(showDir, "Season 01")
	movieDir := filepath.Join(media, "Film")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	movieFile := filepath.Join(movieDir, "Film.mkv")
	if err := os.WriteFile(movieFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	showPoster := filepath.Join(showDir, "poster.jpg")
	seasonPoster := filepath.Join(seasonDir, "poster.jpg")
	if err := os.WriteFile(showPoster, []byte("show"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seasonPoster, []byte("season"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", media)
	if err != nil {
		t.Fatal(err)
	}
	showNodeID, err := d.UpsertNodeChain(ctx, lib.ID, "Show", "dir")
	if err != nil {
		t.Fatal(err)
	}
	seasonNodeID, err := d.UpsertNodeChain(ctx, lib.ID, "Show/Season 01", "dir")
	if err != nil {
		t.Fatal(err)
	}
	movieNodeID, err := d.UpsertNodeChain(ctx, lib.ID, "Film", "dir")
	if err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Show", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetMatchMediaMatch(ctx, showID, "", "", "manual", ""); err != nil {
		t.Fatal(err)
	}
	if err := d.SetMediaItemPosterNFO(ctx, showID, &showPoster, nil); err != nil {
		t.Fatal(err)
	}
	movieID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Film", Path: movieFile, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetMatchMediaMatch(ctx, movieID, "", "", "manual", ""); err != nil {
		t.Fatal(err)
	}
	s := &Server{DB: d}
	showNode, err := d.GetLibraryNode(ctx, showNodeID)
	if err != nil || showNode == nil {
		t.Fatal(err)
	}
	_, id, status, _ := s.nodeActions(ctx, lib, *showNode)
	if id != showID || status != "manual" {
		t.Fatalf("show folder id=%d status=%q", id, status)
	}
	seasonNode, err := d.GetLibraryNode(ctx, seasonNodeID)
	if err != nil || seasonNode == nil {
		t.Fatal(err)
	}
	_, sid, sstatus, _ := s.nodeActions(ctx, lib, *seasonNode)
	if sid != showID || sstatus == "manual" || sstatus == "unmatched" {
		t.Fatalf("season folder id=%d status=%q", sid, sstatus)
	}
	movieNode, err := d.GetLibraryNode(ctx, movieNodeID)
	if err != nil || movieNode == nil {
		t.Fatal(err)
	}
	_, mid, mstatus, _ := s.nodeActions(ctx, lib, *movieNode)
	if mid != movieID || mstatus != "manual" {
		t.Fatalf("movie folder id=%d status=%q", mid, mstatus)
	}
	card := s.dirNodeCard(ctx, lib, *seasonNode)
	if card.Node.PosterPath.String != seasonPoster {
		t.Fatalf("season art %q", card.Node.PosterPath.String)
	}
	if err := os.Remove(seasonPoster); err != nil {
		t.Fatal(err)
	}
	bare := s.dirNodeCard(ctx, lib, *seasonNode)
	if bare.Node.PosterPath.Valid && strings.TrimSpace(bare.Node.PosterPath.String) != "" {
		t.Fatalf("season reused show poster %q", bare.Node.PosterPath.String)
	}
}
