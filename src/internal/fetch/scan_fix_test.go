package fetch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
)

func scanFixLib(t *testing.T) (context.Context, *db.DB, *db.Library, string, string) {
	t.Helper()
	ctx := context.Background()
	store := t.TempDir()
	media := t.TempDir()
	d, err := db.Open(filepath.Join(store, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := d.CreateLibrary(ctx, u.ID, "Lib", media)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, d, lib, store, media
}

func TestLibraryScanRetriesUnmatchedShowOnce(t *testing.T) {
	ctx, d, lib, store, mediaRoot := scanFixLib(t)
	showDir := filepath.Join(mediaRoot, "Cool Show")
	epPath := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(epPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	const libSess = "sess-lib"
	const retrySess = "sess-retry"
	var scanPaths []string
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		scanPaths = append(scanPaths, body.Path)
		sess := libSess
		if body.Path == showDir {
			sess = retrySess
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":1}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":1,"done":1,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		sess := r.URL.Query().Get("session")
		job := map[string]any{
			"id": "job-show", "status": "unmatched", "kind": "show", "path": showDir, "source": "scan",
			"files": []map[string]any{{"path": epPath, "season": "1", "episode": "1"}},
		}
		if sess == retrySess {
			job["status"] = "matched"
			job["match"] = map[string]any{"provider": "tvmaze", "id": "22", "title": "Cool Show", "year": "2020"}
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{job})
	})
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "fakejpeg")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider": "tvmaze", "id": "22", "title": "Cool Show", "year": "2020", "type": "show",
			"synopsis": "plot", "poster": r.URL.Path + "/poster.jpg",
			"seasons": []any{map[string]any{
				"number": "1", "title": "Cool Show",
				"episodes": []any{map[string]any{"number": "1", "title": "Pilot"}},
			}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	if err := w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: true, ScanMode: "rescan"}); err != nil {
		t.Fatal(err)
	}
	if len(scanPaths) != 2 {
		t.Fatalf("scans %#v", scanPaths)
	}
	if scanPaths[0] != mediaRoot || scanPaths[1] != showDir {
		t.Fatalf("scan paths %#v", scanPaths)
	}
	show, err := d.GetMediaItemByPath(ctx, lib.ID, showDir)
	if err != nil || show == nil || show.MatchStatus.String != "matched" {
		t.Fatalf("show %#v %v", show, err)
	}
	if show.Title != "Cool Show" {
		t.Fatalf("title %q", show.Title)
	}
}

func TestLibraryScanRetryStaysUnmatched(t *testing.T) {
	ctx, d, lib, store, mediaRoot := scanFixLib(t)
	showDir := filepath.Join(mediaRoot, "Lost Show")
	epPath := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(epPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var scans int
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		scans++
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"sess-` + strings.Repeat("a", 8) + `","files":1}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":1,"done":1,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": "job-show", "status": "unmatched", "kind": "show", "path": showDir, "source": "scan",
			"files": []map[string]any{{"path": epPath, "season": "1", "episode": "1"}},
		}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	if err := w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	if scans != 2 {
		t.Fatalf("scans %d", scans)
	}
	show, err := d.GetMediaItemByPath(ctx, lib.ID, showDir)
	if err != nil || show == nil || show.MatchStatus.String != "unmatched" {
		t.Fatalf("show %#v %v", show, err)
	}
}

func TestJobUnmatchedThenCandidatesParksManual(t *testing.T) {
	ctx, d, lib, store, mediaRoot := scanFixLib(t)
	showDir := filepath.Join(mediaRoot, "Frieren")
	epPath := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(epPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	const sess = "sess-frieren"
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":1}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		running := n < 6
		_, _ = w.Write([]byte(`{"files":1,"done":1,"running":` + boolJSON(running) + `}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		n := hits.Load()
		job := map[string]any{
			"id": "job-show", "status": "unmatched", "kind": "show", "path": showDir, "source": "scan",
			"files": []map[string]any{{"path": epPath, "season": "1", "episode": "1"}},
		}
		if n >= 3 {
			job["status"] = "manual"
			job["candidates"] = []map[string]any{
				{"provider": "tvmaze", "id": "99", "title": "Frieren", "year": "2023", "score": 0.9},
			}
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{job})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	w := &Worker{DB: d, Store: store, Meta: &matchmedia.Client{Base: srv.URL, HTTP: srv.Client()}}
	if err := w.MatchLibrary(ctx, lib, Opts{Persist: false, Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	show, err := d.GetMediaItemByPath(ctx, lib.ID, showDir)
	if err != nil || show == nil || show.MatchStatus.String != "manual" {
		t.Fatalf("show %#v %v", show, err)
	}
	if !show.MatchCandidates.Valid || !strings.Contains(show.MatchCandidates.String, "tvmaze") {
		t.Fatalf("snapshot %#v", show.MatchCandidates)
	}
}

func TestExtractMissingStillsFillsEmptyLeavesExisting(t *testing.T) {
	ctx, d, lib, store, mediaRoot := scanFixLib(t)
	filmPath := filepath.Join(mediaRoot, "Film", "Film.mkv")
	if err := os.MkdirAll(filepath.Dir(filmPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filmPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	showDir := filepath.Join(mediaRoot, "Show")
	epEmpty := filepath.Join(showDir, "Season 1", "S01E01.mkv")
	epHave := filepath.Join(showDir, "Season 1", "S01E02.mkv")
	if err := os.MkdirAll(filepath.Dir(epEmpty), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epEmpty, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epHave, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	filmID, err := d.UpsertMediaItem(ctx, db.MediaItem{LibraryID: lib.ID, Kind: "movie", Title: "Film", Path: filmPath, Mtime: 1})
	if err != nil {
		t.Fatal(err)
	}
	showID, err := d.UpsertMediaItem(ctx, db.MediaItem{LibraryID: lib.ID, Kind: "show", Title: "Show", Path: showDir, Mtime: 1})
	if err != nil {
		t.Fatal(err)
	}
	s1, err := d.UpsertSeason(ctx, showID, 1, "Season 1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	emptyID, err := d.UpsertEpisode(ctx, db.Episode{SeasonID: s1, ShowID: showID, EpisodeNumber: 1, Path: epEmpty, Mtime: 1})
	if err != nil {
		t.Fatal(err)
	}
	haveID, err := d.UpsertEpisode(ctx, db.Episode{
		SeasonID: s1, ShowID: showID, EpisodeNumber: 2, Path: epHave, Mtime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	keepPath := filepath.Join(store, "keep.jpg")
	if err := os.WriteFile(keepPath, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateEpisodeMeta(ctx, haveID, "", "", "keep.jpg", "", "", ""); err != nil {
		t.Fatal(err)
	}

	var grabbed []string
	old := extractStill
	extractStill = func(src, dst string) error {
		grabbed = append(grabbed, src)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, []byte("jpeg"), 0o644)
	}
	t.Cleanup(func() { extractStill = old })

	w := &Worker{DB: d, Store: store}
	if err := w.extractMissingStills(ctx, lib, nil, Opts{}); err != nil {
		t.Fatal(err)
	}
	if len(grabbed) != 2 {
		t.Fatalf("grabbed %#v", grabbed)
	}
	film, err := d.GetMediaItem(ctx, filmID)
	if err != nil || film == nil || !film.PosterPath.Valid || film.PosterPath.String == "" {
		t.Fatalf("movie poster %#v %v", film, err)
	}
	empty, err := d.GetEpisode(ctx, emptyID)
	if err != nil || empty == nil || !empty.StillPath.Valid || empty.StillPath.String == "" {
		t.Fatalf("empty still %#v %v", empty, err)
	}
	have, err := d.GetEpisode(ctx, haveID)
	if err != nil || have == nil || have.StillPath.String != "keep.jpg" {
		t.Fatalf("existing still %#v %v", have, err)
	}
}

func TestNestedShowCatalogForDoesNotOverwriteParent(t *testing.T) {
	ctx, d, lib, store, mediaRoot := scanFixLib(t)
	parentDir := filepath.Join(mediaRoot, "DanMachi")
	epA := filepath.Join(parentDir, "Season 1", "S01E01.mkv")
	spinDir := filepath.Join(parentDir, "Sword Oratoria")
	epB := filepath.Join(spinDir, "Season 1", "S01E01.mkv")
	movieDir := filepath.Join(parentDir, "Arrow of the Orion")
	moviePath := filepath.Join(movieDir, "Arrow of the Orion.mkv")
	for _, p := range []string{epA, epB, moviePath} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	const sess = "sess-danmachi"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/scan", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"` + sess + `","files":3}`))
	})
	mux.HandleFunc("/v1/scan/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":3,"done":3,"running":false}`))
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id": "job-parent", "status": "matched", "kind": "show", "path": parentDir, "source": "scan",
				"catalog_for": "tvmaze:111",
				"files":       []map[string]any{{"path": epA, "season": "1", "episode": "1"}},
				"match":       map[string]any{"provider": "tvmaze", "id": "111", "title": "Familia Myth", "year": "2015"},
			},
			{
				"id": "job-spin", "status": "matched", "kind": "show", "path": spinDir, "source": "scan",
				"catalog_for": "tvmaze:111",
				"files":       []map[string]any{{"path": epB, "season": "1", "episode": "1"}},
				"match":       map[string]any{"provider": "tvmaze", "id": "222", "title": "Sword Oratoria", "year": "2017"},
			},
			{
				"id": "job-movie", "status": "matched", "kind": "movie", "path": movieDir, "source": "scan",
				"files": []map[string]any{{"path": moviePath}},
				"match": map[string]any{"provider": "tmdb", "id": "333", "title": "Arrow of the Orion", "year": "2019"},
			},
		})
	})
	mux.HandleFunc("/v1/catalog/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, "fakejpeg")
			return
		}
		id := filepath.Base(r.URL.Path)
		title, year := "Familia Myth", "2015"
		switch id {
		case "222":
			title, year = "Sword Oratoria", "2017"
		case "333":
			title, year = "Arrow of the Orion", "2019"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider": filepath.Base(filepath.Dir(r.URL.Path)), "id": id, "title": title, "year": year,
			"synopsis": "plot", "poster": r.URL.Path + "/poster.jpg",
			"seasons": []any{map[string]any{
				"number": "1", "title": title,
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

	parent, err := d.GetMediaItemByPath(ctx, lib.ID, parentDir)
	if err != nil || parent == nil || parent.Title != "Familia Myth" || parent.MetaID.String != "111" {
		t.Fatalf("parent %#v %v", parent, err)
	}
	spin, err := d.GetMediaItemByPath(ctx, lib.ID, spinDir)
	if err != nil || spin == nil || spin.Kind != "show" || spin.Title != "Sword Oratoria" {
		t.Fatalf("nested show %#v %v", spin, err)
	}
	if !spin.ParentID.Valid || spin.ParentID.Int64 != parent.ID {
		t.Fatalf("nested parent %#v", spin.ParentID)
	}
	eps, err := d.ListEpisodesByShow(ctx, parent.ID)
	if err != nil || len(eps) != 1 || eps[0].Path != epA {
		t.Fatalf("parent eps %#v %v", eps, err)
	}
	film, err := d.GetMediaItemByPath(ctx, lib.ID, moviePath)
	if err != nil || film == nil || film.Kind != "movie" || film.Title != "Arrow of the Orion" {
		t.Fatalf("movie %#v %v", film, err)
	}
}

func boolJSON(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
