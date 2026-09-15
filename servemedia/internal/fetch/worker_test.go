package fetch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alyshmahell/servemedia/internal/db"
	"github.com/alyshmahell/servemedia/internal/matchmedia"
)

func TestMatchPathScanOnceAppliesCatalog(t *testing.T) {
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

	filmDir := filepath.Join(media, "Film Title")
	filmPath := filepath.Join(filmDir, "Film Title.mkv")
	if err := os.MkdirAll(filmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filmPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	showDir := filepath.Join(media, "Sample Show")
	epPath := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(epPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	manualDir := filepath.Join(media, "Manual Film")
	manualPath := filepath.Join(manualDir, "Manual Film.mkv")
	if err := os.MkdirAll(manualDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manualPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var scanHits, ingestHits, catalogHits atomic.Int32
	var lastScanPath, lastSelectSession string
	const sess = "20260829T122800Z-a1b2c3d4e5f6g7h8"
	requireSess := func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return false
		}
		return true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	mux.HandleFunc("/v1/ingest", func(w http.ResponseWriter, r *http.Request) {
		ingestHits.Add(1)
		http.Error(w, "ingest removed", 404)
	})
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		scanHits.Add(1)
		var body struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		lastScanPath = body.Path
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":3}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		if !requireSess(w, r) {
			return
		}
		_, _ = w.Write([]byte(`{"files":3,"done":3,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/select") {
			http.NotFound(w, r)
			return
		}
		if !requireSess(w, r) {
			return
		}
		lastSelectSession = r.URL.Query().Get("session")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "job-manual", "status": "matched", "path": manualDir, "source": "scan",
			"files": []map[string]any{{"path": manualPath}},
			"match": map[string]any{"provider": "tmdb", "id": "88", "title": "Picked Title", "year": "2016"},
		})
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if !requireSess(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if scanHits.Load() == 0 {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		jobs := []map[string]any{
			{
				"id": "job-film", "status": "matched", "path": filmDir, "source": "scan",
				"files": []map[string]any{{"path": filmPath}},
				"match": map[string]any{"provider": "tmdb", "id": "11", "title": "Film Title", "year": "2016"},
			},
			{
				"id": "job-show", "status": "matched", "path": showDir, "source": "scan",
				"files": []map[string]any{{"path": epPath, "season": "1", "episode": "1"}},
				"match": map[string]any{"provider": "tmdb", "id": "22", "title": "Sample Show", "year": "2020"},
			},
			{
				"id": "job-manual", "status": "manual", "path": manualDir, "source": "scan",
				"files": []map[string]any{{"path": manualPath}},
				"candidates": []map[string]any{
					{"provider": "tmdb", "id": "88", "title": "Picked Title", "year": "2016", "score": 0.9},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(jobs)
	})
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if !requireSess(w, r) {
			return
		}
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "fakejpeg")
			return
		}
		catalogHits.Add(1)
		id := strings.TrimPrefix(r.URL.Path, "/v1/catalog/tmdb/")
		title := "Film Title"
		year := "2016"
		var seasons []any
		if id == "22" {
			title = "Sample Show"
			year = "2020"
			seasons = []any{map[string]any{
				"number": "1", "title": "Season 1",
				"episodes": []any{map[string]any{
					"number": "1", "title": "Pilot", "poster": "/v1/catalog/tmdb/22/still.jpg",
				}},
			}}
		}
		if id == "88" {
			title = "Picked Title"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider": "tmdb", "id": id, "title": title, "year": year, "type": "movie",
			"synopsis": "plot", "poster": "/v1/catalog/tmdb/" + id + "/poster.jpg",
			"seasons": seasons,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL}}
	if err := w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	if scanHits.Load() != 1 {
		t.Fatalf("scan hits %d want 1", scanHits.Load())
	}
	if ingestHits.Load() != 0 {
		t.Fatalf("ingest should not be called")
	}
	if lastScanPath != media {
		t.Fatalf("scan path %q want library root %q", lastScanPath, media)
	}
	if catalogHits.Load() < 2 {
		t.Fatalf("catalog hits %d", catalogHits.Load())
	}

	film, err := d.GetMediaItemByPath(ctx, lib.ID, filmPath)
	if err != nil || film == nil || film.Title != "Film Title" || !film.MetaID.Valid || film.MetaID.String != "11" {
		t.Fatalf("film %#v %v", film, err)
	}
	if film.Kind != "movie" || film.Path != filmPath {
		t.Fatalf("film kind/path %#v", film)
	}
	if film.MatchStatus.String != "matched" {
		t.Fatalf("film status %q", film.MatchStatus.String)
	}
	if !film.PosterPath.Valid || film.PosterPath.String == "" {
		t.Fatal("film poster missing")
	}

	show, err := d.GetMediaItemByPath(ctx, lib.ID, showDir)
	if err != nil || show == nil || show.Title != "Sample Show" || show.MetaID.String != "22" {
		t.Fatalf("show %#v %v", show, err)
	}
	if show.Kind != "show" {
		t.Fatalf("show kind %q", show.Kind)
	}
	eps, err := d.ListEpisodesByShow(ctx, show.ID)
	if err != nil || len(eps) != 1 {
		t.Fatalf("eps %#v %v", eps, err)
	}
	if eps[0].Path != epPath {
		t.Fatalf("episode path %q want %q", eps[0].Path, epPath)
	}
	if !eps[0].Title.Valid || eps[0].Title.String != "Pilot" {
		t.Fatalf("episode title %#v", eps[0].Title)
	}
	if !eps[0].StillPath.Valid || eps[0].StillPath.String == "" {
		t.Fatal("catalog still missing; ffmpeg should not be required")
	}

	manual, err := d.GetMediaItemByPath(ctx, lib.ID, manualPath)
	if err != nil || manual == nil {
		t.Fatal(err)
	}
	if manual.MatchStatus.String != "manual" || manual.MatchMediaJobID.String != "job-manual" {
		t.Fatalf("manual %#v", manual)
	}
	if manual.MatchMediaSessionID.String != sess {
		t.Fatalf("manual session %q want %q", manual.MatchMediaSessionID.String, sess)
	}
	if film.MatchMediaSessionID.String != sess {
		t.Fatalf("film session %q", film.MatchMediaSessionID.String)
	}
	if manual.MetaID.Valid {
		t.Fatal("manual should not invent a match")
	}

	if err := w.ApplySelect(ctx, manual, "tmdb", "88", false); err != nil {
		t.Fatal(err)
	}
	if lastSelectSession != sess {
		t.Fatalf("select session %q want %q", lastSelectSession, sess)
	}
	picked, err := d.GetMediaItem(ctx, manual.ID)
	if err != nil || picked.Title != "Picked Title" || picked.MetaID.String != "88" || picked.MatchStatus.String != "matched" {
		t.Fatalf("picked %#v %v", picked, err)
	}
}

func TestMatchPathAppliesJobWhileGrouping(t *testing.T) {
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

	filmDir := filepath.Join(media, "Film Title")
	filmPath := filepath.Join(filmDir, "Film Title.mkv")
	if err := os.MkdirAll(filmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filmPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lateDir := filepath.Join(media, "Late Film")
	latePath := filepath.Join(lateDir, "Late Film.mkv")
	if err := os.MkdirAll(lateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(latePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	const sess = "20260829T122800Z-a1b2c3d4e5f6g7h8"
	var statusHits atomic.Int32
	var sawFilmWhileGrouping atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":2}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		n := statusHits.Add(1)
		running := n < 10
		done := 1
		if !running {
			done = 2
		}
		_, _ = fmt.Fprintf(w, `{"files":2,"done":%d,"chunks":2,"chunk":%d,"running":%t}`, done, done, running)
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		n := statusHits.Load()
		film := map[string]any{
			"id": "job-film", "status": "matched", "path": filmDir, "source": "scan",
			"files": []map[string]any{{"path": filmPath}},
			"match": map[string]any{"provider": "tmdb", "id": "11", "title": "Film Title", "year": "2016"},
		}
		late := map[string]any{
			"id": "job-late", "status": "matched", "path": lateDir, "source": "scan",
			"files": []map[string]any{{"path": latePath}},
			"match": map[string]any{"provider": "tmdb", "id": "99", "title": "Late Film", "year": "2017"},
		}
		var jobs []map[string]any
		switch {
		case n < 2:
			jobs = nil
		case n < 10:
			jobs = []map[string]any{film}
		default:
			jobs = []map[string]any{film, late}
		}
		if jobs == nil {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_ = json.NewEncoder(w).Encode(jobs)
	})
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "fakejpeg")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/catalog/tmdb/")
		title, year := "Film Title", "2016"
		if id == "99" {
			title, year = "Late Film", "2017"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider": "tmdb", "id": id, "title": title, "year": year, "type": "movie",
			"synopsis": "plot", "poster": "/v1/catalog/tmdb/" + id + "/poster.jpg",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	scanJobID, err := d.CreateScanJob(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	errCh := make(chan error, 1)
	go func() {
		errCh <- w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: true, ScanJobID: scanJobID})
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		film, _ := d.GetMediaItemByPath(ctx, lib.ID, filmPath)
		late, _ := d.GetMediaItemByPath(ctx, lib.ID, latePath)
		if film != nil && film.Title == "Film Title" && film.PosterPath.Valid {
			job, _ := d.GetScanJob(ctx, scanJobID)
			if job != nil && job.ProgressPct < 40 && strings.Contains(job.Message.String, "titles") && late == nil {
				sawFilmWhileGrouping.Store(true)
				break
			}
		}
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatal(err)
			}
			t.Fatal("scan finished before film was observed mid-grouping")
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sawFilmWhileGrouping.Load() {
		t.Fatal("film should be applied while grouping still running")
	}
	job, err := d.GetScanJob(ctx, scanJobID)
	if err != nil || job == nil {
		t.Fatalf("scan job %v %v", job, err)
	}
	if job.ProgressPct >= 40 {
		t.Fatalf("grouping should stay below 40%%, got %d %q", job.ProgressPct, job.Message.String)
	}

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	late, err := d.GetMediaItemByPath(ctx, lib.ID, latePath)
	if err != nil || late == nil || late.Title != "Late Film" {
		t.Fatalf("late %#v %v", late, err)
	}
}

func TestKindFromJobUsesFiles(t *testing.T) {
	showJob := matchmedia.Job{
		Path: "/media/Show",
		Files: []matchmedia.JobFile{
			{Path: "/media/Show/Season 1/Show S01E01.mkv", Season: "1", Episode: "1"},
		},
	}
	if kind := kindFromJob(showJob, showJob.Files); kind != "show" {
		t.Fatalf("show kind %q", kind)
	}
	movieJob := matchmedia.Job{
		Path:  "/media/Film",
		Files: []matchmedia.JobFile{{Path: "/media/Film/Film.mkv"}},
	}
	if kind := kindFromJob(movieJob, movieJob.Files); kind != "movie" {
		t.Fatalf("movie kind %q", kind)
	}
}

func TestMatchPathPathOnlyJobs(t *testing.T) {
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

	showDir := filepath.Join(media, "Sample Show")
	epPath := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(epPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	filmDir := filepath.Join(media, "Film Title")
	filmPath := filepath.Join(filmDir, "Film Title.mkv")
	if err := os.MkdirAll(filmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filmPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	const sess = "20260829T122800Z-a1b2c3d4e5f6g7h8"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":2}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"files":2,"done":2,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id": "job-show", "status": "matched", "path": showDir, "source": "scan",
				"match": map[string]any{"provider": "tvmaze", "id": "22", "title": "Sample Show", "year": "2020"},
			},
			{
				"id": "job-film", "status": "matched", "path": filmDir, "source": "scan",
				"match": map[string]any{"provider": "tvmaze", "id": "11", "title": "Film Title", "year": "2016"},
			},
		})
	})
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "fakejpeg")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/catalog/tvmaze/")
		title, year := "Film Title", "2016"
		var seasons []any
		if id == "22" {
			title, year = "Sample Show", "2020"
			seasons = []any{map[string]any{
				"number": "1", "title": "Season 1",
				"episodes": []any{map[string]any{
					"number": "1", "title": "Pilot", "poster": "/v1/catalog/tvmaze/22/still.jpg",
				}},
			}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider": "tvmaze", "id": id, "title": title, "year": year,
			"synopsis": "plot", "poster": "/v1/catalog/tvmaze/" + id + "/poster.jpg",
			"seasons": seasons,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	if err := w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: true}); err != nil {
		t.Fatal(err)
	}

	show, err := d.GetMediaItemByPath(ctx, lib.ID, showDir)
	if err != nil || show == nil || show.Kind != "show" {
		t.Fatalf("show %#v %v", show, err)
	}
	if show.Path != showDir {
		t.Fatalf("show path %q want %q", show.Path, showDir)
	}
	eps, err := d.ListEpisodesByShow(ctx, show.ID)
	if err != nil || len(eps) != 1 {
		t.Fatalf("eps %#v %v", eps, err)
	}
	if eps[0].Path != epPath {
		t.Fatalf("episode path %q want %q", eps[0].Path, epPath)
	}
	seasons, err := d.ListSeasons(ctx, show.ID)
	if err != nil || len(seasons) != 1 || seasons[0].SeasonNumber != 1 {
		t.Fatalf("seasons %#v %v", seasons, err)
	}
	if eps[0].EpisodeNumber != 1 {
		t.Fatalf("episode number %d", eps[0].EpisodeNumber)
	}

	film, err := d.GetMediaItemByPath(ctx, lib.ID, filmPath)
	if err != nil || film == nil || film.Kind != "movie" || film.Path != filmPath {
		t.Fatalf("film %#v %v", film, err)
	}
	if got, _ := d.GetMediaItemByPath(ctx, lib.ID, filmDir); got != nil && got.Kind == "show" {
		t.Fatalf("film dir must not be a show: %#v", got)
	}
}

func TestMatchItemIngestAppliesToExisting(t *testing.T) {
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
	filmDir := filepath.Join(media, "Wrong Folder")
	filmPath := filepath.Join(filmDir, "Wrong Folder.mkv")
	if err := os.MkdirAll(filmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filmPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Wrong Folder", SortTitle: "Wrong Folder", Path: filmPath, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err := d.GetMediaItem(ctx, id)
	if err != nil || item == nil {
		t.Fatal(err)
	}

	var ingestHits, scanHits atomic.Int32
	var gotRows []map[string]any
	const sess = "20260831T120000Z-aaaaaaaaaaaaaaaa"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		scanHits.Add(1)
		http.Error(w, "scan should not run", 500)
	})
	mux.HandleFunc("/v1/ingest", func(w http.ResponseWriter, r *http.Request) {
		ingestHits.Add(1)
		_ = json.NewDecoder(r.Body).Decode(&gotRows)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `"}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"no grouping"}`, http.StatusNotFound)
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id": "job-ingest", "status": "matched", "source": "ingest", "title": "Girls",
				"match": map[string]any{"provider": "tmdb", "id": "55", "title": "Girls", "year": "2012"},
			},
		})
	})
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "fakejpeg")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider": "tmdb", "id": "55", "title": "Girls", "year": "2012", "type": "movie",
			"synopsis": "plot", "poster": "/v1/catalog/tmdb/55/poster.jpg",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL}}
	if err := w.MatchItem(ctx, lib, item, Opts{Persist: false, Overwrite: true, QueryTitle: "Girls (2012)"}); err != nil {
		t.Fatal(err)
	}
	if scanHits.Load() != 0 {
		t.Fatalf("scan hits %d", scanHits.Load())
	}
	if ingestHits.Load() != 1 {
		t.Fatalf("ingest hits %d", ingestHits.Load())
	}
	if len(gotRows) != 1 || gotRows[0]["title"] != "Girls (2012)" || fmt.Sprint(gotRows[0]["year"]) != "2012" {
		t.Fatalf("ingest rows %#v", gotRows)
	}
	got, err := d.GetMediaItem(ctx, id)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Title != "Wrong Folder" {
		t.Fatalf("title should stay until pick %#v", got)
	}
	if got.MatchStatus.String != "manual" || got.MatchMediaJobID.String != "job-ingest" {
		t.Fatalf("want manual pick %#v", got)
	}
	if got.MetaID.Valid && got.MetaID.String != "" {
		t.Fatalf("catalog must not apply before pick %#v", got.MetaID)
	}
}

func TestMergeSameCatalogJobsAndAttachMovie(t *testing.T) {
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

	showDir := filepath.Join(media, "Sample Show")
	epPath := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	specialsDir := filepath.Join(showDir, "Specials")
	ovaPath := filepath.Join(specialsDir, "The Hidden OVA.mkv")
	bonusPath := filepath.Join(specialsDir, "My Bonus Feature.mkv")
	for _, p := range []string{epPath, ovaPath, bonusPath} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	movieDir := filepath.Join(media, "Sample Show Movie")
	moviePath := filepath.Join(movieDir, "Sample Show Movie.mkv")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(moviePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	const sess = "20260914T120000Z-bbbbbbbbbbbbbbbb"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":3}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"files":3,"done":3,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id": "job-show", "status": "matched", "path": showDir, "source": "scan",
				"match": map[string]any{"provider": "tvmaze", "id": "22", "title": "Sample Show", "year": "2020"},
			},
			{
				"id": "job-specials", "status": "matched", "path": specialsDir, "source": "scan",
				"match": map[string]any{"provider": "tvmaze", "id": "22", "title": "Sample Show", "year": "2020"},
			},
			{
				"id": "job-movie", "status": "matched", "path": movieDir, "source": "scan",
				"match": map[string]any{"provider": "tmdb", "id": "99", "title": "Sample Show Movie", "year": "2021"},
			},
		})
	})
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "fakejpeg")
			return
		}
		id := filepath.Base(r.URL.Path)
		title, year := "Sample Show", "2020"
		var seasons []any
		if strings.Contains(r.URL.Path, "/tvmaze/") {
			seasons = []any{
				map[string]any{
					"number": "1", "title": "Sample Show",
					"episodes": []any{map[string]any{"number": "1", "title": "Pilot"}},
				},
				map[string]any{
					"number": "2", "title": "Sample Show",
					"episodes": []any{map[string]any{"number": "1", "title": "Cour Two"}},
				},
				map[string]any{
					"number": "0", "title": "Specials",
					"episodes": []any{map[string]any{"number": "1", "title": "The Hidden OVA"}},
				},
			}
		}
		if strings.Contains(r.URL.Path, "/tmdb/") {
			title, year = "Sample Show Movie", "2021"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider": filepath.Base(filepath.Dir(r.URL.Path)), "id": id, "title": title, "year": year,
			"synopsis": "plot", "poster": r.URL.Path + "/poster.jpg",
			"seasons": seasons,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	if err := w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: true}); err != nil {
		t.Fatal(err)
	}

	items, err := d.ListMediaItems(ctx, lib.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != "show" {
		t.Fatalf("library cards %#v", items)
	}
	show := items[0]
	if show.Path != showDir || show.MetaID.String != "22" {
		t.Fatalf("show %#v", show)
	}
	if got, _ := d.GetMediaItemByPath(ctx, lib.ID, specialsDir); got != nil {
		t.Fatalf("specials must not be a card: %#v", got)
	}
	film, err := d.GetMediaItemByPath(ctx, lib.ID, moviePath)
	if err != nil || film == nil || film.Kind != "movie" {
		t.Fatalf("movie card %#v %v", film, err)
	}
	if !film.ParentID.Valid || film.ParentID.Int64 != show.ID {
		t.Fatalf("movie parent %#v", film.ParentID)
	}
	if film.Title != "Sample Show Movie" {
		t.Fatalf("movie title %q", film.Title)
	}
	children, err := d.ListChildMediaItems(ctx, show.ID)
	if err != nil || len(children) != 1 || children[0].ID != film.ID {
		t.Fatalf("children %#v %v", children, err)
	}
	found, err := d.ListMediaItems(ctx, lib.ID, "", "Movie")
	if err != nil || len(found) != 1 || found[0].ID != film.ID {
		t.Fatalf("search should find nested movie %#v %v", found, err)
	}

	eps, err := d.ListEpisodesByShow(ctx, show.ID)
	if err != nil || len(eps) != 3 {
		t.Fatalf("eps %#v %v", eps, err)
	}
	byPath := map[string]db.Episode{}
	for _, ep := range eps {
		byPath[ep.Path] = ep
	}
	if byPath[epPath].Title.String != "Pilot" {
		t.Fatalf("s01 title %#v", byPath[epPath].Title)
	}
	if byPath[ovaPath].SeasonID == 0 || byPath[ovaPath].Title.String != "The Hidden OVA" {
		t.Fatalf("ova %#v", byPath[ovaPath])
	}
	if byPath[bonusPath].Title.String != "My Bonus Feature" {
		t.Fatalf("bonus title %#v", byPath[bonusPath].Title)
	}
	if _, ok := byPath[moviePath]; ok {
		t.Fatal("movie must not be a parent season-0 episode")
	}
	seasons, err := d.ListSeasons(ctx, show.ID)
	if err != nil {
		t.Fatal(err)
	}
	seasonByID := map[int64]int{}
	for _, s := range seasons {
		seasonByID[s.ID] = s.SeasonNumber
		if s.SeasonNumber == 1 && s.Title.Valid && s.Title.String != "Season 1" {
			t.Fatalf("s1 title %q should stay Season 1", s.Title.String)
		}
		if s.SeasonNumber == 0 && (!s.Title.Valid || s.Title.String != "Specials") {
			t.Fatalf("s0 title %#v", s.Title)
		}
	}
	if seasonByID[byPath[ovaPath].SeasonID] != 0 || seasonByID[byPath[bonusPath].SeasonID] != 0 {
		t.Fatalf("specials must be season 0: ova=%d bonus=%d",
			seasonByID[byPath[ovaPath].SeasonID], seasonByID[byPath[bonusPath].SeasonID])
	}
}

func TestRootMovieNestedNotSeason0(t *testing.T) {
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

	showDir := filepath.Join(media, "Overlord")
	epPath := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	moviePath := filepath.Join(showDir, "Overlord The Sacred Kingdom.mkv")
	ovaPath := filepath.Join(showDir, "Specials", "The Hidden OVA.mkv")
	for _, p := range []string{epPath, moviePath, ovaPath} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	const sess = "20260915T120000Z-dddddddddddddddd"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":3}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"files":3,"done":3,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id": "job-show", "status": "matched", "path": showDir, "source": "scan",
				"match": map[string]any{"provider": "tvmaze", "id": "22", "title": "Overlord", "year": "2015"},
			},
			{
				"id": "job-specials", "status": "matched", "path": filepath.Join(showDir, "Specials"), "source": "scan",
				"match": map[string]any{"provider": "tvmaze", "id": "22", "title": "Overlord", "year": "2015"},
			},
			{
				"id": "job-movie", "status": "matched", "path": moviePath, "source": "scan",
				"match": map[string]any{"provider": "tmdb", "id": "99", "title": "Overlord The Sacred Kingdom", "year": "2024"},
			},
		})
	})
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "fakejpeg")
			return
		}
		id := filepath.Base(r.URL.Path)
		title, year := "Overlord The Sacred Kingdom", "2024"
		var seasons []any
		if id == "22" {
			title, year = "Overlord", "2015"
			seasons = []any{map[string]any{
				"number": "1", "title": "Season 1",
				"episodes": []any{map[string]any{"number": "1", "title": "Pilot"}},
			}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider": "tvmaze", "id": id, "title": title, "year": year,
			"synopsis": "plot", "poster": r.URL.Path + "/poster.jpg",
			"seasons": seasons,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	if err := w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: true}); err != nil {
		t.Fatal(err)
	}

	show, err := d.GetMediaItemByPath(ctx, lib.ID, showDir)
	if err != nil || show == nil || show.Kind != "show" {
		t.Fatalf("show %#v %v", show, err)
	}
	film, err := d.GetMediaItemByPath(ctx, lib.ID, moviePath)
	if err != nil || film == nil || film.Kind != "movie" {
		t.Fatalf("movie card %#v %v", film, err)
	}
	if !film.ParentID.Valid || film.ParentID.Int64 != show.ID {
		t.Fatalf("movie parent %#v", film.ParentID)
	}
	eps, err := d.ListEpisodesByShow(ctx, show.ID)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]db.Episode{}
	for _, ep := range eps {
		byPath[ep.Path] = ep
	}
	if _, ok := byPath[moviePath]; ok {
		t.Fatal("nested movie must not be a specials episode")
	}
	if _, ok := byPath[ovaPath]; !ok {
		t.Fatal("OVA in Specials/ should stay season 0")
	}
	if _, ok := byPath[epPath]; !ok {
		t.Fatal("season 1 episode missing")
	}
}

func TestTitleSimilarSpinOffNestsUnderParent(t *testing.T) {
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

	showDir := filepath.Join(media, "Sample Show")
	spinDir := filepath.Join(media, "Sample Show Explosion")
	epA := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	epB := filepath.Join(spinDir, "Season 1", "S01E01.mkv")
	for _, p := range []string{epA, epB} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	const sess = "20260914T120000Z-cccccccccccccccc"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":2}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"files":2,"done":2,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id": "job-a", "status": "matched", "path": showDir, "source": "scan",
				"match": map[string]any{"provider": "tvmaze", "id": "22", "title": "Sample Show", "year": "2020"},
			},
			{
				"id": "job-b", "status": "matched", "path": spinDir, "source": "scan",
				"match": map[string]any{"provider": "tvmaze", "id": "23", "title": "Sample Show Explosion", "year": "2023"},
			},
		})
	})
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "fakejpeg")
			return
		}
		id := filepath.Base(r.URL.Path)
		title := "Sample Show"
		if id == "23" {
			title = "Sample Show Explosion"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider": "tvmaze", "id": id, "title": title, "year": "2020",
			"synopsis": "plot", "poster": r.URL.Path + "/poster.jpg",
			"seasons": []any{map[string]any{
				"number": "1", "title": "Season 1",
				"episodes": []any{map[string]any{"number": "1", "title": "Pilot"}},
			}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	if err := w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	items, err := d.ListMediaItems(ctx, lib.ID, "", "")
	if err != nil || len(items) != 1 || items[0].MetaID.String != "22" {
		t.Fatalf("library cards %#v %v", items, err)
	}
	spin, err := d.GetMediaItemByPath(ctx, lib.ID, spinDir)
	if err != nil || spin == nil || spin.Kind != "show" || spin.MetaID.String != "23" {
		t.Fatalf("spin %#v %v", spin, err)
	}
	if !spin.ParentID.Valid || spin.ParentID.Int64 != items[0].ID {
		t.Fatalf("spin parent %#v want %d", spin.ParentID, items[0].ID)
	}
	eps, err := d.ListEpisodesByShow(ctx, spin.ID)
	if err != nil || len(eps) != 1 || eps[0].Path != epB {
		t.Fatalf("spin eps %#v %v", eps, err)
	}
	parentEps, err := d.ListEpisodesByShow(ctx, items[0].ID)
	if err != nil || len(parentEps) != 1 || parentEps[0].Path != epA {
		t.Fatalf("parent eps %#v %v", parentEps, err)
	}
}

func TestNestedChildShowNotParentSeason(t *testing.T) {
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

	showDir := filepath.Join(media, "Barakamon")
	epA := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	childDir := filepath.Join(showDir, "Hanada Kun")
	epB := filepath.Join(childDir, "Season 1", "S01E01.mkv")
	for _, p := range []string{epA, epB} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	const sess = "20260914T130000Z-dddddddddddddddd"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":2}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"files":2,"done":2,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id": "job-a", "status": "matched", "path": showDir, "source": "scan",
				"match": map[string]any{"provider": "tvmaze", "id": "10", "title": "Barakamon", "year": "2014"},
			},
			{
				"id": "job-b", "status": "matched", "path": childDir, "source": "scan",
				"match": map[string]any{"provider": "tvmaze", "id": "11", "title": "Hanada Kun", "year": "2016"},
			},
		})
	})
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "fakejpeg")
			return
		}
		id := filepath.Base(r.URL.Path)
		title := "Barakamon"
		if id == "11" {
			title = "Hanada Kun"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider": "tvmaze", "id": id, "title": title, "year": "2014",
			"synopsis": "plot", "poster": r.URL.Path + "/poster.jpg",
			"seasons": []any{map[string]any{
				"number": "1", "title": "Season 1",
				"episodes": []any{map[string]any{"number": "1", "title": "Pilot"}},
			}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	if err := w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	items, err := d.ListMediaItems(ctx, lib.ID, "", "")
	if err != nil || len(items) != 1 || items[0].Title != "Barakamon" {
		t.Fatalf("library cards %#v %v", items, err)
	}
	child, err := d.GetMediaItemByPath(ctx, lib.ID, childDir)
	if err != nil || child == nil || child.Title != "Hanada Kun" {
		t.Fatalf("child %#v %v", child, err)
	}
	if !child.ParentID.Valid || child.ParentID.Int64 != items[0].ID {
		t.Fatalf("child parent %#v", child.ParentID)
	}
	parentEps, err := d.ListEpisodesByShow(ctx, items[0].ID)
	if err != nil || len(parentEps) != 1 || parentEps[0].Path != epA {
		t.Fatalf("parent must not ingest Hanada: %#v %v", parentEps, err)
	}
	seasons, err := d.ListSeasons(ctx, items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range seasons {
		if s.SeasonNumber == 2 {
			t.Fatalf("Hanada must not be season 2: %#v", seasons)
		}
	}
	childEps, err := d.ListEpisodesByShow(ctx, child.ID)
	if err != nil || len(childEps) != 1 || childEps[0].Path != epB {
		t.Fatalf("child eps %#v %v", childEps, err)
	}
}

func TestUnrelatedSiblingShowsStayTopLevel(t *testing.T) {
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

	aDir := filepath.Join(media, "Alpha Show")
	bDir := filepath.Join(media, "Zeta Chronicles")
	epA := filepath.Join(aDir, "Season 1", "S01E01.mkv")
	epB := filepath.Join(bDir, "Season 1", "S01E01.mkv")
	for _, p := range []string{epA, epB} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	const sess = "20260914T130000Z-eeeeeeeeeeeeeeee"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":2}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"files":2,"done":2,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id": "job-a", "status": "matched", "path": aDir, "source": "scan",
				"match": map[string]any{"provider": "tvmaze", "id": "1", "title": "Alpha Show", "year": "2020"},
			},
			{
				"id": "job-b", "status": "matched", "path": bDir, "source": "scan",
				"match": map[string]any{"provider": "tvmaze", "id": "2", "title": "Zeta Chronicles", "year": "2021"},
			},
		})
	})
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "fakejpeg")
			return
		}
		id := filepath.Base(r.URL.Path)
		title := "Alpha Show"
		if id == "2" {
			title = "Zeta Chronicles"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider": "tvmaze", "id": id, "title": title, "year": "2020",
			"synopsis": "plot", "poster": r.URL.Path + "/poster.jpg",
			"seasons": []any{map[string]any{
				"number": "1", "title": "Season 1",
				"episodes": []any{map[string]any{"number": "1", "title": "Pilot"}},
			}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	if err := w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	items, err := d.ListMediaItems(ctx, lib.ID, "", "")
	if err != nil || len(items) != 2 {
		t.Fatalf("cards %#v %v", items, err)
	}
	for _, it := range items {
		if it.ParentID.Valid {
			t.Fatalf("unrelated show nested: %#v", it)
		}
	}
}

func TestSeasonAndEpisodeDisplayTitles(t *testing.T) {
	if got := seasonDisplayTitle(2, "Frieren", "Frieren"); got != "Season 2" {
		t.Fatalf("s2 inherited series title: %q", got)
	}
	if got := seasonDisplayTitle(2, "", "Frieren"); got != "Season 2" {
		t.Fatalf("s2 empty catalog: %q", got)
	}
	if got := seasonDisplayTitle(0, "", "Frieren"); got != "Specials" {
		t.Fatalf("s0 empty: %q", got)
	}
	if got := seasonDisplayTitle(0, "OVA", "Frieren"); got != "OVA" {
		t.Fatalf("s0 label: %q", got)
	}
	if got := seasonDisplayTitle(1, "Beginning", "Frieren"); got != "Beginning" {
		t.Fatalf("s1 catalog: %q", got)
	}
	if got := episodeDisplayTitle("/m/S00E01.mkv", 1, nil); got != "Episode 1" {
		t.Fatalf("bare sxxeyy: %q", got)
	}
	if got := episodeDisplayTitle("/m/My Bonus Feature.mkv", 2, nil); got != "My Bonus Feature" {
		t.Fatalf("filename: %q", got)
	}
	ce := &matchmedia.Episode{Title: "The Hidden OVA"}
	if got := episodeDisplayTitle("/m/foo.mkv", 1, ce); got != "The Hidden OVA" {
		t.Fatalf("catalog: %q", got)
	}
}

func TestApplyShowUsesSeasonPosterWhenCatalogPosterEmpty(t *testing.T) {
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
	showDir := filepath.Join(media, "Spider")
	epPath := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(epPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Spider", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	s1, err := d.UpsertSeason(ctx, showID, 1, "Season 1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: s1, ShowID: showID, EpisodeNumber: 1, Path: epPath, Mtime: 1,
	}); err != nil {
		t.Fatal(err)
	}

	const sess = "20260829T122800Z-a1b2c3d4e5f6g7h8"
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "fakejpeg")
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	it, err := d.GetMediaItem(ctx, showID)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	cat := matchmedia.Catalog{
		Provider: "jikan", ID: "1", Title: "So I'm a Spider", Synopsis: "plot",
		Seasons: []matchmedia.Season{{
			Number: "1", Title: "Season 1", Poster: "/v1/catalog/jikan/1/s1.jpg",
		}},
	}
	if err := w.applyShow(ctx, it, cat, false, sess); err != nil {
		t.Fatal(err)
	}
	var stored sql.NullString
	if err := d.SQL.QueryRowContext(ctx, `SELECT poster_path FROM media_items WHERE id=?`, showID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !stored.Valid || stored.String == "" {
		t.Fatal("show poster_path not stored")
	}
	if strings.Contains(stored.String, "S01") {
		t.Fatalf("want show cache poster, got %q", stored.String)
	}
	if _, err := os.Stat(filepath.Join(store, stored.String)); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogSeasonPosterURLPrefersSeason1(t *testing.T) {
	cat := matchmedia.Catalog{Seasons: []matchmedia.Season{
		{Number: "0", Poster: "/s0.jpg"},
		{Number: "2", Poster: "/s2.jpg"},
		{Number: "1", Poster: "/s1.jpg"},
	}}
	if got := catalogSeasonPosterURL(cat); got != "/s1.jpg" {
		t.Fatalf("got %q", got)
	}
}

func catalogArtServer(t *testing.T, sess, provider, id, title, year, kind string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session") != sess {
			http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "fakejpeg")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider": provider, "id": id, "title": title, "year": year, "type": kind,
			"synopsis": "plot", "poster": "/v1/catalog/" + provider + "/" + id + "/poster.jpg",
		})
	})
	return httptest.NewServer(mux)
}

func TestFillMissingArtCopiesShowPosterWithoutOverwrite(t *testing.T) {
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
	showDir := filepath.Join(media, "Witch Hat Atelier")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Witch Hat Atelier", Path: showDir,
		Plot: sql.NullString{String: "plot", Valid: true}, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateMediaItemMeta(ctx, id, "Witch Hat Atelier", 2026, "plot", "", "",
		"metadata/tv/Witch Hat Atelier/tvshow.nfo", 0, "tvmaze", "80316"); err != nil {
		t.Fatal(err)
	}

	const sess = "20260915T133954Z-aaaaaaaaaaaaaaaa"
	srv := catalogArtServer(t, sess, "tvmaze", "80316", "Witch Hat Atelier", "2026", "show")
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	if it.PosterPath.Valid && it.PosterPath.String != "" {
		t.Fatalf("setup poster %#v", it.PosterPath)
	}
	if err := w.applyJob(ctx, it, matchmedia.Job{ID: "j1", Status: "matched"}, Opts{Overwrite: false, ManualSelect: true}, sess); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetMediaItem(ctx, id)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if !got.PosterPath.Valid || got.PosterPath.String == "" {
		t.Fatal("poster_path not set")
	}
	if _, err := os.Stat(filepath.Join(store, got.PosterPath.String)); err != nil {
		t.Fatal(err)
	}
	if got.MatchStatus.Valid && got.MatchStatus.String == "manual" {
		t.Fatalf("matched rescan must not flip to manual %#v", got.MatchStatus)
	}
}

func TestFillMissingArtCopiesMoviePosterWithoutOverwrite(t *testing.T) {
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
	filmDir := filepath.Join(media, "Your Name (2016)")
	filmPath := filepath.Join(filmDir, "Your Name (2016).mkv")
	if err := os.MkdirAll(filmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filmPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "movie", Title: "Your Name", Path: filmPath,
		Year: sql.NullInt64{Int64: 2016, Valid: true},
		Plot: sql.NullString{String: "plot", Valid: true}, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateMediaItemMeta(ctx, id, "Your Name", 2016, "plot", "", "",
		"metadata/movies/Your Name (2016)/movie.nfo", 0, "tvmaze", "58373"); err != nil {
		t.Fatal(err)
	}

	const sess = "20260915T133954Z-bbbbbbbbbbbbbbbb"
	srv := catalogArtServer(t, sess, "tvmaze", "58373", "Your Name", "2016", "movie")
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	if it.PosterPath.Valid && it.PosterPath.String != "" {
		t.Fatalf("setup poster %#v", it.PosterPath)
	}
	if err := w.applyJob(ctx, it, matchmedia.Job{ID: "j1", Status: "matched"}, Opts{Overwrite: false, ManualSelect: true}, sess); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetMediaItem(ctx, id)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if !got.PosterPath.Valid || got.PosterPath.String == "" {
		t.Fatal("poster_path not set")
	}
	if _, err := os.Stat(filepath.Join(store, got.PosterPath.String)); err != nil {
		t.Fatal(err)
	}
}

func setupMatchedShow(t *testing.T, store, media string) (d *db.DB, lib *db.Library, id int64) {
	t.Helper()
	ctx := context.Background()
	var err error
	d, err = db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err = d.CreateLibrary(ctx, u.ID, "Lib", media)
	if err != nil {
		t.Fatal(err)
	}
	showDir := filepath.Join(media, "Witch Hat Atelier")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatal(err)
	}
	id, err = d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Witch Hat Atelier", Path: showDir,
		Plot: sql.NullString{String: "plot", Valid: true}, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateMediaItemMeta(ctx, id, "Witch Hat Atelier", 2026, "plot", "", "",
		"metadata/tv/Witch Hat Atelier/tvshow.nfo", 0, "tvmaze", "80316"); err != nil {
		t.Fatal(err)
	}
	return d, lib, id
}

func TestFillMissingArtUsesStoredSessionWhenJobSessionDiffers(t *testing.T) {
	ctx := context.Background()
	store := t.TempDir()
	media := t.TempDir()
	d, _, id := setupMatchedShow(t, store, media)
	defer d.Close()

	const storedSess = "20260915T133954Z-storedstoredstor"
	const newSess = "20260915T140000Z-newnewnewnewnewn"
	if err := d.SetMatchMediaMatch(ctx, id, storedSess, "old-job", "matched", ""); err != nil {
		t.Fatal(err)
	}

	srv := catalogArtServer(t, storedSess, "tvmaze", "80316", "Witch Hat Atelier", "2026", "show")
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	if err := w.applyJob(ctx, it, matchmedia.Job{ID: "j-new", Status: "matched"}, Opts{Overwrite: false, ManualSelect: true}, newSess); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetMediaItem(ctx, id)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if !got.PosterPath.Valid || got.PosterPath.String == "" {
		t.Fatal("poster_path not set from stored session catalog")
	}
	if _, err := os.Stat(filepath.Join(store, got.PosterPath.String)); err != nil {
		t.Fatal(err)
	}
	if got.MatchStatus.Valid && got.MatchStatus.String == "manual" {
		t.Fatalf("matched rescan must not flip to manual %#v", got.MatchStatus)
	}
}

func TestFillMissingArtCopiesLocalMatchMediaCatalogPoster(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := filepath.Join(root, "store")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	d, _, id := setupMatchedShow(t, store, media)
	defer d.Close()

	catDir := filepath.Join(root, "matchmedia", "catalog", "[tvmaze-80316] Witch Hat Atelier (2026)")
	if err := os.MkdirAll(catDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(catDir, "poster.jpg"), []byte("catalog-jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"session required"}`, http.StatusBadRequest)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	if err := w.applyJob(ctx, it, matchmedia.Job{ID: "j1", Status: "matched"}, Opts{Overwrite: false, ManualSelect: true}, "20260915T140000Z-deaddeaddeaddead"); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetMediaItem(ctx, id)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if !got.PosterPath.Valid || got.PosterPath.String == "" {
		t.Fatal("poster_path not set from on-disk MatchMedia catalog")
	}
	b, err := os.ReadFile(filepath.Join(store, got.PosterPath.String))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "catalog-jpeg" {
		t.Fatalf("copied bytes %q", b)
	}
}

func TestApplyJobErrorStoresStatusAndMessage(t *testing.T) {
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
	showDir := filepath.Join(media, "Witch Hat Atelier")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Witch Hat Atelier", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	w := &Worker{DB: d, Store: store}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	const msg = "tvmaze: cooldown; jikan: cooldown"
	if err := w.applyJob(ctx, it, matchmedia.Job{ID: "j-err", Status: "error", Error: msg}, Opts{}, "sess"); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetMediaItem(ctx, id)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.MatchStatus.String != "error" {
		t.Fatalf("status %q want error", got.MatchStatus.String)
	}
	if got.MatchError.String != msg {
		t.Fatalf("match_error %q want %q", got.MatchError.String, msg)
	}
}

func TestApplyJobUnmatchedStaysUnmatched(t *testing.T) {
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
	showDir := filepath.Join(media, "Unknown Show")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatal(err)
	}
	id, err := d.UpsertMediaItem(ctx, db.MediaItem{
		LibraryID: lib.ID, Kind: "show", Title: "Unknown Show", Path: showDir, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	w := &Worker{DB: d, Store: store}
	it, err := d.GetMediaItem(ctx, id)
	if err != nil || it == nil {
		t.Fatal(err)
	}
	if err := w.applyJob(ctx, it, matchmedia.Job{ID: "j-miss", Status: "unmatched"}, Opts{}, "sess"); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetMediaItem(ctx, id)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.MatchStatus.String != "unmatched" {
		t.Fatalf("status %q want unmatched", got.MatchStatus.String)
	}
	if got.MatchError.Valid && got.MatchError.String != "" {
		t.Fatalf("unmatched must not store match_error %#v", got.MatchError)
	}
}

