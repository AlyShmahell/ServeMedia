package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/alyshmahell/servemedia/internal/fetch"
	"github.com/alyshmahell/servemedia/internal/matchmedia"
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
	var job matchmedia.Job
	var jobErr string
	session := ""
	if it.MatchMediaSessionID.Valid {
		session = strings.TrimSpace(it.MatchMediaSessionID.String)
	}
	if s.Meta != nil && it.MatchMediaJobID.Valid && it.MatchMediaJobID.String != "" && session != "" {
		j, err := s.Meta.Job(session, it.MatchMediaJobID.String)
		if err != nil {
			jobErr = "no MatchMedia job for this title — rescan to match again"
		} else {
			job = j
			if job.Match != nil {
				job.Match.Poster = s.Meta.ResolveURL(job.Match.Poster, session)
			}
			for i := range job.Candidates {
				job.Candidates[i].Poster = s.Meta.ResolveURL(job.Candidates[i].Poster, session)
			}
		}
	} else {
		jobErr = "no MatchMedia job for this title — rescan to match again"
	}
	cands := pickerCandidates(job)
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
	if r.FormValue("skip") == "1" || r.FormValue("skip") == "on" || r.FormValue("skip") == "true" {
		if err := s.DB.SkipMatchMedia(r.Context(), it.ID); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusOK)
		return
	}
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

// pickerCandidates includes job.Match when MatchMedia only set the winner.
func pickerCandidates(job matchmedia.Job) []matchmedia.Candidate {
	cands := append([]matchmedia.Candidate(nil), job.Candidates...)
	if job.Match == nil {
		return cands
	}
	m := *job.Match
	if strings.TrimSpace(m.Provider) == "" || strings.TrimSpace(m.ID) == "" {
		return cands
	}
	for _, c := range cands {
		if strings.EqualFold(strings.TrimSpace(c.Provider), strings.TrimSpace(m.Provider)) && strings.TrimSpace(c.ID) == strings.TrimSpace(m.ID) {
			return cands
		}
	}
	return append([]matchmedia.Candidate{m}, cands...)
}

// RefreshFetchConfig re-syncs the MatchMedia-backed fetch worker after config save / reopen.
func (s *Server) RefreshFetchConfig() {
	s.syncFetchClients()
}
