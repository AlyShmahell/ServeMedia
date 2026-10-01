package server

import (
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func serveFileRange(w http.ResponseWriter, r *http.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, "stat failed", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

func (s *Server) handleStreamMovie(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	it := s.ownedMediaItem(r, id)
	if it == nil {
		http.NotFound(w, r)
		return
	}
	serveFileRange(w, r, it.Path)
}

func (s *Server) handleStreamEpisode(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ep := s.ownedEpisode(r, id)
	if ep == nil {
		http.NotFound(w, r)
		return
	}
	serveFileRange(w, r, ep.Path)
}
