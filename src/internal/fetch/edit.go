package fetch

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

const maxPosterBytes = 8 << 20

func sniffImageExt(data []byte) (string, error) {
	if len(data) < 12 {
		return "", fmt.Errorf("not an image")
	}
	switch http.DetectContentType(data) {
	case "image/jpeg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	case "image/webp":
		return ".webp", nil
	}
	if bytes.HasPrefix(data, []byte("RIFF")) && bytes.Contains(data[:12], []byte("WEBP")) {
		return ".webp", nil
	}
	return "", fmt.Errorf("unsupported image type")
}

func sidecarDest(stored, beside string) string {
	stored = strings.TrimSpace(stored)
	if filepath.IsAbs(stored) {
		return stored
	}
	return beside
}

func legacyStorePath(store, stored string) string {
	stored = strings.TrimSpace(stored)
	if stored == "" || filepath.IsAbs(stored) || store == "" {
		return ""
	}
	return filepath.Join(store, stored)
}

func movieBesidePaths(it *db.MediaItem, posterExt string) (nfoPath, posterPath string) {
	mediaDir := filepath.Dir(it.Path)
	base := strings.TrimSuffix(filepath.Base(it.Path), filepath.Ext(it.Path))
	if metadata.PreferBareMovieSidecar(it.Path) {
		return filepath.Join(mediaDir, "movie.nfo"), filepath.Join(mediaDir, "poster"+posterExt)
	}
	return filepath.Join(mediaDir, base+".nfo"), filepath.Join(mediaDir, base+"-poster"+posterExt)
}

func loadMovieNFO(storePath, besidePath string, it *db.MediaItem) metadata.MovieNFO {
	nfo := metadata.MovieNFO{Title: it.Title, Plot: it.Plot.String}
	if it.Year.Valid {
		nfo.Year = int(it.Year.Int64)
	}
	if n, err := metadata.ReadMovieNFO(storePath); err == nil && n != nil {
		return *n
	}
	if n, err := metadata.ReadMovieNFO(besidePath); err == nil && n != nil {
		return *n
	}
	return nfo
}

func loadTVShowNFO(storePath, besidePath string, it *db.MediaItem) metadata.TVShowNFO {
	nfo := metadata.TVShowNFO{Title: it.Title, Plot: it.Plot.String}
	if it.Year.Valid {
		nfo.Year = int(it.Year.Int64)
	}
	if n, err := metadata.ReadTVShowNFO(storePath); err == nil && n != nil {
		return *n
	}
	if n, err := metadata.ReadTVShowNFO(besidePath); err == nil && n != nil {
		return *n
	}
	return nfo
}

func patchTitlePlot(title, plot *string, setTitle func(string), setPlot func(string), currentTitle string) error {
	if title != nil {
		t := strings.TrimSpace(*title)
		if t == "" {
			return fmt.Errorf("title required")
		}
		setTitle(t)
	} else if currentTitle == "" {
		return fmt.Errorf("title required")
	}
	if plot != nil {
		setPlot(*plot)
	}
	return nil
}

func (w *Worker) EditMediaMeta(ctx context.Context, it *db.MediaItem, title, plot *string) error {
	if it == nil {
		return fmt.Errorf("missing item")
	}
	if it.Kind == "show" {
		return w.editShowMeta(ctx, it, title, plot)
	}
	return w.editMovieMeta(ctx, it, title, plot)
}

func (w *Worker) editMovieMeta(ctx context.Context, it *db.MediaItem, title, plot *string) error {
	besideNFO, _ := movieBesidePaths(it, ".jpg")
	nfoPath := sidecarDest(it.NFOPath.String, besideNFO)
	nfo := loadMovieNFO(legacyStorePath(w.Store, it.NFOPath.String), nfoPath, it)
	if err := patchTitlePlot(title, plot, func(t string) { nfo.Title = t }, func(p string) {
		nfo.Plot = p
		nfo.Outline = p
	}, nfo.Title); err != nil {
		return err
	}
	if nfo.Title == "" {
		return fmt.Errorf("title required")
	}
	if err := metadata.WriteMovieNFO(nfoPath, nfo); err != nil {
		return err
	}
	if err := w.DB.SetMediaItemTitlePlot(ctx, it.ID, title, plot); err != nil {
		return err
	}
	return w.DB.SetMediaItemPosterNFO(ctx, it.ID, nil, &nfoPath)
}

func (w *Worker) editShowMeta(ctx context.Context, it *db.MediaItem, title, plot *string) error {
	besideNFO := filepath.Join(it.Path, "tvshow.nfo")
	nfoPath := sidecarDest(it.NFOPath.String, besideNFO)
	nfo := loadTVShowNFO(legacyStorePath(w.Store, it.NFOPath.String), nfoPath, it)
	if err := patchTitlePlot(title, plot, func(t string) { nfo.Title = t }, func(p string) {
		nfo.Plot = p
		nfo.Outline = p
	}, nfo.Title); err != nil {
		return err
	}
	if nfo.Title == "" {
		return fmt.Errorf("title required")
	}
	if err := metadata.WriteTVShowNFO(nfoPath, nfo); err != nil {
		return err
	}
	if err := w.DB.SetMediaItemTitlePlot(ctx, it.ID, title, plot); err != nil {
		return err
	}
	return w.DB.SetMediaItemPosterNFO(ctx, it.ID, nil, &nfoPath)
}

func (w *Worker) EditMediaPoster(ctx context.Context, it *db.MediaItem, data []byte) (string, error) {
	if it == nil {
		return "", fmt.Errorf("missing item")
	}
	if int64(len(data)) > maxPosterBytes {
		return "", fmt.Errorf("image too large")
	}
	ext, err := sniffImageExt(data)
	if err != nil {
		return "", err
	}
	dst := ""
	if it.PosterPath.Valid && filepath.IsAbs(strings.TrimSpace(it.PosterPath.String)) {
		dst = strings.TrimSpace(it.PosterPath.String)
	} else if it.Kind == "show" {
		dst = filepath.Join(it.Path, "poster"+ext)
	} else {
		_, dst = movieBesidePaths(it, ext)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", err
	}
	if err := w.DB.SetMediaItemPosterNFO(ctx, it.ID, &dst, nil); err != nil {
		return "", err
	}
	return dst, nil
}

func seasonMediaDir(ctx context.Context, w *Worker, show *db.MediaItem, season *db.Season) string {
	eps, _ := w.DB.ListEpisodes(ctx, season.ID)
	if len(eps) > 0 {
		return filepath.Dir(eps[0].Path)
	}
	return show.Path
}

func (w *Worker) EditSeasonMeta(ctx context.Context, show *db.MediaItem, season *db.Season, title, plot *string) error {
	if show == nil || season == nil {
		return fmt.Errorf("missing season")
	}
	nfoName := fmt.Sprintf("season%02d.nfo", season.SeasonNumber)
	mediaDir := seasonMediaDir(ctx, w, show, season)
	beside := filepath.Join(mediaDir, nfoName)
	nfo := metadata.SeasonNFO{Title: season.Title.String, Plot: season.Plot.String}
	if n, err := metadata.ReadSeasonNFO(beside); err == nil && n != nil {
		nfo = *n
	}
	if strings.TrimSpace(nfo.Title) == "" {
		nfo.Title = seasonDisplayTitle(season.SeasonNumber, "", show.Title)
	}
	if err := patchTitlePlot(title, plot, func(t string) { nfo.Title = t }, func(p string) {
		nfo.Plot = p
		nfo.Outline = p
	}, nfo.Title); err != nil {
		return err
	}
	if nfo.Title == "" {
		return fmt.Errorf("title required")
	}
	if err := metadata.WriteSeasonNFO(beside, nfo); err != nil {
		return err
	}
	return w.DB.SetSeasonTitlePlot(ctx, season.ID, title, plot)
}

func (w *Worker) EditSeasonPoster(ctx context.Context, show *db.MediaItem, season *db.Season, data []byte) (string, error) {
	if show == nil || season == nil {
		return "", fmt.Errorf("missing season")
	}
	if int64(len(data)) > maxPosterBytes {
		return "", fmt.Errorf("image too large")
	}
	ext, err := sniffImageExt(data)
	if err != nil {
		return "", err
	}
	mediaDir := seasonMediaDir(ctx, w, show, season)
	beside := filepath.Join(mediaDir, "poster"+ext)
	if cur := strings.TrimSpace(season.PosterPath.String); filepath.IsAbs(cur) && filepath.Clean(filepath.Dir(cur)) == filepath.Clean(mediaDir) {
		beside = cur
	}
	if err := os.MkdirAll(filepath.Dir(beside), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(beside, data, 0o644); err != nil {
		return "", err
	}
	if err := w.DB.SetSeasonPoster(ctx, season.ID, beside); err != nil {
		return "", err
	}
	return beside, nil
}

func (w *Worker) EditEpisodeMeta(ctx context.Context, show *db.MediaItem, season *db.Season, ep *db.Episode, title, plot *string) error {
	if show == nil || season == nil || ep == nil {
		return fmt.Errorf("missing episode")
	}
	base := strings.TrimSuffix(filepath.Base(ep.Path), filepath.Ext(ep.Path))
	beside := sidecarDest(ep.NFOPath.String, filepath.Join(filepath.Dir(ep.Path), base+".nfo"))
	nfo := metadata.EpisodeNFO{
		Title:   ep.Title.String,
		Season:  season.SeasonNumber,
		Episode: ep.EpisodeNumber,
		Plot:    ep.Plot.String,
	}
	if n, err := metadata.ReadEpisodeNFO(legacyStorePath(w.Store, ep.NFOPath.String)); err == nil && n != nil {
		nfo = *n
	} else if n, err := metadata.ReadEpisodeNFO(beside); err == nil && n != nil {
		nfo = *n
	}
	if strings.TrimSpace(nfo.Title) == "" {
		nfo.Title = fmt.Sprintf("Episode %d", ep.EpisodeNumber)
	}
	if err := patchTitlePlot(title, plot, func(t string) { nfo.Title = t }, func(p string) {
		nfo.Plot = p
	}, nfo.Title); err != nil {
		return err
	}
	if nfo.Title == "" {
		return fmt.Errorf("title required")
	}
	if err := metadata.WriteEpisodeNFO(beside, nfo); err != nil {
		return err
	}
	if err := w.DB.SetEpisodeTitlePlot(ctx, ep.ID, title, plot); err != nil {
		return err
	}
	return w.DB.SetEpisodeStillNFO(ctx, ep.ID, nil, &beside)
}

func (w *Worker) EditEpisodePoster(ctx context.Context, show *db.MediaItem, season *db.Season, ep *db.Episode, data []byte) (string, error) {
	if show == nil || season == nil || ep == nil {
		return "", fmt.Errorf("missing episode")
	}
	if int64(len(data)) > maxPosterBytes {
		return "", fmt.Errorf("image too large")
	}
	ext, err := sniffImageExt(data)
	if err != nil {
		return "", err
	}
	base := strings.TrimSuffix(filepath.Base(ep.Path), filepath.Ext(ep.Path))
	beside := sidecarDest(ep.StillPath.String, filepath.Join(filepath.Dir(ep.Path), base+"-thumb"+ext))
	if err := os.MkdirAll(filepath.Dir(beside), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(beside, data, 0o644); err != nil {
		return "", err
	}
	if err := w.DB.SetEpisodeStillNFO(ctx, ep.ID, &beside, nil); err != nil {
		return "", err
	}
	return beside, nil
}

func removeExisting(paths ...string) {
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		_ = os.Remove(p)
	}
}

func posterNameVariants(dir, stem string) []string {
	var out []string
	for _, ext := range []string{".jpg", ".png", ".webp"} {
		out = append(out, filepath.Join(dir, stem+ext))
	}
	return out
}

func (w *Worker) RemoveMediaPoster(ctx context.Context, it *db.MediaItem) error {
	if it == nil {
		return fmt.Errorf("missing item")
	}
	if it.PosterPath.Valid && strings.TrimSpace(it.PosterPath.String) != "" {
		removeExisting(legacyStorePath(w.Store, it.PosterPath.String))
		if filepath.IsAbs(strings.TrimSpace(it.PosterPath.String)) {
			removeExisting(it.PosterPath.String)
		}
	}
	if it.Kind == "show" {
		removeExisting(posterNameVariants(it.Path, "poster")...)
	} else {
		mediaDir := filepath.Dir(it.Path)
		base := strings.TrimSuffix(filepath.Base(it.Path), filepath.Ext(it.Path))
		removeExisting(posterNameVariants(mediaDir, base+"-poster")...)
		if metadata.PreferBareMovieSidecar(it.Path) {
			removeExisting(posterNameVariants(mediaDir, "poster")...)
		}
	}
	return w.DB.ClearMediaItemPoster(ctx, it.ID)
}

func samePosterFile(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" || !filepath.IsAbs(a) || !filepath.IsAbs(b) {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func (w *Worker) RemoveSeasonPoster(ctx context.Context, show *db.MediaItem, season *db.Season) error {
	if show == nil || season == nil {
		return fmt.Errorf("missing season")
	}
	showPoster := ""
	if show.PosterPath.Valid {
		showPoster = strings.TrimSpace(show.PosterPath.String)
	}
	stored := ""
	if season.PosterPath.Valid {
		stored = strings.TrimSpace(season.PosterPath.String)
	}
	if stored != "" && !samePosterFile(stored, showPoster) {
		removeExisting(legacyStorePath(w.Store, stored))
		if filepath.IsAbs(stored) {
			removeExisting(stored)
		}
	}
	mediaDir := seasonMediaDir(ctx, w, show, season)
	if !(metadata.DirHasTVShowNFO(mediaDir) && filepath.Clean(mediaDir) == filepath.Clean(show.Path)) {
		var drop []string
		for _, p := range posterNameVariants(mediaDir, "poster") {
			if samePosterFile(p, showPoster) {
				continue
			}
			drop = append(drop, p)
		}
		removeExisting(drop...)
	}
	return w.DB.ClearSeasonPoster(ctx, season.ID)
}

func (w *Worker) RemoveEpisodePoster(ctx context.Context, show *db.MediaItem, season *db.Season, ep *db.Episode) error {
	if show == nil || season == nil || ep == nil {
		return fmt.Errorf("missing episode")
	}
	if ep.StillPath.Valid && strings.TrimSpace(ep.StillPath.String) != "" {
		removeExisting(legacyStorePath(w.Store, ep.StillPath.String))
		if filepath.IsAbs(strings.TrimSpace(ep.StillPath.String)) {
			removeExisting(ep.StillPath.String)
		}
	}
	base := strings.TrimSuffix(filepath.Base(ep.Path), filepath.Ext(ep.Path))
	removeExisting(posterNameVariants(filepath.Dir(ep.Path), base+"-thumb")...)
	return w.DB.ClearEpisodeStill(ctx, ep.ID)
}

func (w *Worker) SaveNodeStill(ctx context.Context, nodeID int64, filePath string, data []byte) (string, error) {
	if w == nil || w.DB == nil || nodeID == 0 || strings.TrimSpace(filePath) == "" {
		return "", fmt.Errorf("missing node")
	}
	if int64(len(data)) > maxPosterBytes {
		return "", fmt.Errorf("image too large")
	}
	ext, err := sniffImageExt(data)
	if err != nil {
		return "", err
	}
	base := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	dst := filepath.Join(filepath.Dir(filePath), base+"-thumb"+ext)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", err
	}
	if err := w.DB.SetNodeStill(ctx, nodeID, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func (w *Worker) RemoveNodeStill(ctx context.Context, node *db.LibraryNode) error {
	if w == nil || w.DB == nil || node == nil {
		return fmt.Errorf("missing node")
	}
	if p := strings.TrimSpace(node.StillPath.String); p != "" {
		if filepath.IsAbs(p) {
			removeExisting(p)
		} else {
			removeExisting(legacyStorePath(w.Store, p))
			removeExisting(posterNameVariants(filepath.Join(w.Store, "metadata", "nodes", strconv.FormatInt(node.ID, 10)), "still")...)
		}
	}
	return w.DB.SetNodeStill(ctx, node.ID, "")
}
