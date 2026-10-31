package fetch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
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
	unchanged, err := w.groupUnchanged(ctx, lib, j, opts)
	if err != nil {
		return err
	}
	if unchanged {
		return w.refreshUnchangedTitleFacts(ctx, lib, j)
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
	if p := cleanMediaPath(j.Path); p != "" {
		file := false
		if st, err := os.Stat(p); err == nil {
			file = !st.IsDir()
		}
		leaves = append(leaves, leaf{abs: p, file: file})
	}
	for _, f := range j.Files {
		p := cleanMediaPath(f.Path)
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
		if !leaf.file {
			if err := syncNodeFromTitleNFO(ctx, w.DB, id, leaf.abs); err != nil {
				return err
			}
		}
	}
	if !withGroup {
		return nil
	}
	groupRel, err := relWithin(lib.Path, j.Path)
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
				if raw := strings.TrimSpace(j.Match.Poster); filepath.IsAbs(raw) {
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
			// NFO wins over MatchMedia stamp for title/year/premiere/plot/rating.
			groupAbs := j.Path
			if st, err := os.Stat(groupAbs); err == nil && !st.IsDir() {
				groupAbs = filepath.Dir(groupAbs)
			}
			if err := syncNodeFromTitleNFO(ctx, w.DB, id, groupAbs); err != nil {
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
			if raw := strings.TrimSpace(ep.Poster); filepath.IsAbs(raw) {
				still = raw
			}
		}
		// A sidecar NFO title is authoritative over the catalog title.
		side := metadata.VideoSidecar{}
		if p := strings.TrimSpace(leaf.se.NFOPath); p != "" {
			side = metadata.ReadVideoSidecarAt(p)
		} else {
			side = metadata.ReadVideoSidecar(leaf.abs)
		}
		if t := strings.TrimSpace(side.Title); t != "" {
			title = t
			if p := strings.TrimSpace(side.Plot); p != "" {
				plot = p
			}
		}
		if err := w.DB.SetNodeFile(ctx, id, groupID, season, episode, title, plot, still); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) stampSavedTree(ctx context.Context, lib *db.Library, it *db.MediaItem) {
	if w == nil || w.DB == nil || lib == nil || it == nil {
		return
	}
	if it.Kind == "movie" {
		w.stampLeafTitle(ctx, lib, it.Path, it.Title)
		return
	}
	if it.Kind != "show" {
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
			if title := strings.TrimSpace(ep.Title.String); title != "" {
				w.stampLeafTitle(ctx, lib, ep.Path, title)
			}
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

// stampLeafTitle writes a resolved title onto the tree node for path. File
// leaves get file_title; folder nodes get mm_title.
func (w *Worker) stampLeafTitle(ctx context.Context, lib *db.Library, path, title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	rel, err := relWithin(lib.Path, path)
	if err != nil || rel == "" {
		if d := filepath.Dir(path); d != path {
			rel, err = relWithin(lib.Path, d)
		}
		if err != nil || rel == "" {
			return
		}
	}
	id, err := w.DB.NodeIDByRel(ctx, lib.ID, rel)
	if err != nil || id == 0 {
		return
	}
	node, err := w.DB.GetLibraryNode(ctx, id)
	if err != nil || node == nil {
		return
	}
	_ = w.DB.SetNodeDisplay(ctx, id, node.NodeKind != "dir", &title, nil)
}

func (w *Worker) stampNodePoster(ctx context.Context, lib *db.Library, it *db.MediaItem, j matchmedia.Job) {
	if w == nil || w.DB == nil || lib == nil || it == nil || !it.PosterPath.Valid {
		return
	}
	poster := strings.TrimSpace(it.PosterPath.String)
	if poster == "" {
		return
	}
	rel, err := relWithin(lib.Path, j.Path)
	if err != nil {
		return
	}
	id, err := w.DB.NodeIDByRel(ctx, lib.ID, rel)
	if err != nil || id == 0 {
		return
	}
	_ = w.DB.SetNodePoster(ctx, id, poster)
}

// refreshUnchangedTitleFacts updates directory NFO facts when a changes scan
// leaves the file set in place. Premiere and title sort keys stay current.
func (w *Worker) refreshUnchangedTitleFacts(ctx context.Context, lib *db.Library, j matchmedia.Job) error {
	if w == nil || w.DB == nil || lib == nil {
		return nil
	}
	groupAbs := cleanMediaPath(j.Path)
	if groupAbs == "" {
		return nil
	}
	if st, err := os.Stat(groupAbs); err == nil && !st.IsDir() {
		groupAbs = filepath.Dir(groupAbs)
	}
	rel, err := relWithin(lib.Path, groupAbs)
	if err != nil || rel == "" {
		return nil
	}
	id, err := w.DB.NodeIDByRel(ctx, lib.ID, rel)
	if err != nil || id == 0 {
		return err
	}
	return syncNodeFromTitleNFO(ctx, w.DB, id, groupAbs)
}

func (w *Worker) groupUnchanged(ctx context.Context, lib *db.Library, j matchmedia.Job, opts Opts) (bool, error) {
	mode := strings.TrimSpace(opts.ScanMode)
	if opts.Overwrite || (mode != "changes" && mode != "matchmedia-changes") || len(j.Files) == 0 {
		return false, nil
	}
	rel, err := relWithin(lib.Path, j.Path)
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
		frel, err := relWithin(lib.Path, f.Path)
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
	for _, p := range linkPaths(it, j) {
		rel, err := relWithin(lib.Path, p)
		if err != nil {
			continue
		}
		id, err := w.DB.NodeIDByRel(ctx, lib.ID, rel)
		if err != nil || id == 0 {
			continue
		}
		_ = w.DB.SetMediaItemNode(ctx, it.ID, id)
		return
	}
}

func linkPaths(it *db.MediaItem, j matchmedia.Job) []string {
	var out []string
	add := func(p string) {
		p = cleanMediaPath(p)
		if p == "" {
			return
		}
		for _, have := range out {
			if have == p {
				return
			}
		}
		out = append(out, p)
	}
	preferFolder := func(p string) {
		p = cleanMediaPath(p)
		if p == "" {
			return
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			add(filepath.Dir(p))
		}
		add(p)
	}
	if it != nil {
		preferFolder(it.Path)
	}
	preferFolder(j.Path)
	return out
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

// cleanMediaPath drops a blank path and cleans the rest. A trailing space is
// part of the name, so it is not trimmed.
func cleanMediaPath(p string) string {
	if strings.TrimSpace(p) == "" {
		return ""
	}
	return filepath.Clean(p)
}

func relWithin(root, abs string) (string, error) {
	root = cleanMediaPath(root)
	abs = cleanMediaPath(abs)
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
