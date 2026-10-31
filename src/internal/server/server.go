package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alyshmahell/servemedia/src/internal/backup"
	"github.com/alyshmahell/servemedia/src/internal/config"
	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/fetch"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
	"github.com/alyshmahell/servemedia/src/internal/media"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
	"github.com/alyshmahell/servemedia/src/internal/watchdog"
	"github.com/alyshmahell/servemedia/src/internal/webhooks"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const sessionCookie = "servemedia_session"

type Server struct {
	Cfg         *config.Config
	DB          *db.DB
	Backup      *backup.Service
	Fetch       *fetch.Worker
	Transcode   *media.Manager
	Watchdog    *watchdog.Watchdog
	Webhooks    *webhooks.Service
	Meta        matchmedia.Service
	Templates   *template.Template
	Static      fs.FS
	reopen      func() error
	restartMeta func() error
	repointOnce sync.Map
	// Overlord signs every request in as the admin named overlord.
	Overlord bool
}

type ctxKey int

const userKey ctxKey = 1

func New(cfg *config.Config, database *db.DB, bak *backup.Service, tr *media.Manager, webFS fs.FS, meta matchmedia.Service, reopen func() error) (*Server, error) {
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
		DB:        database,
		Store:     cfg.Store.Path,
		MediaRoot: cfg.PrimaryMediaRoot(),
		Meta:      meta,
	}
	s := &Server{
		Cfg: cfg, DB: database, Backup: bak, Fetch: fw, Transcode: tr,
		Webhooks: webhooks.New(cfg), Meta: meta, Templates: tpl, Static: staticFS, reopen: reopen,
	}
	fw.Webhooks = s
	return s, nil
}

func (s *Server) SetRestartMeta(fn func() error) {
	s.restartMeta = fn
}

func (s *Server) MigrateLegacyWebhooks(ctx context.Context) {
	legacy := s.Cfg.Integrations.Webhooks
	if !legacy.Enabled && legacy.ServerURL == "" && len(legacy.Destinations) == 0 && legacy.APIKey == "" {
		return
	}
	users, err := s.DB.ListUsers(ctx)
	if err != nil || len(users) == 0 {
		return
	}
	for _, u := range users {
		if u.Role == db.RoleAdmin {
			_ = s.DB.ImportLegacyWebhooks(ctx, u.ID, legacy)
			return
		}
	}
	_ = s.DB.ImportLegacyWebhooks(ctx, users[0].ID, legacy)
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
			p = strings.TrimSpace(p)
			if p == "" {
				return "/static/placeholder.svg"
			}
			key := p
			if len(metaID) > 0 {
				if m := strings.TrimSpace(metaID[0]); m != "" {
					key = m
				}
			}
			if filepath.IsAbs(p) {
				q := url.Values{}
				q.Set("path", p)
				q.Set("m", key)
				return "/metadata/?" + q.Encode()
			}
			p = strings.TrimPrefix(strings.TrimPrefix(p, "/"), "metadata/")
			return "/metadata/" + p + "?m=" + url.QueryEscape(key)
		},
		"queryEscape": url.QueryEscape,
		"joinPath":    filepath.Join,
		"join": func(parts []string) string {
			var out []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					out = append(out, p)
				}
			}
			return strings.Join(out, ", ")
		},
		"js": func(v any) template.JS {
			b, err := json.Marshal(v)
			if err != nil {
				return template.JS("null")
			}
			return template.JS(b)
		},
		"mediaActions": func(scope string, id int64, title string, metaReady bool, metaDisabledReason, matchStatus, matchError string) map[string]any {
			return map[string]any{"Scope": scope, "ID": id, "Title": title, "MetaReady": metaReady, "MetaDisabledReason": metaDisabledReason, "MatchStatus": matchStatus, "MatchError": matchError}
		},
		"mediaActionsStatic": func(scope string, id int64, title string, metaReady bool, metaDisabledReason, matchStatus, matchError string) map[string]any {
			return map[string]any{"Scope": scope, "ID": id, "Title": title, "MetaReady": metaReady, "MetaDisabledReason": metaDisabledReason, "MatchStatus": matchStatus, "MatchError": matchError, "Static": true}
		},
		"hasStr": func(list []string, want string) bool {
			for _, v := range list {
				if strings.EqualFold(v, want) {
					return true
				}
			}
			return false
		},
		"add1": func(n int) int { return n + 1 },
	})
	return template.Must(t.ParseFS(fsys, "*.html", "*/*.html"))
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Logger, middleware.Recoverer)
	r.Use(s.authMiddleware)

	r.Get("/healthz", s.handleHealthz)
	r.Get("/favicon.ico", s.handleFavicon)
	r.Get("/favicon.svg", s.handleFavicon)
	if s.Cfg != nil {
		vendor := filepath.Join(s.Cfg.ExeDir, "vendor")
		r.Handle("/static/vendor/*", http.StripPrefix("/static/vendor/", http.FileServer(http.Dir(vendor))))
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(s.Static))))

	r.Get("/register", s.handleRegisterGet)
	r.Post("/register", s.handleRegisterPost)
	r.Get("/login", s.handleLoginGet)
	r.Post("/login", s.handleLoginPost)
	r.Post("/logout", s.handleLogout)
	r.Post("/hx/kpa", s.handleKPA)

	r.Group(func(r chi.Router) {
		r.Use(s.requireUser)
		r.Get("/", s.handleHome)
		r.Get("/libraries", s.handleLibraries)
		r.Get("/libraries/{id}", s.handleLibrary)
		r.Get("/libraries/{id}/tree", s.handleLibrary)
		r.Get("/play/movie/{id}", s.handlePlayMovie)
		r.Get("/play/episode/{id}", s.handlePlayEpisode)
		r.Get("/play/file/{id}", s.handlePlayFile)
		r.Get("/about", s.handleAbout)
		r.Get("/settings", s.handleSettings)
		r.Get("/settings/libraries", s.handleSettingsLibrariesRedirect)
		r.Get("/hx/libraries/{id}/items", s.handleHXItems)
		r.Get("/hx/home/continue", s.handleHXContinue)
		r.Get("/hx/home/recent", s.handleHXRecent)
		r.Post("/hx/progress", s.handleProgress)
		r.Post("/hx/playback-prefs", s.handlePlaybackPrefs)
		r.Post("/hx/libraries", s.handleCreateLibrary)
		r.Post("/hx/libraries/{id}/scan", s.handleScanLibrary)
		r.Get("/hx/scan/{id}/status", s.handleScanStatus)
		r.Get("/hx/scan/{id}/entry-status", s.handleEntryScanStatus)
		r.Post("/hx/media/{id}/scan", s.handleScanMedia)
		r.Get("/hx/media/{id}/match", s.handleMatchGet)
		r.Post("/hx/media/{id}/match", s.handleMatchPost)
		r.Post("/hx/media/{id}/meta", s.handleMediaMeta)
		r.Post("/hx/media/{id}/poster", s.handleMediaPoster)
		r.Delete("/hx/media/{id}/poster", s.handleMediaPosterDelete)
		r.Post("/hx/shows/{id}/seasons/{n}/meta", s.handleSeasonMeta)
		r.Post("/hx/shows/{id}/seasons/{n}/poster", s.handleSeasonPoster)
		r.Delete("/hx/shows/{id}/seasons/{n}/poster", s.handleSeasonPosterDelete)
		r.Post("/hx/episodes/{id}/meta", s.handleEpisodeMeta)
		r.Post("/hx/episodes/{id}/poster", s.handleEpisodePoster)
		r.Delete("/hx/episodes/{id}/poster", s.handleEpisodePosterDelete)
		r.Post("/hx/episodes/{id}/poster/generate", s.handleEpisodePosterGenerate)
		r.Post("/hx/nodes/{id}/still", s.handleNodeStill)
		r.Delete("/hx/nodes/{id}/still", s.handleNodeStillDelete)
		r.Post("/hx/nodes/{id}/still/generate", s.handleNodeStillGenerate)
		r.Post("/hx/libraries/{id}", s.handleRenameLibrary)
		r.Delete("/hx/libraries/{id}", s.handleDeleteLibrary)
		r.Get("/hx/media/browse", s.handleMediaBrowse)
		r.Get("/metadata/*", s.handleMetadata)
		r.Get("/stream/movie/{id}", s.handleStreamMovie)
		r.Get("/stream/episode/{id}", s.handleStreamEpisode)
		r.Get("/stream/file/{id}", s.handleStreamFile)
		r.Post("/play/{kind}/{id}/session", s.handlePlaySession)
		r.Get("/play/{kind}/{id}/sub/{index}.vtt", s.handleSubtitleVTT)
		r.Get("/hls/{job}/master.m3u8", s.handleHLSMaster)
		r.Get("/hls/{job}/{seg}", s.handleHLSSeg)

		r.Get("/settings/integrations", s.handleSettingsIntegrations)
		r.Post("/settings/integrations", s.handleSaveIntegrations)
		r.Post("/hx/integrations/webhooks/regenerate-key", s.handleRegenerateWebhookKey)
		r.Post("/hx/integrations/webhooks/test", s.handleTestWebhooks)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAdmin)
			r.Post("/settings", s.handleSaveGeneral)
			r.Get("/settings/engines", s.handleSettingsEngines)
			r.Post("/settings/engines", s.handleSaveEngines)
			r.Post("/settings/engines/restore", s.handleRestoreEngines)
			r.Post("/settings/engines/restart", s.handleRestartEngines)
			r.Get("/hx/engines/matchmedia/status", s.handleEnginesStatus)
			r.Get("/settings/server", s.handleSettingsServer)
			r.Get("/settings/backup", s.handleSettingsBackup)
			r.Get("/settings/users", s.handleSettingsUsers)
			r.Post("/hx/backup", s.handleBackupNow)
			r.Get("/hx/backup/status", s.handleBackupStatus)
			r.Post("/hx/backup/restore", s.handleBackupRestore)
			r.Delete("/hx/backup/{name}", s.handleBackupDelete)
			r.Post("/hx/users", s.handleCreateUser)
			r.Post("/hx/users/{id}/password", s.handleResetUserPassword)
			r.Delete("/hx/users/{id}", s.handleDeleteUser)
			r.Post("/settings/server", s.handleSaveServer)
			r.Post("/settings/backup", s.handleSaveBackup)
		})
	})
	return r
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/healthz" || r.URL.Path == "/favicon.ico" || r.URL.Path == "/favicon.svg" {
			next.ServeHTTP(w, r)
			return
		}
		if s.Overlord {
			u, err := s.DB.EnsureOverlord(r.Context())
			if err != nil || u == nil {
				http.Error(w, "db error", 500)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
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

func transcodeKey(r *http.Request, u *db.User) string {
	uid := "anon"
	if u != nil {
		uid = fmt.Sprintf("%d", u.ID)
	}
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	return uid + "@" + ip
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
	data["Overlord"] = s.Overlord || (u != nil && db.IsOverlord(u.Username))
	if _, ok := data["Version"]; !ok {
		ver := ""
		if s.Cfg != nil {
			ver = s.Cfg.Version
		}
		data["Version"] = ver
	}
	ready, reason, hint := s.metaStatus()
	data["MetaReady"] = ready
	data["MetaDisabledReason"] = reason
	data["MetaHint"] = hint
	applyNav(name, data)
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

func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	name := "favicon.png"
	ctype := "image/png"
	if r.URL.Path == "/favicon.svg" {
		name = "logo.svg"
		ctype = "image/svg+xml"
	}
	b, err := fs.ReadFile(s.Static, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(b)
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
	if s.Webhooks != nil {
		s.dispatchWebhooks(r.Context(), u.ID, webhooks.NotificationUserCreated, webhooks.UserPayload(u))
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
	if db.IsOverlord(r.FormValue("username")) {
		s.render(w, r, "login.html", map[string]any{"Error": "Invalid credentials"})
		return
	}
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
	u := userFrom(r)
	if c, _ := r.Cookie(sessionCookie); c != nil {
		_ = s.DB.DeleteSession(r.Context(), c.Value)
	}
	s.clearSession(w)
	if s.Overlord || (u != nil && db.IsOverlord(u.Username)) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}

type scanStat struct {
	Label string
	Value int
	Tone  string
}

type scanProgress struct {
	Phase string
	Done  int
	Total int
	Title string
	Stats []scanStat
}

type libraryCard struct {
	Library     db.Library
	Posters     []string
	Job         *db.ScanJob
	Message     string
	Lines       []scanProgress
	Poll        bool
	ProgressPct int
	Enriching   bool
	Watched     int
	Total       int
	WatchPct    int
}

func (s *Server) libraryCardData(ctx context.Context, userID int64, lib *db.Library, job *db.ScanJob) libraryCard {
	if lib != nil {
		s.ensureLibraryRatings(ctx, lib.ID)
	}
	posters, _ := s.DB.ListLibraryPosters(ctx, lib.ID)
	card := libraryCard{Library: *lib, Posters: posters}
	if userID > 0 && lib != nil {
		if files, err := s.DB.ListFileWatch(ctx, userID, lib.ID); err == nil {
			card.Watched, card.Total, card.WatchPct = folderWatch(files, "")
		}
	}
	if job != nil {
		card.Job = job
		card.ProgressPct = job.ProgressPct
		card.Poll = job.Status == "running"
		if job.Message.Valid {
			card.Message = job.Message.String
			card.Lines = parseScanLines(card.Message)
			card.Enriching = strings.HasPrefix(card.Message, "Matching") || strings.HasPrefix(card.Message, "Applying")
			for _, line := range card.Lines {
				if line.Phase == "match" || line.Phase == "fetch" || line.Phase == "save" {
					card.Enriching = true
				}
			}
		}
	}
	return card
}

func recentHXTrigger(polling, initial bool) string {
	interval := "every 30s"
	if polling {
		interval = "every 2s"
	}
	if initial {
		return "load, " + interval
	}
	return interval
}

func (s *Server) userHasRunningScan(ctx context.Context, userID int64) bool {
	libs, err := s.DB.ListLibraries(ctx, userID)
	if err != nil {
		return false
	}
	for i := range libs {
		job, err := s.DB.RunningScanForLibrary(ctx, libs[i].ID)
		if err == nil && job != nil {
			return true
		}
	}
	return false
}

// libraryGridPolling refreshes the library grid while a scan is still changing
// titles. Stills run after that, and swapping the grid then resets the scroll.
func (s *Server) libraryGridPolling(ctx context.Context, libraryID int64) bool {
	job, err := s.DB.RunningScanForLibrary(ctx, libraryID)
	if err != nil || job == nil {
		return false
	}
	if !job.Message.Valid {
		return true
	}
	lines := parseScanLines(job.Message.String)
	if len(lines) == 0 {
		return true
	}
	switch lines[len(lines)-1].Phase {
	case "scan", "match", "fetch":
		return true
	default:
		return false
	}
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	cont, _ := s.continueWatching(r.Context(), u.ID, 12)
	recent, _ := s.DB.RecentlyAdded(r.Context(), u.ID, 24)
	libs, _ := s.DB.ListLibraries(r.Context(), u.ID)
	scanID, _ := strconv.ParseInt(r.URL.Query().Get("scan"), 10, 64)
	scanLibID, _ := strconv.ParseInt(r.URL.Query().Get("lib"), 10, 64)
	var cards []libraryCard
	anyRunning := false
	for i := range libs {
		lib := libs[i]
		var job *db.ScanJob
		if scanID > 0 && scanLibID == lib.ID {
			job, _ = s.DB.GetScanJob(r.Context(), scanID)
		}
		running, _ := s.DB.RunningScanForLibrary(r.Context(), lib.ID)
		if running != nil {
			anyRunning = true
			if job == nil {
				job = running
			}
		}
		cards = append(cards, s.libraryCardData(r.Context(), u.ID, &lib, job))
	}
	s.render(w, r, "home.html", map[string]any{
		"Continue": cont, "Recent": s.mediaItemCards(r.Context(), u.ID, recent), "LibraryCards": cards,
		"RecentTrigger": recentHXTrigger(anyRunning, true),
	})
}

func (s *Server) handleLibraries(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusFound)
}

type itemCard struct {
	Item        db.MediaItem
	Href        string
	ProgressPct int
}

func (s *Server) mediaItemCards(ctx context.Context, userID int64, items []db.MediaItem) []itemCard {
	progress, _ := s.DB.MediaItemsProgressPct(ctx, userID, items)
	cards := make([]itemCard, 0, len(items))
	for _, it := range items {
		item := it
		cards = append(cards, itemCard{
			Item:        item,
			Href:        s.showFolderBack(ctx, &item),
			ProgressPct: progress[it.ID],
		})
	}
	return cards
}

type nodeCard struct {
	Node        db.LibraryNode
	Href        string
	Title       string
	Match       string
	Kind        string
	LibraryID   int64
	Rel         string
	Plot        string
	Still       string
	ProgressPct int
	Watched     int
	Total       int
	WatchPct    int
	Facts       []fileFact
	Chips       []heroChip
	MediaID     int64
	MatchStatus string
	MatchError  string
	EditURL     string
	UploadURL   string
	GenerateURL string
}

func (s *Server) libraryPageData(ctx context.Context, u *db.User, lib *db.Library, sort, q, rel string, swapCount bool) map[string]any {
	rel = strings.Trim(strings.ReplaceAll(rel, "\\", "/"), "/")
	n, _ := s.DB.CountLibraryNodes(ctx, lib.ID)
	if n > 0 {
		nodes, _ := s.DB.ListChildNodes(ctx, lib.ID, rel, sort)
		qn := strings.TrimSpace(q)
		if qn != "" {
			under, _ := s.DB.ListNodesUnder(ctx, lib.ID, rel)
			keep := matchingContainers(rel, under, qn)
			var filtered []db.LibraryNode
			for _, node := range nodes {
				if keep[node.RelPath] {
					filtered = append(filtered, node)
				}
			}
			nodes = filtered
		}
		files, _ := s.DB.ListFileWatch(ctx, u.ID, lib.ID)
		pctByID := map[int64]int{}
		for _, f := range files {
			pctByID[f.ID] = f.Pct
		}
		var dirs, fileCards []nodeCard
		for _, node := range nodes {
			if node.NodeKind == "file" {
				card := nodeCard{Node: node, LibraryID: lib.ID, Rel: node.RelPath, Kind: "file"}
				card.EditURL, card.MediaID, card.MatchStatus, card.MatchError = s.nodeActions(ctx, lib, node)
				card.Href = s.nodeHref(ctx, lib, node)
				art := s.nodeArt(ctx, node, card.MediaID)
				card.Title, card.Plot, card.Still, card.Facts = fileCardFields(lib.Path, node, s.fileIsShow(ctx, node))
				if art != "" {
					card.Still = art
				}
				card.ProgressPct = pctByID[node.ID]
				card.UploadURL = "/hx/nodes/" + strconv.FormatInt(node.ID, 10) + "/still"
				card.GenerateURL = card.UploadURL + "/generate"
				fileCards = append(fileCards, card)
				continue
			}
			card := s.dirNodeCard(ctx, lib, node)
			card.Watched, card.Total, card.WatchPct = folderWatch(files, node.RelPath)
			dirs = append(dirs, card)
		}
		out := map[string]any{
			"Library": lib, "Tree": true, "Nodes": dirs, "Files": fileCards, "Path": rel,
			"ItemCount": len(dirs) + len(fileCards), "Sort": sort, "Q": q,
			"Poll": s.libraryGridPolling(ctx, lib.ID), "SwapCount": swapCount,
		}
		if rel != "" {
			if hero := s.folderHeroCard(ctx, lib, rel); hero != nil {
				out["FolderHero"] = *hero
			}
		}
		return out
	}
	all, _ := s.DB.ListMediaItems(ctx, lib.ID, "name", "")
	items, _ := s.DB.ListMediaItems(ctx, lib.ID, sort, q)
	return map[string]any{
		"Library": lib, "Items": s.mediaItemCards(ctx, u.ID, items),
		"ItemCount": len(all), "Sort": sort, "Q": q, "Path": rel,
		"Poll": s.libraryGridPolling(ctx, lib.ID), "SwapCount": swapCount,
	}
}

func (s *Server) dirNodeCard(ctx context.Context, lib *db.Library, node db.LibraryNode) nodeCard {
	card := nodeCard{Node: node, LibraryID: lib.ID, Rel: node.RelPath, Kind: "dir"}
	card.EditURL, card.MediaID, card.MatchStatus, card.MatchError = s.nodeActions(ctx, lib, node)
	card.Href = s.nodeHref(ctx, lib, node)
	art := s.dirArt(ctx, lib, node, card.MediaID)
	seasonNum := 0
	if node.SeasonNum.Valid {
		seasonNum, _ = strconv.Atoi(strings.TrimSpace(node.SeasonNum.String))
	}
	card.Title = folderDisplayTitle(filepath.Join(lib.Path, filepath.FromSlash(node.RelPath)), node.Name, node.MMTitle, node.MMYear, seasonNum)
	if art != "" {
		card.Node.PosterPath = sql.NullString{String: art, Valid: true}
	}
	card.Plot = s.nodePlot(ctx, node, card.MediaID)
	return card
}

func nodePosterUploadURL(editURL string) string {
	if strings.HasSuffix(editURL, "/meta") {
		return strings.TrimSuffix(editURL, "/meta") + "/poster"
	}
	return ""
}

func (s *Server) folderHeroCard(ctx context.Context, lib *db.Library, rel string) *nodeCard {
	rel = strings.Trim(strings.ReplaceAll(rel, "\\", "/"), "/")
	if rel == "" || lib == nil || s.DB == nil {
		return nil
	}
	id, err := s.DB.NodeIDByRel(ctx, lib.ID, rel)
	if err != nil {
		return nil
	}
	if id == 0 {
		name := rel
		if i := strings.LastIndex(rel, "/"); i >= 0 {
			name = rel[i+1:]
		}
		card := nodeCard{Title: folderDisplayTitle(filepath.Join(lib.Path, filepath.FromSlash(rel)), name, sql.NullString{}, sql.NullString{}, 0), Rel: rel, Kind: "dir", LibraryID: lib.ID}
		card.Chips = s.heroChips(ctx, lib, rel, 0)
		return &card
	}
	node, err := s.DB.GetLibraryNode(ctx, id)
	if err != nil || node == nil {
		return nil
	}
	card := s.dirNodeCard(ctx, lib, *node)
	card.UploadURL = nodePosterUploadURL(card.EditURL)
	card.Chips = s.heroChips(ctx, lib, node.RelPath, card.MediaID)
	return &card
}

func (s *Server) nodePlot(ctx context.Context, node db.LibraryNode, mediaID int64) string {
	if p := strings.TrimSpace(node.Plot.String); p != "" {
		return p
	}
	if mediaID <= 0 {
		return ""
	}
	it, err := s.DB.GetMediaItem(ctx, mediaID)
	if err != nil || it == nil || !it.Plot.Valid {
		return ""
	}
	return strings.TrimSpace(it.Plot.String)
}

func (s *Server) fileIsShow(ctx context.Context, node db.LibraryNode) bool {
	if isShowKind(node.MMKind.String) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(node.MMKind.String), "movie") {
		return false
	}
	if !node.GroupNodeID.Valid {
		return false
	}
	g, err := s.DB.GetLibraryNode(ctx, node.GroupNodeID.Int64)
	if err != nil || g == nil {
		return false
	}
	return isShowKind(g.MMKind.String)
}

func (s *Server) nodeActions(ctx context.Context, lib *db.Library, node db.LibraryNode) (editURL string, mediaID int64, status, matchErr string) {
	id, kind, itemStatus, errStr, err := s.DB.ItemByNode(ctx, node.ID)
	if err != nil {
		return
	}
	status = strings.TrimSpace(node.MatchStatus.String)
	if id > 0 {
		mediaID = id
		matchErr = errStr
		if status == "" {
			status = itemStatus
		}
	}
	if mediaID == 0 && node.GroupNodeID.Valid {
		if gid, _, _, _, gerr := s.DB.ItemByNode(ctx, node.GroupNodeID.Int64); gerr == nil && gid > 0 {
			mediaID = gid
		}
	}
	if mediaID == 0 && node.NodeKind != "file" && lib != nil {
		if it, own := s.folderMedia(ctx, lib, node); it != nil {
			mediaID = it.ID
			if own {
				id = it.ID
				kind = it.Kind
				if status == "" && it.MatchStatus.Valid {
					status = strings.TrimSpace(it.MatchStatus.String)
				}
				if matchErr == "" && it.MatchError.Valid {
					matchErr = it.MatchError.String
				}
			}
		}
	}
	if id > 0 && (kind == "movie" || kind == "show") {
		return "/hx/media/" + strconv.FormatInt(id, 10) + "/meta", mediaID, status, matchErr
	}
	if node.NodeKind == "file" {
		abs := filepath.Join(lib.Path, filepath.FromSlash(node.RelPath))
		epID, err := s.DB.EpisodeIDByPath(ctx, abs)
		if err == nil && epID > 0 {
			return "/hx/episodes/" + strconv.FormatInt(epID, 10) + "/meta", mediaID, status, matchErr
		}
	}
	showID, num, ok := s.DB.SeasonEdit(ctx, node.ID)
	if ok {
		if mediaID == 0 {
			mediaID = showID
		}
		return fmt.Sprintf("/hx/shows/%d/seasons/%d/meta", showID, num), mediaID, status, matchErr
	}
	return "", mediaID, status, matchErr
}

func (s *Server) folderMedia(ctx context.Context, lib *db.Library, node db.LibraryNode) (*db.MediaItem, bool) {
	if s == nil || s.DB == nil || lib == nil {
		return nil, false
	}
	abs := filepath.Clean(filepath.Join(lib.Path, filepath.FromSlash(node.RelPath)))
	if found, err := s.DB.GetMediaItemByPath(ctx, lib.ID, abs); err == nil && found != nil {
		return found, true
	}
	if found, err := s.DB.MovieInFolder(ctx, lib.ID, abs); err == nil && found != nil {
		return found, true
	}
	root := filepath.Clean(lib.Path)
	parent := abs
	for {
		next := filepath.Dir(parent)
		if next == parent || !pathInside(root, next) {
			break
		}
		parent = next
		found, err := s.DB.GetMediaItemByPath(ctx, lib.ID, parent)
		if err == nil && found != nil && (found.Kind == "show" || found.Kind == "tv") {
			return found, false
		}
	}
	return nil, false
}

func pathInside(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func (s *Server) dirArt(ctx context.Context, lib *db.Library, node db.LibraryNode, mediaID int64) string {
	if p := strings.TrimSpace(node.PosterPath.String); p != "" {
		return p
	}
	dir := filepath.Join(lib.Path, filepath.FromSlash(node.RelPath))
	if p := metadata.FolderPoster(dir); p != "" {
		return p
	}
	if mediaID <= 0 || s == nil || s.DB == nil || lib == nil {
		return ""
	}
	it, err := s.DB.GetMediaItem(ctx, mediaID)
	if err != nil || it == nil || !it.PosterPath.Valid {
		return ""
	}
	if !sameMediaFolder(lib.Path, node.RelPath, it.Path) {
		return ""
	}
	return strings.TrimSpace(it.PosterPath.String)
}

func sameMediaFolder(libPath, rel, itemPath string) bool {
	abs := filepath.Clean(filepath.Join(libPath, filepath.FromSlash(rel)))
	itemPath = filepath.Clean(strings.TrimSpace(itemPath))
	if itemPath == "" {
		return false
	}
	if abs == itemPath {
		return true
	}
	return filepath.Dir(itemPath) == abs
}

func (s *Server) nodeHref(_ context.Context, lib *db.Library, node db.LibraryNode) string {
	if node.NodeKind != "file" {
		return "/libraries/" + strconv.FormatInt(lib.ID, 10) + "/tree?path=" + urlQueryEscape(node.RelPath)
	}
	return "/play/file/" + strconv.FormatInt(node.ID, 10)
}

func (s *Server) showFolderBack(ctx context.Context, it *db.MediaItem) string {
	if it == nil {
		return ""
	}
	fallback := fmt.Sprintf("/libraries/%d", it.LibraryID)
	nodeID, err := s.DB.MediaItemNodeID(ctx, it.ID)
	if err != nil || nodeID == 0 {
		return fallback
	}
	n, err := s.DB.GetLibraryNode(ctx, nodeID)
	if err != nil || n == nil {
		return fallback
	}
	rel := n.RelPath
	if n.NodeKind == "file" {
		rel = parentRel(rel)
	}
	return libraryFolderBack(it.LibraryID, rel)
}

func urlQueryEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func withinLibrary(root, rel string) (string, error) {
	rel = strings.Trim(strings.ReplaceAll(rel, "\\", "/"), "/")
	if rel == "" || strings.Contains(rel, "..") {
		return "", fmt.Errorf("invalid path")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(filepath.Join(rootAbs, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid path")
	}
	if _, err := os.Stat(abs); err != nil {
		return "", err
	}
	return abs, nil
}

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	lib, err := s.DB.GetLibrary(r.Context(), u.ID, id)
	if err != nil || lib == nil {
		http.NotFound(w, r)
		return
	}
	s.repointLibrary(lib)
	s.render(w, r, "library.html", s.libraryPageData(r.Context(), u, lib, r.URL.Query().Get("sort"), r.URL.Query().Get("q"), r.URL.Query().Get("path"), false))
}

func (s *Server) repointLibrary(lib *db.Library) {
	if s == nil || s.Fetch == nil || s.DB == nil || lib == nil {
		return
	}
	if _, loaded := s.repointOnce.LoadOrStore(lib.ID, true); loaded {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		stale, err := s.Fetch.LibraryUsesStoreMetadata(ctx, lib.ID)
		if err != nil || !stale {
			return
		}
		if job, err := s.DB.RunningScanForLibrary(ctx, lib.ID); err == nil && job != nil {
			s.repointOnce.Delete(lib.ID)
			return
		}
		if err := s.Fetch.RepointLibraryMetadata(ctx, lib); err != nil {
			log.Printf("repoint library %d: %v", lib.ID, err)
			s.repointOnce.Delete(lib.ID)
		}
	}()
}

func (s *Server) handleHXItems(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	lib, _ := s.DB.GetLibrary(r.Context(), u.ID, id)
	if lib == nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, "partials/items_live.html", s.libraryPageData(r.Context(), u, lib, r.URL.Query().Get("sort"), r.URL.Query().Get("q"), r.URL.Query().Get("path"), true))
}

// continueWatching applies an NFO title read-through to the bounded home strip:
// rows are small in number and may be stale until the next rescan.
func (s *Server) continueWatching(ctx context.Context, userID int64, limit int) ([]db.WatchProgress, error) {
	rows, err := s.DB.ContinueWatching(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if t := continueNFOtitle(&rows[i]); t != "" {
			rows[i].Title = t
		}
	}
	return rows, nil
}

func continueNFOtitle(w *db.WatchProgress) string {
	if w == nil {
		return ""
	}
	if w.NodeID.Valid && w.LibraryPath.Valid && w.NodeRelPath.Valid {
		lib := strings.TrimSpace(w.LibraryPath.String)
		rel := strings.TrimSpace(w.NodeRelPath.String)
		if lib != "" && rel != "" {
			abs := filepath.Join(lib, filepath.FromSlash(rel))
			if t := metadata.VideoTitle(abs); t != "" {
				return t
			}
		}
	}
	if w.ItemNFOPath.Valid {
		if p := strings.TrimSpace(w.ItemNFOPath.String); filepath.IsAbs(p) {
			if t := metadata.NFOTitle(p); t != "" {
				return t
			}
		}
	}
	return ""
}

func (s *Server) handleHXContinue(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	cont, _ := s.continueWatching(r.Context(), u.ID, 12)
	s.render(w, r, "partials/continue.html", map[string]any{"Continue": cont})
}

func (s *Server) handleHXRecent(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	recent, _ := s.DB.RecentlyAdded(r.Context(), u.ID, 24)
	s.render(w, r, "partials/recent.html", map[string]any{
		"Recent":        s.mediaItemCards(r.Context(), u.ID, recent),
		"RecentTrigger": recentHXTrigger(s.userHasRunningScan(r.Context(), u.ID), false),
	})
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

func (s *Server) ownedLibraryNode(r *http.Request, id int64) (*db.LibraryNode, *db.Library) {
	u := userFrom(r)
	n, err := s.DB.GetLibraryNode(r.Context(), id)
	if err != nil || n == nil {
		return nil, nil
	}
	lib, err := s.DB.GetLibrary(r.Context(), u.ID, n.LibraryID)
	if err != nil || lib == nil {
		return nil, nil
	}
	return n, lib
}

func (s *Server) handlePlayMovie(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	it := s.ownedMediaItem(r, id)
	if it == nil {
		http.NotFound(w, r)
		return
	}
	title := it.Title
	if t := metadata.NFOTitle(it.NFOPath.String); t != "" {
		title = t
	}
	s.render(w, r, "player.html", map[string]any{
		"Kind": "movie", "ID": id, "Title": title,
		"NavBack":    s.showFolderBack(r.Context(), it),
		"SessionURL": fmt.Sprintf("/play/movie/%d/session", id),
	})
}

func (s *Server) handlePlayFile(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	path, title, ok := s.fileNodePath(r, id)
	if !ok || path == "" {
		http.NotFound(w, r)
		return
	}
	if title == "" {
		title = stripVideoExt(filepath.Base(path))
	}
	n, _ := s.DB.GetLibraryNode(r.Context(), id)
	var libID int64
	folder := ""
	if n != nil {
		libID = n.LibraryID
		folder = parentRel(n.RelPath)
	}
	s.render(w, r, "player.html", map[string]any{
		"Kind": "file", "ID": id, "Title": title, "LibraryID": libID, "Folder": folder,
		"SessionURL": fmt.Sprintf("/play/file/%d/session", id),
	})
}

func (s *Server) fileNodePath(r *http.Request, id int64) (path, title string, ok bool) {
	u := userFrom(r)
	if u == nil {
		return "", "", false
	}
	n, err := s.DB.GetLibraryNode(r.Context(), id)
	if err != nil || n == nil || n.NodeKind != "file" {
		return "", "", false
	}
	lib, err := s.DB.GetLibrary(r.Context(), u.ID, n.LibraryID)
	if err != nil || lib == nil {
		return "", "", false
	}
	abs := filepath.Join(lib.Path, filepath.FromSlash(n.RelPath))
	rel, err := filepath.Rel(lib.Path, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", false
	}
	title = fileTitle(*n, readSideNFO(abs))
	return abs, title, true
}

func (s *Server) handlePlayEpisode(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ep := s.ownedEpisode(r, id)
	if ep == nil {
		http.NotFound(w, r)
		return
	}
	title := ep.Title.String
	if t := metadata.NFOTitle(ep.NFOPath.String); t != "" {
		title = t
	}
	var show *db.MediaItem
	if ep.ShowID > 0 {
		show, _ = s.DB.GetMediaItem(r.Context(), ep.ShowID)
	}
	s.render(w, r, "player.html", map[string]any{
		"Kind": "episode", "ID": id, "Title": title, "ShowID": ep.ShowID,
		"NavBack":    s.showFolderBack(r.Context(), show),
		"SessionURL": fmt.Sprintf("/play/episode/%d/session", id),
	})
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
	case "file":
		path, _, ok := s.fileNodePath(r, id)
		return path, ok
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
	heights := s.Cfg.Transcode.Heights
	encodeHeight, forceEncode := media.ResolvePlaybackHeight(sourceHeight, wantHeight, heights, heightExplicit)

	// Switching away from the default audio on a direct-playable file needs HLS.
	if direct && audioIndex >= 0 && probe != nil && audioIndex != probe.DefaultAudioIndex() {
		direct = false
	}
	if forceEncode {
		direct = false
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
		Mime           string                `json:"mime,omitempty"`
		Title          string                `json:"title,omitempty"`
		Duration       float64               `json:"duration,omitempty"`
		Audio          int                   `json:"audio"`
		Height         int                   `json:"height"`
		Start          float64               `json:"start"`
		Pipeline       string                `json:"pipeline,omitempty"`
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
		Height:         encodeHeight,
		Qualities:      media.QualityOptions(sourceHeight, heights),
		AudioTracks:    audioTracks,
		SubtitleTracks: subTracks,
		Chapters:       chapters,
		Prefs:          prefsOut,
	}
	if dur > 0 {
		resp.Duration = dur
	}
	if kind == "file" {
		if prev, next, err := s.DB.AdjacentFileNodes(r.Context(), id); err == nil {
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
		resp.Mime = media.StreamMIME(path)
		resp.Start = 0
		_ = json.NewEncoder(w).Encode(resp)
		if s.Webhooks != nil && u != nil {
			s.dispatchPlaybackWebhooks(r.Context(), u, kind, id, title, 0, 0, false, webhooks.NotificationPlaybackStart)
		}
		return
	}
	pixFmt := ""
	if probe != nil {
		pixFmt = probe.VideoPixFmt()
	}
	owner := strings.TrimSpace(r.FormValue("tab"))
	if owner == "" {
		owner = transcodeKey(r, u)
	}
	force8Bit := strings.TrimSpace(r.FormValue("bitdepth")) == "8"
	job, err := s.Transcode.Start(r.Context(), owner, path, audioIndex, encodeHeight, startAt, pixFmt, force8Bit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	resp.Mode = "hls"
	resp.URL = fmt.Sprintf("/hls/%s/master.m3u8", job.ID)
	resp.Start = float64(job.StartAt)
	resp.Pipeline = job.Pipeline
	_ = json.NewEncoder(w).Encode(resp)
	if s.Webhooks != nil && u != nil {
		s.dispatchPlaybackWebhooks(r.Context(), u, kind, id, title, 0, 0, false, webhooks.NotificationPlaybackStart)
	}
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
		s.serveSubtitleVTT(w, r, out)
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
	s.serveSubtitleVTT(w, r, out)
}

// serveSubtitleVTT writes a cached WebVTT file. When the player resumes a
// transcode job mid-file it passes ?shift=, the offset its timeline starts at,
// so cue times are moved back to match the replayed media timeline.
func (s *Server) serveSubtitleVTT(w http.ResponseWriter, r *http.Request, file string) {
	shift := 0.0
	if v := r.URL.Query().Get("shift"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n > 0 {
			shift = n
		}
	}
	if shift > 0 {
		f, err := os.Open(file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		var buf bytes.Buffer
		if err := media.WriteShiftedVTT(&buf, f, shift); err != nil {
			log.Printf("shift vtt %s: %v", file, err)
			http.Error(w, "subtitle shift failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, file)
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
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	if abs := strings.TrimSpace(r.URL.Query().Get("path")); abs != "" {
		if !s.sidecarAllowed(r, abs) {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, abs)
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, "/metadata/")
	http.ServeFile(w, r, filepath.Join(s.Cfg.Store.Path, "metadata", rel))
}

func (s *Server) sidecarAllowed(r *http.Request, abs string) bool {
	abs = filepath.Clean(abs)
	if !filepath.IsAbs(abs) || s == nil || s.DB == nil {
		return false
	}
	u := userFrom(r)
	if u == nil {
		return false
	}
	libs, err := s.DB.ListLibraries(r.Context(), u.ID)
	if err != nil {
		return false
	}
	for _, lib := range libs {
		root := filepath.Clean(lib.Path)
		if abs == root || strings.HasPrefix(abs, root+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func (s *Server) handleKPA(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	tab := strings.TrimSpace(r.FormValue("tab"))
	if !validTabID(tab) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	path := r.FormValue("path")
	if len(path) > 512 {
		path = path[:512]
	}
	title := r.FormValue("title")
	if len(title) > 200 {
		title = title[:200]
	}
	if s.Watchdog != nil {
		s.Watchdog.Pulse(u.ID, tab, path, title)
	}
	w.WriteHeader(http.StatusNoContent)
}

func validTabID(id string) bool {
	if id == "" || len(id) > 80 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
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
		if s.Webhooks != nil && u != nil {
			it := s.ownedMediaItem(r, id)
			title := ""
			if it != nil {
				title = it.Title
			}
			if completed {
				s.dispatchPlaybackWebhooks(r.Context(), u, "movie", id, title, pos, dur, true, webhooks.NotificationPlaybackStop)
			} else {
				s.dispatchPlaybackWebhooks(r.Context(), u, "movie", id, title, pos, dur, false, webhooks.NotificationPlaybackProgress)
			}
		}
	}
	if nid := r.FormValue("node_id"); nid != "" {
		id, _ := strconv.ParseInt(nid, 10, 64)
		if _, _, ok := s.fileNodePath(r, id); !ok {
			http.NotFound(w, r)
			return
		}
		_ = s.DB.UpsertWatchNode(r.Context(), u.ID, id, pos, dur, completed)
	}
	if eid := r.FormValue("episode_id"); eid != "" {
		id, _ := strconv.ParseInt(eid, 10, 64)
		if s.ownedEpisode(r, id) == nil {
			http.NotFound(w, r)
			return
		}
		_ = s.DB.UpsertWatchEpisode(r.Context(), u.ID, id, pos, dur, completed)
		if s.Webhooks != nil && u != nil {
			ep := s.ownedEpisode(r, id)
			title := ""
			if ep != nil {
				title = ep.Title.String
				if title == "" {
					title = fmt.Sprintf("Episode %d", ep.EpisodeNumber)
				}
			}
			if completed {
				s.dispatchPlaybackWebhooks(r.Context(), u, "episode", id, title, pos, dur, true, webhooks.NotificationPlaybackStop)
			} else {
				s.dispatchPlaybackWebhooks(r.Context(), u, "episode", id, title, pos, dur, false, webhooks.NotificationPlaybackProgress)
			}
		}
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
	} else if nid := r.FormValue("node_id"); nid != "" {
		id, _ := strconv.ParseInt(nid, 10, 64)
		if _, _, ok := s.fileNodePath(r, id); !ok {
			http.NotFound(w, r)
			return
		}
		scope = nil
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

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, tab string, data map[string]any) {
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	data["SettingsTab"] = tab
	name := "partials/settings_shell.html"
	if r.Header.Get("HX-Target") == "settings-panel" {
		name = "partials/settings_panel.html"
	}
	s.render(w, r, name, data)
}

func (s *Server) finishSettings(w http.ResponseWriter, r *http.Request, tab string, data map[string]any) {
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	data["Notice"] = "Saved"
	data["NoticeLevel"] = "ok"
	r.Header.Set("HX-Target", "settings-panel")
	s.renderSettings(w, r, tab, data)
}

func (s *Server) integrationsData(r *http.Request) (map[string]any, int, error) {
	u := userFrom(r)
	if u == nil {
		return nil, http.StatusUnauthorized, fmt.Errorf("unauthorized")
	}
	if s.Cfg.EnsureIntegrationDefaults() {
		_ = s.Cfg.Save(s.Cfg.OverlaySavePath())
	}
	userWH, err := s.DB.GetUserWebhooks(r.Context(), u.ID)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	return map[string]any{
		"Config":            s.Cfg,
		"UserWebhooks":      userWH.ToConfig(),
		"WebhookKeyMasked":  webhooks.MaskKey(userWH.APIKey),
		"NotificationTypes": webhooks.AllNotificationTypes,
		"ItemTypes":         webhooks.AllItemTypes,
	}, http.StatusOK, nil
}

func (s *Server) accountsData(r *http.Request) (map[string]any, error) {
	users, err := s.accountUsers(r.Context())
	if err != nil {
		return nil, err
	}
	return map[string]any{"Users": users}, nil
}

func (s *Server) accountUsers(ctx context.Context) ([]db.User, error) {
	users, err := s.DB.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]db.User, 0, len(users))
	for _, u := range users {
		if db.IsOverlord(u.Username) {
			continue
		}
		out = append(out, u)
	}
	return out, nil
}

func (s *Server) backupData() map[string]any {
	names, _ := s.Backup.List()
	return map[string]any{
		"Archives": names,
		"Status":   s.Backup.Status(),
		"Config":   s.Cfg,
	}
}

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	dir := "."
	if s.Cfg != nil && strings.TrimSpace(s.Cfg.ExeDir) != "" {
		dir = s.Cfg.ExeDir
	}
	license := ""
	if b, err := os.ReadFile(filepath.Join(dir, "LICENSE")); err == nil {
		license = strings.TrimSpace(string(b))
	}
	if license == "" {
		license = "Unavailable"
	}
	s.render(w, r, "partials/about_body.html", map[string]any{
		"License": license,
	})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u == nil || u.Role != db.RoleAdmin {
		s.handleSettingsIntegrations(w, r)
		return
	}
	s.renderSettings(w, r, "general", map[string]any{"Config": s.Cfg})
}

func (s *Server) handleSaveGeneral(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u == nil || u.Role != db.RoleAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	_ = r.ParseForm()
	ttl, err := strconv.Atoi(strings.TrimSpace(r.FormValue("watchdog_ttl")))
	if err != nil || ttl < 0 {
		http.Error(w, "watchdog ttl must be zero or a positive number", http.StatusBadRequest)
		return
	}
	s.Cfg.Scan.OnStartup = r.FormValue("scan_on_startup") == "on"
	s.Cfg.Watchdog.TTLSeconds = ttl
	if err := s.Cfg.Save(s.Cfg.OverlaySavePath()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.Watchdog != nil {
		s.Watchdog.SetTTL(time.Duration(ttl) * time.Second)
	}
	s.finishSettings(w, r, "general", map[string]any{"Config": s.Cfg})
}

func (s *Server) handleSettingsLibrariesRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleSettingsServer(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleSaveServer(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleSaveBackup(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if v := strings.TrimSpace(r.FormValue("backup_interval")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			http.Error(w, "backup interval must be a positive duration", http.StatusBadRequest)
			return
		}
		s.Cfg.Backup.Interval = d
	}
	if v := r.FormValue("backup_retain"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			s.Cfg.Backup.Retain = n
		}
	}
	s.Cfg.Backup.Enabled = r.FormValue("backup_enabled") == "on"
	_ = s.Cfg.Save(s.Cfg.OverlaySavePath())
	s.Backup.Cfg = s.Cfg
	s.Backup.StartScheduler(context.Background())
	s.finishSettings(w, r, "backup", s.backupData())
}

func (s *Server) handleSettingsBackup(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, "backup", s.backupData())
}

func (s *Server) handleSettingsUsers(w http.ResponseWriter, r *http.Request) {
	data, err := s.accountsData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.renderSettings(w, r, "accounts", data)
}

func (s *Server) handleCreateLibrary(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	_ = r.ParseForm()
	name := r.FormValue("name")
	path := r.FormValue("path")
	if name == "" || path == "" {
		http.Error(w, "bad request", 400)
		return
	}
	clean, err := jailMediaPath(mediaRoots(s.Cfg), path)
	if err != nil {
		http.Error(w, "path must be under the media root", 400)
		return
	}
	st, err := os.Stat(clean)
	if err != nil {
		if os.IsPermission(err) {
			http.Error(w, "permission denied reading path under the media root", 400)
			return
		}
		if os.IsNotExist(err) {
			http.Error(w, "path not found under the media root", 400)
			return
		}
		http.Error(w, "cannot access path under the media root", 400)
		return
	}
	if !st.IsDir() {
		http.Error(w, "path is not a directory under the media root", 400)
		return
	}
	lib, err := s.DB.CreateLibrary(r.Context(), u.ID, name, clean)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	mode, persist, overwrite := parseLibraryScanForm(r)
	s.syncFetchClients()
	if ready, _, _ := s.metaStatus(); !ready {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	jobID, err := s.startLibraryScan(lib, "", mode, persist, overwrite)
	if err != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/?scan=%d&lib=%d", jobID, lib.ID), http.StatusFound)
}

type mediaBrowseDir struct {
	Name string
	Path string
}

type mediaBrowseData struct {
	Path    string
	Parent  string
	Dirs    []mediaBrowseDir
	CanGoUp bool
	Error   string
}

func (s *Server) handleMediaBrowse(w http.ResponseWriter, r *http.Request) {
	roots := mediaRoots(s.Cfg)
	reqPath := r.URL.Query().Get("path")
	browse, err := listMediaDirs(roots, reqPath)
	if err != nil {
		fallback := ""
		if len(roots) == 1 {
			fallback = roots[0]
		}
		browse = &mediaBrowseData{Path: fallback, Error: err.Error()}
	}
	s.render(w, r, "partials/media_browser.html", map[string]any{"MediaBrowse": browse})
}

func mediaRoots(cfg *config.Config) []string {
	if cfg == nil {
		return []string{"/media"}
	}
	return cfg.MediaRoots()
}

func mediaRootIndex(roots []string, p string) int {
	p = filepath.Clean(p)
	for i, r := range roots {
		if filepath.Clean(r) == p {
			return i
		}
	}
	return -1
}

// jailMediaPath ensures path is under one of the media roots.
func jailMediaPath(roots []string, p string) (string, error) {
	if len(roots) == 0 {
		roots = []string{"/media"}
	}
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		if len(roots) == 1 {
			return filepath.Clean(roots[0]), nil
		}
		return "", fmt.Errorf("outside media root")
	}
	clean := filepath.Clean(p)
	if !filepath.IsAbs(clean) {
		if len(roots) != 1 {
			return "", fmt.Errorf("outside media root")
		}
		clean = filepath.Clean(filepath.Join(roots[0], p))
	}
	for _, root := range roots {
		root = filepath.Clean(root)
		if clean == root || strings.HasPrefix(clean, root+string(os.PathSeparator)) {
			return clean, nil
		}
	}
	return "", fmt.Errorf("outside media root")
}

func listVirtualMediaRoots(roots []string) *mediaBrowseData {
	counts := map[string]int{}
	bases := make([]string, len(roots))
	for i, r := range roots {
		bases[i] = filepath.Base(r)
		counts[bases[i]]++
	}
	dirs := make([]mediaBrowseDir, 0, len(roots))
	for i, r := range roots {
		name := bases[i]
		clean := filepath.Clean(r)
		if counts[name] > 1 {
			name = clean
		}
		dirs = append(dirs, mediaBrowseDir{Name: name, Path: clean})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	return &mediaBrowseData{Path: "", Dirs: dirs, CanGoUp: false}
}

func listMediaDirs(roots []string, reqPath string) (*mediaBrowseData, error) {
	if len(roots) == 0 {
		roots = []string{"/media"}
	}
	if len(roots) > 1 && (strings.TrimSpace(reqPath) == "" || reqPath == "/") {
		return listVirtualMediaRoots(roots), nil
	}
	clean, err := jailMediaPath(roots, reqPath)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(clean)
	if err != nil {
		return nil, err
	}
	var dirs []mediaBrowseDir
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(clean, e.Name())
		st, err := os.Stat(p)
		if err != nil || !st.IsDir() {
			continue
		}
		dirs = append(dirs, mediaBrowseDir{Name: e.Name(), Path: p})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	parent := filepath.Dir(clean)
	canUp := true
	if mediaRootIndex(roots, clean) >= 0 {
		if len(roots) == 1 {
			canUp = false
			parent = ""
		} else {
			parent = ""
		}
	} else if p, err := jailMediaPath(roots, parent); err != nil {
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
	// OOB strips only. The leftover empty body removes the card (outerHTML). 204 would not swap.
	cont, _ := s.continueWatching(r.Context(), u.ID, 12)
	recent, _ := s.DB.RecentlyAdded(r.Context(), u.ID, 24)
	s.render(w, r, "partials/library_deleted.html", map[string]any{
		"Continue":      cont,
		"Recent":        s.mediaItemCards(r.Context(), u.ID, recent),
		"RecentTrigger": recentHXTrigger(s.userHasRunningScan(r.Context(), u.ID), false),
		"OOB":           true,
	})
}

func parseLibraryScanForm(r *http.Request) (mode string, persist, overwrite bool) {
	mode = r.FormValue("mode")
	switch mode {
	case "rescan", "changes", "missing", "nfo", "local", "matchmedia", "matchmedia-changes":
	default:
		mode = "changes"
	}
	persist = r.FormValue("persist") == "1" || r.FormValue("persist") == "on" || r.FormValue("persist") == "true"
	overwrite = r.FormValue("overwrite") == "1" || r.FormValue("overwrite") == "on" || r.FormValue("overwrite") == "true"
	switch mode {
	case "nfo", "local":
		persist = false
		overwrite = false
	case "changes", "matchmedia-changes":
		overwrite = false
	case "rescan", "matchmedia":
		overwrite = true
	}
	return mode, persist, overwrite
}

func matchMediaScanMode(mode string) string {
	switch mode {
	case "nfo", "local":
		return "nfo"
	case "changes", "matchmedia-changes":
		return "changes"
	case "missing":
		return "missing"
	default:
		return "rescan"
	}
}

func (s *Server) startLibraryScan(lib *db.Library, scanAbs, mode string, persist, overwrite bool) (int64, error) {
	jobID, err := s.DB.CreateScanJob(context.Background(), lib.ID)
	if err != nil {
		return 0, err
	}
	go s.runLibraryScan(lib, scanAbs, jobID, mode, persist, overwrite)
	return jobID, nil
}

func (s *Server) runLibraryScan(lib *db.Library, scanAbs string, jobID int64, mode string, persist, overwrite bool) {
	ctx := context.Background()
	s.syncFetchClients()
	_ = s.DB.UpdateScanJob(ctx, jobID, "running", 0, "scan 0/0")
	scanMode := matchMediaScanMode(mode)
	if scanMode == "changes" {
		overwrite = false
	}
	opts := fetch.Opts{Persist: persist, Overwrite: overwrite, ScanJobID: jobID, ScanMode: scanMode}
	var err error
	if scanAbs != "" && filepath.Clean(scanAbs) != filepath.Clean(lib.Path) {
		err = s.Fetch.MatchPath(ctx, lib, scanAbs, nil, opts)
	} else {
		err = s.Fetch.MatchLibrary(ctx, lib, opts)
	}
	if err != nil {
		log.Printf("match library %d: %v", lib.ID, err)
		_ = s.DB.UpdateScanJob(ctx, jobID, "error", 100, err.Error())
		return
	}
	_ = s.DB.UpdateScanJob(ctx, jobID, "done", 100, "Complete")
	if s.Webhooks != nil {
		s.dispatchWebhooks(ctx, lib.UserID, webhooks.NotificationTaskCompleted, webhooks.TaskPayload("Library scan complete"))
	}
}

func (s *Server) renderLibraryCard(w http.ResponseWriter, r *http.Request, lib *db.Library, job *db.ScanJob) {
	u := userFrom(r)
	var userID int64
	if u != nil {
		userID = u.ID
	}
	card := s.libraryCardData(r.Context(), userID, lib, job)
	s.render(w, r, "partials/library_card.html", map[string]any{
		"Library": card.Library, "Posters": card.Posters, "Job": card.Job,
		"Message": card.Message, "Lines": card.Lines, "Poll": card.Poll, "ProgressPct": card.ProgressPct,
		"Enriching": card.Enriching, "Watched": card.Watched, "Total": card.Total, "WatchPct": card.WatchPct,
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
	s.syncFetchClients()
	if ready, reason, _ := s.metaStatus(); !ready {
		if reason == "" {
			reason = "Metadata service unavailable"
		}
		http.Error(w, reason, 400)
		return
	}
	mode, persist, overwrite := parseLibraryScanForm(r)
	scanAbs := ""
	if rel := strings.TrimSpace(r.FormValue("path")); rel != "" {
		var err error
		scanAbs, err = withinLibrary(lib.Path, rel)
		if err != nil {
			http.Error(w, "invalid path", 400)
			return
		}
	}
	jobID, err := s.startLibraryScan(lib, scanAbs, mode, persist, overwrite)
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
	s.renderLibraryCardStatus(w, r, lib, j)
}

func (s *Server) renderLibraryCardStatus(w http.ResponseWriter, r *http.Request, lib *db.Library, job *db.ScanJob) {
	u := userFrom(r)
	var userID int64
	if u != nil {
		userID = u.ID
	}
	card := s.libraryCardData(r.Context(), userID, lib, job)
	s.render(w, r, "partials/library_card_status_poll.html", map[string]any{
		"Library": card.Library, "Job": card.Job, "Posters": card.Posters,
		"Message": card.Message, "Lines": card.Lines, "Poll": card.Poll, "ProgressPct": card.ProgressPct,
		"Enriching": card.Enriching, "Total": card.Total, "WatchPct": card.WatchPct,
	})
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
	case "local", "matchmedia", "dissociate":
	default:
		mode = "local"
	}
	persist := r.FormValue("persist") == "1" || r.FormValue("persist") == "on" || r.FormValue("persist") == "true"
	overwrite := r.FormValue("overwrite") == "1" || r.FormValue("overwrite") == "on" || r.FormValue("overwrite") == "true"
	if mode == "local" || mode == "dissociate" {
		persist = false
		overwrite = false
	}
	if mode == "matchmedia" {
		overwrite = true
	}
	if mode == "matchmedia" || mode == "local" {
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
	queryTitle := ""
	if mode == "matchmedia" {
		queryTitle = strings.TrimSpace(r.FormValue("title"))
		if queryTitle != "" {
			_ = s.DB.UpdateMediaItemTitle(r.Context(), it.ID, queryTitle)
			it.Title = queryTitle
			it.SortTitle = queryTitle
		} else {
			queryTitle = strings.TrimSpace(it.Title)
		}
	}
	go s.runMediaScan(lib, it, jobID, mode, persist, overwrite, queryTitle)
	j, _ := s.DB.GetScanJob(r.Context(), jobID)
	if j == nil {
		j = &db.ScanJob{ID: jobID, Status: "running", ProgressPct: 0, LibraryID: sql.NullInt64{Int64: lib.ID, Valid: true}}
	}
	poll := j.Status == "running"
	s.render(w, r, "partials/entry_scan_progress.html", entryScanProgressData(j, poll, it.ID, mode == "matchmedia", it.MatchStatus.String))
}

func (s *Server) runMediaScan(lib *db.Library, item *db.MediaItem, jobID int64, mode string, persist, overwrite bool, queryTitle string) {
	ctx := context.Background()
	s.syncFetchClients()
	switch mode {
	case "matchmedia":
		_ = s.DB.UpdateScanJob(ctx, jobID, "running", 0, "Matching…")
		opts := fetch.Opts{Persist: persist, Overwrite: overwrite, ScanJobID: jobID, QueryTitle: strings.TrimSpace(queryTitle), ScanMode: "rescan"}
		if err := s.Fetch.MatchItem(ctx, lib, item, opts); err != nil {
			log.Printf("match media %d: %v", item.ID, err)
			_ = s.DB.UpdateScanJob(ctx, jobID, "error", 100, err.Error())
			return
		}
		_ = s.DB.UpdateScanJob(ctx, jobID, "done", 100, "Complete")
	case "dissociate":
		if err := s.Fetch.DissociateMediaItem(ctx, lib, item, jobID); err != nil {
			log.Printf("dissociate media %d: %v", item.ID, err)
			_ = s.DB.UpdateScanJob(ctx, jobID, "error", 100, err.Error())
			return
		}
		_ = s.DB.UpdateScanJob(ctx, jobID, "done", 100, "Complete")
	default:
		_ = s.DB.UpdateScanJob(ctx, jobID, "running", 0, "Matching…")
		opts := fetch.Opts{Persist: persist, Overwrite: overwrite, ScanJobID: jobID, ScanMode: "nfo"}
		if err := s.Fetch.MatchItem(ctx, lib, item, opts); err != nil {
			log.Printf("match media %d: %v", item.ID, err)
			_ = s.DB.UpdateScanJob(ctx, jobID, "error", 100, err.Error())
			return
		}
		_ = s.DB.UpdateScanJob(ctx, jobID, "done", 100, "Complete")
	}
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
	mediaID, _ := strconv.ParseInt(r.URL.Query().Get("media"), 10, 64)
	matchMedia := r.URL.Query().Get("matchmedia") == "1"
	matchStatus := ""
	if mediaID > 0 {
		if it := s.ownedMediaItem(r, mediaID); it != nil {
			matchStatus = it.MatchStatus.String
		}
	}
	poll := j.Status == "running"
	s.render(w, r, "partials/entry_scan_progress.html", entryScanProgressData(j, poll, mediaID, matchMedia, matchStatus))
}

func entryScanProgressData(j *db.ScanJob, poll bool, mediaID int64, matchMedia bool, matchStatus string) map[string]any {
	needPick := false
	reload := false
	if j != nil && j.Status == "done" {
		if matchMedia && mediaID > 0 && (matchStatus == "manual" || matchStatus == "unmatched") {
			needPick = true
		} else {
			reload = true
		}
	}
	var lines []scanProgress
	if j != nil && j.Message.Valid {
		lines = parseScanLines(j.Message.String)
	}
	return map[string]any{
		"Job": j, "Poll": poll, "MediaID": mediaID, "MatchMedia": matchMedia,
		"NeedPick": needPick, "Reload": reload, "Lines": lines,
	}
}

func parseScanLines(msg string) []scanProgress {
	var out []scanProgress
	for _, line := range strings.Split(msg, "\n") {
		if p := parseScanProgress(strings.TrimSpace(line)); p.Phase != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseScanProgress(msg string) scanProgress {
	msg = strings.TrimSpace(msg)
	phase, rest, ok := strings.Cut(msg, " ")
	if !ok {
		return scanProgress{}
	}
	switch phase {
	case "scan", "match", "fetch", "save", "stills":
	default:
		return scanProgress{}
	}
	head, tail, _ := strings.Cut(rest, " ")
	done, total, ok := parsePhaseCount(head)
	if !ok {
		return scanProgress{}
	}
	out := scanProgress{Phase: phase, Done: done, Total: total}
	switch phase {
	case "scan", "stills":
		out.Title = strings.TrimSpace(tail)
	case "match":
		out.Stats, out.Title = parseScanStats(tail, []string{"matched", "manual", "unmatched", "error", "sidecar", "replay", "folded"})
	case "fetch", "save":
		out.Stats, out.Title = parseScanStats(tail, []string{"nfo", "episode", "poster"})
	}
	return out
}

func parsePhaseCount(head string) (done, total int, ok bool) {
	if doneS, totalS, slash := strings.Cut(head, "/"); slash {
		d, err1 := strconv.Atoi(doneS)
		t, err2 := strconv.Atoi(totalS)
		if err1 != nil || err2 != nil {
			return 0, 0, false
		}
		return d, t, true
	}
	d, err := strconv.Atoi(head)
	if err != nil {
		return 0, 0, false
	}
	return d, 0, true
}

func parseScanStats(s string, keys []string) ([]scanStat, string) {
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	fields := strings.Fields(s)
	var stats []scanStat
	i := 0
	for i < len(fields) {
		k, v, ok := strings.Cut(fields[i], ":")
		if !ok || !want[k] {
			break
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			break
		}
		stats = append(stats, scanStat{Label: k, Value: n, Tone: k})
		i++
	}
	return stats, strings.Join(fields[i:], " ")
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
	names, _ := s.Backup.List()
	s.render(w, r, "partials/backup_status_live.html", map[string]any{
		"Status":   s.Backup.Status(),
		"Archives": names,
	})
}

func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := r.FormValue("name")
	if err := s.Backup.Restore(r.Context(), name, s.reopen); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.finishSettings(w, r, "backup", s.backupData())
}

func (s *Server) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.Backup.Delete(name); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	names, _ := s.Backup.List()
	s.render(w, r, "partials/backup_archives.html", map[string]any{"Archives": names})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	username := strings.TrimSpace(r.FormValue("username"))
	pass := r.FormValue("password")
	confirm := r.FormValue("confirm")
	if db.IsOverlord(username) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if username == "" || pass == "" || pass != confirm {
		http.Error(w, "invalid input or passwords do not match", http.StatusBadRequest)
		return
	}
	_, err := s.DB.CreateUser(r.Context(), username, pass, db.RoleUser)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if u, err := s.DB.GetUserByUsername(r.Context(), username); err == nil && u != nil && s.Webhooks != nil {
		s.dispatchWebhooks(r.Context(), u.ID, webhooks.NotificationUserCreated, webhooks.UserPayload(u))
	}
	data, err := s.accountsData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.finishSettings(w, r, "accounts", data)
}

func (s *Server) handleResetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	_ = r.ParseForm()
	pass := r.FormValue("password")
	confirm := r.FormValue("confirm")
	if pass == "" || pass != confirm {
		http.Error(w, "invalid input or passwords do not match", http.StatusBadRequest)
		return
	}
	u, err := s.DB.GetUser(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if u == nil || u.Role == db.RoleAdmin || db.IsOverlord(u.Username) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := s.DB.UpdateUserPassword(r.Context(), id, pass); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	data, err := s.accountsData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.finishSettings(w, r, "accounts", data)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	u, _ := s.DB.GetUser(r.Context(), id)
	if u != nil && db.IsOverlord(u.Username) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var whCfg *config.WebhooksConfig
	if u != nil {
		if userWH, err := s.DB.GetUserWebhooks(r.Context(), u.ID); err == nil && userWH != nil {
			cfg := userWH.ToConfig()
			whCfg = &cfg
		}
	}
	if err := s.DB.DeleteUser(r.Context(), id); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if u != nil && s.Webhooks != nil && whCfg != nil {
		s.Webhooks.Dispatch(r.Context(), whCfg, s.serverID(), webhooks.NotificationUserDeleted, webhooks.UserPayload(u))
	}
	users, err := s.accountUsers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, r, "partials/users_table_body.html", map[string]any{"Users": users})
}

func (s *Server) handleSettingsIntegrations(w http.ResponseWriter, r *http.Request) {
	data, code, err := s.integrationsData(r)
	if err != nil {
		http.Error(w, err.Error(), code)
		return
	}
	s.renderSettings(w, r, "webhooks", data)
}

func (s *Server) handleSaveIntegrations(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	_ = r.ParseForm()
	wh := config.WebhooksConfig{
		Enabled:   r.FormValue("webhook_enabled") == "on",
		ServerURL: strings.TrimSpace(r.FormValue("webhook_server_url")),
	}
	wh.Destinations = parseWebhookDestinationsFromForm(r)
	if _, err := s.DB.SaveUserWebhooks(r.Context(), u.ID, wh); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	data, code, err := s.integrationsData(r)
	if err != nil {
		http.Error(w, err.Error(), code)
		return
	}
	s.finishSettings(w, r, "webhooks", data)
}

func parseWebhookDestinationsFromForm(r *http.Request) []config.WebhookDestination {
	destCount, _ := strconv.Atoi(r.FormValue("dest_count"))
	if destCount < 0 {
		destCount = 0
	}
	dests := make([]config.WebhookDestination, 0, destCount)
	for i := 0; i < destCount; i++ {
		prefix := fmt.Sprintf("dest_%d_", i)
		name := strings.TrimSpace(r.FormValue(prefix + "name"))
		url := strings.TrimSpace(r.FormValue(prefix + "url"))
		if name == "" && url == "" {
			continue
		}
		dest := config.WebhookDestination{
			Name:              name,
			URL:               url,
			Enabled:           r.FormValue(prefix+"enabled") == "on",
			NotificationTypes: r.Form[prefix+"types"],
			ItemTypes:         r.Form[prefix+"items"],
			Template:          r.FormValue(prefix + "template"),
		}
		headerCount, _ := strconv.Atoi(r.FormValue(prefix + "header_count"))
		for j := 0; j < headerCount; j++ {
			hk := strings.TrimSpace(r.FormValue(fmt.Sprintf("%sheader_%d_key", prefix, j)))
			hv := r.FormValue(fmt.Sprintf("%sheader_%d_val", prefix, j))
			if hk != "" {
				dest.Headers = append(dest.Headers, config.WebhookHeader{Key: hk, Value: hv})
			}
		}
		dests = append(dests, dest)
	}
	return dests
}

func (s *Server) handleRegenerateWebhookKey(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if _, err := s.DB.RegenerateUserWebhookKey(r.Context(), u.ID); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	data, code, err := s.integrationsData(r)
	if err != nil {
		http.Error(w, err.Error(), code)
		return
	}
	s.finishSettings(w, r, "webhooks", data)
}

func (s *Server) handleTestWebhooks(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.Webhooks == nil {
		http.Error(w, "webhooks unavailable", 500)
		return
	}
	userWH, err := s.DB.GetUserWebhooks(r.Context(), u.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	cfg := userWH.ToConfig()
	sent, errs := s.Webhooks.DispatchTest(r.Context(), &cfg, s.serverID(), u.Username)
	msg := fmt.Sprintf("Sent to %d destination(s).", sent)
	if len(errs) > 0 {
		msg += " Errors: " + strings.Join(errs, "; ")
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(msg))
}

func (s *Server) serverID() string {
	if s.Cfg.Integrations.Webhooks.ServerID == "" {
		s.Cfg.EnsureIntegrationDefaults()
	}
	return s.Cfg.Integrations.Webhooks.ServerID
}

func (s *Server) dispatchWebhooks(ctx context.Context, userID int64, notificationType string, extra map[string]any) {
	if s.Webhooks == nil || userID == 0 {
		return
	}
	userWH, err := s.DB.GetUserWebhooks(ctx, userID)
	if err != nil || userWH == nil {
		return
	}
	cfg := userWH.ToConfig()
	s.Webhooks.Dispatch(ctx, &cfg, s.serverID(), notificationType, extra)
}

func (s *Server) dispatchPlaybackWebhooks(ctx context.Context, u *db.User, kind string, id int64, title string, pos, dur float64, completed bool, notificationType string) {
	if u == nil {
		return
	}
	s.dispatchWebhooks(ctx, u.ID, notificationType, webhooks.PlaybackPayload(kind, id, title, u.Username, u.ID, pos, dur, completed))
}

func (s *Server) DispatchItemAdded(ctx context.Context, userID int64, item *db.MediaItem) {
	s.dispatchWebhooks(ctx, userID, webhooks.NotificationItemAdded, webhooks.MediaItemPayload(item))
}

func (s *Server) DispatchEpisodeAdded(ctx context.Context, userID int64, ep *db.Episode, show *db.MediaItem) {
	s.dispatchWebhooks(ctx, userID, webhooks.NotificationItemAdded, webhooks.EpisodePayload(ep, show))
}

func (s *Server) DispatchTaskCompleted(ctx context.Context, userID int64, message string) {
	s.dispatchWebhooks(ctx, userID, webhooks.NotificationTaskCompleted, webhooks.TaskPayload(message))
}
