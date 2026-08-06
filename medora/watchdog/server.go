package watchdog

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httputil"
	"time"
)

const tokenHeader = "X-Watchdog-Token"

type Server struct {
	cfg        Config
	sessions   *SessionStore
	supervisor *Supervisor
	proxy      *httputil.ReverseProxy
}

func NewServer(cfg Config, sessions *SessionStore, supervisor *Supervisor) *Server {
	return &Server{
		cfg:        cfg,
		sessions:   sessions,
		supervisor: supervisor,
		proxy:      newReverseProxy(cfg.ProxyURL),
	}
}

func (s *Server) Handler() http.Handler {
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/watchdog/session" && r.Method == http.MethodPost:
		s.handleSession(w, r)
	case r.URL.Path == "/watchdog/pulse" && r.Method == http.MethodPost:
		s.handlePulse(w, r)
	case medoraUp(r.Context(), s.cfg.HealthURL):
		s.proxy.ServeHTTP(w, r)
	case r.URL.Path == "/healthz":
		http.Error(w, "starting", http.StatusServiceUnavailable)
	case r.Method == http.MethodGet:
		s.handleWake(w, r)
	default:
		http.Error(w, "Medora is starting", http.StatusServiceUnavailable)
	}
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	resp := s.sessions.Create(time.Now())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handlePulse(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get(tokenHeader)
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	if !s.sessions.Validate(token, time.Now()) {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	s.supervisor.Pulse(time.Now())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleWake(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := renderWakePage(w, s.cfg); err != nil {
		log.Printf("watchdog: wake page: %v", err)
		http.Error(w, "Medora is starting", http.StatusServiceUnavailable)
	}
}
