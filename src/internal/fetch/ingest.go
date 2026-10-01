package fetch

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

type WebhookNotifier interface {
	DispatchItemAdded(ctx context.Context, userID int64, item *db.MediaItem)
	DispatchEpisodeAdded(ctx context.Context, userID int64, ep *db.Episode, show *db.MediaItem)
	DispatchTaskCompleted(ctx context.Context, userID int64, message string)
}

// NumberedEpisode is a video with MatchMedia-assigned season/episode numbers.
type NumberedEpisode struct {
	Path    string
	Season  int
	Episode int
}

// IngestNumberedEpisodes upserts videos using caller-supplied season/episode
// numbers (MatchMedia files[]). Season 0 is kinds/extras on the parent show.
func (w *Worker) IngestNumberedEpisodes(ctx context.Context, showID int64, eps []NumberedEpisode) error {
	for _, ep := range eps {
		path := strings.TrimSpace(ep.Path)
		if path == "" || !metadata.IsVideo(filepath.Base(path)) {
			continue
		}
		if err := w.ingestEpisodeAt(ctx, showID, path, ep.Season, ep.Episode); err != nil {
			return err
		}
	}
	return nil
}

// IngestVideosAsSeason0 appends videos as the next season-0 episodes.
func (w *Worker) IngestVideosAsSeason0(ctx context.Context, showID int64, paths []string) error {
	next := 1
	if n, err := w.DB.MaxEpisodeNumber(ctx, showID, 0); err == nil {
		next = n + 1
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if path == "" || seen[path] || !metadata.IsVideo(filepath.Base(path)) {
			continue
		}
		if ok, _ := w.DB.EpisodeExistsAtPath(ctx, showID, path); ok {
			seen[path] = true
			continue
		}
		if err := w.ingestEpisodeAt(ctx, showID, path, 0, next); err != nil {
			return err
		}
		seen[path] = true
		next++
	}
	return nil
}

func (w *Worker) ingestEpisodeAt(ctx context.Context, showID int64, path string, season, episode int) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	seasonID, err := w.ensureSeason(ctx, showID, season, filepath.Dir(path))
	if err != nil {
		return err
	}
	title := metadata.CleanEpisodeTitle(filepath.Base(path))
	dir := filepath.Dir(path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	epPlot := ""
	runtime := 0
	nfoRel := ""
	nfoSrc := metadata.FindSidecar(dir, base, "episode.nfo", base+".nfo")
	if nfoSrc != "" {
		if n, err := metadata.ReadEpisodeNFO(nfoSrc); err == nil {
			if n.Title != "" {
				title = n.Title
			}
			epPlot = n.Plot
			runtime = n.Runtime
		}
		show, _ := w.DB.GetMediaItem(ctx, showID)
		showKey := "show"
		if show != nil {
			showKey = metadata.SanitizePathSegment(show.Title)
		}
		nfoRel = filepath.Join("metadata", "tv", showKey, fmt.Sprintf("S%02d", season), base+".nfo")
		_ = metadata.CopyFile(nfoSrc, filepath.Join(w.Store, nfoRel))
	}
	stillRel := ""
	if p := metadata.FindSidecar(dir, base, "thumb.jpg", "thumb.png", base+"-thumb.jpg", base+".jpg", base+".png"); p != "" {
		show, _ := w.DB.GetMediaItem(ctx, showID)
		showKey := "show"
		if show != nil {
			showKey = metadata.SanitizePathSegment(show.Title)
		}
		ext := filepath.Ext(p)
		if ext == "" {
			ext = ".jpg"
		}
		stillRel = filepath.Join("metadata", "tv", showKey, fmt.Sprintf("S%02d", season), base+"-thumb"+ext)
		_ = metadata.CopyFile(p, filepath.Join(w.Store, stillRel))
	}
	existed, _ := w.DB.EpisodeExistsAtPath(ctx, showID, path)
	id, err := w.DB.UpsertEpisode(ctx, db.Episode{
		SeasonID:       seasonID,
		ShowID:         showID,
		EpisodeNumber:  episode,
		Title:          sql.NullString{String: title, Valid: true},
		Path:           path,
		RuntimeSeconds: nullInt(runtime),
		Plot:           sql.NullString{String: epPlot, Valid: epPlot != ""},
		StillPath:      sql.NullString{String: stillRel, Valid: stillRel != ""},
		NFOPath:        sql.NullString{String: nfoRel, Valid: nfoRel != ""},
		Mtime:          info.ModTime().Unix(),
	})
	if err == nil && !existed && w.Webhooks != nil {
		ep := db.Episode{
			ID: id, SeasonID: seasonID, ShowID: showID, EpisodeNumber: episode,
			Title: sql.NullString{String: title, Valid: true}, Path: path,
		}
		show, _ := w.DB.GetMediaItem(ctx, showID)
		ownerID := w.showOwnerID(ctx, showID)
		w.Webhooks.DispatchEpisodeAdded(ctx, ownerID, &ep, show)
	}
	return err
}

// ensureSeason upserts season metadata from the episode's directory and show root.
func (w *Worker) ensureSeason(ctx context.Context, showID int64, seasonNum int, episodeDir string) (int64, error) {
	seasonDir := episodeDir
	title := ""
	plot := ""
	show, _ := w.DB.GetMediaItem(ctx, showID)
	showRoot := ""
	if show != nil {
		showRoot = show.Path
	}
	nfoDirs := []string{seasonDir}
	if showRoot != "" && filepath.Clean(showRoot) != filepath.Clean(seasonDir) {
		nfoDirs = append(nfoDirs, showRoot)
	}
	for _, dir := range nfoDirs {
		if nfo := metadata.FindSeasonNFO(dir, seasonNum); nfo != "" {
			if n, err := metadata.ReadSeasonNFO(nfo); err == nil {
				if n.Title != "" {
					title = n.Title
				}
				if n.Plot != "" {
					plot = n.Plot
				}
				break
			}
		}
	}
	showKey := "show"
	if show != nil {
		showKey = metadata.SanitizePathSegment(show.Title)
	}
	posterRel := ""
	posterDirs := []string{seasonDir}
	if showRoot != "" && filepath.Clean(showRoot) != filepath.Clean(seasonDir) {
		posterDirs = append(posterDirs, showRoot)
	}
	base := fmt.Sprintf("season%02d", seasonNum)
	baseAlt := fmt.Sprintf("season%d", seasonNum)
	for _, dir := range posterDirs {
		p := metadata.FindSidecar(dir, base, "poster.jpg", "folder.jpg", "poster.png", "folder.png")
		if p == "" {
			p = metadata.FindSidecar(dir, baseAlt, "poster.jpg", "folder.jpg", "poster.png", "folder.png")
		}
		if p == "" {
			for _, name := range []string{
				base + "-poster.jpg", base + "-poster.png",
				baseAlt + "-poster.jpg", baseAlt + "-poster.png",
			} {
				cand := filepath.Join(dir, name)
				if fileExists(cand) {
					p = cand
					break
				}
			}
		}
		if p != "" {
			posterRel = filepath.Join("metadata", "tv", showKey, fmt.Sprintf("S%02d", seasonNum), "poster"+filepath.Ext(p))
			_ = metadata.CopyFile(p, filepath.Join(w.Store, posterRel))
			break
		}
	}
	if title == "" {
		existing, _ := w.DB.GetSeasonByShowNum(ctx, showID, seasonNum)
		if existing == nil {
			if seasonNum == 0 {
				title = "Specials"
			} else {
				title = fmt.Sprintf("Season %d", seasonNum)
			}
		}
	}
	return w.DB.UpsertSeason(ctx, showID, seasonNum, title, posterRel, plot)
}

func nullInt(y int) sql.NullInt64 {
	if y == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(y), Valid: true}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (w *Worker) showOwnerID(ctx context.Context, showID int64) int64 {
	show, err := w.DB.GetMediaItem(ctx, showID)
	if err != nil || show == nil {
		return 0
	}
	var uid int64
	if err := w.DB.SQL.QueryRowContext(ctx, `SELECT user_id FROM libraries WHERE id = ?`, show.LibraryID).Scan(&uid); err != nil {
		return 0
	}
	return uid
}
