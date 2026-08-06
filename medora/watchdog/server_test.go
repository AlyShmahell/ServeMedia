package watchdog

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServerWatchdogPulseRoutes(t *testing.T) {
	cfg := LoadConfig()
	// Use a port nothing listens on so tests pass even when Medora runs on the host during image build.
	cfg.HealthURL = "http://127.0.0.1:1/healthz"
	cfg.ProxyURL = "http://127.0.0.1:1"
	sessions := NewSessionStore(time.Hour)
	srv := NewServer(cfg, sessions, NewSupervisor(cfg))

	t.Run("session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/watchdog/session", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("session status = %d", rec.Code)
		}
	})

	t.Run("pulse missing token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/watchdog/pulse", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("pulse status = %d", rec.Code)
		}
	})

	t.Run("wake page when medora down", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("wake status = %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Medora is waking up") {
			t.Fatal("expected wake page body")
		}
		if !strings.Contains(body, "wake-card") {
			t.Fatal("expected wake card layout")
		}
	})

	t.Run("healthz 503 when medora down", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("healthz status = %d", rec.Code)
		}
	})
}
