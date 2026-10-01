package fetch

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/media"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

var extractStill = media.ExtractStillJPEG

func (w *Worker) extractMissingStills(ctx context.Context, lib *db.Library, only *db.MediaItem, opts Opts) error {
	if w == nil || w.DB == nil || lib == nil {
		return nil
	}
	w.progress(ctx, opts.ScanJobID, 95, "Extracting stills…")
	if only != nil {
		got, err := w.DB.GetMediaItem(ctx, only.ID)
		if err != nil || got == nil {
			return err
		}
		return w.extractItemStills(ctx, got, opts.Persist)
	}
	items, err := w.DB.ListAllMediaItems(ctx, lib.ID)
	if err != nil {
		return err
	}
	for i := range items {
		if err := w.extractItemStills(ctx, &items[i], opts.Persist); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) extractItemStills(ctx context.Context, it *db.MediaItem, persist bool) error {
	if it == nil {
		return nil
	}
	switch it.Kind {
	case "movie":
		if !emptyPath(it.PosterPath) {
			return nil
		}
		return w.extractMovieStill(ctx, it, persist)
	case "show":
		seasons, err := w.DB.ListSeasons(ctx, it.ID)
		if err != nil {
			return err
		}
		for i := range seasons {
			eps, err := w.DB.ListEpisodes(ctx, seasons[i].ID)
			if err != nil {
				return err
			}
			for j := range eps {
				if !emptyPath(eps[j].StillPath) {
					continue
				}
				if err := w.extractEpisodeStill(ctx, it, &seasons[i], &eps[j], persist); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func copyIfAbsent(src, dst string) error {
	if stillFileOK(dst) {
		return nil
	}
	return metadata.CopyFile(src, dst)
}

func (w *Worker) extractMovieStill(ctx context.Context, it *db.MediaItem, persist bool) error {
	year := 0
	if it.Year.Valid {
		year = int(it.Year.Int64)
	}
	cacheKey := metadata.SanitizePathSegment(metadata.TitleYear(it.Title, year))
	if cacheKey == "" {
		cacheKey = metadata.SanitizePathSegment(strings.TrimSuffix(filepath.Base(it.Path), filepath.Ext(it.Path)))
	}
	cacheDir := filepath.Join(w.Store, "metadata", "movies", cacheKey)
	dst := filepath.Join(cacheDir, "poster.jpg")
	posterRel := filepath.Join("metadata", "movies", cacheKey, "poster.jpg")
	if !stillFileOK(dst) {
		if err := extractStill(it.Path, dst); err != nil {
			log.Printf("still %s: %v", it.Path, err)
			return nil
		}
		if !stillFileOK(dst) {
			return nil
		}
	}
	if persist {
		mediaDir := filepath.Dir(it.Path)
		base := strings.TrimSuffix(filepath.Base(it.Path), filepath.Ext(it.Path))
		beside := filepath.Join(mediaDir, base+"-poster.jpg")
		if metadata.PreferBareMovieSidecar(it.Path) {
			beside = filepath.Join(mediaDir, "poster.jpg")
		}
		_ = copyIfAbsent(dst, beside)
	}
	return w.DB.UpdateMediaItemMeta(ctx, it.ID, "", year, "", posterRel, "", "", 0, "", "")
}

func (w *Worker) extractEpisodeStill(ctx context.Context, show *db.MediaItem, season *db.Season, ep *db.Episode, persist bool) error {
	showKey := metadata.SanitizePathSegment(show.Title)
	seasonDir := fmt.Sprintf("S%02d", season.SeasonNumber)
	cacheDir := filepath.Join(w.Store, "metadata", "tv", showKey, seasonDir)
	base := strings.TrimSuffix(filepath.Base(ep.Path), filepath.Ext(ep.Path))
	dst := filepath.Join(cacheDir, base+"-thumb.jpg")
	stillRel := filepath.Join("metadata", "tv", showKey, seasonDir, base+"-thumb.jpg")
	if !stillFileOK(dst) {
		if err := extractStill(ep.Path, dst); err != nil {
			log.Printf("still %s: %v", ep.Path, err)
			return nil
		}
		if !stillFileOK(dst) {
			return nil
		}
	}
	if persist {
		_ = copyIfAbsent(dst, filepath.Join(filepath.Dir(ep.Path), base+"-thumb.jpg"))
	}
	return w.DB.UpdateEpisodeMeta(ctx, ep.ID, "", "", stillRel, "", "", "")
}

func stillFileOK(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Size() > 0
}
