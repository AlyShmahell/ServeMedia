package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeSubtitleVTTShiftsCues(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sub-0.vtt")
	if err := os.WriteFile(file, []byte("WEBVTT\n\n00:30.000 --> 00:32.000\nhi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/play/movie/1/sub/0.vtt?shift=10", nil)
	s.serveSubtitleVTT(rec, req, file)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/vtt; charset=utf-8" {
		t.Fatalf("content-type %q", got)
	}
	if !strings.Contains(rec.Body.String(), "00:00:20.000 --> 00:00:22.000") {
		t.Fatalf("body %q", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=86400" {
		t.Fatalf("cache %q", got)
	}
}

func TestServeSubtitleVTTShiftErrorIsNotCached(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sub-0.vtt")
	line := strings.Repeat("a", 4*1024*1024+8)
	if err := os.WriteFile(file, []byte("WEBVTT\n\n"+line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/play/movie/1/sub/0.vtt?shift=10", nil)
	s.serveSubtitleVTT(rec, req, file)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("cache %q", got)
	}
}

func TestServeSubtitleVTTWithoutShiftServesOriginal(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sub-0.vtt")
	want := "WEBVTT\n\n00:30.000 --> 00:32.000\nhi\n"
	if err := os.WriteFile(file, []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/play/movie/1/sub/0.vtt", nil)
	s.serveSubtitleVTT(rec, req, file)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Body.String() != want {
		t.Fatalf("body %q want %q", rec.Body.String(), want)
	}
}
