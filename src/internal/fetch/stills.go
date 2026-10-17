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

type stillTask struct {
	name string
	run  func() error
}

func (w *Worker) extractMissingStills(ctx context.Context, lib *db.Library, only *db.MediaItem, opts Opts) error {
	if w == nil || w.DB == nil || lib == nil {
		return nil
	}
	var items []db.MediaItem
	if only != nil {
		got, err := w.DB.GetMediaItem(ctx, only.ID)
		if err != nil || got == nil {
			return err
		}
		items = []db.MediaItem{*got}
	} else {
		var err error
		items, err = w.DB.ListAllMediaItems(ctx, lib.ID)
		if err != nil {
			return err
		}
	}
	var tasks []stillTask
	for i := range items {
		more, err := w.stillTasks(ctx, lib, &items[i], opts.Persist)
		if err != nil {
			return err
		}
		tasks = append(tasks, more...)
	}
	total := len(tasks)
	if opts.ScanJobID != 0 {
		name := ""
		if total > 0 {
			name = tasks[0].name
		}
		w.progressStills(ctx, opts.ScanJobID, 80, fmt.Sprintf("stills 0/%d %s", total, strings.TrimSpace(name)))
	}
	for i, task := range tasks {
		if opts.ScanJobID != 0 && total > 0 {
			pct := 80 + (i+1)*19/total
			if pct > 99 {
				pct = 99
			}
			w.progressStills(ctx, opts.ScanJobID, pct, fmt.Sprintf("stills %d/%d %s", i+1, total, task.name))
		}
		if err := task.run(); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) progressStills(ctx context.Context, jobID int64, pct int, stillsLine string) {
	prev := ""
	if job, err := w.DB.GetScanJob(ctx, jobID); err == nil && job != nil && job.Message.Valid {
		prev = job.Message.String
	}
	w.progress(ctx, jobID, pct, mergeStillsLine(prev, stillsLine))
}

func mergeStillsLine(prev, stillsLine string) string {
	var keep []string
	for _, line := range strings.Split(prev, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "stills ") {
			continue
		}
		keep = append(keep, line)
	}
	stillsLine = strings.TrimSpace(stillsLine)
	if stillsLine != "" {
		keep = append(keep, stillsLine)
	}
	return strings.Join(keep, "\n")
}

func (w *Worker) stillTasks(ctx context.Context, lib *db.Library, it *db.MediaItem, persist bool) ([]stillTask, error) {
	if it == nil {
		return nil, nil
	}
	switch it.Kind {
	case "movie":
		item := *it
		return []stillTask{{
			name: stillName(item.Path, item.Title),
			run:  func() error { return w.extractMovieStill(ctx, lib, &item, persist) },
		}}, nil
	case "show":
		seasons, err := w.DB.ListSeasons(ctx, it.ID)
		if err != nil {
			return nil, err
		}
		var tasks []stillTask
		show := *it
		for i := range seasons {
			eps, err := w.DB.ListEpisodes(ctx, seasons[i].ID)
			if err != nil {
				return nil, err
			}
			season := seasons[i]
			for j := range eps {
				if !emptyPath(eps[j].StillPath) {
					continue
				}
				ep := eps[j]
				tasks = append(tasks, stillTask{
					name: stillName(ep.Path, ep.Title.String),
					run:  func() error { return w.extractEpisodeStill(ctx, lib, &show, &season, &ep, persist) },
				})
			}
		}
		return tasks, nil
	}
	return nil, nil
}

func stillName(path, title string) string {
	title = strings.TrimSpace(title)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if title == "" || metadata.IsVideo(title) {
		if base != "" {
			return base
		}
	}
	if title != "" {
		return strings.TrimSuffix(title, filepath.Ext(title))
	}
	return base
}

func copyIfAbsent(src, dst string) error {
	if stillFileOK(dst) {
		return nil
	}
	return metadata.CopyFile(src, dst)
}

func (w *Worker) stampFileStill(ctx context.Context, lib *db.Library, filePath, stillRel string) {
	if w == nil || w.DB == nil || lib == nil {
		return
	}
	stillRel = strings.TrimSpace(stillRel)
	if stillRel == "" {
		return
	}
	rel, err := relWithin(lib.Path, filePath)
	if err != nil || rel == "" {
		return
	}
	id, err := w.DB.NodeIDByRel(ctx, lib.ID, rel)
	if err != nil || id == 0 {
		return
	}
	_ = w.DB.SetNodeStill(ctx, id, stillRel)
}

func (w *Worker) extractMovieStill(ctx context.Context, lib *db.Library, it *db.MediaItem, persist bool) error {
	year := 0
	if it.Year.Valid {
		year = int(it.Year.Int64)
	}
	cacheKey := metadata.SanitizePathSegment(metadata.TitleYear(it.Title, year))
	if cacheKey == "" {
		cacheKey = metadata.SanitizePathSegment(strings.TrimSuffix(filepath.Base(it.Path), filepath.Ext(it.Path)))
	}
	cacheDir := filepath.Join(w.Store, "metadata", "movies", cacheKey)
	dst := filepath.Join(cacheDir, "still.jpg")
	stillRel := filepath.Join("metadata", "movies", cacheKey, "still.jpg")
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
		_ = copyIfAbsent(dst, filepath.Join(mediaDir, base+"-thumb.jpg"))
	}
	w.stampFileStill(ctx, lib, it.Path, stillRel)
	return nil
}

func (w *Worker) extractEpisodeStill(ctx context.Context, lib *db.Library, show *db.MediaItem, season *db.Season, ep *db.Episode, persist bool) error {
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
	if err := w.DB.UpdateEpisodeMeta(ctx, ep.ID, "", "", stillRel, "", "", ""); err != nil {
		return err
	}
	w.stampFileStill(ctx, lib, ep.Path, stillRel)
	return nil
}

func stillFileOK(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Size() > 0
}
