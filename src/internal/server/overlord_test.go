package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyshmahell/servemedia/src/internal/config"
	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/web"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	cfg := &config.Config{}
	cfg.Store.Path = dir
	s, err := New(cfg, d, nil, nil, web.FS, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOverlordSkipsSetupAndHidesLogout(t *testing.T) {
	s := testServer(t)
	s.Overlord = true
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, `action="/logout"`) || strings.Contains(body, "topbar-logout") {
		t.Fatal("logout form present")
	}
	if !strings.Contains(body, "ServeMedia") {
		t.Fatal("home page missing")
	}
	users, err := s.DB.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || !db.IsOverlord(users[0].Username) || users[0].Role != db.RoleAdmin {
		t.Fatalf("users %+v", users)
	}
	visible, err := s.accountUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 0 {
		t.Fatalf("accounts table %+v", visible)
	}
}

func TestNoUsersRedirectsToRegister(t *testing.T) {
	s := testServer(t)
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusFound {
		t.Fatalf("status %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/register" {
		t.Fatalf("location %q", loc)
	}
}

func TestLoginRejectsOverlord(t *testing.T) {
	s := testServer(t)
	if _, err := s.DB.CreateUser(context.Background(), "overlord", "secret", db.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"username": {"overlord"}, "password": {"secret"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Invalid credentials") {
		t.Fatalf("body %s", w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			t.Fatal("session cookie set")
		}
	}
}
