package matchora

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
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"session":"20260829T122800Z-a1b2c3d4e5f6g7h8","files":3}`))
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client()}
	got, err := c.Scan("/media/tv")
	if err != nil || got.Files != 3 || got.Session != "20260829T122800Z-a1b2c3d4e5f6g7h8" || gotPath != "/media/tv" {
		t.Fatalf("got=%+v path=%q err=%v", got, gotPath, err)
	}
}

func TestScanMissingSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"files":3}`))
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client()}
	if _, err := c.Scan("/media/tv"); err == nil {
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

func TestWriteOverlayMedoraKeysLast(t *testing.T) {
	extra := filepath.Join(t.TempDir(), "extra.yaml")
	if err := os.WriteFile(extra, []byte("browse_root: /media\nllama:\n  host: stub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDORA_MATCHORA_OVERLAY", extra)
	home := t.TempDir()
	data := t.TempDir()
	if err := writeOverlay(home, data, "127.0.0.1:7680", "/"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "data", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		BrowseRoot string `yaml:"browse_root"`
		DataDir    string `yaml:"data_dir"`
		HTTP       struct {
			Addr string `yaml:"addr"`
		} `yaml:"http"`
		Llama struct {
			Host string `yaml:"host"`
		} `yaml:"llama"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.BrowseRoot != "/" {
		t.Fatalf("browse_root=%q want / (Medora key must win)", cfg.BrowseRoot)
	}
	if cfg.Llama.Host != "stub" {
		t.Fatalf("llama.host=%q", cfg.Llama.Host)
	}
	if cfg.HTTP.Addr != "127.0.0.1:7680" {
		t.Fatalf("http.addr=%q", cfg.HTTP.Addr)
	}
	if cfg.DataDir != data {
		t.Fatalf("data_dir=%q", cfg.DataDir)
	}
}
