package fetch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
)

func (w *Worker) recordTreeStructure(ctx context.Context, lib *db.Library, j matchmedia.Job, opts Opts) error {
	return w.recordTreeMode(ctx, lib, j, opts, false)
}

func (w *Worker) recordTree(ctx context.Context, lib *db.Library, j matchmedia.Job, opts Opts) error {
	return w.recordTreeMode(ctx, lib, j, opts, true)
}

func (w *Worker) recordTreeMode(ctx context.Context, lib *db.Library, j matchmedia.Job, opts Opts, withGroup bool) error {
	if w == nil || w.DB == nil || lib == nil || strings.TrimSpace(lib.Path) == "" {
		return nil
	}
	if unchanged, err := w.groupUnchanged(ctx, lib, j, opts); err != nil || unchanged {
		return err
	}
	if _, err := w.DB.EnsureLibraryRoot(ctx, lib.ID); err != nil {
		return err
	}
	type leaf struct {
		abs  string
		file bool
		se   matchmedia.JobFile
	}
	var leaves []leaf
	if p := strings.TrimSpace(j.Path); p != "" {
		file := false
		if st, err := os.Stat(p); err == nil {
			file = !st.IsDir()
		}
		leaves = append(leaves, leaf{abs: p, file: file})
	}
	for _, f := range j.Files {
		p := strings.TrimSpace(f.Path)
		if p == "" {
			continue
		}
		leaves = append(leaves, leaf{abs: p, file: true, se: f})
	}
	ids := map[string]int64{}
	for _, leaf := range leaves {
		rel, err := relWithin(lib.Path, leaf.abs)
		if err != nil {
			continue
		}
		kind := "dir"
		if leaf.file {
			kind = "file"
		}
		id, err := w.DB.UpsertNodeChain(ctx, lib.ID, rel, kind)
		if err != nil {
			return err
		}
		ids[rel] = id
	}
	if !withGroup {
		return nil
	}
	groupRel, err := relWithin(lib.Path, strings.TrimSpace(j.Path))
	if err == nil {
		if id, ok := ids[groupRel]; ok {
			score := 0.0
			jaccard := 0.0
			provider, metaID, title, year, poster := "", "", j.Title, j.Year, ""
			plot := ""
			if j.Match != nil {
				provider = j.Match.Provider
				metaID = j.Match.ID
				if strings.TrimSpace(j.Match.Title) != "" {
					title = j.Match.Title
				}
				if strings.TrimSpace(j.Match.Year) != "" {
					year = j.Match.Year
				}
				if raw := strings.TrimSpace(j.Match.Poster); strings.HasPrefix(raw, "metadata/") {
					poster = raw
				}
				plot = j.Match.Synopsis
				score = j.Match.Score
				jaccard = j.Match.Jaccard
			}
			if err := w.DB.SetNodeGroup(ctx, id, db.NodeGroup{
				Source: j.Source, Role: j.Role, Kind: j.Kind, Title: title, Year: year,
				ParentTitle: j.Parent, Status: j.Status, Err: j.Error, Ranker: j.Ranker,
				Provider: provider, MetaID: metaID, Score: score, Jaccard: jaccard,
				CatalogFor: j.CatalogFor, Plot: plot, Poster: poster,
				Candidates: EncodePickerSnapshot(j),
			}); err != nil {
				return err
			}
		}
	}
	for _, leaf := range leaves {
		if !leaf.file {
			continue
		}
		rel, err := relWithin(lib.Path, leaf.abs)
		if err != nil {
			continue
		}
		id, ok := ids[rel]
		if !ok {
			continue
		}
		var groupID int64
		if groupRel != "" && err == nil {
			groupID = ids[groupRel]
		}
		title, plot, still := "", "", ""
		season, episode := "", ""
		if _, _, ok := jobFileNumbers(leaf.se); ok {
			season, episode = strings.TrimSpace(leaf.se.Season), strings.TrimSpace(leaf.se.Episode)
		}
		if kindFromJob(j, expandJobFiles(j)) == "movie" {
			season, episode = "", ""
		}
		if ep := catalogEpisode(j, season, episode); ep != nil {
			title = ep.Title
			plot = ep.Synopsis
			if raw := strings.TrimSpace(ep.Poster); strings.HasPrefix(raw, "metadata/") {
				still = raw
			}
		}
		if err := w.DB.SetNodeFile(ctx, id, groupID, season, episode, title, plot, still); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) stampSavedTree(ctx context.Context, lib *db.Library, it *db.MediaItem) {
	if w == nil || w.DB == nil || lib == nil || it == nil || it.Kind != "show" {
		return
	}
	seasons, err := w.DB.ListSeasons(ctx, it.ID)
	if err != nil {
		return
	}
	for _, se := range seasons {
		eps, err := w.DB.ListEpisodes(ctx, se.ID)
		if err != nil || len(eps) == 0 {
			continue
		}
		if se.PosterPath.Valid && strings.TrimSpace(se.PosterPath.String) != "" {
			if rel, err := relWithin(lib.Path, filepath.Dir(eps[0].Path)); err == nil && rel != "" {
				if id, err := w.DB.NodeIDByRel(ctx, lib.ID, rel); err == nil && id != 0 {
					_ = w.DB.SetNodePoster(ctx, id, se.PosterPath.String)
				}
			}
		}
		for _, ep := range eps {
			if !ep.StillPath.Valid || strings.TrimSpace(ep.StillPath.String) == "" {
				continue
			}
			rel, err := relWithin(lib.Path, ep.Path)
			if err != nil || rel == "" {
				continue
			}
			id, err := w.DB.NodeIDByRel(ctx, lib.ID, rel)
			if err != nil || id == 0 {
				continue
			}
			_ = w.DB.SetNodeStill(ctx, id, ep.StillPath.String)
		}
	}
}

func (w *Worker) stampNodePoster(ctx context.Context, lib *db.Library, it *db.MediaItem, j matchmedia.Job) {
	if w == nil || w.DB == nil || lib == nil || it == nil || !it.PosterPath.Valid {
		return
	}
	poster := strings.TrimSpace(it.PosterPath.String)
	if poster == "" {
		return
	}
	rel, err := relWithin(lib.Path, strings.TrimSpace(j.Path))
	if err != nil {
		return
	}
	id, err := w.DB.NodeIDByRel(ctx, lib.ID, rel)
	if err != nil || id == 0 {
		return
	}
	_ = w.DB.SetNodePoster(ctx, id, poster)
}

func (w *Worker) groupUnchanged(ctx context.Context, lib *db.Library, j matchmedia.Job, opts Opts) (bool, error) {
	mode := strings.TrimSpace(opts.ScanMode)
	if opts.Overwrite || (mode != "changes" && mode != "matchmedia-changes") || len(j.Files) == 0 {
		return false, nil
	}
	rel, err := relWithin(lib.Path, strings.TrimSpace(j.Path))
	if err != nil {
		return false, nil
	}
	id, err := w.DB.NodeIDByRel(ctx, lib.ID, rel)
	if err != nil || id == 0 {
		return false, err
	}
	node, err := w.DB.GetLibraryNode(ctx, id)
	if err != nil || node == nil || !node.MetaID.Valid || strings.TrimSpace(node.MetaID.String) == "" {
		return false, err
	}
	for _, f := range j.Files {
		frel, err := relWithin(lib.Path, strings.TrimSpace(f.Path))
		if err != nil {
			return false, nil
		}
		fid, err := w.DB.NodeIDByRel(ctx, lib.ID, frel)
		if err != nil || fid == 0 {
			return false, err
		}
	}
	return true, nil
}

func (w *Worker) stampSelectNode(ctx context.Context, it *db.MediaItem, j matchmedia.Job) error {
	if w == nil || w.DB == nil || it == nil {
		return nil
	}
	root, err := w.DB.LibraryPath(ctx, it.LibraryID)
	if err != nil || strings.TrimSpace(root) == "" {
		return err
	}
	lib := &db.Library{ID: it.LibraryID, Path: root}
	if err := w.recordTree(ctx, lib, j, Opts{Overwrite: true}); err != nil {
		return err
	}
	w.linkItemNode(ctx, lib, it, j)
	w.stampNodePoster(ctx, lib, it, j)
	w.stampSavedTree(ctx, lib, it)
	return nil
}

func (w *Worker) linkItemNode(ctx context.Context, lib *db.Library, it *db.MediaItem, j matchmedia.Job) {
	if w == nil || w.DB == nil || lib == nil || it == nil {
		return
	}
	rel, err := relWithin(lib.Path, strings.TrimSpace(j.Path))
	if err != nil {
		return
	}
	id, err := w.DB.NodeIDByRel(ctx, lib.ID, rel)
	if err != nil || id == 0 {
		return
	}
	_ = w.DB.SetMediaItemNode(ctx, it.ID, id)
}

func catalogEpisode(j matchmedia.Job, season, episode string) *matchmedia.Episode {
	if strings.TrimSpace(season) == "" || strings.TrimSpace(episode) == "" {
		return nil
	}
	for i := range j.Catalog {
		if strings.TrimLeft(j.Catalog[i].Number, "0") != strings.TrimLeft(season, "0") && j.Catalog[i].Number != season {
			continue
		}
		for k := range j.Catalog[i].Episodes {
			e := &j.Catalog[i].Episodes[k]
			if strings.TrimLeft(e.Number, "0") == strings.TrimLeft(episode, "0") || e.Number == episode {
				return e
			}
		}
	}
	return nil
}

func relWithin(root, abs string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	abs = filepath.Clean(strings.TrimSpace(abs))
	if root == "" || abs == "" {
		return "", fmt.Errorf("empty path")
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("path escapes library")
	}
	if rel == "." {
		return "", nil
	}
	return rel, nil
}
