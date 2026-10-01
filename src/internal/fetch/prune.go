package fetch

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyshmahell/servemedia/internal/db"
	"github.com/alyshmahell/servemedia/internal/metadata"
)

func pathGone(p string) bool {
	_, err := os.Stat(p)
	return os.IsNotExist(err)
}

func identityTitle(kind, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if kind == "show" {
		title := metadata.CleanEpisodeTitle(filepath.Base(path))
		if title == "" {
			title = filepath.Base(path)
		}
		return strings.ToLower(strings.TrimSpace(title))
	}
	title, _ := metadata.TitleYearFromVideoPath(path)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return strings.ToLower(strings.TrimSpace(title))
}

// PruneMissing deletes media items and episodes in lib whose paths no longer
// exist. Skips prune when the library root itself cannot be stat'd (mount down).
func (w *Worker) PruneMissing(ctx context.Context, lib *db.Library) error {
	if w == nil || w.DB == nil || lib == nil {
		return nil
	}
	root := lib.Path
	if !filepath.IsAbs(root) {
		root = filepath.Join(w.MediaRoot, root)
	}
	if _, err := os.Stat(root); err != nil {
		return nil
	}
	items, err := w.DB.ListAllMediaItems(ctx, lib.ID)
	if err != nil {
		return err
	}
	for _, it := range items {
		if !pathGone(it.Path) {
			continue
		}
		if err := w.DB.DeleteMediaItem(ctx, it.ID); err != nil {
			return err
		}
	}
	items, err = w.DB.ListAllMediaItems(ctx, lib.ID)
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.Kind != "show" {
			continue
		}
		eps, err := w.DB.ListEpisodesByShow(ctx, it.ID)
		if err != nil {
			return err
		}
		for _, ep := range eps {
			if !pathGone(ep.Path) {
				continue
			}
			if err := w.DB.DeleteEpisode(ctx, ep.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReattachMovedPath points a unique gone-path row of the same kind/title at newPath.
func (w *Worker) ReattachMovedPath(ctx context.Context, lib *db.Library, kind, newPath string) error {
	if w == nil || w.DB == nil || lib == nil {
		return nil
	}
	newPath = strings.TrimSpace(newPath)
	if newPath == "" {
		return nil
	}
	existed, err := w.DB.MediaItemExistsAtPath(ctx, lib.ID, newPath)
	if err != nil || existed {
		return err
	}
	want := identityTitle(kind, newPath)
	if want == "" {
		return nil
	}
	items, err := w.DB.ListAllMediaItems(ctx, lib.ID)
	if err != nil {
		return err
	}
	var hit *db.MediaItem
	for i := range items {
		it := &items[i]
		if it.Kind != kind || !pathGone(it.Path) {
			continue
		}
		if identityTitle(kind, it.Path) != want {
			continue
		}
		if hit != nil {
			return nil
		}
		hit = it
	}
	if hit == nil {
		return nil
	}
	return w.DB.UpdateMediaItemPath(ctx, hit.ID, newPath)
}
