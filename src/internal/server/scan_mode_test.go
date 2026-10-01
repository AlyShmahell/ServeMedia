package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alyshmahell/servemedia/src/internal/config"
	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/fetch"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
	"github.com/go-chi/chi/v5"
)

func TestRunLibraryScanMatchMediaSkipsMixedWalk(t *testing.T) {
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
	decoy := filepath.Join(media, "Decoy.mkv")
	realDir := filepath.Join(media, "Real Film")
	real := filepath.Join(realDir, "Real Film.mkv")
	if err := os.WriteFile(decoy, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var scanHits int
	var scanMode string
	const sess = "20260829T122800Z-a1b2c3d4e5f6g7h8"
	requireSess := func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return false
		}
		return true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		scanHits++
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		scanMode = body["mode"]
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":1}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		if !requireSess(w, r) {
			return
		}
		_, _ = w.Write([]byte(`{"files":1,"done":1,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if !requireSess(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if scanHits == 0 {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": "job-real", "status": "unmatched", "path": realDir, "source": "scan",
			"files": []map[string]any{{"path": real}},
		}})
	})
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if !requireSess(w, r) {
			return
		}
		http.NotFound(w, r)
	})
	stub := httptest.NewServer(mux)
	defer stub.Close()

	cfg := &config.Config{}
	cfg.Store.Path = store
	meta := &matchmedia.Client{Base: stub.URL, HTTP: stub.Client()}
	s := &Server{
		Cfg:     cfg,
		DB:      d,
		Fetch:   &fetch.Worker{DB: d, Store: store, Meta: meta},
		Meta:    meta,
	}
	jobID, err := d.CreateScanJob(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.runLibraryScan(lib, jobID, "matchmedia", false, true)
	if scanMode != "rescan" {
		t.Fatalf("mode %q want rescan", scanMode)
	}
	if got, _ := d.GetMediaItemByPath(ctx, lib.ID, decoy); got != nil {
		t.Fatal("matchmedia mode must not ingest walker-only files")
	}
	got, err := d.GetMediaItemByPath(ctx, lib.ID, real)
	if err != nil || got == nil || got.Path != real {
		t.Fatalf("files[].path upsert %#v %v", got, err)
	}
}

func TestRunLibraryScanLocalSendsNFO(t *testing.T) {
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
	film := filepath.Join(media, "Local Film.mkv")
	if err := os.WriteFile(film, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var gotMode string
	const sess = "20260829T122800Z-localnfo000000"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		gotMode = body["mode"]
		if _, ok := body["require_episode_nfo"]; ok {
			t.Fatal("nfo must omit require_episode_nfo")
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":1}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":1,"done":1,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": "job-local", "status": "unmatched", "kind": "movie", "path": film, "source": "scan",
			"files": []map[string]any{{"path": film}},
		}})
	})
	stub := httptest.NewServer(mux)
	defer stub.Close()
	meta := &matchmedia.Client{Base: stub.URL, HTTP: stub.Client()}
	s := &Server{
		Cfg: &config.Config{}, DB: d,
		Fetch: &fetch.Worker{DB: d, Store: store, Meta: meta}, Meta: meta,
	}
	s.Cfg.Store.Path = store
	jobID, err := d.CreateScanJob(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.runLibraryScan(lib, jobID, "local", false, false)
	if gotMode != "nfo" {
		t.Fatalf("mode %q want nfo", gotMode)
	}
	got, err := d.GetMediaItemByPath(ctx, lib.ID, film)
	if err != nil || got == nil {
		t.Fatalf("nfo upsert %#v %v", got, err)
	}
}

func TestRunLibraryScanChangesSendsMode(t *testing.T) {
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
	film := filepath.Join(media, "New Film.mkv")
	if err := os.WriteFile(film, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var gotMode string
	const sess = "20260829T122800Z-bbbbbbbbbbbbbbbb"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		gotMode = body["mode"]
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":1,"mode":"changes"}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":1,"done":1,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": "job-new", "status": "unmatched", "path": film, "source": "scan",
			"files": []map[string]any{{"path": film}},
		}})
	})
	stub := httptest.NewServer(mux)
	defer stub.Close()
	meta := &matchmedia.Client{Base: stub.URL, HTTP: stub.Client()}
	s := &Server{
		Cfg: &config.Config{}, DB: d,
		Fetch: &fetch.Worker{DB: d, Store: store, Meta: meta}, Meta: meta,
	}
	s.Cfg.Store.Path = store
	jobID, err := d.CreateScanJob(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.runLibraryScan(lib, jobID, "matchmedia-changes", true, true)
	if gotMode != "changes" {
		t.Fatalf("mode %q want changes", gotMode)
	}
	got, err := d.GetMediaItemByPath(ctx, lib.ID, film)
	if err != nil || got == nil {
		t.Fatalf("changes upsert %#v %v", got, err)
	}
}

func TestHandleScanLibraryRejectsWhenMetaDown(t *testing.T) {
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
	cfg := &config.Config{}
	cfg.Store.Path = store
	s := &Server{Cfg: cfg, DB: d}
	rtr := chi.NewRouter()
	rtr.Post("/hx/libraries/{id}/scan", s.handleScanLibrary)
	form := url.Values{"mode": {"local"}}
	req := httptest.NewRequest(http.MethodPost, "/hx/libraries/"+strconv.FormatInt(lib.ID, 10)+"/scan", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateLibraryStartsChangesScan(t *testing.T) {
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
	var gotMode string
	const sess = "20260829T122800Z-addlibchanges00"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"healthy":true,"version":"0.1.0"}`))
	})
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		gotMode = body["mode"]
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":0}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":0,"done":0,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	stub := httptest.NewServer(mux)
	defer stub.Close()
	meta := &matchmedia.Client{Base: stub.URL, HTTP: stub.Client()}
	cfg := &config.Config{}
	cfg.Store.Path = store
	cfg.Media.Path = media
	s := &Server{
		Cfg: cfg, DB: d,
		Fetch: &fetch.Worker{DB: d, Store: store, Meta: meta}, Meta: meta,
	}
	form := url.Values{"name": {"Lib"}, "path": {media}, "mode": {"matchmedia-changes"}, "persist": {"1"}}
	req := httptest.NewRequest(http.MethodPost, "/hx/libraries", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleCreateLibrary(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	libs, err := d.ListLibraries(ctx, u.ID)
	if err != nil || len(libs) != 1 {
		t.Fatalf("libs %#v %v", libs, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if gotMode == "changes" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("mode %q want changes", gotMode)
}

func TestHandleCreateLibraryRescanMode(t *testing.T) {
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
	var gotMode string
	const sess = "20260829T122800Z-addlibrescan000"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"healthy":true,"version":"0.1.0"}`))
	})
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		gotMode = body["mode"]
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":0}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":0,"done":0,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	stub := httptest.NewServer(mux)
	defer stub.Close()
	meta := &matchmedia.Client{Base: stub.URL, HTTP: stub.Client()}
	cfg := &config.Config{}
	cfg.Store.Path = store
	cfg.Media.Path = media
	s := &Server{
		Cfg: cfg, DB: d,
		Fetch: &fetch.Worker{DB: d, Store: store, Meta: meta}, Meta: meta,
	}
	form := url.Values{"name": {"Lib"}, "path": {media}, "mode": {"matchmedia"}, "persist": {"1"}}
	req := httptest.NewRequest(http.MethodPost, "/hx/libraries", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleCreateLibrary(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status %d", w.Code)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if gotMode == "rescan" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("mode %q want rescan", gotMode)
}

func TestHandleCreateLibrarySkipsScanWhenMetaDown(t *testing.T) {
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
	cfg := &config.Config{}
	cfg.Store.Path = store
	cfg.Media.Path = media
	s := &Server{Cfg: cfg, DB: d}
	form := url.Values{"name": {"Lib"}, "path": {media}, "mode": {"matchmedia-changes"}}
	req := httptest.NewRequest(http.MethodPost, "/hx/libraries", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleCreateLibrary(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	libs, err := d.ListLibraries(ctx, u.ID)
	if err != nil || len(libs) != 1 {
		t.Fatalf("libs %#v %v", libs, err)
	}
	job, err := d.LatestScanForLibrary(ctx, libs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if job != nil {
		t.Fatalf("scan should be skipped, got %#v", job)
	}
}
