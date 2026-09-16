package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyshmahell/servemedia/internal/db"
	"github.com/alyshmahell/servemedia/internal/metadata"
)

// DissociateMediaItem removes NFO/poster files (store and beside-media),
// clears UI metadata, reverts the title to the path name, and skips later
// library MatchMedia scans until the user rematches from the entry modal.
func (s *Scanner) DissociateMediaItem(ctx context.Context, lib *db.Library, item *db.MediaItem, jobID int64) error {
	if s == nil || item == nil || lib == nil {
		return fmt.Errorf("missing media item")
	}
	_ = s.DB.UpdateScanJob(ctx, jobID, "running", 20, "Removing NFO and posters…")
	s.removeBesideSidecars(ctx, item)
	title, year := dissociateTitleYear(item)
	_ = s.DB.UpdateScanJob(ctx, jobID, "running", 70, "Clearing metadata…")
	if err := s.DB.DissociateMediaItem(ctx, item.ID, title, year, s.StorePath); err != nil {
		return err
	}
	if item.Kind == "show" {
		eps, err := s.DB.ListEpisodesByShow(ctx, item.ID)
		if err != nil {
			return err
		}
		for _, ep := range eps {
			t := metadata.CleanEpisodeTitle(filepath.Base(ep.Path))
			if t == "" {
				t = filepath.Base(ep.Path)
			}
			_ = s.DB.SetEpisodeTitle(ctx, ep.ID, t)
		}
	}
	return nil
}

func dissociateTitleYear(item *db.MediaItem) (string, int) {
	if item.Kind == "show" {
		title := metadata.CleanEpisodeTitle(filepath.Base(item.Path))
		if title == "" {
			title = filepath.Base(item.Path)
		}
		return title, 0
	}
	title, year := metadata.TitleYearFromVideoPath(item.Path)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(item.Path), filepath.Ext(item.Path))
	}
	if title == "" {
		title = item.Title
	}
	return title, year
}

func (s *Scanner) removeBesideSidecars(ctx context.Context, item *db.MediaItem) {
	if item == nil {
		return
	}
	switch item.Kind {
	case "movie":
		removeMovieBesideSidecars(item.Path)
	case "show":
		s.removeShowBesideSidecars(ctx, item)
	}
}

func removeMovieBesideSidecars(videoPath string) {
	dir := filepath.Dir(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	if metadata.PreferBareMovieSidecar(videoPath) {
		removeExistingFiles(
			filepath.Join(dir, "movie.nfo"),
			filepath.Join(dir, base+".nfo"),
			filepath.Join(dir, "poster.jpg"),
			filepath.Join(dir, "poster.png"),
			filepath.Join(dir, "poster.webp"),
			filepath.Join(dir, "folder.jpg"),
			filepath.Join(dir, "folder.png"),
			filepath.Join(dir, "fanart.jpg"),
			filepath.Join(dir, "backdrop.jpg"),
			filepath.Join(dir, "banner.jpg"),
			filepath.Join(dir, base+"-poster.jpg"),
			filepath.Join(dir, base+"-poster.png"),
			filepath.Join(dir, base+"-poster.webp"),
			filepath.Join(dir, base+"-fanart.jpg"),
			filepath.Join(dir, base+"-fanart.png"),
		)
		return
	}
	removeExistingFiles(
		filepath.Join(dir, base+".nfo"),
		filepath.Join(dir, base+"-poster.jpg"),
		filepath.Join(dir, base+"-poster.png"),
		filepath.Join(dir, base+"-poster.webp"),
		filepath.Join(dir, base+"-fanart.jpg"),
		filepath.Join(dir, base+"-fanart.png"),
		filepath.Join(dir, base+"-backdrop.jpg"),
		filepath.Join(dir, base+"-banner.jpg"),
	)
}

func (s *Scanner) removeShowBesideSidecars(ctx context.Context, item *db.MediaItem) {
	showPath := item.Path
	removeExistingFiles(
		filepath.Join(showPath, "tvshow.nfo"),
		filepath.Join(showPath, "poster.jpg"),
		filepath.Join(showPath, "poster.png"),
		filepath.Join(showPath, "poster.webp"),
		filepath.Join(showPath, "folder.jpg"),
		filepath.Join(showPath, "folder.png"),
		filepath.Join(showPath, "fanart.jpg"),
	)
	if t := metadata.SanitizePathSegment(item.Title); t != "" {
		removeExistingFiles(
			filepath.Join(showPath, t+"-poster.jpg"),
			filepath.Join(showPath, t+"-poster.png"),
		)
	}
	dirs := map[string]struct{}{showPath: {}}
	seasons, _ := s.DB.ListSeasons(ctx, item.ID)
	eps, _ := s.DB.ListEpisodesByShow(ctx, item.ID)
	for _, ep := range eps {
		dirs[filepath.Dir(ep.Path)] = struct{}{}
		base := strings.TrimSuffix(filepath.Base(ep.Path), filepath.Ext(ep.Path))
		epDir := filepath.Dir(ep.Path)
		removeExistingFiles(
			filepath.Join(epDir, "episode.nfo"),
			filepath.Join(epDir, base+".nfo"),
			filepath.Join(epDir, "thumb.jpg"),
			filepath.Join(epDir, "thumb.png"),
			filepath.Join(epDir, base+"-thumb.jpg"),
			filepath.Join(epDir, base+"-thumb.png"),
			filepath.Join(epDir, base+".jpg"),
			filepath.Join(epDir, base+".png"),
		)
	}
	for _, season := range seasons {
		n := season.SeasonNumber
		base := fmt.Sprintf("season%02d", n)
		baseAlt := fmt.Sprintf("season%d", n)
		for dir := range dirs {
			if p := metadata.FindSeasonNFO(dir, n); p != "" {
				removeExistingFiles(p)
			}
			removeExistingFiles(
				filepath.Join(dir, base+"-poster.jpg"),
				filepath.Join(dir, base+"-poster.png"),
				filepath.Join(dir, baseAlt+"-poster.jpg"),
				filepath.Join(dir, baseAlt+"-poster.png"),
				filepath.Join(dir, "poster.jpg"),
				filepath.Join(dir, "poster.png"),
				filepath.Join(dir, "folder.jpg"),
				filepath.Join(dir, "folder.png"),
			)
		}
	}
}

func removeExistingFiles(paths ...string) {
	for _, p := range paths {
		if p == "" {
			continue
		}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(p))
		switch ext {
		case ".nfo", ".jpg", ".jpeg", ".png", ".webp":
			_ = os.Remove(p)
		}
	}
}
