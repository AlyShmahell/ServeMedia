package fetch

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

// RepointLibraryMetadata rewrites poster and NFO paths to the absolute file
// beside the media, then drops store/metadata files nothing still references.
func (w *Worker) RepointLibraryMetadata(ctx context.Context, lib *db.Library) error {
	if w == nil || w.DB == nil || lib == nil {
		return nil
	}
	items, err := w.libraryArtRows(ctx, lib.ID)
	if err != nil {
		return err
	}
	for i := range items {
		if err := w.repointItem(ctx, &items[i]); err != nil {
			return err
		}
		if items[i].Kind != "show" {
			continue
		}
		seasons, err := w.DB.ListSeasons(ctx, items[i].ID)
		if err != nil {
			return err
		}
		for _, season := range seasons {
			if err := w.repointSeason(ctx, &items[i], &season); err != nil {
				return err
			}
		}
		eps, err := w.DB.ListEpisodesByShow(ctx, items[i].ID)
		if err != nil {
			return err
		}
		for j := range eps {
			if err := w.repointEpisode(ctx, &eps[j]); err != nil {
				return err
			}
		}
	}
	fresh, err := w.libraryArtRows(ctx, lib.ID)
	if err != nil {
		return err
	}
	for i := range fresh {
		if fresh[i].Kind == "show" {
			w.stampSavedTree(ctx, lib, &fresh[i])
		}
		if fresh[i].PosterPath.Valid {
			w.stampNodePoster(ctx, lib, &fresh[i], matchmedia.Job{Path: fresh[i].Path})
		}
	}
	if err := w.repointNodes(ctx, lib); err != nil {
		return err
	}
	return w.sweepStoreMetadata(ctx)
}

func (w *Worker) libraryArtRows(ctx context.Context, libraryID int64) ([]db.MediaItem, error) {
	rows, err := w.DB.SQL.QueryContext(ctx, `
		SELECT id, kind, path, title, COALESCE(year,0),
			COALESCE(poster_path,''), COALESCE(nfo_path,''), COALESCE(backdrop_path,'')
		FROM media_items WHERE library_id=?`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []db.MediaItem
	for rows.Next() {
		var it db.MediaItem
		var year int
		var poster, nfo, backdrop string
		if err := rows.Scan(&it.ID, &it.Kind, &it.Path, &it.Title, &year, &poster, &nfo, &backdrop); err != nil {
			return nil, err
		}
		if year > 0 {
			it.Year = sql.NullInt64{Int64: int64(year), Valid: true}
		}
		if poster != "" {
			it.PosterPath = sql.NullString{String: poster, Valid: true}
		}
		if nfo != "" {
			it.NFOPath = sql.NullString{String: nfo, Valid: true}
		}
		if backdrop != "" {
			it.BackdropPath = sql.NullString{String: backdrop, Valid: true}
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func storedIsRelative(p string) bool {
	p = strings.TrimSpace(p)
	return p != "" && !filepath.IsAbs(p)
}

func (w *Worker) repointItem(ctx context.Context, it *db.MediaItem) error {
	var poster, nfo *string
	if it.PosterPath.Valid && storedIsRelative(it.PosterPath.String) {
		next, ok := w.retarget(it.PosterPath.String, itemPosterBeside(it, it.PosterPath.String))
		if ok {
			poster = &next
		}
	}
	if it.NFOPath.Valid && storedIsRelative(it.NFOPath.String) {
		next, ok := w.retarget(it.NFOPath.String, itemNFOBeside(it))
		if ok {
			nfo = &next
		}
	}
	if poster != nil || nfo != nil {
		if err := w.DB.SetMediaItemPosterNFO(ctx, it.ID, poster, nfo); err != nil {
			return err
		}
	}
	if it.BackdropPath.Valid && storedIsRelative(it.BackdropPath.String) {
		next, ok := w.retarget(it.BackdropPath.String, itemBackdropBeside(it, it.BackdropPath.String))
		if !ok {
			return nil
		}
		_, err := w.DB.SQL.ExecContext(ctx, `UPDATE media_items SET backdrop_path=NULLIF(?,'') WHERE id=?`, next, it.ID)
		return err
	}
	return nil
}

func (w *Worker) repointSeason(ctx context.Context, show *db.MediaItem, season *db.Season) error {
	if season == nil || !season.PosterPath.Valid || !storedIsRelative(season.PosterPath.String) {
		return nil
	}
	dir := seasonMediaDir(ctx, w, show, season)
	next, ok := w.retarget(season.PosterPath.String, seasonPosterDest(dir, show, season.SeasonNumber, season.PosterPath.String))
	if !ok {
		return nil
	}
	return w.DB.SetSeasonPoster(ctx, season.ID, next)
}

func (w *Worker) repointEpisode(ctx context.Context, ep *db.Episode) error {
	var still, nfo *string
	if ep.StillPath.Valid && storedIsRelative(ep.StillPath.String) {
		next, ok := w.retarget(ep.StillPath.String, episodeStillBeside(ep.Path, ep.StillPath.String))
		if ok {
			still = &next
		}
	}
	if ep.NFOPath.Valid && storedIsRelative(ep.NFOPath.String) {
		next, ok := w.retarget(ep.NFOPath.String, episodeNFOBeside(ep.Path))
		if ok {
			nfo = &next
		}
	}
	if still == nil && nfo == nil {
		return nil
	}
	return w.DB.SetEpisodeStillNFO(ctx, ep.ID, still, nfo)
}

func (w *Worker) repointNodes(ctx context.Context, lib *db.Library) error {
	nodes, err := w.DB.ListNodesUnder(ctx, lib.ID, "")
	if err != nil {
		return err
	}
	for _, n := range nodes {
		abs := filepath.Join(lib.Path, filepath.FromSlash(n.RelPath))
		if n.StillPath.Valid && storedIsRelative(n.StillPath.String) {
			dest := episodeStillBeside(abs, n.StillPath.String)
			if n.NodeKind != "file" {
				dest = filepath.Join(abs, "poster"+extOr(n.StillPath.String, ".jpg"))
			}
			next, ok := w.retarget(n.StillPath.String, dest)
			if ok {
				if err := w.DB.SetNodeStill(ctx, n.ID, next); err != nil {
					return err
				}
			}
		}
		if n.PosterPath.Valid && storedIsRelative(n.PosterPath.String) {
			dest := filepath.Join(abs, "poster"+extOr(n.PosterPath.String, ".jpg"))
			if n.NodeKind == "file" {
				dest = episodeStillBeside(abs, n.PosterPath.String)
			}
			next, ok := w.retarget(n.PosterPath.String, dest)
			if ok {
				if err := w.DB.SetNodePoster(ctx, n.ID, next); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// retarget resolves a store-relative path to an absolute sidecar. The bool
// reports whether the stored column should be replaced.
func (w *Worker) retarget(stored, beside string) (string, bool) {
	stored = strings.TrimSpace(stored)
	if !storedIsRelative(stored) {
		return "", false
	}
	storeFile := ""
	if w != nil && w.Store != "" {
		storeFile = filepath.Join(w.Store, filepath.FromSlash(stored))
	}
	if beside != "" && regularFile(beside) {
		if storeFile != "" && filepath.Clean(storeFile) != filepath.Clean(beside) {
			_ = os.Remove(storeFile)
		}
		return beside, true
	}
	if storeFile != "" && regularFile(storeFile) && beside != "" {
		if err := metadata.CopyFile(storeFile, beside); err != nil {
			return "", false
		}
		if filepath.Clean(storeFile) != filepath.Clean(beside) {
			_ = os.Remove(storeFile)
		}
		return beside, true
	}
	if storeFile != "" {
		_ = os.Remove(storeFile)
	}
	return "", true
}

func itemMediaDir(it *db.MediaItem) string {
	if it == nil {
		return ""
	}
	if it.Kind == "show" {
		return it.Path
	}
	return filepath.Dir(it.Path)
}

func itemNFOBeside(it *db.MediaItem) string {
	if it == nil {
		return ""
	}
	if it.Kind == "show" {
		return filepath.Join(it.Path, "tvshow.nfo")
	}
	nfo, _ := movieBesidePaths(it, ".jpg")
	return nfo
}

func itemPosterBeside(it *db.MediaItem, stored string) string {
	if it == nil {
		return ""
	}
	ext := extOr(stored, ".jpg")
	dir := itemMediaDir(it)
	if it.Kind == "show" {
		if p := metadata.FolderPoster(dir); p != "" {
			return p
		}
		return filepath.Join(dir, "poster"+ext)
	}
	_, poster := movieBesidePaths(it, ext)
	if regularFile(poster) {
		return poster
	}
	if metadata.PreferBareMovieSidecar(it.Path) {
		if p := metadata.FolderPoster(dir); p != "" {
			return p
		}
	}
	return poster
}

func itemBackdropBeside(it *db.MediaItem, stored string) string {
	dir := itemMediaDir(it)
	ext := extOr(stored, ".jpg")
	for _, name := range []string{"fanart.jpg", "fanart.png", "backdrop.jpg", "backdrop.png"} {
		p := filepath.Join(dir, name)
		if regularFile(p) {
			return p
		}
	}
	return filepath.Join(dir, "fanart"+ext)
}

func seasonPosterDest(seasonDir string, show *db.MediaItem, seasonNum int, stored string) string {
	showRoot := ""
	if show != nil {
		showRoot = show.Path
	}
	if p := findSeasonPoster(seasonDir, "", seasonNum); p != "" {
		return p
	}
	if showRoot != "" && filepath.Clean(showRoot) != filepath.Clean(seasonDir) {
		if p := seasonNamedPoster(showRoot, seasonNum); p != "" {
			return p
		}
	}
	if strings.TrimSpace(seasonDir) == "" {
		seasonDir = showRoot
	}
	return filepath.Join(seasonDir, "poster"+extOr(stored, ".jpg"))
}

func seasonNamedPoster(dir string, seasonNum int) string {
	base := fmt.Sprintf("season%02d", seasonNum)
	baseAlt := fmt.Sprintf("season%d", seasonNum)
	for _, name := range []string{
		base + "-poster.jpg", base + "-poster.png",
		baseAlt + "-poster.jpg", baseAlt + "-poster.png",
	} {
		cand := filepath.Join(dir, name)
		if regularFile(cand) {
			return cand
		}
	}
	return ""
}

func episodeNFOBeside(video string) string {
	dir := filepath.Dir(video)
	base := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	if p := metadata.FindSidecar(dir, base, "episode.nfo", base+".nfo"); p != "" {
		return p
	}
	return episodeBesideNFO(video)
}

func episodeStillBeside(video, stored string) string {
	dir := filepath.Dir(video)
	base := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	if p := metadata.FindSidecar(dir, base, "thumb.jpg", "thumb.png", base+"-thumb.jpg", base+"-thumb.png", base+"-thumb.webp", base+".jpg", base+".png"); p != "" {
		return p
	}
	return filepath.Join(dir, base+"-thumb"+extOr(stored, ".jpg"))
}

func extOr(path, fallback string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".nfo":
		if ext == ".jpeg" {
			return ".jpg"
		}
		return ext
	}
	return fallback
}

func regularFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func (w *Worker) sweepStoreMetadata(ctx context.Context) error {
	if w == nil || w.Store == "" || w.DB == nil {
		return nil
	}
	root := filepath.Join(w.Store, "metadata")
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	keep, err := w.storeMetadataRefs(ctx)
	if err != nil {
		return err
	}
	var files []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(w.Store, path)
		if err != nil {
			return nil
		}
		if _, ok := keep[filepath.ToSlash(rel)]; ok {
			return nil
		}
		files = append(files, path)
		return nil
	})
	for _, f := range files {
		_ = os.Remove(f)
	}
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i])
	}
	return nil
}

func (w *Worker) storeMetadataRefs(ctx context.Context) (map[string]struct{}, error) {
	keep := map[string]struct{}{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if !storedIsRelative(p) {
			return
		}
		keep[filepath.ToSlash(filepath.Clean(p))] = struct{}{}
	}
	rows, err := w.DB.SQL.QueryContext(ctx, `SELECT COALESCE(nfo_path,''), COALESCE(poster_path,''), COALESCE(backdrop_path,'') FROM media_items`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var nfo, poster, backdrop string
		if err := rows.Scan(&nfo, &poster, &backdrop); err != nil {
			rows.Close()
			return nil, err
		}
		add(nfo)
		add(poster)
		add(backdrop)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = w.DB.SQL.QueryContext(ctx, `SELECT COALESCE(nfo_path,''), COALESCE(still_path,'') FROM episodes`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var nfo, still string
		if err := rows.Scan(&nfo, &still); err != nil {
			rows.Close()
			return nil, err
		}
		add(nfo)
		add(still)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = w.DB.SQL.QueryContext(ctx, `SELECT COALESCE(poster_path,'') FROM seasons`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var poster string
		if err := rows.Scan(&poster); err != nil {
			rows.Close()
			return nil, err
		}
		add(poster)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = w.DB.SQL.QueryContext(ctx, `SELECT COALESCE(poster_path,''), COALESCE(still_path,'') FROM library_nodes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var poster, still string
		if err := rows.Scan(&poster, &still); err != nil {
			return nil, err
		}
		add(poster)
		add(still)
	}
	return keep, rows.Err()
}
