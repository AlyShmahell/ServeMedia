package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/db"
)

func TestSettingsNetworkSaveResetAndValidation(t *testing.T) {
	s, u := enginesTestServer(t, nil)
	path := filepath.Join(t.TempDir(), "overlay.yaml")
	s.Cfg.OverlayPath = path
	var calls int
	var gotHost string
	var gotPort int
	var disabled bool
	s.SetNetwork(true, "http://127.0.0.1:9", false, NetworkBind{
		Host: "127.0.0.1", Port: "9", URL: "http://127.0.0.1:9",
	}, func(host string, port int, disable bool) (NetworkBind, error) {
		calls++
		gotHost, gotPort, disabled = host, port, disable
		if disable {
			return NetworkBind{Host: "127.0.0.1", Port: "9", URL: "http://127.0.0.1:9"}, nil
		}
		return NetworkBind{
			Host: host, Port: strconv.Itoa(port), URL: "http://" + host + ":" + strconv.Itoa(port),
			Note: "Port 1 is in use; listening on 127.0.0.1:9.", LanUp: true,
		}, nil
	})

	post := func(form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/settings/network", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req = req.WithContext(context.WithValue(req.Context(), userKey, u))
		w := httptest.NewRecorder()
		s.handleSaveNetwork(w, req)
		return w
	}

	w := post(url.Values{"host": {"not-an-ip"}, "port": {"80"}})
	if calls != 0 {
		t.Fatal("invalid address rebound")
	}
	if !strings.Contains(w.Body.String(), "literal address") || !strings.Contains(w.Body.String(), "settings-notice-error") {
		t.Fatalf("body %s", w.Body.String())
	}

	w = post(url.Values{"host": {"127.0.0.1"}, "port": {"8080"}})
	body := w.Body.String()
	if calls != 1 || gotHost != "127.0.0.1" || gotPort != 8080 || disabled {
		t.Fatalf("apply host %s port %d disable %v calls %d", gotHost, gotPort, disabled, calls)
	}
	if !strings.Contains(body, "settings-notice-warn") || !strings.Contains(body, "8080") || !strings.Contains(body, "This window: http://127.0.0.1:9") {
		t.Fatalf("body %s", body)
	}
	if !strings.Contains(body, "stays up while the network listener is on") {
		t.Fatal("missing lan hold note")
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "network:") || !strings.Contains(string(saved), "8080") {
		t.Fatalf("overlay:\n%s", saved)
	}
	s.Cfg.Scan.OnStartup = true
	if err := s.Cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	saved, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "network:") || !strings.Contains(string(saved), "on_startup: true") {
		t.Fatalf("general save dropped network:\n%s", saved)
	}

	w = post(url.Values{"reset": {"1"}})
	if !disabled || s.Cfg.Network != nil {
		t.Fatalf("reset disable %v network %#v", disabled, s.Cfg.Network)
	}
	if !strings.Contains(w.Body.String(), "settings-notice-ok") {
		t.Fatalf("reset body %s", w.Body.String())
	}
	saved, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "network:") {
		t.Fatalf("reset kept network:\n%s", saved)
	}

	req := httptest.NewRequest(http.MethodGet, "/settings/network", nil)
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(context.WithValue(req.Context(), userKey, u))
	w = httptest.NewRecorder()
	s.handleSettingsNetwork(w, req)
	if !strings.Contains(w.Body.String(), "Networks") || !strings.Contains(w.Body.String(), "id=\"network-host\"") {
		t.Fatalf("shell %s", w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/settings/network", strings.NewReader(url.Values{"port": {"80"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	other, err := s.DB.CreateUser(context.Background(), "viewer", "x", db.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(context.WithValue(req.Context(), userKey, other))
	w = httptest.NewRecorder()
	s.handleSaveNetwork(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d", w.Code)
	}
}
