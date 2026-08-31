package fetch

import (
	"context"
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

	"github.com/alyshmahell/medora/internal/db"
	"github.com/alyshmahell/medora/internal/matchora"
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

	w := &Worker{DB: d, Store: store, Meta: &matchora.Client{Base: srv.URL}}
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
	if manual.MatchStatus.String != "manual" || manual.MatchoraJobID.String != "job-manual" {
		t.Fatalf("manual %#v", manual)
	}
	if manual.MatchoraSessionID.String != sess {
		t.Fatalf("manual session %q want %q", manual.MatchoraSessionID.String, sess)
	}
	if film.MatchoraSessionID.String != sess {
		t.Fatalf("film session %q", film.MatchoraSessionID.String)
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
	w := &Worker{DB: d, Store: store, Meta: &matchora.Client{Base: srv.URL, HTTP: srv.Client()}}
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
	showJob := matchora.Job{
		Path: "/media/Show",
		Files: []matchora.JobFile{
			{Path: "/media/Show/Season 1/Show S01E01.mkv", Season: "1", Episode: "1"},
		},
	}
	if kind := kindFromJob(showJob, showJob.Files); kind != "show" {
		t.Fatalf("show kind %q", kind)
	}
	movieJob := matchora.Job{
		Path:  "/media/Film",
		Files: []matchora.JobFile{{Path: "/media/Film/Film.mkv"}},
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

	w := &Worker{DB: d, Store: store, Meta: &matchora.Client{Base: srv.URL, HTTP: srv.Client()}}
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

	w := &Worker{DB: d, Store: store, Meta: &matchora.Client{Base: srv.URL}}
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
	if err != nil || got == nil || got.Title != "Girls" || got.MetaID.String != "55" {
		t.Fatalf("item %#v %v", got, err)
	}
}

