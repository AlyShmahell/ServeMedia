package matchmedia

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/config"
	"gopkg.in/yaml.v3"
)

func TestResolveURL(t *testing.T) {
	c := &Client{Base: "http://127.0.0.1:7680"}
	if got := c.ResolveURL("/v1/catalog/tvmaze/1/poster.jpg", ""); got != "http://127.0.0.1:7680/v1/catalog/tvmaze/1/poster.jpg" {
		t.Fatalf("relative: %q", got)
	}
	sess := "20260829T122800Z-a1b2c3d4e5f6g7h8"
	got := c.ResolveURL("/v1/catalog/tvmaze/1/poster.jpg", sess)
	if got != "http://127.0.0.1:7680/v1/catalog/tvmaze/1/poster.jpg?session="+sess {
		t.Fatalf("session: %q", got)
	}
	abs := "https://img.example/p.jpg"
	if got := c.ResolveURL(abs, sess); got != abs {
		t.Fatalf("absolute: %q", got)
	}
	if c.ResolveURL("", sess) != "" {
		t.Fatal("empty")
	}
}

func TestWithSession(t *testing.T) {
	got := withSession("/v1/jobs", "20260829T122800Z-a1b2c3d4e5f6g7h8")
	if got != "/v1/jobs?session=20260829T122800Z-a1b2c3d4e5f6g7h8" {
		t.Fatalf("got %q", got)
	}
	if withSession("/v1/jobs", "") != "/v1/jobs" {
		t.Fatal("empty session")
	}
}

func TestFindSeasonEpisodePadded(t *testing.T) {
	cat := Catalog{Seasons: []Season{{
		Number:   "01",
		Episodes: []Episode{{Number: "02", Title: "Pilot", Poster: "/v1/catalog/tvmaze/1/seasons/1/episodes/2/poster.jpg"}},
	}}}
	if cat.FindSeason(1) == nil {
		t.Fatal("season 1 vs 01")
	}
	ep := cat.FindEpisode(1, 2)
	if ep == nil || ep.Title != "Pilot" {
		t.Fatalf("episode: %#v", ep)
	}
}

func TestScanJSONPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/scan" || r.Method != http.MethodPost {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		gotPath = body["path"]
		if _, ok := body["mode"]; ok {
			t.Fatal("empty mode must be omitted")
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"20260829T122800Z-a1b2c3d4e5f6g7h8","files":3}`))
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client()}
	got, err := c.postScan("/media/tv", "", nil)
	if err != nil || got.Files != 3 || got.Session != "20260829T122800Z-a1b2c3d4e5f6g7h8" || gotPath != "/media/tv" {
		t.Fatalf("got=%+v path=%q err=%v", got, gotPath, err)
	}
}

func TestScanJSONMode(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"20260829T122800Z-a1b2c3d4e5f6g7h8","files":1,"mode":"changes"}`))
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client()}
	out, err := c.postScan("/media/tv", "changes", nil)
	if err != nil || out.Session == "" {
		t.Fatal(err)
	}
	if got["path"] != "/media/tv" || got["mode"] != "changes" {
		t.Fatalf("body %#v", got)
	}
}

func TestScanJSONRequireEpisodeNFO(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"20260829T122800Z-a1b2c3d4e5f6g7h8","files":1}`))
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client()}
	flag := true
	if _, err := c.postScan("/media/tv", "changes", &flag); err != nil {
		t.Fatal(err)
	}
	if got["path"] != "/media/tv" || got["mode"] != "changes" || got["require_episode_nfo"] != true {
		t.Fatalf("body %#v", got)
	}
}

func TestScanMissingSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"files":3}`))
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client()}
	if _, err := c.postScan("/media/tv", "", nil); err == nil {
		t.Fatal("expected missing session")
	}
}

func TestJobsSessionQuery(t *testing.T) {
	sess := "20260829T122800Z-a1b2c3d4e5f6g7h8"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/jobs" || r.URL.Query().Get("session") != sess {
			t.Fatalf("got %s %s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client()}
	if _, err := c.Jobs(sess); err != nil {
		t.Fatal(err)
	}
}

func TestStatusHealthVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"healthy":true,"version":"0.1.0"}`))
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client()}
	st, err := c.Status()
	if err != nil || !st.Ready || st.Version != "0.1.0" {
		t.Fatalf("status %+v err=%v", st, err)
	}
}

func TestConfigGetPost(t *testing.T) {
	var posted map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"match":{"min_score":0.72}}`))
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"healthy":true}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client()}
	got, err := c.Config()
	if err != nil {
		t.Fatal(err)
	}
	match, _ := got["match"].(map[string]any)
	if match["min_score"] != 0.72 {
		t.Fatalf("config %#v", got)
	}
	if err := c.SetConfig(map[string]any{"match": map[string]any{"min_score": 0.8}}); err != nil {
		t.Fatal(err)
	}
	m, _ := posted["match"].(map[string]any)
	if m["min_score"] != 0.8 {
		t.Fatalf("posted %#v", posted)
	}
}

func TestIngestJSON(t *testing.T) {
	var got []IngestRow
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ingest" || r.Method != http.MethodPost {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"20260829T122800Z-a1b2c3d4e5f6g7h8","jobs":1}`))
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client()}
	out, err := c.Ingest([]IngestRow{{Title: "Girls", Year: "2012"}})
	if err != nil || out.Session != "20260829T122800Z-a1b2c3d4e5f6g7h8" || out.Jobs != 1 {
		t.Fatalf("got=%+v err=%v", out, err)
	}
	if len(got) != 1 || got[0].Title != "Girls" || got[0].Year != "2012" || got[0].Type != "" {
		t.Fatalf("rows %#v", got)
	}
}

func TestScanResultJobsCount(t *testing.T) {
	var got ScanResult
	if err := json.Unmarshal([]byte(`{"session":"s1","files":2,"jobs":3}`), &got); err != nil || got.Session != "s1" || got.Files != 2 || got.Jobs != 3 {
		t.Fatalf("count: %+v %v", got, err)
	}
	got = ScanResult{}
	if err := json.Unmarshal([]byte(`{"session":"s1","files":2}`), &got); err != nil || got.Jobs != 0 {
		t.Fatalf("omitted: %+v %v", got, err)
	}
}

func TestJobUnmarshalContractScalars(t *testing.T) {
	raw := []byte(`[
		{
			"id": "job-1",
			"status": "manual",
			"kind": "movie",
			"files": [{"path": "/m/a.mkv", "season": "0", "episode": "1"}],
			"candidates": [{"provider": "jikan", "id": "2016", "title": "Film Title.", "year": "2016", "score": 0.91}]
		}
	]`)
	var jobs []Job
	if err := json.Unmarshal(raw, &jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs %d", len(jobs))
	}
	if jobs[0].Files[0].Season != "0" || jobs[0].Files[0].Episode != "1" {
		t.Fatalf("files %#v", jobs[0].Files)
	}
	if len(jobs[0].Candidates) != 1 {
		t.Fatalf("cands %#v", jobs[0].Candidates)
	}
	c := jobs[0].Candidates[0]
	if c.ID != "2016" || c.Year != "2016" || c.Score < 0.9 {
		t.Fatalf("candidate %#v", c)
	}
}

func TestWithinFilesystemRoot(t *testing.T) {
	if !within("/", "/mnt/microsd/media/anime") {
		t.Fatal("absolute path under /")
	}
	if !within("/", "/") {
		t.Fatal("root is inside itself")
	}
	if within("/media", "/mnt") {
		t.Fatal("sibling")
	}
}

func TestOverlayDest(t *testing.T) {
	data := t.TempDir()
	got := overlayDest(data)
	want := filepath.Join(data, "overlay.yaml")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func testEngine(t *testing.T) *Engine {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "share"))
	t.Setenv("SERVEMEDIA_HOME", "")
	t.Setenv("SERVEMEDIA_MATCHMEDIA_OVERLAY", "")
	e, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func numIs(v any, want float64) bool {
	switch n := v.(type) {
	case int:
		return float64(n) == want
	case int64:
		return float64(n) == want
	case uint64:
		return float64(n) == want
	case float64:
		return n == want
	case float32:
		return float64(n) == want
	default:
		return false
	}
}

func yamlRoundTrip(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	b, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOpenUsesMatchMediaDefault(t *testing.T) {
	e := testEngine(t)
	dataDir, err := config.MatchMediaDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "overlay.yaml")); !os.IsNotExist(err) {
		t.Fatalf("overlay created: %v", err)
	}
	doc, err := e.Config()
	if err != nil {
		t.Fatal(err)
	}
	if doc["version"] != "0.2.0" {
		t.Fatalf("version %#v", doc["version"])
	}
	if _, ok := doc["data_dir"]; ok {
		t.Fatalf("data_dir leaked into config %#v", doc["data_dir"])
	}
	providers, _ := doc["providers"].(map[string]any)
	tmdb, _ := providers["tmdb"].(map[string]any)
	movie, _ := tmdb["movie"].(map[string]any)
	search, _ := movie["search"].(map[string]any)
	if search["url"] != "{base}/search/movie" {
		t.Fatalf("movie search %#v", search)
	}
	tvmaze, _ := providers["tvmaze"].(map[string]any)
	tvSearch, _ := tvmaze["search"].(map[string]any)
	if !numIs(tvSearch["min_interval_ms"], 501) {
		t.Fatalf("tvmaze interval %#v", tvSearch["min_interval_ms"])
	}
}

func TestOpenKeepsOverlayDelta(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "share"))
	t.Setenv("SERVEMEDIA_HOME", "")
	t.Setenv("SERVEMEDIA_MATCHMEDIA_OVERLAY", "")
	dataDir, err := config.MatchMediaDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("match:\n  min_score: 0.81\nproviders:\n  tmdb:\n    api_key: secret\n")
	path := filepath.Join(dataDir, "overlay.yaml")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(body) {
		t.Fatalf("overlay rewritten:\n%s", raw)
	}
	doc, err := e.Config()
	if err != nil {
		t.Fatal(err)
	}
	match, _ := doc["match"].(map[string]any)
	if !numIs(match["min_score"], 0.81) {
		t.Fatalf("min_score %#v", match["min_score"])
	}
	if doc["version"] != "0.2.0" {
		t.Fatalf("default version missing %#v", doc["version"])
	}
	providers, _ := doc["providers"].(map[string]any)
	tmdb, _ := providers["tmdb"].(map[string]any)
	if tmdb["api_key"] != "secret" {
		t.Fatalf("api_key %#v", tmdb["api_key"])
	}
	movie, _ := tmdb["movie"].(map[string]any)
	search, _ := movie["search"].(map[string]any)
	if search["url"] != "{base}/search/movie" {
		t.Fatalf("default search lost %#v", search)
	}
}

func TestSetConfigWritesOnlyDelta(t *testing.T) {
	e := testEngine(t)
	doc, err := e.Config()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetConfig(yamlRoundTrip(t, doc)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(e.overlay); !os.IsNotExist(err) {
		t.Fatalf("unchanged save wrote overlay: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(e.overlay)); !os.IsNotExist(err) {
		t.Fatalf("empty matchmedia dir: %v", err)
	}
	edited := yamlRoundTrip(t, doc)
	match, _ := edited["match"].(map[string]any)
	match["min_score"] = 0.81
	providers, _ := edited["providers"].(map[string]any)
	tmdb, _ := providers["tmdb"].(map[string]any)
	tmdb["api_key"] = "secret"
	if err := e.SetConfig(edited); err != nil {
		t.Fatal(err)
	}
	saved, err := loadYAMLMap(e.overlay)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := saved["version"]; ok {
		t.Fatalf("default copied into overlay %#v", saved)
	}
	if _, ok := saved["http"]; ok {
		t.Fatalf("default copied into overlay %#v", saved)
	}
	gotMatch, _ := saved["match"].(map[string]any)
	if len(gotMatch) != 1 || !numIs(gotMatch["min_score"], 0.81) {
		t.Fatalf("match delta %#v", gotMatch)
	}
	gotProviders, _ := saved["providers"].(map[string]any)
	gotTMDB, _ := gotProviders["tmdb"].(map[string]any)
	if len(gotTMDB) != 1 || gotTMDB["api_key"] != "secret" {
		t.Fatalf("provider delta %#v", gotProviders)
	}
	again, err := e.Config()
	if err != nil {
		t.Fatal(err)
	}
	shown, _ := again["match"].(map[string]any)
	if !numIs(shown["min_score"], 0.81) {
		t.Fatalf("resolved min_score %#v", shown["min_score"])
	}
	shownProviders, _ := again["providers"].(map[string]any)
	shownTMDB, _ := shownProviders["tmdb"].(map[string]any)
	if shownTMDB["api_key"] != "secret" {
		t.Fatalf("resolved api_key %#v", shownTMDB["api_key"])
	}
}

func TestOpenMergesExtraOverlay(t *testing.T) {
	extra := filepath.Join(t.TempDir(), "extra.yaml")
	if err := os.WriteFile(extra, []byte("browse_roots: [/media]\nproviders:\n  omdb:\n    base: http://omdb-stub:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "share"))
	t.Setenv("SERVEMEDIA_HOME", "")
	t.Setenv("SERVEMEDIA_MATCHMEDIA_OVERLAY", extra)
	e, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	cfg := e.config()
	if cfg == nil || cfg.Providers["omdb"].Base != "http://omdb-stub:8080" {
		t.Fatalf("omdb base %#v", cfg)
	}
	raw, err := os.ReadFile(e.overlay)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := loadYAMLMap(e.overlay)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := saved["data_dir"]; ok {
		t.Fatalf("data_dir invented %#v", saved)
	}
	if _, ok := saved["http"]; ok {
		t.Fatalf("http invented %#v", saved)
	}
	providers, _ := saved["providers"].(map[string]any)
	omdb, _ := providers["omdb"].(map[string]any)
	if omdb["base"] != "http://omdb-stub:8080" {
		t.Fatalf("overlay %#v\n%s", saved, raw)
	}
}

func TestScrubSidecarOverlayKeepsAPIKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overlay.yaml")
	body := []byte("version: \"0.1.0\"\nbrowse_roots:\n  - /mnt\nhttp:\n  addr: 127.0.0.1:7680\ngroup:\n  extras:\n    - trailers\nproviders:\n  tmdb:\n    api_key: tmdb-key\n    url: https://example.test\n  omdb:\n    api_key: omdb-key\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := scrubSidecarFile(path); err != nil {
		t.Fatal(err)
	}
	got, err := loadYAMLMap(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["browse_roots"]; ok {
		t.Fatalf("sidecar keys survived %#v", got)
	}
	if _, ok := got["http"]; ok {
		t.Fatalf("http survived %#v", got)
	}
	providers, _ := got["providers"].(map[string]any)
	tmdb, _ := providers["tmdb"].(map[string]any)
	omdb, _ := providers["omdb"].(map[string]any)
	if tmdb["api_key"] != "tmdb-key" || omdb["api_key"] != "omdb-key" {
		t.Fatalf("keys %#v", got)
	}
	if _, ok := tmdb["url"]; ok {
		t.Fatalf("provider url survived %#v", tmdb)
	}
	if err := scrubSidecarFile(path); err != nil {
		t.Fatal(err)
	}
	again, err := loadYAMLMap(path)
	if err != nil {
		t.Fatal(err)
	}
	if again["providers"] == nil {
		t.Fatalf("second scrub dropped keys %#v", again)
	}
}
