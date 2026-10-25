package fetch

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
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
type gonePlan struct {
	items []int64
	eps   []int64
	nodes []int64
}

func (w *Worker) stillsWithPrune(ctx context.Context, lib *db.Library, opts Opts) error {
	stillsCh := make(chan error, 1)
	goneCh := make(chan struct {
		plan gonePlan
		err  error
	}, 1)
	go func() {
		stillsCh <- w.extractMissingStills(ctx, lib, nil, opts)
	}()
	go func() {
		plan, err := w.listGone(ctx, lib)
		goneCh <- struct {
			plan gonePlan
			err  error
		}{plan, err}
	}()
	stillErr := <-stillsCh
	gone := <-goneCh
	if stillErr != nil {
		return stillErr
	}
	if gone.err != nil {
		return gone.err
	}
	return w.applyGone(ctx, gone.plan)
}

func (w *Worker) listGone(ctx context.Context, lib *db.Library) (gonePlan, error) {
	var plan gonePlan
	if w == nil || w.DB == nil || lib == nil {
		return plan, nil
	}
	root := lib.Path
	if !filepath.IsAbs(root) {
		root = filepath.Join(w.MediaRoot, root)
	}
	if _, err := os.Stat(root); err != nil {
		return plan, nil
	}
	items, err := w.DB.ListAllMediaItems(ctx, lib.ID)
	if err != nil {
		return plan, err
	}
	goneItem := map[int64]bool{}
	for _, it := range items {
		if !pathGone(it.Path) {
			continue
		}
		plan.items = append(plan.items, it.ID)
		goneItem[it.ID] = true
	}
	for _, it := range items {
		if it.Kind != "show" || goneItem[it.ID] {
			continue
		}
		eps, err := w.DB.ListEpisodesByShow(ctx, it.ID)
		if err != nil {
			return plan, err
		}
		for _, ep := range eps {
			if !pathGone(ep.Path) {
				continue
			}
			plan.eps = append(plan.eps, ep.ID)
		}
	}
	nodes, err := w.DB.ListGoneLibraryNodeIDs(ctx, lib.ID, root)
	if err != nil {
		return plan, err
	}
	plan.nodes = nodes
	return plan, nil
}

func (w *Worker) applyGone(ctx context.Context, plan gonePlan) error {
	if w == nil || w.DB == nil {
		return nil
	}
	for _, id := range plan.items {
		if err := w.DB.DeleteMediaItem(ctx, id); err != nil {
			return err
		}
	}
	for _, id := range plan.eps {
		if err := w.DB.DeleteEpisode(ctx, id); err != nil {
			return err
		}
	}
	return w.DB.DeleteLibraryNodes(ctx, plan.nodes)
}

func (w *Worker) PruneMissing(ctx context.Context, lib *db.Library) error {
	plan, err := w.listGone(ctx, lib)
	if err != nil {
		return err
	}
	return w.applyGone(ctx, plan)
}

// ReattachMovedPath points a unique gone-path row of the same kind/title at newPath.
func (w *Worker) ReattachMovedPath(ctx context.Context, lib *db.Library, kind, newPath string) error {
	if w == nil || w.DB == nil || lib == nil {
		return nil
	}
	newPath = cleanMediaPath(newPath)
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
