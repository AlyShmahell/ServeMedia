package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/config"
	"github.com/alyshmahell/medora/internal/db"
	"github.com/alyshmahell/medora/internal/fetch"
	"github.com/alyshmahell/medora/internal/matchora"
	"github.com/alyshmahell/medora/internal/scanner"
)

func TestRunLibraryScanMatchoraSkipsMixedWalk(t *testing.T) {
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

	sc := &scanner.Scanner{DB: d, StorePath: store, MediaRoot: media}
	cfg := &config.Config{}
	cfg.Store.Path = store
	meta := &matchora.Client{Base: stub.URL, HTTP: stub.Client()}
	s := &Server{
		Cfg:     cfg,
		DB:      d,
		Scanner: sc,
		Fetch:   &fetch.Worker{DB: d, Store: store, Meta: meta},
		Meta:    meta,
	}
	jobID, err := d.CreateScanJob(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.runLibraryScan(lib, jobID, "matchora", false, true)
	if sc.MixedWalks() != 0 {
		t.Fatalf("mixed walks %d want 0", sc.MixedWalks())
	}
	if got, _ := d.GetMediaItemByPath(ctx, lib.ID, decoy); got != nil {
		t.Fatal("matchora mode must not ingest walker-only files")
	}
	got, err := d.GetMediaItemByPath(ctx, lib.ID, real)
	if err != nil || got == nil || got.Path != real {
		t.Fatalf("files[].path upsert %#v %v", got, err)
	}
}

func TestRunLibraryScanLocalStillWalks(t *testing.T) {
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
	sc := &scanner.Scanner{DB: d, StorePath: store, MediaRoot: media}
	cfg := &config.Config{}
	cfg.Store.Path = store
	s := &Server{Cfg: cfg, DB: d, Scanner: sc}
	jobID, err := d.CreateScanJob(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.runLibraryScan(lib, jobID, "local", false, false)
	if sc.MixedWalks() != 1 {
		t.Fatalf("mixed walks %d want 1", sc.MixedWalks())
	}
	got, err := d.GetMediaItemByPath(ctx, lib.ID, film)
	if err != nil || got == nil {
		t.Fatalf("local walk %#v %v", got, err)
	}
}
