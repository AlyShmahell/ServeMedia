package fetch

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alyshmahell/medora/internal/db"
	"github.com/alyshmahell/medora/internal/matchora"
	"github.com/alyshmahell/medora/internal/metadata"
	"github.com/alyshmahell/medora/internal/scanner"
)

type Worker struct {
	DB    *db.DB
	Store string
	Meta  *matchora.Client
}

type Opts struct {
	Persist   bool
	Overwrite bool
	ScanJobID int64
}

func (w *Worker) progress(ctx context.Context, jobID int64, pct int, msg string) {
	if jobID <= 0 {
		return
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 99 {
		pct = 99
	}
	_ = w.DB.UpdateScanJob(ctx, jobID, "running", pct, msg)
}

func (w *Worker) MatchLibrary(ctx context.Context, lib *db.Library, opts Opts) error {
	if lib == nil {
		return fmt.Errorf("missing library")
	}
	return w.MatchPath(ctx, lib, lib.Path, nil, opts)
}

func (w *Worker) MatchItem(ctx context.Context, lib *db.Library, item *db.MediaItem, opts Opts) error {
	if item == nil || lib == nil {
		return fmt.Errorf("missing media item")
	}
	scanPath := item.Path
	if item.Kind == "movie" {
		st, err := os.Stat(item.Path)
		if err == nil && !st.IsDir() {
			scanPath = filepath.Dir(item.Path)
		}
	}
	return w.MatchPath(ctx, lib, scanPath, item, opts)
}

func (w *Worker) MatchPath(ctx context.Context, lib *db.Library, scanPath string, only *db.MediaItem, opts Opts) error {
	if w.Meta == nil {
		return fmt.Errorf("metadata service unavailable")
	}
	st, err := w.Meta.Status()
	if err != nil || !st.Ready {
		return fmt.Errorf("metadata service unavailable")
	}
	w.progress(ctx, opts.ScanJobID, 5, "Matching titles…")
	scanned, err := w.Meta.Scan(scanPath)
	if err != nil {
		return err
	}
	return w.streamJobs(ctx, lib, scanned.Session, only, opts)
}

func (w *Worker) streamJobs(ctx context.Context, lib *db.Library, session string, only *db.MediaItem, opts Opts) error {
	applied := map[string]struct{}{}
	deadline := time.Now().Add(75 * time.Minute)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("matchora timed out")
		}
		p, err := w.Meta.ScanStatus(session)
		if err != nil {
			return err
		}
		jobs, err := w.Meta.Jobs(session)
		if err != nil {
			return err
		}
		pending := 0
		for _, j := range jobs {
			if j.Status == "pending" || j.Status == "" {
				pending++
			}
		}
		w.reportStreamProgress(ctx, opts.ScanJobID, p, len(applied), len(jobs))
		appliedOne := false
		for _, j := range jobs {
			if j.Status == "pending" || j.Status == "" {
				continue
			}
			if _, ok := applied[j.ID]; ok {
				continue
			}
			if err := w.applyFinishedJob(ctx, lib, j, only, opts, session); err != nil {
				return err
			}
			applied[j.ID] = struct{}{}
			appliedOne = true
			p, err = w.Meta.ScanStatus(session)
			if err != nil {
				return err
			}
			w.reportStreamProgress(ctx, opts.ScanJobID, p, len(applied), len(jobs))
			break
		}
		if !appliedOne && !p.Running && pending == 0 {
			return nil
		}
		if !appliedOne {
			time.Sleep(400 * time.Millisecond)
		}
	}
}

func (w *Worker) reportStreamProgress(ctx context.Context, jobID int64, p matchora.ScanProgress, applied, total int) {
	if p.Running {
		pct, msg := groupingProgress(p, applied)
		w.progress(ctx, jobID, pct, msg)
		return
	}
	pct := 40
	if total > 0 {
		pct = 40 + 59*applied/total
	}
	w.progress(ctx, jobID, pct, fmt.Sprintf("Applied %d/%d", applied, total))
}

func groupingProgress(p matchora.ScanProgress, applied int) (int, string) {
	done, total := p.Done, p.Files
	if total <= 0 {
		done, total = p.Chunk, p.Chunks
	}
	if total <= 0 {
		return 5, "Matching titles…"
	}
	pct := 5 + 34*done/total
	if pct > 39 {
		pct = 39
	}
	msg := fmt.Sprintf("Grouping %d/%d", done, total)
	if applied > 0 {
		msg = fmt.Sprintf("Grouping %d/%d · %d titles", done, total, applied)
	}
	return pct, msg
}

func (w *Worker) applyFinishedJob(ctx context.Context, lib *db.Library, j matchora.Job, only *db.MediaItem, opts Opts, session string) error {
	if only != nil && !jobTouchesItem(j, only) {
		return nil
	}
	it, err := w.upsertFromJob(ctx, lib.ID, j, opts.Overwrite)
	if err != nil {
		return err
	}
	if it == nil {
		log.Printf("matchora job %s path %s: no library item", j.ID, j.Path)
		return nil
	}
	if only != nil && it.ID != only.ID && !jobTouchesItem(j, only) {
		return nil
	}
	if err := w.applyJob(ctx, it, j, opts, session); err != nil {
		log.Printf("apply %s: %v", j.ID, err)
	}
	return nil
}

func (w *Worker) ApplySelect(ctx context.Context, item *db.MediaItem, provider, id string, persist bool) error {
	if item == nil || !item.MatchoraJobID.Valid || item.MatchoraJobID.String == "" {
		return fmt.Errorf("no matchora job for this title")
	}
	session := ""
	if item.MatchoraSessionID.Valid {
		session = strings.TrimSpace(item.MatchoraSessionID.String)
	}
	if session == "" {
		return fmt.Errorf("no Matchora job for this title — rescan to match again")
	}
	j, err := w.Meta.Select(session, item.MatchoraJobID.String, provider, id)
	if err != nil {
		return err
	}
	it, err := w.upsertFromJob(ctx, item.LibraryID, j, true)
	if err != nil {
		return err
	}
	if it == nil {
		it = item
	}
	return w.applyJob(ctx, it, j, Opts{Persist: persist, Overwrite: true}, session)
}

func (w *Worker) upsertFromJob(ctx context.Context, libraryID int64, j matchora.Job, overwrite bool) (*db.MediaItem, error) {
	files := expandJobFiles(j)
	if len(files) == 0 {
		return nil, nil
	}
	kind := kindFromJob(j, files)
	itemPath := strings.TrimSpace(j.Path)
	if kind == "movie" {
		itemPath = files[0].Path
	}
	if itemPath == "" {
		itemPath = files[0].Path
	}
	title := strings.TrimSpace(j.Title)
	if title == "" && j.Match != nil {
		title = strings.TrimSpace(j.Match.Title)
	}
	if title == "" {
		title = metadata.CleanEpisodeTitle(filepath.Base(itemPath))
	}
	if title == "" {
		title = filepath.Base(itemPath)
	}
	mtime := fileMtime(itemPath)
	existing, err := w.DB.GetMediaItemByPath(ctx, libraryID, itemPath)
	if err != nil {
		return nil, err
	}
	keepMeta := existing != nil && existing.MetaID.Valid && strings.TrimSpace(existing.MetaID.String) != "" && !overwrite
	var id int64
	if keepMeta {
		id = existing.ID
		_ = w.DB.TouchMediaItemMtime(ctx, id, mtime)
	} else {
		id, err = w.DB.UpsertMediaItem(ctx, db.MediaItem{
			LibraryID: libraryID,
			Kind:      kind,
			Title:     title,
			SortTitle: title,
			Path:      itemPath,
			Mtime:     mtime,
		})
		if err != nil {
			return nil, err
		}
	}
	it, err := w.DB.GetMediaItem(ctx, id)
	if err != nil || it == nil {
		return it, err
	}
	if kind == "show" {
		showPath := strings.TrimSpace(j.Path)
		if showPath == "" {
			showPath = filepath.Dir(files[0].Path)
		}
		sc := &scanner.Scanner{DB: w.DB, StorePath: w.Store}
		if err := sc.IngestShowEpisodes(ctx, it.ID, showPath); err != nil {
			return nil, err
		}
	}
	return w.DB.GetMediaItem(ctx, id)
}

func expandJobFiles(j matchora.Job) []matchora.JobFile {
	var videos []matchora.JobFile
	for _, f := range j.Files {
		if strings.TrimSpace(f.Path) == "" || !isVideoPath(f.Path) {
			continue
		}
		videos = append(videos, f)
	}
	if len(videos) > 0 {
		return videos
	}
	path := strings.TrimSpace(j.Path)
	if path == "" {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if !st.IsDir() {
		if isVideoPath(path) {
			return []matchora.JobFile{{Path: path}}
		}
		return nil
	}
	out := make([]matchora.JobFile, 0)
	for _, p := range scanner.CollectShowVideos(path) {
		out = append(out, matchora.JobFile{Path: p})
	}
	return out
}

func kindFromJob(j matchora.Job, files []matchora.JobFile) string {
	root := strings.TrimSpace(j.Path)
	if root != "" {
		if st, err := os.Stat(root); err == nil {
			if st.IsDir() {
				if scanner.LooksLikeShowDir(root) {
					return "show"
				}
				return "movie"
			}
			if isVideoPath(root) {
				return "movie"
			}
		}
	}
	for _, f := range files {
		if strings.TrimSpace(f.Season) != "" || strings.TrimSpace(f.Episode) != "" {
			return "show"
		}
		if j.Path != "" && fileNestedUnderDir(f.Path, j.Path) {
			return "show"
		}
	}
	if len(files) == 1 && isVideoPath(files[0].Path) {
		return "movie"
	}
	if len(files) > 1 {
		return "show"
	}
	if isVideoPath(root) {
		return "movie"
	}
	return "movie"
}

func jobTouchesItem(j matchora.Job, item *db.MediaItem) bool {
	if item == nil {
		return true
	}
	ip := filepath.Clean(item.Path)
	jp := filepath.Clean(j.Path)
	if jp == ip {
		return true
	}
	sep := string(os.PathSeparator)
	if jp != "" && strings.HasPrefix(ip, jp+sep) {
		return true
	}
	if ip != "" && strings.HasPrefix(jp, ip+sep) {
		return true
	}
	for _, f := range j.Files {
		fp := filepath.Clean(f.Path)
		if fp == ip || (ip != "" && strings.HasPrefix(fp, ip+sep)) {
			return true
		}
	}
	return false
}

func fileNestedUnderDir(file, dir string) bool {
	dir = filepath.Clean(dir)
	parent := filepath.Clean(filepath.Dir(file))
	if parent == dir {
		return false
	}
	rel, err := filepath.Rel(dir, parent)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	return true
}

func isVideoPath(path string) bool {
	return metadata.IsVideo(filepath.Base(path))
}

func fileMtime(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return time.Now().Unix()
	}
	return st.ModTime().Unix()
}

func (w *Worker) applyJob(ctx context.Context, it *db.MediaItem, j matchora.Job, opts Opts, session string) error {
	hasMeta := it.MetaID.Valid && strings.TrimSpace(it.MetaID.String) != ""
	switch j.Status {
	case "manual", "multiple":
		if hasMeta && !opts.Overwrite {
			return nil
		}
		return w.DB.SetMatchoraMatch(ctx, it.ID, session, j.ID, "manual")
	case "unmatched", "error":
		if hasMeta && !opts.Overwrite {
			return nil
		}
		status := j.Status
		if status == "error" {
			status = "unmatched"
		}
		return w.DB.SetMatchoraMatch(ctx, it.ID, session, j.ID, status)
	case "matched":
		if hasMeta && !opts.Overwrite {
			return w.fillMissingStills(ctx, it, opts.Persist, session)
		}
		return w.applyMatched(ctx, it, j, opts.Persist, session)
	default:
		return nil
	}
}

func (w *Worker) applyMatched(ctx context.Context, it *db.MediaItem, j matchora.Job, persist bool, session string) error {
	cand := j.Match
	if cand == nil {
		return fmt.Errorf("unmatched")
	}
	cat, err := w.Meta.Catalog(session, cand.Provider, cand.ID)
	if err != nil {
		cat = matchora.Catalog{
			Provider: cand.Provider, ID: cand.ID, Title: cand.Title, Year: cand.Year,
			Synopsis: cand.Synopsis, Poster: cand.Poster,
		}
	}
	if it.Kind == "show" {
		if err := w.applyShow(ctx, it, cat, persist, session); err != nil {
			return err
		}
	} else {
		if err := w.applyMovie(ctx, it, cat, persist, session); err != nil {
			return err
		}
	}
	return w.DB.SetMatchoraMatch(ctx, it.ID, session, j.ID, "matched")
}

func (w *Worker) applyMovie(ctx context.Context, it *db.MediaItem, cat matchora.Catalog, persist bool, session string) error {
	title := cat.Title
	if title == "" {
		title = it.Title
	}
	year := atoiYear(cat.Year)
	mediaDir := filepath.Dir(it.Path)
	base := strings.TrimSuffix(filepath.Base(it.Path), filepath.Ext(it.Path))
	nfo := metadata.MovieNFO{Title: title, Year: year, Plot: cat.Synopsis}
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
	posterRel := ""
	if p := w.writeImage(cacheDir, "poster", cat.Poster, session); p != "" {
		posterRel = filepath.Join("metadata", "movies", cacheKey, filepath.Base(p))
		if persist {
			if bareOK {
				_ = metadata.CopyFile(p, filepath.Join(mediaDir, filepath.Base(p)))
			} else {
				_ = metadata.CopyFile(p, filepath.Join(mediaDir, base+"-poster"+filepath.Ext(p)))
			}
		}
	}
	return w.DB.UpdateMediaItemMeta(ctx, it.ID, nfo.Title, nfo.Year, nfo.Plot, posterRel, "", nfoRel, 0, cat.Provider, cat.ID)
}

func (w *Worker) applyShow(ctx context.Context, it *db.MediaItem, cat matchora.Catalog, persist bool, session string) error {
	title := cat.Title
	if title == "" {
		title = it.Title
	}
	year := atoiYear(cat.Year)
	nfo := metadata.TVShowNFO{Title: title, Year: year, Plot: cat.Synopsis}
	cacheKey := metadata.SanitizePathSegment(nfo.Title)
	cacheDir := filepath.Join(w.Store, "metadata", "tv", cacheKey)
	_ = os.MkdirAll(cacheDir, 0o755)
	nfoRel := filepath.Join("metadata", "tv", cacheKey, "tvshow.nfo")
	storeNFO := filepath.Join(w.Store, nfoRel)
	if err := metadata.WriteTVShowNFO(storeNFO, nfo); err != nil {
		return err
	}
	if persist {
		_ = metadata.CopyFile(storeNFO, filepath.Join(it.Path, "tvshow.nfo"))
	}
	posterRel := ""
	if p := w.writeImage(cacheDir, "poster", cat.Poster, session); p != "" {
		posterRel = filepath.Join("metadata", "tv", cacheKey, filepath.Base(p))
		if persist {
			_ = metadata.CopyFile(p, filepath.Join(it.Path, filepath.Base(p)))
		}
	}
	if err := w.DB.UpdateMediaItemMeta(ctx, it.ID, nfo.Title, nfo.Year, nfo.Plot, posterRel, "", nfoRel, 0, cat.Provider, cat.ID); err != nil {
		return err
	}
	it.Title = nfo.Title
	if posterRel != "" {
		it.PosterPath = sql.NullString{String: posterRel, Valid: true}
	}
	seasons, _ := w.DB.ListSeasons(ctx, it.ID)
	for _, season := range seasons {
		cs := cat.FindSeason(season.SeasonNumber)
		stitle := fmt.Sprintf("Season %d", season.SeasonNumber)
		plot := ""
		sposter := ""
		if cs != nil {
			if cs.Title != "" {
				stitle = cs.Title
			}
			plot = cs.Synopsis
			sposter = cs.Poster
		}
		eps, _ := w.DB.ListEpisodes(ctx, season.ID)
		mediaDir := it.Path
		if len(eps) > 0 {
			mediaDir = filepath.Dir(eps[0].Path)
		}
		sdir := filepath.Join(w.Store, "metadata", "tv", cacheKey, fmt.Sprintf("S%02d", season.SeasonNumber))
		_ = os.MkdirAll(sdir, 0o755)
		nfoName := fmt.Sprintf("season%02d.nfo", season.SeasonNumber)
		_ = metadata.WriteSeasonNFO(filepath.Join(sdir, nfoName), metadata.SeasonNFO{Title: stitle, Plot: plot})
		if persist {
			_ = metadata.CopyFile(filepath.Join(sdir, nfoName), filepath.Join(mediaDir, nfoName))
		}
		posterRel := ""
		if p := w.writeImage(sdir, "poster", sposter, session); p != "" {
			posterRel = filepath.Join("metadata", "tv", cacheKey, fmt.Sprintf("S%02d", season.SeasonNumber), filepath.Base(p))
			if persist {
				_ = metadata.CopyFile(p, filepath.Join(mediaDir, filepath.Base(p)))
			}
		}
		if posterRel == "" && it.PosterPath.Valid && it.PosterPath.String != "" {
			src := filepath.Join(w.Store, it.PosterPath.String)
			dst := filepath.Join(sdir, "poster"+filepath.Ext(src))
			if filepath.Ext(dst) == "" {
				dst = filepath.Join(sdir, "poster.jpg")
			}
			if err := metadata.CopyFile(src, dst); err == nil {
				posterRel = filepath.Join("metadata", "tv", cacheKey, fmt.Sprintf("S%02d", season.SeasonNumber), filepath.Base(dst))
			}
		}
		_ = w.DB.UpdateSeasonMeta(ctx, season.ID, stitle, plot, posterRel, cat.Provider, cat.ID)
		for _, ep := range eps {
			ce := cat.FindEpisode(season.SeasonNumber, ep.EpisodeNumber)
			w.applyEpisode(ctx, it, &season, &ep, ce, persist, session)
		}
	}
	return nil
}

func (w *Worker) applyEpisode(ctx context.Context, show *db.MediaItem, season *db.Season, ep *db.Episode, ce *matchora.Episode, persist bool, session string) {
	mediaDir := filepath.Dir(ep.Path)
	base := strings.TrimSuffix(filepath.Base(ep.Path), filepath.Ext(ep.Path))
	showKey := metadata.SanitizePathSegment(show.Title)
	cacheDir := filepath.Join(w.Store, "metadata", "tv", showKey, fmt.Sprintf("S%02d", season.SeasonNumber))
	_ = os.MkdirAll(cacheDir, 0o755)
	title := fmt.Sprintf("Episode %d", ep.EpisodeNumber)
	plot := ""
	stillURL := ""
	if ce != nil {
		if ce.Title != "" {
			title = ce.Title
		}
		plot = ce.Synopsis
		stillURL = ce.Poster
	}
	nfoRel := filepath.Join("metadata", "tv", showKey, fmt.Sprintf("S%02d", season.SeasonNumber), base+".nfo")
	_ = metadata.WriteEpisodeNFO(filepath.Join(w.Store, nfoRel), metadata.EpisodeNFO{
		Title: title, Season: season.SeasonNumber, Episode: ep.EpisodeNumber, Plot: plot,
	})
	if persist {
		_ = metadata.CopyFile(filepath.Join(w.Store, nfoRel), filepath.Join(mediaDir, base+".nfo"))
	}
	stillRel := ""
	if p := w.writeImage(cacheDir, base+"-thumb", stillURL, session); p != "" {
		thumbName := base + "-thumb" + filepath.Ext(p)
		stillRel = filepath.Join("metadata", "tv", showKey, fmt.Sprintf("S%02d", season.SeasonNumber), thumbName)
		if persist {
			_ = metadata.CopyFile(p, filepath.Join(mediaDir, thumbName))
		}
	}
	prov, pid := "", ""
	if show.MetaProvider.Valid {
		prov = show.MetaProvider.String
	}
	if show.MetaID.Valid {
		pid = show.MetaID.String
	}
	_ = w.DB.UpdateEpisodeMeta(ctx, ep.ID, title, plot, stillRel, nfoRel, prov, pid)
}

func (w *Worker) fillMissingStills(ctx context.Context, it *db.MediaItem, persist bool, session string) error {
	if it.Kind != "show" || !it.MetaProvider.Valid || !it.MetaID.Valid {
		return nil
	}
	if session == "" && it.MatchoraSessionID.Valid {
		session = it.MatchoraSessionID.String
	}
	cat, err := w.Meta.Catalog(session, it.MetaProvider.String, it.MetaID.String)
	if err != nil {
		return nil
	}
	seasons, _ := w.DB.ListSeasons(ctx, it.ID)
	for _, season := range seasons {
		eps, _ := w.DB.ListEpisodes(ctx, season.ID)
		for i := range eps {
			if eps[i].StillPath.Valid && eps[i].StillPath.String != "" {
				continue
			}
			ce := cat.FindEpisode(season.SeasonNumber, eps[i].EpisodeNumber)
			w.applyEpisode(ctx, it, &season, &eps[i], ce, persist, session)
		}
	}
	return nil
}

func (w *Worker) writeImage(dir, baseName, imageURL, session string) string {
	if imageURL == "" || dir == "" || w.Meta == nil {
		return ""
	}
	img, ext, err := w.Meta.DownloadURL(imageURL, session)
	if err != nil {
		return ""
	}
	if ext == "" {
		ext = ".jpg"
	}
	path, err := metadata.WriteBytesBesideDir(dir, baseName+ext, img)
	if err != nil {
		return ""
	}
	return path
}

func atoiYear(s string) int {
	s = strings.TrimSpace(s)
	if len(s) >= 4 {
		var n int
		_, _ = fmt.Sscanf(s[:4], "%d", &n)
		return n
	}
	var n int
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}
