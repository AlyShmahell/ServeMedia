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
// numbers (MatchMedia files[]). Only positive season and episode numbers count.
func (w *Worker) IngestNumberedEpisodes(ctx context.Context, showID int64, eps []NumberedEpisode) error {
	for _, ep := range eps {
		path := cleanMediaPath(ep.Path)
		if path == "" || !metadata.IsVideo(filepath.Base(path)) {
			continue
		}
		if ep.Season <= 0 || ep.Episode <= 0 {
			continue
		}
		if err := w.ingestEpisodeAt(ctx, showID, path, ep.Season, ep.Episode); err != nil {
			return err
		}
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
	nfoPath := ""
	if nfoSrc := metadata.FindSidecar(dir, base, "episode.nfo", base+".nfo"); nfoSrc != "" {
		if n, err := metadata.ReadEpisodeNFO(nfoSrc); err == nil {
			if n.Title != "" {
				title = n.Title
			}
			epPlot = n.Plot
			runtime = n.Runtime
		}
		nfoPath = nfoSrc
	}
	stillPath := metadata.FindSidecar(dir, base, "thumb.jpg", "thumb.png", base+"-thumb.jpg", base+".jpg", base+".png")
	if nfoPath == "" || stillPath == "" {
		if prevID, err := w.DB.EpisodeIDByPath(ctx, path); err == nil && prevID > 0 {
			if prev, err := w.DB.GetEpisode(ctx, prevID); err == nil && prev != nil {
				if nfoPath == "" && prev.NFOPath.Valid {
					nfoPath = prev.NFOPath.String
				}
				if stillPath == "" && prev.StillPath.Valid {
					stillPath = prev.StillPath.String
				}
			}
		}
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
		StillPath:      sql.NullString{String: stillPath, Valid: stillPath != ""},
		NFOPath:        sql.NullString{String: nfoPath, Valid: nfoPath != ""},
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
	poster := findSeasonPoster(seasonDir, showRoot, seasonNum)
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
	return w.DB.UpsertSeason(ctx, showID, seasonNum, title, poster, plot)
}

// findSeasonPoster returns the season artwork already beside the media.
func findSeasonPoster(seasonDir, showRoot string, seasonNum int) string {
	dirs := []string{seasonDir}
	if showRoot != "" && filepath.Clean(showRoot) != filepath.Clean(seasonDir) {
		dirs = append(dirs, showRoot)
	}
	base := fmt.Sprintf("season%02d", seasonNum)
	baseAlt := fmt.Sprintf("season%d", seasonNum)
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
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
			return p
		}
	}
	return ""
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
