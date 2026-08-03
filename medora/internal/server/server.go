package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alyshmahell/medora/internal/backup"
	"github.com/alyshmahell/medora/internal/config"
	"github.com/alyshmahell/medora/internal/db"
	"github.com/alyshmahell/medora/internal/fetch"
	"github.com/alyshmahell/medora/internal/media"
	"github.com/alyshmahell/medora/internal/metadata"
	"github.com/alyshmahell/medora/internal/scanner"
	"github.com/alyshmahell/medora/internal/stream"
	"github.com/alyshmahell/medora/internal/providers"
	"github.com/alyshmahell/medora/internal/transcode"
	"github.com/alyshmahell/medora/internal/version"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const sessionCookie = "medora_session"

type Server struct {
	Cfg       *config.Config
	DB        *db.DB
	Backup    *backup.Service
	Scanner   *scanner.Scanner
	Fetch     *fetch.Worker
	Transcode *transcode.Manager
	Templates *template.Template
	Static    fs.FS
	reopen    func() error
}

type ctxKey int

const userKey ctxKey = 1

func New(cfg *config.Config, database *db.DB, bak *backup.Service, sc *scanner.Scanner, tr *transcode.Manager, webFS fs.FS, reopen func() error) (*Server, error) {
	tplFS, err := fs.Sub(webFS, "templates")
	if err != nil {
		return nil, err
	}
	staticFS, err := fs.Sub(webFS, "static")
	if err != nil {
		return nil, err
	}
	tpl := MustParseTemplates(tplFS)
	fw := &fetch.Worker{
		DB:    database,
		Store: cfg.Store.Path,
		Meta:  &providers.Client{Socket: cfg.Providers.Socket},
	}
	return &Server{
		Cfg: cfg, DB: database, Backup: bak, Scanner: sc, Fetch: fw, Transcode: tr,
		Templates: tpl, Static: staticFS, reopen: reopen,
	}, nil
}

func MustParseTemplates(fsys fs.FS) *template.Template {
	t := template.New("").Funcs(template.FuncMap{
		"year": func(n interface{}) string {
			switch v := n.(type) {
			case int64:
				if v == 0 {
					return ""
				}
				return strconv.FormatInt(v, 10)
			case int:
				if v == 0 {
					return ""
				}
				return strconv.Itoa(v)
			default:
				return fmt.Sprint(n)
			}
		},
		"posterURL": func(p string, metaID ...string) string {
			p = strings.TrimPrefix(strings.TrimSpace(p), "/")
			p = strings.TrimPrefix(p, "metadata/")
			if p == "" {
				return "/static/placeholder.svg"
			}
			out := "/metadata/" + p
			if len(metaID) > 0 {
				if m := strings.TrimSpace(metaID[0]); m != "" {
					out += "?m=" + url.QueryEscape(m)
				}
			}
			return out
		},
		"queryEscape": url.QueryEscape,
		"joinPath":    filepath.Join,
		"join":        func(parts []string) string {
			var out []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					out = append(out, p)
				}
			}
			return strings.Join(out, ", ")
		},
		"mediaActions": func(scope string, id int64, metaReady bool, metaDisabledReason string) map[string]any {
			return map[string]any{"Scope": scope, "ID": id, "MetaReady": metaReady, "MetaDisabledReason": metaDisabledReason}
		},
		"mediaActionsStatic": func(scope string, id int64, metaReady bool, metaDisabledReason string) map[string]any {
			return map[string]any{"Scope": scope, "ID": id, "MetaReady": metaReady, "MetaDisabledReason": metaDisabledReason, "Static": true}
		},
	})
	return template.Must(t.ParseFS(fsys, "*.html", "*/*.html"))
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Logger, middleware.Recoverer)
	r.Use(s.authMiddleware)

	r.Get("/healthz", s.handleHealthz)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(s.Static))))

	r.Get("/register", s.handleRegisterGet)
	r.Post("/register", s.handleRegisterPost)
	r.Get("/login", s.handleLoginGet)
	r.Post("/login", s.handleLoginPost)
	r.Post("/logout", s.handleLogout)

	r.Group(func(r chi.Router) {
		r.Use(s.requireUser)
		r.Get("/", s.handleHome)
		r.Get("/libraries", s.handleLibraries)
		r.Get("/libraries/{id}", s.handleLibrary)
		r.Get("/movies/{id}", s.handleMovie)
		r.Get("/shows/{id}", s.handleShow)
		r.Get("/shows/{id}/seasons/{n}", s.handleSeason)
		r.Get("/play/movie/{id}", s.handlePlayMovie)
		r.Get("/play/episode/{id}", s.handlePlayEpisode)
		r.Get("/about", s.handleAbout)
			r.Get("/settings", s.handleSettings)
			r.Get("/settings/libraries", s.handleSettingsLibrariesRedirect)
		r.Get("/hx/libraries/{id}/items", s.handleHXItems)
		r.Get("/hx/search", s.handleHXSearch)
		r.Get("/hx/home/continue", s.handleHXContinue)
		r.Post("/hx/progress", s.handleProgress)
		r.Post("/hx/playback-prefs", s.handlePlaybackPrefs)
		r.Post("/hx/libraries", s.handleCreateLibrary)
		r.Post("/hx/libraries/{id}/scan", s.handleScanLibrary)
		r.Get("/hx/scan/{id}/status", s.handleScanStatus)
		r.Get("/hx/scan/{id}/entry-status", s.handleEntryScanStatus)
		r.Post("/hx/media/{id}/scan", s.handleScanMedia)
		r.Post("/hx/libraries/{id}", s.handleRenameLibrary)
		r.Delete("/hx/libraries/{id}", s.handleDeleteLibrary)
		r.Get("/hx/media/browse", s.handleMediaBrowse)
		r.Post("/hx/fetch/metadata", s.handleMetadataFetch)
		r.Get("/hx/jobs/{id}/status", s.handleAsyncJobStatus)
		r.Get("/metadata/*", s.handleMetadata)
		r.Get("/stream/movie/{id}", s.handleStreamMovie)
		r.Get("/stream/episode/{id}", s.handleStreamEpisode)
		r.Post("/play/{kind}/{id}/session", s.handlePlaySession)
		r.Get("/play/{kind}/{id}/sub/{index}.vtt", s.handleSubtitleVTT)
		r.Get("/hls/{job}/master.m3u8", s.handleHLSMaster)
		r.Get("/hls/{job}/{seg}", s.handleHLSSeg)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAdmin)
			r.Get("/settings/server", s.handleSettingsServer)
			r.Get("/settings/backup", s.handleSettingsBackup)
			r.Get("/settings/users", s.handleSettingsUsers)
			r.Post("/hx/backup", s.handleBackupNow)
			r.Get("/hx/backup/status", s.handleBackupStatus)
			r.Post("/hx/backup/restore", s.handleBackupRestore)
			r.Delete("/hx/backup/{name}", s.handleBackupDelete)
			r.Post("/hx/users", s.handleCreateUser)
			r.Delete("/hx/users/{id}", s.handleDeleteUser)
			r.Post("/settings/server", s.handleSaveServer)
			r.Post("/settings/backup", s.handleSaveBackup)
		})
	})
	return r
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		n, err := s.DB.UserCount(r.Context())
		if err != nil {
			http.Error(w, "db error", 500)
			return
		}
		if n == 0 {
			if r.URL.Path == "/register" {
				next.ServeHTTP(w, r)
				return
			}
			http.Redirect(w, r, "/register", http.StatusFound)
			return
		}
		if r.URL.Path == "/register" {
			http.NotFound(w, r)
			return
		}
		c, _ := r.Cookie(sessionCookie)
		token := ""
		if c != nil {
			token = c.Value
		}
		u, _ := s.DB.UserBySession(r.Context(), token)
		if u != nil {
			ctx := context.WithValue(r.Context(), userKey, u)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r) == nil {
			if r.URL.Path == "/login" || r.URL.Path == "/register" {
				next.ServeHTTP(w, r)
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r)
		if u == nil || u.Role != db.RoleAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func userFrom(r *http.Request) *db.User {
	u, _ := r.Context().Value(userKey).(*db.User)
	return u
}

func (s *Server) setSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 3600,
	})
}

func (s *Server) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}

func (s *Server) metaStatus() (ready bool, disabledReason, hint string) {
	if s.Fetch == nil || s.Fetch.Meta == nil {
		return false, "Metadata service unavailable", ""
	}
	st, err := s.Fetch.Meta.Status()
	if err != nil {
		return false, "Metadata service unavailable", ""
	}
	reason := st.DisabledReason
	if !st.Ready && reason == "" {
		reason = "Metadata service unavailable"
	}
	return st.Ready, reason, st.Hint
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	u := userFrom(r)
	data["User"] = u
	if _, ok := data["Version"]; !ok {
		data["Version"] = version.Version
	}
	ready, reason, hint := s.metaStatus()
	data["MetaReady"] = ready
	data["MetaDisabledReason"] = reason
	data["MetaHint"] = hint
	if u != nil {
		libs, _ := s.DB.ListLibraries(r.Context(), u.ID)
		data["NavLibraries"] = libs
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.Templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template %s: %v", name, err)
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.Ping(); err != nil {
		http.Error(w, "db", 500)
		return
	}
	w.Write([]byte("ok"))
}

func (s *Server) handleRegisterGet(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "register.html", nil)
}

func (s *Server) handleRegisterPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	username := strings.TrimSpace(r.FormValue("username"))
	pass := r.FormValue("password")
	confirm := r.FormValue("confirm")
	if username == "" || pass == "" || pass != confirm {
		s.render(w, r, "register.html", map[string]any{"Error": "Invalid input or passwords do not match"})
		return
	}
	u, err := s.DB.CreateUser(r.Context(), username, pass, db.RoleAdmin)
	if err != nil {
		s.render(w, r, "register.html", map[string]any{"Error": err.Error()})
		return
	}
	token, err := s.DB.CreateSession(r.Context(), u.ID, 30)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.setSession(w, token)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	if userFrom(r) != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	s.render(w, r, "login.html", nil)
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	u, err := s.DB.GetUserByUsername(r.Context(), r.FormValue("username"))
	if err != nil || u == nil || !db.CheckPassword(u.PasswordHash, r.FormValue("password")) {
		s.render(w, r, "login.html", map[string]any{"Error": "Invalid credentials"})
		return
	}
	token, err := s.DB.CreateSession(r.Context(), u.ID, 30)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.setSession(w, token)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, _ := r.Cookie(sessionCookie); c != nil {
		_ = s.DB.DeleteSession(r.Context(), c.Value)
	}
	s.clearSession(w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

type libraryCard struct {
	Library     db.Library
	Posters     []string
	Job         *db.ScanJob
	Message     string
	Poll        bool
	ProgressPct int
	Enriching   bool
}

func (s *Server) libraryCardData(ctx context.Context, lib *db.Library, job *db.ScanJob) libraryCard {
	posters, _ := s.DB.ListLibraryPosters(ctx, lib.ID)
	card := libraryCard{Library: *lib, Posters: posters}
	if job != nil {
		card.Job = job
		card.ProgressPct = job.ProgressPct
		card.Poll = job.Status == "running"
		if job.Message.Valid {
			card.Message = job.Message.String
			card.Enriching = strings.HasPrefix(card.Message, "Enriching")
		}
	}
	return card
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	cont, _ := s.DB.ContinueWatching(r.Context(), u.ID, 12)
	recent, _ := s.DB.RecentlyAdded(r.Context(), u.ID, 24)
	progress, _ := s.DB.MediaItemsProgressPct(r.Context(), u.ID, recent)
	recentCards := make([]itemCard, 0, len(recent))
	for _, it := range recent {
		recentCards = append(recentCards, itemCard{Item: it, ProgressPct: progress[it.ID]})
	}
	libs, _ := s.DB.ListLibraries(r.Context(), u.ID)
	scanID, _ := strconv.ParseInt(r.URL.Query().Get("scan"), 10, 64)
	scanLibID, _ := strconv.ParseInt(r.URL.Query().Get("lib"), 10, 64)
	var cards []libraryCard
	for i := range libs {
		lib := libs[i]
		var job *db.ScanJob
		if scanID > 0 && scanLibID == lib.ID {
			job, _ = s.DB.GetScanJob(r.Context(), scanID)
		}
		if job == nil {
			job, _ = s.DB.RunningScanForLibrary(r.Context(), lib.ID)
		}
		cards = append(cards, s.libraryCardData(r.Context(), &lib, job))
	}
	s.render(w, r, "home.html", map[string]any{
		"Continue": cont, "Recent": recentCards, "LibraryCards": cards,
	})
}

func (s *Server) handleLibraries(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	libs, _ := s.DB.ListLibraries(r.Context(), u.ID)
	s.render(w, r, "libraries.html", map[string]any{"Libraries": libs})
}

type itemCard struct {
	Item        db.MediaItem
	ProgressPct int
}

type episodeCard struct {
	Episode     db.Episode
	ProgressPct int
}

func (s *Server) mediaItemCards(ctx context.Context, userID int64, items []db.MediaItem) []itemCard {
	progress, _ := s.DB.MediaItemsProgressPct(ctx, userID, items)
	cards := make([]itemCard, 0, len(items))
	for _, it := range items {
		cards = append(cards, itemCard{Item: it, ProgressPct: progress[it.ID]})
	}
	return cards
}

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	lib, err := s.DB.GetLibrary(r.Context(), u.ID, id)
	if err != nil || lib == nil {
		http.NotFound(w, r)
		return
	}
	sort := r.URL.Query().Get("sort")
	q := r.URL.Query().Get("q")
	all, _ := s.DB.ListMediaItems(r.Context(), id, "name", "")
	items, _ := s.DB.ListMediaItems(r.Context(), id, sort, q)
	s.render(w, r, "library.html", map[string]any{
		"Library": lib, "Items": s.mediaItemCards(r.Context(), u.ID, items),
		"ItemCount": len(all), "Sort": sort, "Q": q,
	})
}

func (s *Server) handleHXItems(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if lib, _ := s.DB.GetLibrary(r.Context(), u.ID, id); lib == nil {
		http.NotFound(w, r)
		return
	}
	items, _ := s.DB.ListMediaItems(r.Context(), id, r.URL.Query().Get("sort"), r.URL.Query().Get("q"))
	s.render(w, r, "partials/items.html", map[string]any{"Items": s.mediaItemCards(r.Context(), u.ID, items)})
}

func (s *Server) handleHXSearch(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	q := r.URL.Query().Get("q")
	libID, _ := strconv.ParseInt(r.URL.Query().Get("library"), 10, 64)
	if lib, _ := s.DB.GetLibrary(r.Context(), u.ID, libID); lib == nil {
		http.NotFound(w, r)
		return
	}
	items, _ := s.DB.ListMediaItems(r.Context(), libID, "name", q)
	s.render(w, r, "partials/items.html", map[string]any{"Items": s.mediaItemCards(r.Context(), u.ID, items)})
}

func (s *Server) handleHXContinue(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	cont, _ := s.DB.ContinueWatching(r.Context(), u.ID, 12)
	s.render(w, r, "partials/continue.html", map[string]any{"Continue": cont})
}

func (s *Server) ownedMediaItem(r *http.Request, id int64) *db.MediaItem {
	u := userFrom(r)
	ok, err := s.DB.UserOwnsMediaItem(r.Context(), u.ID, id)
	if err != nil || !ok {
		return nil
	}
	it, err := s.DB.GetMediaItem(r.Context(), id)
	if err != nil || it == nil {
		return nil
	}
	return it
}

func (s *Server) ownedEpisode(r *http.Request, id int64) *db.Episode {
	u := userFrom(r)
	ok, err := s.DB.UserOwnsEpisode(r.Context(), u.ID, id)
	if err != nil || !ok {
		return nil
	}
	ep, err := s.DB.GetEpisode(r.Context(), id)
	if err != nil || ep == nil {
		return nil
	}
	return ep
}

func (s *Server) loadMovieNFO(it *db.MediaItem) *metadata.MovieNFO {
	if it == nil || !it.NFOPath.Valid || it.NFOPath.String == "" {
		return nil
	}
	n, err := metadata.ReadMovieNFO(filepath.Join(s.Cfg.Store.Path, it.NFOPath.String))
	if err != nil {
		return nil
	}
	return n
}

func (s *Server) loadShowNFO(it *db.MediaItem) *metadata.TVShowNFO {
	if it == nil || !it.NFOPath.Valid || it.NFOPath.String == "" {
		return nil
	}
	n, err := metadata.ReadTVShowNFO(filepath.Join(s.Cfg.Store.Path, it.NFOPath.String))
	if err != nil {
		return nil
	}
	return n
}

func (s *Server) handleMovie(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	it := s.ownedMediaItem(r, id)
	if it == nil || it.Kind != "movie" {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, "movie.html", map[string]any{"Item": it, "NFO": s.loadMovieNFO(it)})
}

func (s *Server) handleShow(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	it := s.ownedMediaItem(r, id)
	if it == nil || it.Kind != "show" {
		http.NotFound(w, r)
		return
	}
	seasons, _ := s.DB.ListSeasons(r.Context(), id)
	type seasonCard struct {
		Season       db.Season
		EpisodeCount int
		ProgressPct  int
	}
	cards := make([]seasonCard, 0, len(seasons))
	for _, se := range seasons {
		n, _ := s.DB.CountEpisodes(r.Context(), se.ID)
		pct, _ := s.DB.SeasonWatchProgressPct(r.Context(), u.ID, se.ID)
		cards = append(cards, seasonCard{Season: se, EpisodeCount: n, ProgressPct: pct})
	}
	s.render(w, r, "show.html", map[string]any{"Item": it, "SeasonCards": cards, "NFO": s.loadShowNFO(it)})
}

func (s *Server) handleSeason(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	n, _ := strconv.Atoi(chi.URLParam(r, "n"))
	it := s.ownedMediaItem(r, id)
	if it == nil {
		http.NotFound(w, r)
		return
	}
	season, _ := s.DB.GetSeasonByShowNum(r.Context(), id, n)
	if season == nil {
		http.NotFound(w, r)
		return
	}
	eps, _ := s.DB.ListEpisodes(r.Context(), season.ID)
	ids := make([]int64, 0, len(eps))
	for _, ep := range eps {
		ids = append(ids, ep.ID)
	}
	pcts, _ := s.DB.WatchProgressPctByEpisodeIDs(r.Context(), u.ID, ids)
	cards := make([]episodeCard, 0, len(eps))
	for _, ep := range eps {
		cards = append(cards, episodeCard{Episode: ep, ProgressPct: pcts[ep.ID]})
	}
	s.render(w, r, "season.html", map[string]any{"Item": it, "Season": season, "Episodes": cards})
}

func (s *Server) handlePlayMovie(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	it := s.ownedMediaItem(r, id)
	if it == nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, "player.html", map[string]any{"Kind": "movie", "ID": id, "Title": it.Title, "SessionURL": fmt.Sprintf("/play/movie/%d/session", id)})
}

func (s *Server) handlePlayEpisode(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ep := s.ownedEpisode(r, id)
	if ep == nil {
		http.NotFound(w, r)
		return
	}
	title := ep.Title.String
	s.render(w, r, "player.html", map[string]any{"Kind": "episode", "ID": id, "Title": title, "SessionURL": fmt.Sprintf("/play/episode/%d/session", id)})
}

func (s *Server) playMediaPath(r *http.Request) (path string, ok bool) {
	kind := chi.URLParam(r, "kind")
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	switch kind {
	case "movie":
		it := s.ownedMediaItem(r, id)
		if it == nil {
			return "", false
		}
		return it.Path, true
	case "episode":
		ep := s.ownedEpisode(r, id)
		if ep == nil {
			return "", false
		}
		return ep.Path, true
	default:
		return "", false
	}
}

func (s *Server) playbackScope(r *http.Request, kind string, id int64) *db.PlaybackScopeIDs {
	switch kind {
	case "movie":
		sc, err := s.DB.PlaybackScopeForMovie(r.Context(), id)
		if err != nil {
			return nil
		}
		return sc
	case "episode":
		sc, err := s.DB.PlaybackScopeForEpisode(r.Context(), id)
		if err != nil {
			return nil
		}
		return sc
	default:
		return nil
	}
}

func (s *Server) handlePlaySession(w http.ResponseWriter, r *http.Request) {
	path, ok := s.playMediaPath(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	kind := chi.URLParam(r, "kind")
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	u := userFrom(r)

	direct := media.GuessDirectPlayByExt(path)
	var (
		dur          float64
		audioTracks  []media.Track
		subTracks    []media.Track
		chapters     []media.Chapter
		sourceHeight int
		audioIndex   = -1
		probe        *media.Probe
	)
	if p, err := media.Ffprobe(path); err == nil {
		probe = p
		direct = media.CanDirectPlay(p)
		dur = p.DurationSeconds()
		audioTracks = p.AudioTracks()
		subTracks = p.SubtitleTracks()
		chapters = p.ChapterList()
		sourceHeight = p.VideoHeight()
		audioIndex = p.DefaultAudioIndex()
	}
	sidecars := media.DiscoverSidecarSubtitles(path)
	if len(sidecars) > 0 {
		baseIdx := 10000
		subTracks = append(subTracks, media.SidecarTracks(sidecars, baseIdx)...)
	}

	scope := s.playbackScope(r, kind, id)
	resolved, _ := s.DB.ResolvePlaybackPrefs(r.Context(), u.ID, scope)

	audioStr := r.FormValue("audio")
	if audioStr == "" {
		audioStr = r.URL.Query().Get("audio")
	}
	audioExplicit := audioStr != ""
	if audioExplicit {
		if n, err := strconv.Atoi(audioStr); err == nil {
			if probe == nil || probe.HasAudioStream(n) {
				audioIndex = n
			}
		}
	} else if resolved.AudioLang != "" {
		if idx := media.MatchTrackByLang(audioTracks, resolved.AudioLang); idx >= 0 {
			audioIndex = idx
		}
	}

	// height preference: 0 = source; >0 = force that encode height (HLS).
	wantHeight := 0
	heightStr := r.FormValue("height")
	if heightStr == "" {
		heightStr = r.URL.Query().Get("height")
	}
	heightExplicit := heightStr != ""
	if heightExplicit {
		if n, err := strconv.Atoi(heightStr); err == nil && n >= 0 {
			wantHeight = n
		}
	} else if resolved.Height != nil {
		wantHeight = *resolved.Height
	}
	if wantHeight > 0 && sourceHeight > 0 && wantHeight > sourceHeight {
		wantHeight = sourceHeight
	}
	if wantHeight > 0 && sourceHeight > 0 && wantHeight == sourceHeight {
		wantHeight = 0
	}
	// Drop preferred height if not in offered qualities (except 0/source).
	if !heightExplicit && wantHeight > 0 {
		okH := false
		for _, q := range media.QualityOptions(sourceHeight) {
			if q.Height == wantHeight {
				okH = true
				break
			}
		}
		if !okH {
			wantHeight = 0
		}
	}

	// Switching away from the default audio on a direct-playable file needs HLS.
	if direct && audioIndex >= 0 && probe != nil && audioIndex != probe.DefaultAudioIndex() {
		direct = false
	}
	// Explicit quality below source forces transcode.
	if wantHeight > 0 {
		direct = false
	}

	maxH := s.Cfg.Transcode.MaxHeight
	if maxH <= 0 {
		maxH = 2160
	}
	encodeHeight := 0
	if !direct {
		if wantHeight > 0 {
			encodeHeight = wantHeight
		} else if sourceHeight > 0 {
			target := sourceHeight
			if maxH > 0 && maxH < target {
				target = maxH
			}
			if target < sourceHeight {
				encodeHeight = target
			}
		}
	}

	startAt := 0.0
	startStr := r.FormValue("start")
	if startStr == "" {
		startStr = r.URL.Query().Get("start")
	}
	if startStr != "" {
		if n, err := strconv.ParseFloat(startStr, 64); err == nil && n > 0 {
			startAt = n
			if dur > 0 && startAt >= dur {
				startAt = dur - 1
				if startAt < 0 {
					startAt = 0
				}
			}
		}
	}

	title := ""
	switch kind {
	case "movie":
		if it := s.ownedMediaItem(r, id); it != nil {
			title = it.Title
		}
	case "episode":
		if ep := s.ownedEpisode(r, id); ep != nil {
			title = ep.Title.String
			if title == "" {
				title = fmt.Sprintf("Episode %d", ep.EpisodeNumber)
			}
		}
	}

	type prefsJSON struct {
		Volume       float64 `json:"volume"`
		Muted        bool    `json:"muted"`
		Height       *int    `json:"height,omitempty"`
		AudioLang    string  `json:"audioLang,omitempty"`
		SubtitleLang *string `json:"subtitleLang"`
	}
	prefsOut := prefsJSON{
		Volume:    1,
		Muted:     false,
		AudioLang: resolved.AudioLang,
	}
	if resolved.HasVolume {
		prefsOut.Volume = resolved.Volume
	}
	if resolved.HasMuted {
		prefsOut.Muted = resolved.Muted
	}
	if resolved.Height != nil {
		prefsOut.Height = resolved.Height
	}
	prefsOut.SubtitleLang = resolved.SubtitleLang

	w.Header().Set("Content-Type", "application/json")
	type playResp struct {
		Mode           string                `json:"mode"`
		URL            string                `json:"url"`
		Title          string                `json:"title,omitempty"`
		Duration       float64               `json:"duration,omitempty"`
		Audio          int                   `json:"audio"`
		Height         int                   `json:"height"`
		Start          float64               `json:"start"`
		Qualities      []media.QualityOption `json:"qualities,omitempty"`
		AudioTracks    []media.Track         `json:"audioTracks,omitempty"`
		SubtitleTracks []media.Track         `json:"subtitleTracks,omitempty"`
		Chapters       []media.Chapter       `json:"chapters,omitempty"`
		PrevEpisodeID  *int64                `json:"prevEpisodeId,omitempty"`
		NextEpisodeID  *int64                `json:"nextEpisodeId,omitempty"`
		Prefs          prefsJSON             `json:"prefs"`
	}
	resp := playResp{
		Title:          title,
		Audio:          audioIndex,
		Height:         wantHeight,
		Qualities:      media.QualityOptions(sourceHeight),
		AudioTracks:    audioTracks,
		SubtitleTracks: subTracks,
		Chapters:       chapters,
		Prefs:          prefsOut,
	}
	if dur > 0 {
		resp.Duration = dur
	}
	if kind == "episode" {
		if prev, next, err := s.DB.AdjacentEpisodes(r.Context(), id); err == nil {
			if prev != nil {
				pid := prev.ID
				resp.PrevEpisodeID = &pid
			}
			if next != nil {
				nid := next.ID
				resp.NextEpisodeID = &nid
			}
		}
	}
	if direct {
		resp.Mode = "direct"
		resp.URL = fmt.Sprintf("/stream/%s/%d", kind, id)
		resp.Start = 0
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	job, err := s.Transcode.Start(r.Context(), path, audioIndex, encodeHeight, startAt)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	resp.Mode = "hls"
	resp.URL = fmt.Sprintf("/hls/%s/master.m3u8", job.ID)
	resp.Start = float64(job.StartAt)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleSubtitleVTT(w http.ResponseWriter, r *http.Request) {
	path, ok := s.playMediaPath(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	key := chi.URLParam(r, "index")
	if strings.HasPrefix(key, "sc-") {
		sc := media.FindSidecarByID(path, key)
		if sc == nil {
			http.NotFound(w, r)
			return
		}
		out := media.SidecarVTTPath(s.Cfg.Transcode.Path, sc.Path)
		if err := media.EnsureSidecarVTT(sc.Path, out); err != nil {
			log.Printf("sidecar vtt %s: %v", sc.Path, err)
			http.Error(w, "subtitle convert failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=86400")
		http.ServeFile(w, r, out)
		return
	}
	idx, err := strconv.Atoi(key)
	if err != nil || idx < 0 {
		http.NotFound(w, r)
		return
	}
	p, err := media.Ffprobe(path)
	if err != nil || !p.HasSubtitleStream(idx) {
		http.NotFound(w, r)
		return
	}
	out := media.SubtitleVTTPath(s.Cfg.Transcode.Path, path, idx)
	if err := media.EnsureSubtitleVTT(path, idx, out); err != nil {
		log.Printf("subtitle vtt %s #%d: %v", path, idx, err)
		http.Error(w, "subtitle extract failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, out)
}

func (s *Server) handleStreamMovie(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	it := s.ownedMediaItem(r, id)
	if it == nil {
		http.NotFound(w, r)
		return
	}
	stream.ServeFileRange(w, r, it.Path)
}

func (s *Server) handleStreamEpisode(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ep := s.ownedEpisode(r, id)
	if ep == nil {
		http.NotFound(w, r)
		return
	}
	stream.ServeFileRange(w, r, ep.Path)
}

func (s *Server) handleHLSMaster(w http.ResponseWriter, r *http.Request) {
	job := s.Transcode.Get(chi.URLParam(r, "job"))
	if job == nil {
		http.NotFound(w, r)
		return
	}
	master := filepath.Join(job.OutputDir, "master.m3u8")
	if _, err := os.Stat(master); err != nil {
		waitCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		if err := s.Transcode.WaitPlaylist(waitCtx, job, 15*time.Second); err != nil {
			http.Error(w, "playlist not ready", http.StatusServiceUnavailable)
			return
		}
	}
	// Read+write (no Last-Modified/ETag) so HLS.js never gets a stale 304 body.
	b, err := os.ReadFile(master)
	if err != nil {
		http.Error(w, "playlist not ready", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

func (s *Server) handleHLSSeg(w http.ResponseWriter, r *http.Request) {
	job := s.Transcode.Get(chi.URLParam(r, "job"))
	if job == nil {
		http.NotFound(w, r)
		return
	}
	seg := filepath.Base(chi.URLParam(r, "seg"))
	path := filepath.Join(job.OutputDir, seg)
	if _, err := os.Stat(path); err != nil {
		// Wait only for the requested segment while ffmpeg is still writing.
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(path); err == nil {
				break
			}
			if job.Status == "error" {
				http.NotFound(w, r)
				return
			}
			select {
			case <-r.Context().Done():
				http.Error(w, "canceled", http.StatusRequestTimeout)
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
		if _, err := os.Stat(path); err != nil {
			http.Error(w, "segment not ready", http.StatusServiceUnavailable)
			return
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
}

func (s *Server) handleMetadata(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/metadata/")
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	http.ServeFile(w, r, filepath.Join(s.Cfg.Store.Path, "metadata", rel))
}

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	u := userFrom(r)
	pos, _ := strconv.ParseFloat(r.FormValue("position"), 64)
	dur, _ := strconv.ParseFloat(r.FormValue("duration"), 64)
	completed := r.FormValue("completed") == "1" || (dur > 0 && pos/dur > 0.9)
	if mid := r.FormValue("movie_id"); mid != "" {
		id, _ := strconv.ParseInt(mid, 10, 64)
		if s.ownedMediaItem(r, id) == nil {
			http.NotFound(w, r)
			return
		}
		_ = s.DB.UpsertWatchMovie(r.Context(), u.ID, id, pos, dur, completed)
	}
	if eid := r.FormValue("episode_id"); eid != "" {
		id, _ := strconv.ParseInt(eid, 10, 64)
		if s.ownedEpisode(r, id) == nil {
			http.NotFound(w, r)
			return
		}
		_ = s.DB.UpsertWatchEpisode(r.Context(), u.ID, id, pos, dur, completed)
	}
	w.WriteHeader(204)
}

func (s *Server) handlePlaybackPrefs(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	u := userFrom(r)
	var scope *db.PlaybackScopeIDs
	if mid := r.FormValue("movie_id"); mid != "" {
		id, _ := strconv.ParseInt(mid, 10, 64)
		if s.ownedMediaItem(r, id) == nil {
			http.NotFound(w, r)
			return
		}
		scope = s.playbackScope(r, "movie", id)
	} else if eid := r.FormValue("episode_id"); eid != "" {
		id, _ := strconv.ParseInt(eid, 10, 64)
		if s.ownedEpisode(r, id) == nil {
			http.NotFound(w, r)
			return
		}
		scope = s.playbackScope(r, "episode", id)
	} else {
		http.Error(w, "movie_id or episode_id required", http.StatusBadRequest)
		return
	}

	var patch db.PrefsPatch
	if v := r.FormValue("volume"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			if f < 0 {
				f = 0
			}
			if f > 1 {
				f = 1
			}
			patch.Volume = &f
		}
	}
	if v := r.FormValue("muted"); v != "" {
		m := v == "1" || strings.EqualFold(v, "true")
		patch.Muted = &m
	}
	if v := r.FormValue("height"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n >= 0 {
			patch.Height = &n
		}
	}
	if _, ok := r.Form["audio_lang"]; ok {
		al := r.FormValue("audio_lang")
		patch.AudioLang = &al
	}
	if _, ok := r.Form["subtitle_lang"]; ok {
		sl := r.FormValue("subtitle_lang")
		patch.SubtitleLang = &sl
	}
	if patch.Volume == nil && patch.Muted == nil && patch.Height == nil && patch.AudioLang == nil && patch.SubtitleLang == nil {
		w.WriteHeader(204)
		return
	}
	if err := s.DB.SavePlaybackPrefs(r.Context(), u.ID, scope, patch); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	dir := "/legal"
	if s.Cfg != nil && strings.TrimSpace(s.Cfg.Legal.Path) != "" {
		dir = s.Cfg.Legal.Path
	}
	readLegal := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	authorRaw := readLegal("AUTHOR")
	ver := readLegal("VERSION")
	copyright := readLegal("COPYRIGHT")
	license := readLegal("LICENSE")
	authorName := "Unavailable"
	var authorLinks []map[string]string
	if authorRaw != "" {
		lines := strings.Split(authorRaw, "\n")
		var parts []string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				parts = append(parts, line)
			}
		}
		if len(parts) > 0 {
			authorName = parts[0]
		}
		for _, line := range parts[1:] {
			href := line
			if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
				href = "https://" + href
			}
			label := strings.TrimPrefix(strings.TrimPrefix(line, "https://"), "http://")
			authorLinks = append(authorLinks, map[string]string{"Href": href, "Label": label})
		}
	}
	if ver == "" {
		ver = version.Version
	}
	if copyright == "" {
		copyright = "Unavailable"
	}
	if license == "" {
		license = "Unavailable"
	}
	s.render(w, r, "about.html", map[string]any{
		"Author":      authorName,
		"AuthorLinks": authorLinks,
		"Version":     ver,
		"Copyright":   copyright,
		"License":     license,
	})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "settings.html", nil)
}

func (s *Server) handleSettingsLibrariesRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleSettingsServer(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "settings_server.html", map[string]any{"Config": s.Cfg})
}

func (s *Server) handleSaveServer(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/settings/server", http.StatusFound)
}

func (s *Server) handleSaveBackup(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if v := r.FormValue("backup_interval"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			s.Cfg.Backup.Interval = d
		}
	}
	if v := r.FormValue("backup_retain"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			s.Cfg.Backup.Retain = n
		}
	}
	s.Cfg.Backup.Enabled = r.FormValue("backup_enabled") == "on"
	_ = s.Cfg.Save(filepath.Join(s.Cfg.Store.Path, "config.yaml"))
	s.Backup.Cfg = s.Cfg
	s.Backup.StartScheduler(context.Background())
	http.Redirect(w, r, "/settings/backup", http.StatusFound)
}

func (s *Server) handleSettingsBackup(w http.ResponseWriter, r *http.Request) {
	names, _ := s.Backup.List()
	st := s.Backup.Status()
	s.render(w, r, "settings_backup.html", map[string]any{
		"Archives": names,
		"Status":   st,
		"Config":   s.Cfg,
	})
}

func (s *Server) handleSettingsUsers(w http.ResponseWriter, r *http.Request) {
	users, _ := s.DB.ListUsers(r.Context())
	s.render(w, r, "settings_users.html", map[string]any{"Users": users})
}

func (s *Server) handleCreateLibrary(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	_ = r.ParseForm()
	name := r.FormValue("name")
	typ := r.FormValue("type")
	path := r.FormValue("path")
	if name == "" || (typ != "movies" && typ != "tv" && typ != "anime") || path == "" {
		http.Error(w, "bad request", 400)
		return
	}
	clean, err := jailMediaPath(mediaRoot(s.Cfg), path)
	if err != nil {
		http.Error(w, "path must be under /media", 400)
		return
	}
	st, err := os.Stat(clean)
	if err != nil {
		if os.IsPermission(err) {
			http.Error(w, "permission denied reading path under /media", 400)
			return
		}
		if os.IsNotExist(err) {
			http.Error(w, "path not found under /media", 400)
			return
		}
		http.Error(w, "cannot access path under /media", 400)
		return
	}
	if !st.IsDir() {
		http.Error(w, "path is not a directory under /media", 400)
		return
	}
	lib, err := s.DB.CreateLibrary(r.Context(), u.ID, name, typ, clean)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jobID, err := s.startLibraryScan(lib, "enrich_missing", true)
	if err != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/?scan=%d&lib=%d", jobID, lib.ID), http.StatusFound)
}

type mediaBrowseData struct {
	Path    string
	Parent  string
	Dirs    []string
	CanGoUp bool
	Error   string
}

func (s *Server) handleMediaBrowse(w http.ResponseWriter, r *http.Request) {
	root := mediaRoot(s.Cfg)
	reqPath := r.URL.Query().Get("path")
	browse, err := s.listMediaDirs(root, reqPath)
	if err != nil {
		browse = &mediaBrowseData{Path: root, Error: err.Error()}
	}
	s.render(w, r, "partials/media_browser.html", map[string]any{"MediaBrowse": browse})
}

func mediaRoot(cfg *config.Config) string {
	if cfg != nil && cfg.Media.Path != "" {
		return filepath.Clean(cfg.Media.Path)
	}
	return "/media"
}

// jailMediaPath ensures path is under root (typically /media).
func jailMediaPath(root, p string) (string, error) {
	root = filepath.Clean(root)
	if p == "" {
		p = root
	}
	clean := filepath.Clean(p)
	if !filepath.IsAbs(clean) {
		clean = filepath.Join(root, clean)
		clean = filepath.Clean(clean)
	}
	if clean != root && !strings.HasPrefix(clean, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("outside media root")
	}
	return clean, nil
}

func (s *Server) listMediaDirs(root, reqPath string) (*mediaBrowseData, error) {
	clean, err := jailMediaPath(root, reqPath)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(clean)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	parent := filepath.Dir(clean)
	canUp := clean != filepath.Clean(root)
	if !canUp {
		parent = ""
	} else if p, err := jailMediaPath(root, parent); err != nil {
		canUp = false
		parent = ""
	} else {
		parent = p
	}
	return &mediaBrowseData{
		Path:    clean,
		Parent:  parent,
		Dirs:    dirs,
		CanGoUp: canUp,
	}, nil
}

func (s *Server) handleRenameLibrary(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	_ = r.ParseForm()
	if err := s.DB.RenameLibrary(r.Context(), u.ID, id, r.FormValue("name")); err != nil {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleDeleteLibrary(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := s.DB.DeleteLibrary(r.Context(), u.ID, id, s.Cfg.Store.Path); err != nil {
		http.NotFound(w, r)
		return
	}
	// 200 + empty body so HTMX swaps the card away (204 does not swap).
	w.WriteHeader(200)
}

func (s *Server) startLibraryScan(lib *db.Library, mode string, persist bool) (int64, error) {
	jobID, err := s.DB.CreateScanJob(context.Background(), lib.ID)
	if err != nil {
		return 0, err
	}
	go s.runLibraryScan(lib, jobID, mode, persist)
	return jobID, nil
}

func (s *Server) runLibraryScan(lib *db.Library, jobID int64, mode string, persist bool) {
	ctx := context.Background()
	enrich := mode == "enrich_missing" || mode == "refetch_all"
	if enrich {
		s.Scanner.ScanLibraryKeepOpen(ctx, lib, jobID)
	} else {
		s.Scanner.ScanLibrary(ctx, lib, jobID)
		return
	}
	j, _ := s.DB.GetScanJob(ctx, jobID)
	if j == nil || j.Status == "error" {
		return
	}
	_ = s.DB.UpdateScanJob(ctx, jobID, "running", j.ProgressPct, "Enriching…")
	opts := fetch.EnrichOpts{
		All:                mode == "refetch_all",
		PersistBesideMedia: persist,
		ScanJobID:          jobID,
	}
	if err := s.Fetch.EnrichLibrary(ctx, lib, opts); err != nil {
		log.Printf("enrich library %d: %v", lib.ID, err)
		_ = s.DB.UpdateScanJob(ctx, jobID, "error", 100, err.Error())
		return
	}
	_ = s.DB.UpdateScanJob(ctx, jobID, "done", 100, "Complete")
}

func (s *Server) renderLibraryCard(w http.ResponseWriter, r *http.Request, lib *db.Library, job *db.ScanJob) {
	card := s.libraryCardData(r.Context(), lib, job)
	s.render(w, r, "partials/library_card.html", map[string]any{
		"Library": card.Library, "Posters": card.Posters, "Job": card.Job,
		"Message": card.Message, "Poll": card.Poll, "ProgressPct": card.ProgressPct,
		"Enriching": card.Enriching,
	})
}

func (s *Server) handleScanLibrary(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	lib, _ := s.DB.GetLibrary(r.Context(), u.ID, id)
	if lib == nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	mode := r.FormValue("mode")
	switch mode {
	case "local", "enrich_missing", "refetch_all":
	default:
		mode = "local"
	}
	persist := r.FormValue("persist") == "1" || r.FormValue("persist") == "on" || r.FormValue("persist") == "true"
	if mode == "local" {
		// persist only applies to provider enrich modes
		persist = false
	}
	jobID, err := s.startLibraryScan(lib, mode, persist)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	j, _ := s.DB.GetScanJob(r.Context(), jobID)
	if j == nil {
		j = &db.ScanJob{ID: jobID, Status: "running", ProgressPct: 0, LibraryID: sql.NullInt64{Int64: id, Valid: true}}
	}
	s.renderLibraryCard(w, r, lib, j)
}

func (s *Server) handleScanStatus(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	j, _ := s.DB.GetScanJob(r.Context(), id)
	if j == nil || !j.LibraryID.Valid {
		http.NotFound(w, r)
		return
	}
	lib, _ := s.DB.GetLibrary(r.Context(), u.ID, j.LibraryID.Int64)
	if lib == nil {
		http.NotFound(w, r)
		return
	}
	s.renderLibraryCard(w, r, lib, j)
}

func (s *Server) handleScanMedia(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	it := s.ownedMediaItem(r, id)
	if it == nil || (it.Kind != "movie" && it.Kind != "show") {
		http.NotFound(w, r)
		return
	}
	u := userFrom(r)
	lib, _ := s.DB.GetLibrary(r.Context(), u.ID, it.LibraryID)
	if lib == nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	mode := r.FormValue("mode")
	switch mode {
	case "local", "enrich_missing", "refetch_all":
	default:
		mode = "local"
	}
	persist := r.FormValue("persist") == "1" || r.FormValue("persist") == "on" || r.FormValue("persist") == "true"
	if mode == "local" {
		persist = false
	}
	if mode == "enrich_missing" || mode == "refetch_all" {
		ready, reason, _ := s.metaStatus()
		if !ready {
			if reason == "" {
				reason = "Metadata service unavailable"
			}
			http.Error(w, reason, 400)
			return
		}
	}
	jobID, err := s.DB.CreateScanJob(r.Context(), lib.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	go s.runMediaScan(lib, it, jobID, mode, persist)
	j, _ := s.DB.GetScanJob(r.Context(), jobID)
	if j == nil {
		j = &db.ScanJob{ID: jobID, Status: "running", ProgressPct: 0, LibraryID: sql.NullInt64{Int64: lib.ID, Valid: true}}
	}
	poll := j.Status == "running"
	s.render(w, r, "partials/entry_scan_progress.html", map[string]any{"Job": j, "Poll": poll})
}

func (s *Server) runMediaScan(lib *db.Library, item *db.MediaItem, jobID int64, mode string, persist bool) {
	ctx := context.Background()
	s.syncFetchClients()
	if err := s.Scanner.RescanMediaItem(ctx, lib, item, jobID); err != nil {
		log.Printf("rescan media %d: %v", item.ID, err)
		_ = s.DB.UpdateScanJob(ctx, jobID, "error", 100, err.Error())
		return
	}
	enrich := mode == "enrich_missing" || mode == "refetch_all"
	if !enrich {
		_ = s.DB.UpdateScanJob(ctx, jobID, "done", 100, "Complete")
		return
	}
	fresh, _ := s.DB.GetMediaItem(ctx, item.ID)
	if fresh == nil {
		fresh = item
	}
	_ = s.DB.UpdateScanJob(ctx, jobID, "running", 0, "Enriching…")
	opts := fetch.EnrichOpts{
		All:                mode == "refetch_all",
		PersistBesideMedia: persist,
		ScanJobID:          jobID,
	}
	if err := s.Fetch.EnrichMediaItem(ctx, fresh, opts); err != nil {
		log.Printf("enrich media %d: %v", item.ID, err)
		_ = s.DB.UpdateScanJob(ctx, jobID, "error", 100, err.Error())
		return
	}
	_ = s.DB.UpdateScanJob(ctx, jobID, "done", 100, "Complete")
}

func (s *Server) handleEntryScanStatus(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	j, _ := s.DB.GetScanJob(r.Context(), id)
	if j == nil || !j.LibraryID.Valid {
		http.NotFound(w, r)
		return
	}
	lib, _ := s.DB.GetLibrary(r.Context(), u.ID, j.LibraryID.Int64)
	if lib == nil {
		http.NotFound(w, r)
		return
	}
	poll := j.Status == "running"
	s.render(w, r, "partials/entry_scan_progress.html", map[string]any{"Job": j, "Poll": poll})
}

func (s *Server) handleBackupNow(w http.ResponseWriter, r *http.Request) {
	go func() {
		if err := s.Backup.RunBackup(context.Background()); err != nil {
			log.Printf("backup: %v", err)
		}
	}()
	w.WriteHeader(202)
}

func (s *Server) handleBackupStatus(w http.ResponseWriter, r *http.Request) {
	st := s.Backup.Status()
	s.render(w, r, "partials/backup_status.html", map[string]any{"Status": st})
}

func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := r.FormValue("name")
	if err := s.Backup.Restore(r.Context(), name, s.reopen); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/settings/backup", http.StatusFound)
}

func (s *Server) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.Backup.Delete(name); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	_, err := s.DB.CreateUser(r.Context(), strings.TrimSpace(r.FormValue("username")), r.FormValue("password"), db.RoleUser)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/settings/users", http.StatusFound)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := s.DB.DeleteUser(r.Context(), id); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.WriteHeader(204)
}
