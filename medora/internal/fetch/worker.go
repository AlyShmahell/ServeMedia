package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyshmahell/medora/internal/db"
	"github.com/alyshmahell/medora/internal/media"
	"github.com/alyshmahell/medora/internal/metadata"
	"github.com/alyshmahell/medora/internal/plugins"
	"github.com/alyshmahell/medora-plugin-sdk/rpcapi"
)

// Worker runs metadata async jobs via the metadata plugin.
type Worker struct {
	DB    *db.DB
	Store string
	Meta  *plugins.MetadataClient
}

// EnrichOpts controls library-wide provider enrichment after a scan.
type EnrichOpts struct {
	All                bool // true = refetch all; false = missing meta_id only
	PersistBesideMedia bool
	ScanJobID          int64
}

type metaPayload struct {
	PersistBesideMedia bool `json:"persist"`
}

func (w *Worker) progress(ctx context.Context, jobID int64, i, n int, msg string) {
	pct := 5
	if n > 0 {
		pct = 5 + (95 * i / n)
		if pct > 99 {
			pct = 99
		}
	}
	_ = w.DB.UpdateAsyncJob(ctx, jobID, "running", pct, msg)
}

func (w *Worker) Run(ctx context.Context, jobID int64) {
	job, err := w.DB.GetAsyncJob(ctx, jobID)
	if err != nil || job == nil {
		return
	}
	switch job.Kind {
	case "subtitles":
		msg := "Failed: subtitle generation is not available"
		_ = w.DB.UpdateAsyncJob(ctx, jobID, "error", 100, msg)
		log.Printf("async job %d: %s", jobID, msg)
		return
	case "metadata":
		if err := w.runMetadata(ctx, job); err != nil {
			log.Printf("async job %d: %v", jobID, err)
			_ = w.DB.UpdateAsyncJob(ctx, jobID, "error", 100, err.Error())
			return
		}
		_ = w.DB.UpdateAsyncJob(ctx, jobID, "done", 100, "Complete")
	default:
		_ = w.DB.UpdateAsyncJob(ctx, jobID, "error", 100, fmt.Sprintf("unknown job kind %q", job.Kind))
	}
}

func persistFromJob(job *db.AsyncJob) bool {
	if job == nil || !job.PayloadJSON.Valid || job.PayloadJSON.String == "" {
		return true
	}
	var p metaPayload
	if json.Unmarshal([]byte(job.PayloadJSON.String), &p) != nil {
		return true
	}
	return p.PersistBesideMedia
}

func (w *Worker) requireMeta(ctx context.Context, jobID int64) error {
	if w.Meta == nil {
		return fmt.Errorf("metadata service unavailable")
	}
	st, err := w.Meta.Status()
	if err != nil {
		return fmt.Errorf("metadata service unavailable")
	}
	if !st.Ready {
		if st.DisabledReason != "" {
			return fmt.Errorf("%s", st.DisabledReason)
		}
		return fmt.Errorf("metadata service unavailable")
	}
	msg := "Looking up metadata"
	if st.Hint != "" {
		msg = st.Hint
	}
	_ = w.DB.UpdateAsyncJob(ctx, jobID, "running", 10, msg)
	return nil
}

func (w *Worker) runMetadata(ctx context.Context, job *db.AsyncJob) error {
	persist := persistFromJob(job)
	// Episode still-only (ffmpeg) does not need the providers sidecar.
	if job.Scope == "episode" {
		return w.enrichEpisode(ctx, job.ID, job.TargetID, persist)
	}
	if err := w.requireMeta(ctx, job.ID); err != nil {
		return err
	}
	switch job.Scope {
	case "movie":
		return w.enrichMovie(ctx, job.ID, job.TargetID, persist)
	case "show":
		return w.enrichShow(ctx, job.ID, job.TargetID, persist)
	case "season":
		return w.enrichSeason(ctx, job.ID, job.TargetID, persist)
	default:
		return fmt.Errorf("unknown scope %q", job.Scope)
	}
}

// EnrichLibrary runs provider metadata jobs for a library serially.
func (w *Worker) EnrichLibrary(ctx context.Context, lib *db.Library, opts EnrichOpts) error {
	if w.Meta == nil {
		return fmt.Errorf("metadata service unavailable")
	}
	st, err := w.Meta.Status()
	if err != nil {
		return fmt.Errorf("metadata service unavailable")
	}
	if !st.Ready {
		if st.DisabledReason != "" {
			return fmt.Errorf("%s", st.DisabledReason)
		}
		return fmt.Errorf("metadata service unavailable")
	}
	targets, err := w.DB.ListLibraryMetaTargets(ctx, lib.ID, !opts.All)
	if err != nil {
		return err
	}
	n := len(targets)
	if n == 0 {
		if opts.ScanJobID > 0 {
			_ = w.DB.UpdateScanJob(ctx, opts.ScanJobID, "running", 100, "Nothing to enrich")
		}
		return nil
	}
	payload, _ := json.Marshal(metaPayload{PersistBesideMedia: opts.PersistBesideMedia})
	for i, t := range targets {
		if opts.ScanJobID > 0 {
			pct := (100 * i) / n
			_ = w.DB.UpdateScanJob(ctx, opts.ScanJobID, "running", pct, fmt.Sprintf("Enriching %d/%d", i+1, n))
		}
		jobID, err := w.DB.CreateAsyncJob(ctx, "metadata", t.Scope, t.ID, string(payload))
		if err != nil {
			log.Printf("enrich create job: %v", err)
			continue
		}
		w.Run(ctx, jobID)
	}
	return nil
}

// EnrichMediaItem runs provider metadata for one movie or one show tree.
func (w *Worker) EnrichMediaItem(ctx context.Context, item *db.MediaItem, opts EnrichOpts) error {
	if item == nil {
		return fmt.Errorf("missing media item")
	}
	if w.Meta == nil {
		return fmt.Errorf("metadata service unavailable")
	}
	st, err := w.Meta.Status()
	if err != nil {
		return fmt.Errorf("metadata service unavailable")
	}
	if !st.Ready {
		if st.DisabledReason != "" {
			return fmt.Errorf("%s", st.DisabledReason)
		}
		return fmt.Errorf("metadata service unavailable")
	}
	targets, err := w.DB.ListMediaMetaTargets(ctx, item.ID, !opts.All)
	if err != nil {
		return err
	}
	n := len(targets)
	if n == 0 {
		if opts.ScanJobID > 0 {
			_ = w.DB.UpdateScanJob(ctx, opts.ScanJobID, "running", 100, "Nothing to enrich")
		}
		return nil
	}
	payload, _ := json.Marshal(metaPayload{PersistBesideMedia: opts.PersistBesideMedia})
	for i, t := range targets {
		if opts.ScanJobID > 0 {
			pct := (100 * i) / n
			_ = w.DB.UpdateScanJob(ctx, opts.ScanJobID, "running", pct, fmt.Sprintf("Enriching %d/%d", i+1, n))
		}
		jobID, err := w.DB.CreateAsyncJob(ctx, "metadata", t.Scope, t.ID, string(payload))
		if err != nil {
			log.Printf("enrich create job: %v", err)
			continue
		}
		w.Run(ctx, jobID)
	}
	return nil
}

func (w *Worker) writeImage(dir, baseName, imageURL string) string {
	if imageURL == "" || dir == "" {
		return ""
	}
	img, ext, err := w.Meta.DownloadURL(imageURL)
	if err != nil {
		return ""
	}
	if ext == "" {
		ext = ".jpg"
	}
	name := baseName + ext
	path, err := metadata.WriteBytesBesideDir(dir, name, img)
	if err != nil {
		return ""
	}
	return path
}

func (w *Worker) libraryType(ctx context.Context, mediaItemID int64) string {
	typ, _ := w.DB.LibraryTypeForMediaItem(ctx, mediaItemID)
	return typ
}

func (w *Worker) enrichMovie(ctx context.Context, jobID, movieID int64, persist bool) error {
	it, err := w.DB.GetMediaItem(ctx, movieID)
	if err != nil || it == nil || it.Kind != "movie" {
		return fmt.Errorf("movie not found")
	}
	w.progress(ctx, jobID, 1, 4, "Searching metadata")
	// Prefer folder/file name over DB/NFO title so a prior bad match cannot
	// poison Meta refetch (stale NFO/DB title stuck after a wrong match).
	lookupTitle, lookupYear := metadata.TitleYearFromVideoPath(it.Path)
	if lookupTitle == "" {
		lookupTitle = it.Title
		if it.Year.Valid {
			lookupYear = int(it.Year.Int64)
		}
	}
	libType := w.libraryType(ctx, it.ID)
	durMin := 0
	if p, err := media.Ffprobe(it.Path); err == nil && p != nil {
		if sec := p.DurationSeconds(); sec > 0 {
			durMin = int(sec/60 + 0.5)
		}
	}
	res, err := w.Meta.LookupMovie(lookupTitle, lookupYear, libType, durMin)
	if err != nil && strings.EqualFold(libType, "anime") {
		if stripped := metadata.StripMovieTitleSuffix(lookupTitle); stripped != "" && stripped != lookupTitle {
			res, err = w.Meta.LookupMovie(stripped, lookupYear, libType, durMin)
		}
	}
	if err != nil {
		return err
	}
	if res.Message != "" {
		w.progress(ctx, jobID, 2, 4, res.Message)
	} else {
		w.progress(ctx, jobID, 2, 4, "Fetching details")
	}
	mediaDir := filepath.Dir(it.Path)
	base := strings.TrimSuffix(filepath.Base(it.Path), filepath.Ext(it.Path))
	nfo := metadata.MovieNFO{
		Title: res.Title, Year: res.Year, Plot: res.Plot, Tagline: res.Tagline,
		Runtime: res.Runtime, Rating: res.Rating,
	}
	if nfo.Title == "" {
		nfo.Title = lookupTitle
	}
	cacheKey := metadata.SanitizePathSegment(metadata.TitleYear(nfo.Title, nfo.Year))
	cacheDir := filepath.Join(w.Store, "metadata", "movies", cacheKey)
	_ = os.MkdirAll(cacheDir, 0o755)
	nfoRel := filepath.Join("metadata", "movies", cacheKey, "movie.nfo")
	storeNFO := filepath.Join(w.Store, nfoRel)
	if err := metadata.WriteMovieNFO(storeNFO, nfo); err != nil {
		return err
	}
	bareOK := metadata.PreferBareMovieSidecar(it.Path)
	if persist {
		if bareOK {
			_ = metadata.CopyFile(storeNFO, filepath.Join(mediaDir, "movie.nfo"))
		} else {
			metadata.QuarantineMedoraRejected(filepath.Join(mediaDir, "movie.nfo"))
			metadata.QuarantineMedoraRejected(filepath.Join(mediaDir, "poster.jpg"))
			_ = metadata.CopyFile(storeNFO, filepath.Join(mediaDir, base+".nfo"))
		}
	}
	w.progress(ctx, jobID, 3, 4, "Downloading artwork")
	posterRel := ""
	if p := w.writeImage(cacheDir, "poster", res.PosterURL); p != "" {
		posterRel = filepath.Join("metadata", "movies", cacheKey, filepath.Base(p))
		if persist {
			if bareOK {
				_ = metadata.CopyFile(p, filepath.Join(mediaDir, filepath.Base(p)))
			} else {
				_ = metadata.CopyFile(p, filepath.Join(mediaDir, base+"-poster"+filepath.Ext(p)))
			}
		}
	}
	backdropRel := ""
	if p := w.writeImage(cacheDir, "fanart", res.BackdropURL); p != "" {
		backdropRel = filepath.Join("metadata", "movies", cacheKey, filepath.Base(p))
		if persist {
			if bareOK {
				_ = metadata.CopyFile(p, filepath.Join(mediaDir, filepath.Base(p)))
			} else {
				_ = metadata.CopyFile(p, filepath.Join(mediaDir, base+"-fanart"+filepath.Ext(p)))
			}
		}
	}
	return w.DB.UpdateMediaItemMeta(ctx, it.ID, nfo.Title, nfo.Year, nfo.Plot, posterRel, backdropRel, nfoRel, nfo.Rating, res.Provider, res.ProviderID)
}

func (w *Worker) enrichShow(ctx context.Context, jobID, showID int64, persist bool) error {
	it, err := w.DB.GetMediaItem(ctx, showID)
	if err != nil || it == nil || it.Kind != "show" {
		return fmt.Errorf("show not found")
	}
	w.progress(ctx, jobID, 1, 3, "Searching metadata")
	libType := w.libraryType(ctx, it.ID)
	// Prefer folder name over DB/NFO title so a prior bad match cannot poison Meta.
	lookupTitle, lookupYear := metadata.ParseTitleYear(filepath.Base(it.Path))
	lookupTitle = strings.TrimSpace(lookupTitle)
	if lookupTitle == "" || lookupTitle == "." {
		lookupTitle = strings.TrimSpace(it.Title)
		if it.Year.Valid {
			lookupYear = int(it.Year.Int64)
		}
	}
	dedupProvider := "tvmaze"
	if w.Meta != nil {
		dedupProvider = w.Meta.ShowDedupProvider()
	}
	excludeIDs, _ := w.DB.ListTakenMetaIDs(ctx, it.LibraryID, dedupProvider, it.ID)
	res, err := w.Meta.LookupShow(lookupTitle, lookupYear, libType, excludeIDs...)
	if err != nil {
		return err
	}
	if res.Message != "" {
		w.progress(ctx, jobID, 2, 3, res.Message)
	}
	mediaDir := it.Path
	nfo := metadata.TVShowNFO{
		Title: res.Title, Year: res.Year, Plot: res.Plot, Tagline: res.Tagline,
		Runtime: res.Runtime, Rating: res.Rating,
	}
	if nfo.Title == "" {
		nfo.Title = lookupTitle
	}
	cacheKey := metadata.SanitizePathSegment(nfo.Title)
	cacheDir := filepath.Join(w.Store, "metadata", "tv", cacheKey)
	_ = os.MkdirAll(cacheDir, 0o755)
	nfoRel := filepath.Join("metadata", "tv", cacheKey, "tvshow.nfo")
	storeNFO := filepath.Join(w.Store, nfoRel)
	if err := metadata.WriteTVShowNFO(storeNFO, nfo); err != nil {
		return err
	}
	if persist {
		_ = metadata.CopyFile(storeNFO, filepath.Join(mediaDir, "tvshow.nfo"))
	}
	w.progress(ctx, jobID, 2, 3, "Downloading artwork")
	posterRel := ""
	if p := w.writeImage(cacheDir, "poster", res.PosterURL); p != "" {
		posterRel = filepath.Join("metadata", "tv", cacheKey, filepath.Base(p))
		if persist {
			_ = metadata.CopyFile(p, filepath.Join(mediaDir, filepath.Base(p)))
		}
	}
	if p := w.writeImage(cacheDir, "fanart", res.BackdropURL); p != "" && persist {
		_ = metadata.CopyFile(p, filepath.Join(mediaDir, filepath.Base(p)))
	}
	return w.DB.UpdateMediaItemMeta(ctx, it.ID, nfo.Title, nfo.Year, nfo.Plot, posterRel, "", nfoRel, nfo.Rating, res.Provider, res.ProviderID)
}

func (w *Worker) enrichSeason(ctx context.Context, jobID, seasonID int64, persist bool) error {
	season, err := w.DB.GetSeason(ctx, seasonID)
	if err != nil || season == nil {
		return fmt.Errorf("season not found")
	}
	show, err := w.DB.GetMediaItem(ctx, season.ShowID)
	if err != nil || show == nil {
		return fmt.Errorf("show not found")
	}
	w.progress(ctx, jobID, 1, 3, "Searching metadata")
	libType := w.libraryType(ctx, show.ID)
	showProvider, showProviderID := "", ""
	if show.MetaProvider.Valid {
		showProvider = show.MetaProvider.String
	}
	if show.MetaID.Valid {
		showProviderID = show.MetaID.String
	}
	res, err := w.Meta.LookupSeason(show.Title, season.SeasonNumber, libType, showProvider, showProviderID)
	if err != nil {
		return err
	}
	if res.Message != "" {
		w.progress(ctx, jobID, 2, 3, res.Message)
	}
	eps, _ := w.DB.ListEpisodes(ctx, season.ID)
	mediaDir := show.Path
	if len(eps) > 0 {
		mediaDir = filepath.Dir(eps[0].Path)
	}
	title := res.Title
	if title == "" {
		title = fmt.Sprintf("Season %d", season.SeasonNumber)
	}
	showKey := metadata.SanitizePathSegment(show.Title)
	cacheDir := filepath.Join(w.Store, "metadata", "tv", showKey, fmt.Sprintf("S%02d", season.SeasonNumber))
	_ = os.MkdirAll(cacheDir, 0o755)
	nfoName := fmt.Sprintf("season%02d.nfo", season.SeasonNumber)
	storeNFO := filepath.Join(cacheDir, nfoName)
	if err := metadata.WriteSeasonNFO(storeNFO, metadata.SeasonNFO{Title: title, Plot: res.Plot}); err != nil {
		return err
	}
	if persist {
		_ = metadata.CopyFile(storeNFO, filepath.Join(mediaDir, nfoName))
	}
	w.progress(ctx, jobID, 2, 3, "Downloading artwork")
	posterRel := ""
	if p := w.writeImage(cacheDir, "poster", res.PosterURL); p != "" {
		posterRel = filepath.Join("metadata", "tv", showKey, fmt.Sprintf("S%02d", season.SeasonNumber), filepath.Base(p))
		if persist {
			_ = metadata.CopyFile(p, filepath.Join(mediaDir, filepath.Base(p)))
		}
	}
	// When the provider has no season art, reuse the show poster.
	if posterRel == "" && show.PosterPath.Valid && show.PosterPath.String != "" {
		src := filepath.Join(w.Store, show.PosterPath.String)
		dst := filepath.Join(cacheDir, "poster"+filepath.Ext(src))
		if filepath.Ext(dst) == "" {
			dst = filepath.Join(cacheDir, "poster.jpg")
		}
		if err := metadata.CopyFile(src, dst); err == nil {
			posterRel = filepath.Join("metadata", "tv", showKey, fmt.Sprintf("S%02d", season.SeasonNumber), filepath.Base(dst))
			if persist {
				_ = metadata.CopyFile(dst, filepath.Join(mediaDir, filepath.Base(dst)))
			}
		}
	}
	return w.DB.UpdateSeasonMeta(ctx, season.ID, title, res.Plot, posterRel, res.Provider, res.ProviderID)
}

func (w *Worker) enrichEpisode(ctx context.Context, jobID, episodeID int64, persist bool) error {
	ep, err := w.DB.GetEpisode(ctx, episodeID)
	if err != nil || ep == nil {
		return fmt.Errorf("episode not found")
	}
	show, err := w.DB.GetMediaItem(ctx, ep.ShowID)
	if err != nil || show == nil {
		return fmt.Errorf("show not found")
	}
	season, err := w.DB.GetSeason(ctx, ep.SeasonID)
	if err != nil || season == nil {
		return fmt.Errorf("season not found")
	}
	mediaDir := filepath.Dir(ep.Path)
	base := strings.TrimSuffix(filepath.Base(ep.Path), filepath.Ext(ep.Path))
	showKey := metadata.SanitizePathSegment(show.Title)
	cacheDir := filepath.Join(w.Store, "metadata", "tv", showKey, fmt.Sprintf("S%02d", season.SeasonNumber))
	_ = os.MkdirAll(cacheDir, 0o755)

	// Already have provider meta but no still: only generate a frame grab.
	stillOnly := ep.MetaID.Valid && ep.MetaID.String != "" && (!ep.StillPath.Valid || ep.StillPath.String == "")
	var res rpcapi.Result
	nfoRel := ""
	if stillOnly {
		w.progress(ctx, jobID, 1, 2, "Generating still")
		if ep.Title.Valid {
			res.Title = ep.Title.String
		}
		if ep.Plot.Valid {
			res.Plot = ep.Plot.String
		}
		if ep.MetaProvider.Valid {
			res.Provider = ep.MetaProvider.String
		}
		res.ProviderID = ep.MetaID.String
		if ep.NFOPath.Valid {
			nfoRel = ep.NFOPath.String
		}
	} else {
		if err := w.requireMeta(ctx, jobID); err != nil {
			return err
		}
		w.progress(ctx, jobID, 1, 3, "Searching metadata")
		libType := w.libraryType(ctx, show.ID)
		showProvider, showProviderID := "", ""
		if show.MetaProvider.Valid {
			showProvider = show.MetaProvider.String
		}
		if show.MetaID.Valid {
			showProviderID = show.MetaID.String
		}
		res, err = w.Meta.LookupEpisode(show.Title, season.SeasonNumber, ep.EpisodeNumber, libType, showProvider, showProviderID)
		if err != nil {
			return err
		}
		if res.Message != "" {
			w.progress(ctx, jobID, 2, 3, res.Message)
		}
		nfo := metadata.EpisodeNFO{
			Title: res.Title, Season: season.SeasonNumber, Episode: ep.EpisodeNumber,
			Plot: res.Plot, Runtime: res.Runtime,
		}
		if nfo.Title == "" {
			nfo.Title = fmt.Sprintf("Episode %d", ep.EpisodeNumber)
		}
		nfoRel = filepath.Join("metadata", "tv", showKey, fmt.Sprintf("S%02d", season.SeasonNumber), base+".nfo")
		storeNFO := filepath.Join(w.Store, nfoRel)
		if err := metadata.WriteEpisodeNFO(storeNFO, nfo); err != nil {
			return err
		}
		if persist {
			_ = metadata.CopyFile(storeNFO, filepath.Join(mediaDir, base+".nfo"))
		}
		res.Title = nfo.Title
	}

	w.progress(ctx, jobID, 2, 3, "Downloading still")
	stillRel := ""
	if p := w.writeImage(cacheDir, base+"-thumb", res.StillURL); p != "" {
		ext := filepath.Ext(p)
		thumbName := base + "-thumb" + ext
		stillRel = filepath.Join("metadata", "tv", showKey, fmt.Sprintf("S%02d", season.SeasonNumber), thumbName)
		if persist {
			_ = metadata.CopyFile(p, filepath.Join(mediaDir, thumbName))
		}
	}
	if stillRel == "" {
		w.progress(ctx, jobID, 2, 3, "Generating still from video")
		thumbName := base + "-thumb.jpg"
		outPath := filepath.Join(cacheDir, thumbName)
		if err := media.ExtractStill(ep.Path, outPath); err != nil {
			log.Printf("episode %d still: %v", ep.ID, err)
		} else {
			stillRel = filepath.Join("metadata", "tv", showKey, fmt.Sprintf("S%02d", season.SeasonNumber), thumbName)
			if persist {
				_ = metadata.CopyFile(outPath, filepath.Join(mediaDir, thumbName))
			}
		}
	}
	title := res.Title
	if title == "" {
		title = fmt.Sprintf("Episode %d", ep.EpisodeNumber)
	}
	return w.DB.UpdateEpisodeMeta(ctx, ep.ID, title, res.Plot, stillRel, nfoRel, res.Provider, res.ProviderID)
}
