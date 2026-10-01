package fetch

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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

func writeStoreAndBeside(storePath, besidePath string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(storePath, data, 0o644); err != nil {
		return err
	}
	if besidePath == "" {
		return nil
	}
	return metadata.CopyFile(storePath, besidePath)
}

func (w *Worker) mediaNFORel(it *db.MediaItem) string {
	if it.NFOPath.Valid && strings.TrimSpace(it.NFOPath.String) != "" {
		return it.NFOPath.String
	}
	if it.Kind == "movie" {
		year := 0
		if it.Year.Valid {
			year = int(it.Year.Int64)
		}
		key := metadata.SanitizePathSegment(metadata.TitleYear(it.Title, year))
		return filepath.Join("metadata", "movies", key, "movie.nfo")
	}
	key := metadata.SanitizePathSegment(it.Title)
	return filepath.Join("metadata", "tv", key, "tvshow.nfo")
}

func (w *Worker) showCacheKey(show *db.MediaItem) string {
	if show.NFOPath.Valid && strings.TrimSpace(show.NFOPath.String) != "" {
		return filepath.Base(filepath.Dir(show.NFOPath.String))
	}
	return metadata.SanitizePathSegment(show.Title)
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
	nfoRel := w.mediaNFORel(it)
	storePath := filepath.Join(w.Store, nfoRel)
	besideNFO, _ := movieBesidePaths(it, ".jpg")
	nfo := loadMovieNFO(storePath, besideNFO, it)
	if err := patchTitlePlot(title, plot, func(t string) { nfo.Title = t }, func(p string) {
		nfo.Plot = p
		nfo.Outline = p
	}, nfo.Title); err != nil {
		return err
	}
	if nfo.Title == "" {
		return fmt.Errorf("title required")
	}
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		return err
	}
	if err := metadata.WriteMovieNFO(storePath, nfo); err != nil {
		return err
	}
	if err := metadata.CopyFile(storePath, besideNFO); err != nil {
		return err
	}
	if err := w.DB.SetMediaItemTitlePlot(ctx, it.ID, title, plot); err != nil {
		return err
	}
	return w.DB.SetMediaItemPosterNFO(ctx, it.ID, nil, &nfoRel)
}

func (w *Worker) editShowMeta(ctx context.Context, it *db.MediaItem, title, plot *string) error {
	nfoRel := w.mediaNFORel(it)
	storePath := filepath.Join(w.Store, nfoRel)
	besideNFO := filepath.Join(it.Path, "tvshow.nfo")
	nfo := loadTVShowNFO(storePath, besideNFO, it)
	if err := patchTitlePlot(title, plot, func(t string) { nfo.Title = t }, func(p string) {
		nfo.Plot = p
		nfo.Outline = p
	}, nfo.Title); err != nil {
		return err
	}
	if nfo.Title == "" {
		return fmt.Errorf("title required")
	}
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		return err
	}
	if err := metadata.WriteTVShowNFO(storePath, nfo); err != nil {
		return err
	}
	if err := metadata.CopyFile(storePath, besideNFO); err != nil {
		return err
	}
	if err := w.DB.SetMediaItemTitlePlot(ctx, it.ID, title, plot); err != nil {
		return err
	}
	return w.DB.SetMediaItemPosterNFO(ctx, it.ID, nil, &nfoRel)
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
	nfoRel := w.mediaNFORel(it)
	cacheDir := filepath.Join(w.Store, filepath.Dir(nfoRel))
	storePath := filepath.Join(cacheDir, "poster"+ext)
	var beside string
	if it.Kind == "show" {
		beside = filepath.Join(it.Path, "poster"+ext)
	} else {
		_, beside = movieBesidePaths(it, ext)
	}
	if err := writeStoreAndBeside(storePath, beside, data); err != nil {
		return "", err
	}
	rel := filepath.Join(filepath.Dir(nfoRel), "poster"+ext)
	if err := w.DB.SetMediaItemPosterNFO(ctx, it.ID, &rel, nil); err != nil {
		return "", err
	}
	return rel, nil
}

func seasonStoreDir(show *db.MediaItem, seasonNum int, w *Worker) string {
	return filepath.Join(w.Store, "metadata", "tv", w.showCacheKey(show), fmt.Sprintf("S%02d", seasonNum))
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
	sdir := seasonStoreDir(show, season.SeasonNumber, w)
	storePath := filepath.Join(sdir, nfoName)
	mediaDir := seasonMediaDir(ctx, w, show, season)
	beside := filepath.Join(mediaDir, nfoName)
	nfo := metadata.SeasonNFO{Title: season.Title.String, Plot: season.Plot.String}
	if n, err := metadata.ReadSeasonNFO(storePath); err == nil && n != nil {
		nfo = *n
	} else if n, err := metadata.ReadSeasonNFO(beside); err == nil && n != nil {
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
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		return err
	}
	if err := metadata.WriteSeasonNFO(storePath, nfo); err != nil {
		return err
	}
	if err := metadata.CopyFile(storePath, beside); err != nil {
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
	sdir := seasonStoreDir(show, season.SeasonNumber, w)
	storePath := filepath.Join(sdir, "poster"+ext)
	beside := filepath.Join(seasonMediaDir(ctx, w, show, season), "poster"+ext)
	if err := writeStoreAndBeside(storePath, beside, data); err != nil {
		return "", err
	}
	rel := filepath.Join("metadata", "tv", w.showCacheKey(show), fmt.Sprintf("S%02d", season.SeasonNumber), "poster"+ext)
	if err := w.DB.SetSeasonPoster(ctx, season.ID, rel); err != nil {
		return "", err
	}
	return rel, nil
}

func (w *Worker) EditEpisodeMeta(ctx context.Context, show *db.MediaItem, season *db.Season, ep *db.Episode, title, plot *string) error {
	if show == nil || season == nil || ep == nil {
		return fmt.Errorf("missing episode")
	}
	base := strings.TrimSuffix(filepath.Base(ep.Path), filepath.Ext(ep.Path))
	nfoRel := ep.NFOPath.String
	if strings.TrimSpace(nfoRel) == "" {
		nfoRel = filepath.Join("metadata", "tv", w.showCacheKey(show), fmt.Sprintf("S%02d", season.SeasonNumber), base+".nfo")
	}
	storePath := filepath.Join(w.Store, nfoRel)
	mediaDir := filepath.Dir(ep.Path)
	beside := filepath.Join(mediaDir, base+".nfo")
	nfo := metadata.EpisodeNFO{
		Title:   ep.Title.String,
		Season:  season.SeasonNumber,
		Episode: ep.EpisodeNumber,
		Plot:    ep.Plot.String,
	}
	if n, err := metadata.ReadEpisodeNFO(storePath); err == nil && n != nil {
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
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		return err
	}
	if err := metadata.WriteEpisodeNFO(storePath, nfo); err != nil {
		return err
	}
	if err := metadata.CopyFile(storePath, beside); err != nil {
		return err
	}
	if err := w.DB.SetEpisodeTitlePlot(ctx, ep.ID, title, plot); err != nil {
		return err
	}
	return w.DB.SetEpisodeStillNFO(ctx, ep.ID, nil, &nfoRel)
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
	thumbName := base + "-thumb" + ext
	rel := filepath.Join("metadata", "tv", w.showCacheKey(show), fmt.Sprintf("S%02d", season.SeasonNumber), thumbName)
	storePath := filepath.Join(w.Store, rel)
	beside := filepath.Join(filepath.Dir(ep.Path), thumbName)
	if err := writeStoreAndBeside(storePath, beside, data); err != nil {
		return "", err
	}
	if err := w.DB.SetEpisodeStillNFO(ctx, ep.ID, &rel, nil); err != nil {
		return "", err
	}
	return rel, nil
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
		removeExisting(filepath.Join(w.Store, it.PosterPath.String))
	}
	nfoRel := w.mediaNFORel(it)
	cacheDir := filepath.Join(w.Store, filepath.Dir(nfoRel))
	removeExisting(posterNameVariants(cacheDir, "poster")...)
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

func (w *Worker) RemoveSeasonPoster(ctx context.Context, show *db.MediaItem, season *db.Season) error {
	if show == nil || season == nil {
		return fmt.Errorf("missing season")
	}
	if season.PosterPath.Valid && strings.TrimSpace(season.PosterPath.String) != "" {
		removeExisting(filepath.Join(w.Store, season.PosterPath.String))
	}
	sdir := seasonStoreDir(show, season.SeasonNumber, w)
	removeExisting(posterNameVariants(sdir, "poster")...)
	mediaDir := seasonMediaDir(ctx, w, show, season)
	if !(metadata.DirHasTVShowNFO(mediaDir) && filepath.Clean(mediaDir) == filepath.Clean(show.Path)) {
		removeExisting(posterNameVariants(mediaDir, "poster")...)
	}
	return w.DB.ClearSeasonPoster(ctx, season.ID)
}

func (w *Worker) RemoveEpisodePoster(ctx context.Context, show *db.MediaItem, season *db.Season, ep *db.Episode) error {
	if show == nil || season == nil || ep == nil {
		return fmt.Errorf("missing episode")
	}
	if ep.StillPath.Valid && strings.TrimSpace(ep.StillPath.String) != "" {
		removeExisting(filepath.Join(w.Store, ep.StillPath.String))
	}
	base := strings.TrimSuffix(filepath.Base(ep.Path), filepath.Ext(ep.Path))
	sdir := seasonStoreDir(show, season.SeasonNumber, w)
	removeExisting(posterNameVariants(sdir, base+"-thumb")...)
	removeExisting(posterNameVariants(filepath.Dir(ep.Path), base+"-thumb")...)
	return w.DB.ClearEpisodeStill(ctx, ep.ID)
}
