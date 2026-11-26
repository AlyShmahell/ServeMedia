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
	SetBigPicture(false)
	s := testServer(t)
	s.Overlord = true
	w := httptest.NewRecorder()
	WithLoopback(s.Router()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
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
	if strings.Contains(body, `id="topbar-menu"`) {
		t.Fatal("burger still present")
	}
	tag := bigPictureTag(body)
	if tag == "" || !strings.Contains(tag, " hidden") || !strings.Contains(tag, `data-webkit="1"`) || !strings.Contains(tag, `aria-pressed="false"`) || !strings.Contains(tag, `title="Big picture"`) {
		t.Fatalf("big picture button %q", tag)
	}
	settings := strings.Index(body, `id="settings-open"`)
	about := strings.Index(body, `id="about-open"`)
	big := strings.Index(body, `id="big-picture"`)
	logo := strings.Index(body, `class="topbar-logo"`)
	if settings < 0 || about < settings || big < about || logo < big {
		t.Fatal("top bar buttons are not before the logo")
	}
	if !strings.Contains(body, `id="settings-open" aria-label="Settings" title="Settings"`) || !strings.Contains(body, `id="about-open" aria-label="About" title="About"`) {
		t.Fatal("settings or about tooltip missing")
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

func TestBigPictureButtonInBrowser(t *testing.T) {
	SetBigPicture(true)
	t.Cleanup(func() { SetBigPicture(false) })
	s := testServer(t)
	u, err := s.DB.CreateUser(context.Background(), "admin", "x", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.DB.CreateSession(context.Background(), u.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, `id="topbar-menu"`) {
		t.Fatal("burger still present")
	}
	tag := bigPictureTag(body)
	if tag == "" || !strings.Contains(tag, " hidden") || strings.Contains(tag, `data-webkit=`) || !strings.Contains(tag, `aria-pressed="false"`) || !strings.Contains(tag, `title="Big picture"`) {
		t.Fatalf("browser big picture button %q", tag)
	}
	if !strings.Contains(body, `title="Settings"`) || !strings.Contains(body, `title="About"`) {
		t.Fatal("settings or about tooltip missing")
	}
}

func TestBigPicturePressedInWebKit(t *testing.T) {
	SetBigPicture(true)
	t.Cleanup(func() { SetBigPicture(false) })
	s := testServer(t)
	s.Overlord = true
	w := httptest.NewRecorder()
	WithLoopback(s.Router()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	tag := bigPictureTag(body)
	if tag == "" || strings.Contains(tag, " hidden") || !strings.Contains(tag, `data-webkit="1"`) || !strings.Contains(tag, `aria-pressed="true"`) {
		t.Fatalf("webkit big picture row %q", tag)
	}
}

func bigPictureTag(body string) string {
	i := strings.Index(body, `id="big-picture"`)
	if i < 0 {
		return ""
	}
	start := strings.LastIndex(body[:i], "<button")
	if start < 0 {
		start = i
	}
	end := strings.Index(body[i:], ">")
	if end < 0 {
		return body[start:]
	}
	return body[start : i+end]
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

func TestOverlordStaysOffTheNetwork(t *testing.T) {
	s := testServer(t)
	s.Overlord = true
	if _, err := s.DB.EnsureOverlord(context.Background()); err != nil {
		t.Fatal(err)
	}
	plain := func(req *http.Request) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, req)
		return w
	}
	w := plain(httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/login" {
		t.Fatalf("plain status %d location %q body %s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	fwd := httptest.NewRequest(http.MethodGet, "/", nil)
	fwd.Header.Set("X-Forwarded-For", "127.0.0.1")
	w = plain(fwd)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/login" {
		t.Fatalf("forwarded status %d location %q", w.Code, w.Header().Get("Location"))
	}
	u, err := s.DB.GetUserByUsername(context.Background(), "overlord")
	if err != nil || u == nil {
		t.Fatal(err)
	}
	token, err := s.DB.CreateSession(context.Background(), u.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	sess := httptest.NewRequest(http.MethodGet, "/", nil)
	sess.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	w = plain(sess)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/login" {
		t.Fatalf("session status %d location %q", w.Code, w.Header().Get("Location"))
	}
}

func TestRegisterRejectsOverlord(t *testing.T) {
	s := testServer(t)
	form := url.Values{"username": {"overlord"}, "password": {"secret"}, "confirm": {"secret"}}
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Invalid input") {
		t.Fatalf("body %s", w.Body.String())
	}
	u, err := s.DB.GetUserByUsername(context.Background(), "overlord")
	if err != nil {
		t.Fatal(err)
	}
	if u != nil {
		t.Fatal("overlord user created")
	}
}
