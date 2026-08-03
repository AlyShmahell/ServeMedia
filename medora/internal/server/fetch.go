package server

import (
	"context"
	"net/http"
	"strconv"

	"github.com/alyshmahell/medora/internal/fetch"
	"github.com/alyshmahell/medora/internal/providers"
	"github.com/go-chi/chi/v5"
)

func (s *Server) syncFetchClients() {
	if s.Fetch == nil {
		s.Fetch = &fetch.Worker{DB: s.DB, Store: s.Cfg.Store.Path}
	}
	s.Fetch.DB = s.DB
	s.Fetch.Store = s.Cfg.Store.Path
	s.Fetch.Meta = &providers.Client{Socket: s.Cfg.Providers.Socket}
}

func (s *Server) ownsFetchTarget(r *http.Request, scope string, id int64) bool {
	u := userFrom(r)
	ctx := r.Context()
	switch scope {
	case "movie", "show":
		ok, _ := s.DB.UserOwnsMediaItem(ctx, u.ID, id)
		return ok
	case "episode":
		ok, _ := s.DB.UserOwnsEpisode(ctx, u.ID, id)
		return ok
	case "season":
		ok, _ := s.DB.UserOwnsSeason(ctx, u.ID, id)
		return ok
	default:
		return false
	}
}

func (s *Server) handleMetadataFetch(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	scope := r.FormValue("scope")
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if !s.ownsFetchTarget(r, scope, id) {
		http.NotFound(w, r)
		return
	}
	ready, reason, _ := s.metaStatus()
	if !ready {
		if reason == "" {
			reason = "Metadata service unavailable"
		}
		http.Error(w, reason, 400)
		return
	}
	jobID, err := s.DB.CreateAsyncJob(r.Context(), "metadata", scope, id, "")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	go s.Fetch.Run(context.Background(), jobID)
	j, _ := s.DB.GetAsyncJob(r.Context(), jobID)
	s.render(w, r, "partials/job_progress.html", map[string]any{"Job": j, "Poll": true})
}

func (s *Server) handleAsyncJobStatus(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	j, err := s.DB.GetAsyncJob(r.Context(), id)
	if err != nil || j == nil {
		http.NotFound(w, r)
		return
	}
	if !s.ownsFetchTarget(r, j.Scope, j.TargetID) {
		http.NotFound(w, r)
		return
	}
	poll := j.Status == "running"
	s.render(w, r, "partials/job_progress.html", map[string]any{"Job": j, "Poll": poll})
}

// RefreshFetchConfig reloads API clients after config save / reopen.
func (s *Server) RefreshFetchConfig() {
	s.syncFetchClients()
}
