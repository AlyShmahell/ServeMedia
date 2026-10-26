package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/backup"
	"github.com/alyshmahell/servemedia/src/internal/config"
	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
	"github.com/alyshmahell/servemedia/src/web"
	"github.com/go-chi/chi/v5"
)

func enginesTestServer(t *testing.T, meta matchmedia.Service) (*Server, *db.User) {
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
	cfg.Scan.OnStartup = true
	cfg.Watchdog.TTLSeconds = 15
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
	if loc := w.Header().Get("Location"); loc != "/" {
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
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleSettingsEngines(w, req)
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, body)
	}
	for _, want := range []string{"settings-sidenav", "MatchMedia", "FFmpeg", "Webhooks", "tab-matchmedia", "secret-omdb", "overlay-yaml", "browse_roots", "Provider keys"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
	for _, gone := range []string{"engines-restart", "Restart MatchMedia", "id=\"engines-status\"", "H.264 CRF", "tab-ffmpeg"} {
		if strings.Contains(body, gone) {
			t.Fatalf("still present %q", gone)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/settings/engines?tab=ffmpeg", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "settings-panel")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w = httptest.NewRecorder()
	s.handleSettingsEngines(w, req)
	body = w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("ffmpeg status %d body %s", w.Code, body)
	}
	for _, want := range []string{"tab-ffmpeg", "H.264 CRF", "Max height", "Hardware acceleration"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, "overlay-yaml") || strings.Contains(body, "settings-sidenav") || strings.Contains(body, "scan_on_startup") {
		t.Fatalf("ffmpeg panel mixed: %s", body)
	}
}

func TestHandleSaveEnginesSecretsAndConfig(t *testing.T) {
	var posted map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"healthy":true}`))
	})
	mux.HandleFunc("/v1/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"providers":{"omdb":{"api_key":"keep"}}}`))
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
	form.Set("secret_tmdb", "new-tmdb")
	form.Set("overlay_yaml", "match:\n  min_score: 0.8\nproviders:\n  tmdb:\n    url: https://api.themoviedb.org/3\n  omdb:\n    url: https://www.omdbapi.com/\n")
	req := httptest.NewRequest(http.MethodPost, "/settings/engines", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleSaveEngines(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	match, _ := posted["match"].(map[string]any)
	if match["min_score"] != 0.8 {
		t.Fatalf("posted %#v", posted)
	}
	providers, _ := posted["providers"].(map[string]any)
	tmdb, _ := providers["tmdb"].(map[string]any)
	omdb, _ := providers["omdb"].(map[string]any)
	if tmdb["api_key"] != "new-tmdb" || omdb["api_key"] != "keep" {
		t.Fatalf("keys dropped %#v", posted)
	}
}

func TestHandleSaveEnginesTMDBKeySurvivesYAML(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "share"))
	t.Setenv("SERVEMEDIA_HOME", "")
	t.Setenv("SERVEMEDIA_MATCHMEDIA_OVERLAY", "")
	engine, err := matchmedia.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.SetSecrets(map[string]string{"omdb": "keep-me"}); err != nil {
		t.Fatal(err)
	}
	s, u := enginesTestServer(t, engine)
	form := url.Values{}
	form.Set("secret_tmdb", "new-tmdb")
	form.Set("overlay_yaml", "providers:\n  tmdb:\n    url: https://api.themoviedb.org/3\n  omdb:\n    url: https://www.omdbapi.com/\n")
	req := httptest.NewRequest(http.MethodPost, "/settings/engines", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "settings-panel")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleSaveEngines(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "(set)") {
		t.Fatalf("key not shown set: %s", w.Body.String())
	}
	status, err := engine.SecretsStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !status["tmdb"] || !status["omdb"] {
		t.Fatalf("secrets %#v", status)
	}
	dir, err := config.MatchMediaDir()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "overlay.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "new-tmdb") || !strings.Contains(text, "keep-me") {
		t.Fatalf("overlay:\n%s", text)
	}
}

func TestHandleRestoreEngines(t *testing.T) {
	tree := t.TempDir()
	t.Setenv("HOME", filepath.Join(tree, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tree, "xdg"))
	t.Setenv("SERVEMEDIA_HOME", "")
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
	dataDir, err := config.MatchMediaDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "overlay.yaml"), []byte("extra: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var posted map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"healthy":true}`))
	})
	mux.HandleFunc("/v1/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"extra":1,"browse_roots":["/mnt"],"providers":{"omdb":{"api_key":"k"}}}`))
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
	req := httptest.NewRequest(http.MethodPost, "/settings/engines/restore", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleRestoreEngines(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if _, ok := posted["http"]; ok {
		t.Fatalf("restore posted sidecar config %#v", posted)
	}
	if _, ok := posted["extra"]; ok {
		t.Fatalf("extra survived %#v", posted)
	}
	if _, ok := posted["browse_roots"]; ok {
		t.Fatalf("browse_roots survived %#v", posted)
	}
	providers, _ := posted["providers"].(map[string]any)
	omdb, _ := providers["omdb"].(map[string]any)
	if omdb["api_key"] != "k" {
		t.Fatalf("api key not kept %#v", posted)
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
	if loc := w.Header().Get("Location"); loc != "/" {
		t.Fatalf("location %q", loc)
	}
}

func TestSaveFFmpegSettings(t *testing.T) {
	s, u := enginesTestServer(t, nil)
	s.Cfg.OverlayPath = filepath.Join(t.TempDir(), "overlay.yaml")
	if err := os.WriteFile(s.Cfg.OverlayPath, []byte("media:\n  path: /keep/media\nstore:\n  path: /old/store\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	form.Set("ffmpeg_settings", "1")
	form.Set("crf", "20")
	form.Set("max_height", "1080")
	form.Set("segment_seconds", "4")
	form.Set("cleanup_hours", "12")
	form.Set("hwaccel", "none")
	req := httptest.NewRequest(http.MethodPost, "/settings/engines", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleSaveEngines(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/" {
		t.Fatalf("location %q", loc)
	}
	if s.Cfg.Transcode.CRF != 20 || s.Cfg.Transcode.MaxHeight != 1080 || s.Cfg.Transcode.HWAccel != "none" {
		t.Fatalf("transcode %+v", s.Cfg.Transcode)
	}
	if !s.Cfg.Scan.OnStartup || s.Cfg.Watchdog.TTLSeconds != 15 {
		t.Fatalf("scan %v ttl %d", s.Cfg.Scan.OnStartup, s.Cfg.Watchdog.TTLSeconds)
	}
	b, err := os.ReadFile(s.Cfg.OverlayPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "/keep/media") || strings.Contains(text, "/old/store") {
		t.Fatalf("overlay:\n%s", text)
	}
}

func TestSaveGeneralSettings(t *testing.T) {
	s, u := enginesTestServer(t, nil)
	s.Cfg.OverlayPath = filepath.Join(t.TempDir(), "overlay.yaml")
	if err := os.WriteFile(s.Cfg.OverlayPath, []byte("media:\n  path: /keep/media\nstore:\n  path: /old/store\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Cfg.Scan.OnStartup = false
	s.Cfg.Watchdog.TTLSeconds = 60
	form := url.Values{}
	form.Set("scan_on_startup", "on")
	form.Set("watchdog_ttl", "0")
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "settings-panel")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleSaveGeneral(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if !s.Cfg.Scan.OnStartup || s.Cfg.Watchdog.TTLSeconds != 0 {
		t.Fatalf("scan %v ttl %d", s.Cfg.Scan.OnStartup, s.Cfg.Watchdog.TTLSeconds)
	}
	body := w.Body.String()
	if !strings.Contains(body, "scan_on_startup") || !strings.Contains(body, `id="watchdog-ttl"`) || strings.Contains(body, "settings-sidenav") {
		t.Fatalf("panel %s", body)
	}
	if !strings.Contains(body, "checked") {
		t.Fatalf("scan checkbox: %s", body)
	}
	b, err := os.ReadFile(s.Cfg.OverlayPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "/keep/media") || strings.Contains(text, "/old/store") {
		t.Fatalf("overlay:\n%s", text)
	}
}

func TestSettingsAndAboutFragments(t *testing.T) {
	s, u := enginesTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w := httptest.NewRecorder()
	s.handleSettings(w, req)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("direct settings %d %q", w.Code, w.Header().Get("Location"))
	}

	req = httptest.NewRequest(http.MethodGet, "/settings", nil)
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w = httptest.NewRecorder()
	s.handleSettings(w, req)
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, body)
	}
	for _, want := range []string{"settings-sidenav", "General", "FFmpeg", "MatchMedia", "Webhooks", "Backup", "Accounts", "scan_on_startup", "aria-current=\"page\""} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, "tab-ffmpeg") || strings.Contains(body, "tab-matchmedia") {
		t.Fatalf("general shell included another panel: %s", body)
	}

	s.Cfg.OverlayPath = filepath.Join(t.TempDir(), "overlay.yaml")
	req = httptest.NewRequest(http.MethodGet, "/settings/integrations", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "settings-panel")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w = httptest.NewRecorder()
	s.handleSettingsIntegrations(w, req)
	body = w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "webhook-api-key") || strings.Contains(body, "settings-sidenav") {
		t.Fatalf("webhooks %d %s", w.Code, body)
	}
	req = httptest.NewRequest(http.MethodGet, "/settings/users", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "settings-panel")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w = httptest.NewRecorder()
	s.handleSettingsUsers(w, req)
	body = w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "add-user-open") || !strings.Contains(body, u.Username) {
		t.Fatalf("accounts %d %s", w.Code, body)
	}
	var backupHTML strings.Builder
	if err := s.Templates.ExecuteTemplate(&backupHTML, "partials/settings_backup.html", map[string]any{
		"Config":   s.Cfg,
		"Archives": []string{"snap.tar"},
		"Status":   backup.Status{},
		"User":     u,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(backupHTML.String(), "backup_enabled") || !strings.Contains(backupHTML.String(), "snap.tar") {
		t.Fatalf("backup %s", backupHTML.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/about", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w = httptest.NewRecorder()
	s.handleAbout(w, req)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("direct about %d %q", w.Code, w.Header().Get("Location"))
	}
	req = httptest.NewRequest(http.MethodGet, "/about", nil)
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w = httptest.NewRecorder()
	s.handleAbout(w, req)
	body = w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "License") || !strings.Contains(body, "Attribution") {
		t.Fatalf("about %d %s", w.Code, body)
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
