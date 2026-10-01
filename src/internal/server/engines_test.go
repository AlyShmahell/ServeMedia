package server

import (
	"context"
	"encoding/json"
	"io"
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
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
	"github.com/alyshmahell/servemedia/src/web"
	"github.com/go-chi/chi/v5"
)

func enginesTestServer(t *testing.T, meta *matchmedia.Client) (*Server, *db.User) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	u, err := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	tplFS, err := fs.Sub(web.FS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Transcode.CRF = 23
	cfg.Transcode.MaxHeight = 2160
	s := &Server{Cfg: &cfg, DB: d, Templates: MustParseTemplates(tplFS), Meta: meta}
	return s, u
}

func TestHandleSettingsServerRedirect(t *testing.T) {
	s, u := enginesTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/settings/server", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleSettingsServer(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/settings/engines?tab=ffmpeg" {
		t.Fatalf("location %q", loc)
	}
}

func TestHandleRestartEngines(t *testing.T) {
	s, u := enginesTestServer(t, nil)
	var n int
	s.SetRestartMeta(func() error {
		n++
		return nil
	})
	req := httptest.NewRequest(http.MethodPost, "/settings/engines/restart", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleRestartEngines(w, req)
	if n != 1 {
		t.Fatalf("restart called %d", n)
	}
	if w.Code != http.StatusFound {
		t.Fatalf("status %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/settings/engines/restart", nil)
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w = httptest.NewRecorder()
	s.handleRestartEngines(w, req)
	if n != 2 {
		t.Fatalf("hx restart called %d", n)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("hx status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "engines-status-text") {
		t.Fatalf("status partial: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Off") && !strings.Contains(w.Body.String(), "On") {
		t.Fatalf("power label: %s", w.Body.String())
	}
}

func TestHandleEnginesStatus(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"healthy":true,"version":"0.1.0"}`))
	}))
	t.Cleanup(stub.Close)
	s, u := enginesTestServer(t, &matchmedia.Client{Base: stub.URL, HTTP: stub.Client()})
	req := httptest.NewRequest(http.MethodGet, "/hx/engines/matchmedia/status", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleEnginesStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "engine-power on") || !strings.Contains(body, `id="engines-status-text">On`) {
		t.Fatalf("body %s", body)
	}
	if strings.Contains(body, "MatchMedia ready") {
		t.Fatalf("old ready copy: %s", body)
	}
}

func TestHandleSettingsEnginesTabs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"healthy":true,"version":"0.1.0"}`))
	})
	mux.HandleFunc("/v1/secrets", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"omdb":true,"tmdb":false}`))
	})
	mux.HandleFunc("/v1/config", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"browse_roots":["/mnt"],"providers":{}}`))
	})
	stub := httptest.NewServer(mux)
	t.Cleanup(stub.Close)
	s, u := enginesTestServer(t, &matchmedia.Client{Base: stub.URL, HTTP: stub.Client()})
	req := httptest.NewRequest(http.MethodGet, "/settings/engines", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleSettingsEngines(w, req)
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, body)
	}
	for _, want := range []string{"tab-matchmedia", "tab-ffmpeg", "secret-omdb", "overlay-yaml", "browse_roots", "H.264 CRF", "Max height"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
}

func TestHandleSaveEnginesSecretsAndConfig(t *testing.T) {
	var secrets map[string]string
	var posted map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"healthy":true}`))
	})
	mux.HandleFunc("/v1/secrets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"omdb":false}`))
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&secrets); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
	})
	stub := httptest.NewServer(mux)
	t.Cleanup(stub.Close)
	s, u := enginesTestServer(t, &matchmedia.Client{Base: stub.URL, HTTP: stub.Client()})
	form := url.Values{}
	form.Set("secret_omdb", "k")
	form.Set("overlay_yaml", "match:\n  min_score: 0.8\n")
	req := httptest.NewRequest(http.MethodPost, "/settings/engines", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleSaveEngines(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if secrets["omdb"] != "k" {
		t.Fatalf("secrets %#v", secrets)
	}
	match, _ := posted["match"].(map[string]any)
	if match["min_score"] != 0.8 {
		t.Fatalf("posted %#v", posted)
	}
}

func TestHandleRestoreEngines(t *testing.T) {
	tree := t.TempDir()
	t.Setenv("HOME", filepath.Join(tree, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tree, "xdg"))
	t.Setenv("SERVEMEDIA_ROOT", filepath.Join(tree, "servemedia"))
	t.Setenv("SERVEMEDIA_MATCHMEDIA_OVERLAY", "")
	mmShare := filepath.Join(tree, "matchmedia", "config")
	if err := os.MkdirAll(mmShare, 0o755); err != nil {
		t.Fatal(err)
	}
	seedBody := "http:\n  addr: 127.0.0.1:7680\nbrowse_roots:\n  - /mnt\n"
	if err := os.WriteFile(filepath.Join(mmShare, "default.yaml"), []byte(seedBody), 0o644); err != nil {
		t.Fatal(err)
	}
	dataDir, err := config.MatchMediaXDGDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "config", "overlay.yaml"), []byte("extra: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var posted map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"healthy":true}`))
	})
	mux.HandleFunc("/v1/config", func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &posted); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
	})
	stub := httptest.NewServer(mux)
	t.Cleanup(stub.Close)
	s, u := enginesTestServer(t, &matchmedia.Client{Base: stub.URL, HTTP: stub.Client()})
	req := httptest.NewRequest(http.MethodPost, "/settings/engines/restore", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleRestoreEngines(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dataDir, "config", "overlay.yaml")); !os.IsNotExist(err) {
		t.Fatalf("overlay still present: %v", err)
	}
	httpCfg, _ := posted["http"].(map[string]any)
	if httpCfg["addr"] != "127.0.0.1:7680" {
		t.Fatalf("posted seed %#v", posted)
	}
	if _, ok := posted["extra"]; ok {
		t.Fatalf("extra survived %#v", posted)
	}
}

func TestHandleSaveServerRedirect(t *testing.T) {
	s, u := enginesTestServer(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/settings/server", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleSaveServer(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/settings/engines?tab=ffmpeg" {
		t.Fatalf("location %q", loc)
	}
}

func TestEnginesRoutesRequireAdmin(t *testing.T) {
	s, _ := enginesTestServer(t, nil)
	ctx := context.Background()
	user, err := s.DB.CreateUser(ctx, "bob", "x", db.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	rtr := chi.NewRouter()
	rtr.Use(s.requireAdmin)
	rtr.Get("/settings/engines", s.handleSettingsEngines)
	req := httptest.NewRequest(http.MethodGet, "/settings/engines", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, user))
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d", w.Code)
	}
}
