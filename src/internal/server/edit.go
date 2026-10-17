package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/media"
	"github.com/go-chi/chi/v5"
)

var (
	videoDuration     = media.VideoDuration
	extractStillAt    = media.ExtractStillJPEGAt
	randomStillOffset = media.RandomStillOffset
)

func formPtr(r *http.Request, key string) *string {
	if _, ok := r.PostForm[key]; !ok {
		return nil
	}
	v := r.PostForm.Get(key)
	if key == "title" {
		v = strings.TrimSpace(v)
	}
	return &v
}

func writeEditJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func editErr(w http.ResponseWriter, status int, err error) {
	msg := "error"
	if err != nil {
		msg = err.Error()
	}
	writeEditJSON(w, status, map[string]string{"error": msg})
}

func posterSrc(rel string) string {
	p := strings.TrimPrefix(strings.TrimPrefix(rel, "/"), "metadata/")
	return "/metadata/" + p + "?m=" + strconv.FormatInt(time.Now().Unix(), 10)
}

func (s *Server) readPosterFile(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPosterUpload)
	if err := r.ParseMultipartForm(maxPosterUpload); err != nil {
		return nil, fmt.Errorf("image too large")
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("missing file")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPosterUpload+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxPosterUpload {
		return nil, fmt.Errorf("image too large")
	}
	return data, nil
}

const maxPosterUpload = 8 << 20

func (s *Server) requireFetch() error {
	if s.Fetch == nil {
		return fmt.Errorf("unavailable")
	}
	return nil
}

func (s *Server) ownedSeason(r *http.Request, showID int64, num int) (*db.MediaItem, *db.Season) {
	show := s.ownedMediaItem(r, showID)
	if show == nil || show.Kind != "show" {
		return nil, nil
	}
	sn, err := s.DB.GetSeasonByShowNum(r.Context(), show.ID, num)
	if err != nil || sn == nil {
		return nil, nil
	}
	return show, sn
}

func (s *Server) handleMediaMeta(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	it := s.ownedMediaItem(r, id)
	if it == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.requireFetch(); err != nil {
		editErr(w, http.StatusServiceUnavailable, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	title, plotPtr := formPtr(r, "title"), formPtr(r, "plot")
	if err := s.Fetch.EditMediaMeta(r.Context(), it, title, plotPtr); err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	s.syncMediaNode(r.Context(), id, title, plotPtr)
	it, err := s.DB.GetMediaItem(r.Context(), id)
	if err != nil || it == nil {
		editErr(w, http.StatusInternalServerError, err)
		return
	}
	plot := ""
	if it.Plot.Valid {
		plot = it.Plot.String
	}
	writeEditJSON(w, http.StatusOK, map[string]any{"title": it.Title, "plot": plot})
}

func (s *Server) handleMediaPoster(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	it := s.ownedMediaItem(r, id)
	if it == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.requireFetch(); err != nil {
		editErr(w, http.StatusServiceUnavailable, err)
		return
	}
	data, err := s.readPosterFile(w, r)
	if err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	rel, err := s.Fetch.EditMediaPoster(r.Context(), it, data)
	if err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	writeEditJSON(w, http.StatusOK, map[string]any{"src": posterSrc(rel)})
}

func (s *Server) handleSeasonMeta(w http.ResponseWriter, r *http.Request) {
	showID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	num, _ := strconv.Atoi(chi.URLParam(r, "n"))
	show, season := s.ownedSeason(r, showID, num)
	if show == nil || season == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.requireFetch(); err != nil {
		editErr(w, http.StatusServiceUnavailable, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	titlePtr, plotPtr := formPtr(r, "title"), formPtr(r, "plot")
	if err := s.Fetch.EditSeasonMeta(r.Context(), show, season, titlePtr, plotPtr); err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	s.syncSeasonNode(r.Context(), show, num, titlePtr, plotPtr)
	season, err := s.DB.GetSeason(r.Context(), season.ID)
	if err != nil || season == nil {
		editErr(w, http.StatusInternalServerError, err)
		return
	}
	title, plot := "", ""
	if season.Title.Valid {
		title = season.Title.String
	}
	if season.Plot.Valid {
		plot = season.Plot.String
	}
	writeEditJSON(w, http.StatusOK, map[string]any{"title": title, "plot": plot})
}

func (s *Server) handleSeasonPoster(w http.ResponseWriter, r *http.Request) {
	showID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	num, _ := strconv.Atoi(chi.URLParam(r, "n"))
	show, season := s.ownedSeason(r, showID, num)
	if show == nil || season == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.requireFetch(); err != nil {
		editErr(w, http.StatusServiceUnavailable, err)
		return
	}
	data, err := s.readPosterFile(w, r)
	if err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	rel, err := s.Fetch.EditSeasonPoster(r.Context(), show, season, data)
	if err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	writeEditJSON(w, http.StatusOK, map[string]any{"src": posterSrc(rel)})
}

func (s *Server) handleEpisodeMeta(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ep := s.ownedEpisode(r, id)
	if ep == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.requireFetch(); err != nil {
		editErr(w, http.StatusServiceUnavailable, err)
		return
	}
	show, err := s.DB.GetMediaItem(r.Context(), ep.ShowID)
	if err != nil || show == nil {
		http.NotFound(w, r)
		return
	}
	season, err := s.DB.GetSeason(r.Context(), ep.SeasonID)
	if err != nil || season == nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	titlePtr, plotPtr := formPtr(r, "title"), formPtr(r, "plot")
	if err := s.Fetch.EditEpisodeMeta(r.Context(), show, season, ep, titlePtr, plotPtr); err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	s.syncEpisodeNode(r.Context(), show, ep, titlePtr, plotPtr)
	ep, err = s.DB.GetEpisode(r.Context(), id)
	if err != nil || ep == nil {
		editErr(w, http.StatusInternalServerError, err)
		return
	}
	title, plot := "", ""
	if ep.Title.Valid {
		title = ep.Title.String
	}
	if ep.Plot.Valid {
		plot = ep.Plot.String
	}
	writeEditJSON(w, http.StatusOK, map[string]any{"title": title, "plot": plot})
}

func (s *Server) syncMediaNode(ctx context.Context, itemID int64, title, plot *string) {
	nodeID, err := s.DB.MediaItemNodeID(ctx, itemID)
	if err != nil || nodeID == 0 {
		return
	}
	_ = s.DB.SetNodeDisplay(ctx, nodeID, false, title, plot)
}

func (s *Server) syncSeasonNode(ctx context.Context, show *db.MediaItem, seasonNum int, title, plot *string) {
	groupID, err := s.DB.MediaItemNodeID(ctx, show.ID)
	if err != nil || groupID == 0 {
		return
	}
	rel, ok := s.DB.SeasonDirRel(ctx, groupID, seasonNum)
	if !ok {
		return
	}
	nodeID, err := s.DB.NodeIDByRel(ctx, show.LibraryID, rel)
	if err != nil || nodeID == 0 {
		return
	}
	_ = s.DB.SetNodeDisplay(ctx, nodeID, false, title, plot)
}

func (s *Server) syncEpisodeNode(ctx context.Context, show *db.MediaItem, ep *db.Episode, title, plot *string) {
	root, err := s.DB.LibraryPath(ctx, show.LibraryID)
	if err != nil || root == "" {
		return
	}
	rel, err := filepath.Rel(root, ep.Path)
	if err != nil {
		return
	}
	nodeID, err := s.DB.NodeIDByRel(ctx, show.LibraryID, filepath.ToSlash(rel))
	if err != nil || nodeID == 0 {
		return
	}
	_ = s.DB.SetNodeDisplay(ctx, nodeID, true, title, plot)
}

func (s *Server) handleEpisodePoster(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ep := s.ownedEpisode(r, id)
	if ep == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.requireFetch(); err != nil {
		editErr(w, http.StatusServiceUnavailable, err)
		return
	}
	show, err := s.DB.GetMediaItem(r.Context(), ep.ShowID)
	if err != nil || show == nil {
		http.NotFound(w, r)
		return
	}
	season, err := s.DB.GetSeason(r.Context(), ep.SeasonID)
	if err != nil || season == nil {
		http.NotFound(w, r)
		return
	}
	data, err := s.readPosterFile(w, r)
	if err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	rel, err := s.Fetch.EditEpisodePoster(r.Context(), show, season, ep, data)
	if err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	writeEditJSON(w, http.StatusOK, map[string]any{"src": posterSrc(rel)})
}

func (s *Server) handleMediaPosterDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	it := s.ownedMediaItem(r, id)
	if it == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.requireFetch(); err != nil {
		editErr(w, http.StatusServiceUnavailable, err)
		return
	}
	if err := s.Fetch.RemoveMediaPoster(r.Context(), it); err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	writeEditJSON(w, http.StatusOK, map[string]any{"src": "/static/placeholder.svg"})
}

func (s *Server) handleSeasonPosterDelete(w http.ResponseWriter, r *http.Request) {
	showID, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	num, _ := strconv.Atoi(chi.URLParam(r, "n"))
	show, season := s.ownedSeason(r, showID, num)
	if show == nil || season == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.requireFetch(); err != nil {
		editErr(w, http.StatusServiceUnavailable, err)
		return
	}
	if err := s.Fetch.RemoveSeasonPoster(r.Context(), show, season); err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	writeEditJSON(w, http.StatusOK, map[string]any{"src": "/static/placeholder.svg"})
}

func (s *Server) handleEpisodePosterDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ep := s.ownedEpisode(r, id)
	if ep == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.requireFetch(); err != nil {
		editErr(w, http.StatusServiceUnavailable, err)
		return
	}
	show, err := s.DB.GetMediaItem(r.Context(), ep.ShowID)
	if err != nil || show == nil {
		http.NotFound(w, r)
		return
	}
	season, err := s.DB.GetSeason(r.Context(), ep.SeasonID)
	if err != nil || season == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.Fetch.RemoveEpisodePoster(r.Context(), show, season, ep); err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	writeEditJSON(w, http.StatusOK, map[string]any{"src": "/static/placeholder.svg"})
}

func (s *Server) stillJPEG(src string) ([]byte, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, fmt.Errorf("missing video")
	}
	dur, err := videoDuration(src)
	if err != nil {
		dur = 0
	}
	tmp, err := os.CreateTemp("", "servemedia-still-*.jpg")
	if err != nil {
		return nil, err
	}
	name := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(name)
	if err := extractStillAt(src, name, randomStillOffset(dur)); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty still")
	}
	return data, nil
}

func (s *Server) writeStillJPEG(w http.ResponseWriter, src string) {
	data, err := s.stillJPEG(src)
	if err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleEpisodePosterGenerate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ep := s.ownedEpisode(r, id)
	if ep == nil {
		http.NotFound(w, r)
		return
	}
	s.writeStillJPEG(w, ep.Path)
}

func (s *Server) handleNodeStill(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	node, lib := s.ownedLibraryNode(r, id)
	if node == nil || lib == nil || node.NodeKind != "file" {
		http.NotFound(w, r)
		return
	}
	if err := s.requireFetch(); err != nil {
		editErr(w, http.StatusServiceUnavailable, err)
		return
	}
	data, err := s.readPosterFile(w, r)
	if err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	abs := filepath.Join(lib.Path, filepath.FromSlash(node.RelPath))
	if epID, err := s.DB.EpisodeIDByPath(r.Context(), abs); err == nil && epID > 0 {
		ep := s.ownedEpisode(r, epID)
		if ep == nil {
			http.NotFound(w, r)
			return
		}
		show, err := s.DB.GetMediaItem(r.Context(), ep.ShowID)
		if err != nil || show == nil {
			http.NotFound(w, r)
			return
		}
		season, err := s.DB.GetSeason(r.Context(), ep.SeasonID)
		if err != nil || season == nil {
			http.NotFound(w, r)
			return
		}
		rel, err := s.Fetch.EditEpisodePoster(r.Context(), show, season, ep, data)
		if err != nil {
			editErr(w, http.StatusBadRequest, err)
			return
		}
		_ = s.DB.SetNodeStill(r.Context(), node.ID, rel)
		writeEditJSON(w, http.StatusOK, map[string]any{"src": posterSrc(rel)})
		return
	}
	rel, err := s.Fetch.SaveNodeStill(r.Context(), node.ID, data)
	if err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	writeEditJSON(w, http.StatusOK, map[string]any{"src": posterSrc(rel)})
}

func (s *Server) handleNodeStillDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	node, lib := s.ownedLibraryNode(r, id)
	if node == nil || lib == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.requireFetch(); err != nil {
		editErr(w, http.StatusServiceUnavailable, err)
		return
	}
	abs := filepath.Join(lib.Path, filepath.FromSlash(node.RelPath))
	if epID, err := s.DB.EpisodeIDByPath(r.Context(), abs); err == nil && epID > 0 {
		ep := s.ownedEpisode(r, epID)
		if ep != nil {
			if show, err := s.DB.GetMediaItem(r.Context(), ep.ShowID); err == nil && show != nil {
				if season, err := s.DB.GetSeason(r.Context(), ep.SeasonID); err == nil && season != nil {
					_ = s.Fetch.RemoveEpisodePoster(r.Context(), show, season, ep)
				}
			}
		}
	}
	if err := s.Fetch.RemoveNodeStill(r.Context(), node); err != nil {
		editErr(w, http.StatusBadRequest, err)
		return
	}
	writeEditJSON(w, http.StatusOK, map[string]any{"src": "/static/placeholder.svg"})
}

func (s *Server) handleNodeStillGenerate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	node, lib := s.ownedLibraryNode(r, id)
	if node == nil || lib == nil || node.NodeKind != "file" {
		http.NotFound(w, r)
		return
	}
	s.writeStillJPEG(w, filepath.Join(lib.Path, filepath.FromSlash(node.RelPath)))
}
