package matchmedia

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
		Number: "01",
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
	got, err := c.Scan("/media/tv", "")
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
	out, err := c.Scan("/media/tv", "changes")
	if err != nil || out.Session == "" {
		t.Fatal(err)
	}
	if got["path"] != "/media/tv" || got["mode"] != "changes" {
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
	if _, err := c.Scan("/media/tv", ""); err == nil {
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

func TestScanResultJobsArrayOrCount(t *testing.T) {
	got, err := decodeScanResult([]byte(`{"session":"s1","files":2,"jobs":[{"id":"a"},{"id":"b"}]}`))
	if err != nil || got.Session != "s1" || got.Files != 2 || got.Jobs != 2 {
		t.Fatalf("array: %+v %v", got, err)
	}
	got, err = decodeScanResult([]byte(`{"session":"s1","files":2,"jobs":3}`))
	if err != nil || got.Jobs != 3 {
		t.Fatalf("count: %+v %v", got, err)
	}
}

func TestCommonRoot(t *testing.T) {
	if got := CommonRoot([]string{"/media/movies", "/media/tv"}); got != "/media" {
		t.Fatalf("got %q", got)
	}
	if got := CommonRoot([]string{"/data", "/mnt"}); got != "/" {
		t.Fatalf("disjoint: %q", got)
	}
	if got := CommonRoot([]string{"/media"}); got != "/media" {
		t.Fatalf("single: %q", got)
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
	want := filepath.Join(data, "config", "overlay.yaml")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSeedXDGSkipsExisting(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "config", "default.yaml"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "public", "index.html"), []byte("pub"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	if err := os.MkdirAll(filepath.Join(data, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "config", "default.yaml"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := seedXDG(src, data); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(data, "config", "default.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep" {
		t.Fatalf("seed overwrote existing: %q", got)
	}
	if _, err := os.Stat(filepath.Join(data, "public", "index.html")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteOverlayServeMediaKeysLast(t *testing.T) {
	extra := filepath.Join(t.TempDir(), "extra.yaml")
	if err := os.WriteFile(extra, []byte("browse_roots: [/media]\nbrowse_root: /media\ndata_dir: /old\nproviders:\n  omdb:\n    base: http://omdb-stub:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SERVEMEDIA_MATCHMEDIA_OVERLAY", extra)
	data := t.TempDir()
	if err := writeOverlay(data, "127.0.0.1:7680", []string{"/", "/mnt"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(data, "config", "overlay.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		BrowseRoot  string   `yaml:"browse_root"`
		BrowseRoots []string `yaml:"browse_roots"`
		DataDir     string   `yaml:"data_dir"`
		HTTP        struct {
			Addr string `yaml:"addr"`
		} `yaml:"http"`
		Providers struct {
			OMDb struct {
				Base string `yaml:"base"`
			} `yaml:"omdb"`
		} `yaml:"providers"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.BrowseRoot != "" {
		t.Fatalf("browse_root should be dropped, got %q", cfg.BrowseRoot)
	}
	if cfg.DataDir != "" {
		t.Fatalf("data_dir should be dropped, got %q", cfg.DataDir)
	}
	if len(cfg.BrowseRoots) != 2 || cfg.BrowseRoots[0] != "/" || cfg.BrowseRoots[1] != "/mnt" {
		t.Fatalf("browse_roots=%#v (ServeMedia keys must win)", cfg.BrowseRoots)
	}
	if cfg.Providers.OMDb.Base != "http://omdb-stub:8080" {
		t.Fatalf("omdb.base=%q", cfg.Providers.OMDb.Base)
	}
	if cfg.HTTP.Addr != "127.0.0.1:7680" {
		t.Fatalf("http.addr=%q", cfg.HTTP.Addr)
	}
}
