package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/alyshmahell/medora/internal/fetch"
	"github.com/alyshmahell/medora/internal/matchora"
	"github.com/go-chi/chi/v5"
)

func (s *Server) syncFetchClients() {
	if s.Fetch == nil {
		s.Fetch = &fetch.Worker{DB: s.DB, Store: s.Cfg.Store.Path}
	}
	s.Fetch.DB = s.DB
	s.Fetch.Store = s.Cfg.Store.Path
	s.Fetch.Meta = s.Meta
}

func (s *Server) handleMatchGet(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	it := s.ownedMediaItem(r, id)
	if it == nil {
		http.NotFound(w, r)
		return
	}
	s.syncFetchClients()
	var job matchora.Job
	var jobErr string
	session := ""
	if it.MatchoraSessionID.Valid {
		session = strings.TrimSpace(it.MatchoraSessionID.String)
	}
	if s.Meta != nil && it.MatchoraJobID.Valid && it.MatchoraJobID.String != "" && session != "" {
		j, err := s.Meta.Job(session, it.MatchoraJobID.String)
		if err != nil {
			jobErr = "no Matchora job for this title — rescan to match again"
		} else {
			job = j
			for i := range job.Candidates {
				job.Candidates[i].Poster = s.Meta.ResolveURL(job.Candidates[i].Poster, session)
			}
		}
	} else {
		jobErr = "no Matchora job for this title — rescan to match again"
	}
	cands := append([]matchora.Candidate(nil), job.Candidates...)
	sort.Slice(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })
	s.render(w, r, "partials/match_dialog.html", map[string]any{
		"Item": it, "Job": job, "Candidates": cands, "Error": jobErr,
	})
}

func (s *Server) handleMatchPost(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	it := s.ownedMediaItem(r, id)
	if it == nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	provider := strings.TrimSpace(r.FormValue("provider"))
	candID := strings.TrimSpace(r.FormValue("id"))
	if provider == "" || candID == "" {
		http.Error(w, "provider and id required", 400)
		return
	}
	s.syncFetchClients()
	persist := r.FormValue("persist") != "0"
	if err := s.Fetch.ApplySelect(r.Context(), it, provider, candID, persist); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

// RefreshFetchConfig re-syncs the Matchora-backed fetch worker after config save / reopen.
func (s *Server) RefreshFetchConfig() {
	s.syncFetchClients()
}
