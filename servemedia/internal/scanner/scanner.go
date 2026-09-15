package scanner

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/alyshmahell/medora/internal/db"
	"github.com/alyshmahell/medora/internal/metadata"
)

type Scanner struct {
	DB         *db.DB
	StorePath  string
	MediaRoot  string
	Webhooks   WebhookNotifier
	mixedWalks atomic.Int32
}

// MixedWalks is how many times the local disk walker ran (tests).
func (s *Scanner) MixedWalks() int {
	if s == nil {
		return 0
	}
	return int(s.mixedWalks.Load())
}

type WebhookNotifier interface {
	DispatchItemAdded(ctx context.Context, userID int64, item *db.MediaItem)
	DispatchEpisodeAdded(ctx context.Context, userID int64, ep *db.Episode, show *db.MediaItem)
	DispatchTaskCompleted(ctx context.Context, userID int64, message string)
}

// ScanLibrary walks the library and marks the job done on success.
func (s *Scanner) ScanLibrary(ctx context.Context, lib *db.Library, jobID int64) {
	s.scanLibrary(ctx, lib, jobID, true)
}

func (s *Scanner) scanLibrary(ctx context.Context, lib *db.Library, jobID int64, markDone bool) {
	_ = s.DB.UpdateScanJob(ctx, jobID, "running", 1, "Scanning library…")
	err := s.scanMixed(ctx, lib, jobID)
	if err != nil {
		log.Printf("scan library %d: %v", lib.ID, err)
		_ = s.DB.UpdateScanJob(ctx, jobID, "error", 100, err.Error())
		return
	}
	if markDone {
		_ = s.DB.UpdateScanJob(ctx, jobID, "done", 100, "Complete")
		if s.Webhooks != nil {
			s.Webhooks.DispatchTaskCompleted(ctx, lib.UserID, "Library scan complete")
		}
		return
	}
	_ = s.DB.UpdateScanJob(ctx, jobID, "running", 100, "Scan complete")
}

// RescanMediaItem re-ingests a single movie file or show directory (and its episodes).
func (s *Scanner) RescanMediaItem(ctx context.Context, lib *db.Library, item *db.MediaItem, jobID int64) error {
	if item == nil || lib == nil {
		return fmt.Errorf("missing media item")
	}
	_ = s.DB.UpdateScanJob(ctx, jobID, "running", 5, "Scanning…")
	switch item.Kind {
	case "movie":
		_ = s.DB.UpdateScanJob(ctx, jobID, "running", 40, "Scanning movie")
		return s.ingestMovie(ctx, lib, item.Path)
	case "show":
		showPath := item.Path
		_ = s.DB.UpdateScanJob(ctx, jobID, "running", 20, "Indexing show")
		showID, err := s.ingestShow(ctx, lib, showPath)
		if err != nil {
			return err
		}
		paths := collectShowVideos(showPath)
		_ = s.DB.UpdateScanJob(ctx, jobID, "running", 50, fmt.Sprintf("Scanning %d episodes", len(paths)))
		return s.ingestAnimeEpisodes(ctx, showID, showPath, paths)
	default:
		return fmt.Errorf("unsupported kind %q", item.Kind)
	}
}

func (s *Scanner) progress(ctx context.Context, jobID int64, i, n int, msg string) {
	pct := 5
	if n > 0 {
		pct = 5 + (95 * i / n)
		if pct > 99 {
			pct = 99
		}
	}
	_ = s.DB.UpdateScanJob(ctx, jobID, "running", pct, msg)
}

// collectMovieFiles walks root for primary movie videos: skips extras, and in
// dedicated movie folders with multiple versions keeps only the best file.
func collectMovieFiles(root string) []string {
	byDir := map[string][]string{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && metadata.IsMovieExtraFolderName(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !metadata.IsVideo(d.Name()) || metadata.IsMovieExtra(path) {
			return nil
		}
		dir := filepath.Dir(path)
		byDir[dir] = append(byDir[dir], path)
		return nil
	})
	var files []string
	for dir, list := range byDir {
		sort.Strings(list)
		if metadata.IsGenericLibraryFolderName(filepath.Base(dir)) || dir == filepath.Clean(root) {
			// Flat multi-movie: each primary is its own card.
			files = append(files, list...)
			continue
		}
		if len(list) > 1 {
			files = append(files, metadata.PickPrimaryMovie(list))
			continue
		}
		files = append(files, list...)
	}
	sort.Strings(files)
	return files
}

func (s *Scanner) ingestMovie(ctx context.Context, lib *db.Library, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	// Provider-matched movies: do not re-apply poisoned local NFO/posters on scan.
	if existing, err := s.DB.GetMediaItemByPath(ctx, lib.ID, path); err == nil && existing != nil {
		if existing.MetaID.Valid && strings.TrimSpace(existing.MetaID.String) != "" {
			return s.DB.TouchMediaItemMtime(ctx, existing.ID, info.ModTime().Unix())
		}
	}
	dir := filepath.Dir(path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	title, year := metadata.TitleYearFromVideoPath(path)
	pathTitle := title
	plot := ""
	runtime := 0
	rating := 0.0
	bareOK := metadata.PreferBareMovieSidecar(path)
	nfoSrc := metadata.FindMovieSidecar(dir, base, bareOK, "movie.nfo", base+".nfo")
	if nfoSrc != "" {
		if n, err := metadata.ReadMovieNFO(nfoSrc); err == nil {
			if n.Title != "" && !metadata.MovieNFOMatchesPath(n.Title, pathTitle) {
				metadata.QuarantineMedoraRejected(nfoSrc)
				nfoSrc = ""
			} else {
				if n.Title != "" {
					title = n.Title
				}
				if n.Year != 0 {
					year = n.Year
				}
				plot = n.Plot
				runtime = n.Runtime
				rating = metadata.NumericRating(n.Rating, n.Ratings)
			}
		}
	}
	// Quarantine shared bare NFO in flat dirs even if unused for this file.
	if !bareOK {
		metadata.QuarantineMedoraRejected(filepath.Join(dir, "movie.nfo"))
		metadata.QuarantineMedoraRejected(filepath.Join(dir, "poster.jpg"))
	}
	cacheDir := filepath.Join(s.StorePath, "metadata", "movies", sanitize(titleYear(title, year)))
	_ = os.MkdirAll(cacheDir, 0o755)
	nfoRel := filepath.Join("metadata", "movies", sanitize(titleYear(title, year)), "movie.nfo")
	nfoDst := filepath.Join(s.StorePath, nfoRel)
	if nfoSrc != "" {
		_ = metadata.CopyFile(nfoSrc, nfoDst)
	} else {
		_ = metadata.WriteMovieNFO(nfoDst, metadata.MovieNFO{Title: title, Year: year, Plot: plot, Runtime: runtime})
	}
	posterRel := ""
	if p := metadata.FindMovieSidecar(dir, base, bareOK, "poster.jpg", "folder.jpg", "poster.png"); p != "" {
		posterRel = filepath.Join("metadata", "movies", sanitize(titleYear(title, year)), "poster.jpg")
		_ = metadata.CopyFile(p, filepath.Join(s.StorePath, posterRel))
	}
	backdropRel := ""
	if p := metadata.FindMovieSidecar(dir, base, bareOK, "fanart.jpg", "backdrop.jpg", "banner.jpg"); p != "" {
		backdropRel = filepath.Join("metadata", "movies", sanitize(titleYear(title, year)), "fanart.jpg")
		_ = metadata.CopyFile(p, filepath.Join(s.StorePath, backdropRel))
	}
	it := db.MediaItem{
		LibraryID:      lib.ID,
		Kind:           "movie",
		Title:          title,
		SortTitle:      title,
		Year:           nullInt(year),
		Path:           path,
		RuntimeSeconds: sql.NullInt64{Int64: int64(runtime), Valid: runtime > 0},
		Plot:           sql.NullString{String: plot, Valid: plot != ""},
		PosterPath:     sql.NullString{String: posterRel, Valid: posterRel != ""},
		BackdropPath:   sql.NullString{String: backdropRel, Valid: backdropRel != ""},
		NFOPath:        sql.NullString{String: nfoRel, Valid: true},
		Rating:         sql.NullFloat64{Float64: rating, Valid: rating > 0},
		Mtime:          info.ModTime().Unix(),
	}
	existed, _ := s.DB.MediaItemExistsAtPath(ctx, lib.ID, path)
	id, err := s.DB.UpsertMediaItem(ctx, it)
	if err == nil && !existed && s.Webhooks != nil {
		it.ID = id
		s.Webhooks.DispatchItemAdded(ctx, lib.UserID, &it)
	}
	return err
}

func (s *Scanner) scanMixed(ctx context.Context, lib *db.Library, jobID int64) error {
	s.mixedWalks.Add(1)
	root := lib.Path
	if !filepath.IsAbs(root) {
		root = filepath.Join(s.MediaRoot, root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var showDirs []string
	var movieFiles []string
	for _, e := range entries {
		full := filepath.Join(root, e.Name())
		if e.IsDir() {
			if isFilmPack(full) {
				movieFiles = append(movieFiles, filmPackMovies(full)...)
				continue
			}
			if isFranchisePack(full) {
				showDirs = append(showDirs, expandShowRoots(full)...)
				ents, _ := os.ReadDir(full)
				for _, fe := range ents {
					if fe.IsDir() {
						continue
					}
					fp := filepath.Join(full, fe.Name())
					if metadata.IsVideo(fe.Name()) && !metadata.IsMovieExtra(fp) {
						movieFiles = append(movieFiles, fp)
					}
				}
				continue
			}
			if looksLikeShowDir(full) {
				showDirs = append(showDirs, full)
				continue
			}
			// Film walk only non-show top-level trees.
			_ = filepath.WalkDir(full, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					if path != full && metadata.IsMovieExtraFolderName(d.Name()) {
						return filepath.SkipDir
					}
					return nil
				}
				if !metadata.IsVideo(d.Name()) || metadata.IsMovieExtra(path) {
					return nil
				}
				movieFiles = append(movieFiles, path)
				return nil
			})
			// Multi-version folder: keep best only.
			if !metadata.IsGenericLibraryFolderName(e.Name()) {
				primaries := metadata.ListPrimaryVideosInDir(full)
				if len(primaries) > 1 {
					// Remove walked files under this dir and keep pick.
					filtered := movieFiles[:0]
					prefix := full + string(os.PathSeparator)
					for _, p := range movieFiles {
						if p == full || strings.HasPrefix(p, prefix) {
							continue
						}
						filtered = append(filtered, p)
					}
					movieFiles = append(filtered, metadata.PickPrimaryMovie(primaries))
				}
			}
			continue
		}
		if metadata.IsVideo(e.Name()) && !metadata.IsMovieExtra(full) {
			movieFiles = append(movieFiles, full)
		}
	}
	n := len(showDirs) + len(movieFiles)
	type epFile struct {
		showDir string
		path    string
	}
	var eps []epFile
	for _, showPath := range showDirs {
		for _, p := range collectShowVideos(showPath) {
			eps = append(eps, epFile{showDir: showPath, path: p})
		}
	}
	total := len(showDirs) + len(eps) + len(movieFiles)
	if total == 0 {
		total = n
	}
	_ = s.DB.UpdateScanJob(ctx, jobID, "running", 5, fmt.Sprintf("Found %d shows, %d films", len(showDirs), len(movieFiles)))
	done := 0
	showIDs := map[string]int64{}
	for _, showPath := range showDirs {
		s.progress(ctx, jobID, done, total, fmt.Sprintf("Show %d/%d", done+1, len(showDirs)))
		id, err := s.ingestShow(ctx, lib, showPath)
		if err != nil {
			return err
		}
		showIDs[showPath] = id
		done++
	}
	byShow := map[string][]string{}
	for _, f := range eps {
		byShow[f.showDir] = append(byShow[f.showDir], f.path)
	}
	epDone := 0
	for showPath, paths := range byShow {
		showID := showIDs[showPath]
		if showID == 0 {
			continue
		}
		s.progress(ctx, jobID, done+epDone, total, fmt.Sprintf("Episode %d/%d", epDone+1, len(eps)))
		if err := s.ingestAnimeEpisodes(ctx, showID, showPath, paths); err != nil {
			return err
		}
		epDone += len(paths)
	}
	done += len(eps)
	for i, path := range movieFiles {
		s.progress(ctx, jobID, done+i, total, fmt.Sprintf("Film %d/%d", i+1, len(movieFiles)))
		if err := s.ingestMovie(ctx, lib, path); err != nil {
			return err
		}
	}
	return nil
}

// LooksLikeShowDir reports whether dir should be ingested as a show (same rules
// as the local mixed walker). Single-video film folders are not shows.
func LooksLikeShowDir(dir string) bool {
	return looksLikeShowDir(dir)
}

// CollectShowVideos lists all videos under a show (including Movies/ packs and
// root films). Those become season 0 episodes at ingest — not library movies.
func CollectShowVideos(showPath string) []string {
	return collectShowVideos(showPath)
}

// IngestShowEpisodes numbers and upserts episodes under an already-created show
// row. Walks only showPath (not the whole library).
func (s *Scanner) IngestShowEpisodes(ctx context.Context, showID int64, showPath string) error {
	return s.ingestAnimeEpisodes(ctx, showID, showPath, collectShowVideos(showPath))
}

// collectShowVideos lists all videos under a show (including Movies/ packs and
// root films). Those become season 0 episodes at ingest — not library movies.
func collectShowVideos(showPath string) []string {
	var episodes []string
	_ = filepath.WalkDir(showPath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !metadata.IsVideo(d.Name()) {
			return err
		}
		episodes = append(episodes, path)
		return nil
	})
	return episodes
}

// looksLikeShowDir is anime-only show detection.
// Priority: tvshow.nfo (unless single-video film override), nested SxxEyy,
// immediate Season N / Sxx / OVA child, 2+ child dirs that contain videos
// (irregular season folders without Season N markers), or 2+ non-extra videos
// (flat / single-cour packs without Season N markers).
func looksLikeShowDir(dir string) bool {
	if isSingleVideoFilmDir(dir) {
		return false
	}
	if fileExists(filepath.Join(dir, "tvshow.nfo")) {
		return true
	}
	foundSxxEyy := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !metadata.IsVideo(d.Name()) {
			return err
		}
		if _, _, ok := metadata.ParseEpisode(d.Name()); ok {
			foundSxxEyy = true
			return filepath.SkipAll
		}
		return nil
	})
	if foundSxxEyy {
		return true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && metadata.IsSeasonFolderName(e.Name()) {
			return true
		}
	}
	// Irregular multi-cour layout: several child dirs with videos, no Season N names.
	if len(metadata.SeasonIndexForShow(dir)) >= 2 {
		return true
	}
	// Flat or single-cour multi-episode packs without Season N / SxxEyy markers.
	return countNonExtraVideos(dir) >= 2
}

// countNonExtraVideos counts videos under dir that are not OP/ED/trailer extras.
func countNonExtraVideos(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !metadata.IsVideo(d.Name()) {
			return err
		}
		if metadata.IsEpisodeExtra(d.Name()) {
			return nil
		}
		n++
		return nil
	})
	return n
}

// isStrongNestedShow reports a non-season child that looks like its own series
// (tvshow.nfo or immediate Season/Sxx/OVA folders).
func isStrongNestedShow(dir string) bool {
	base := filepath.Base(dir)
	if metadata.IsSeasonFolderName(base) {
		return false
	}
	if fileExists(filepath.Join(dir, "tvshow.nfo")) {
		return true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && metadata.IsSeasonFolderName(e.Name()) {
			return true
		}
	}
	return false
}

// isFranchisePack is true when dir groups 2+ nested series
// rather than a single show with Season/cour/Movies/OVA children.
func isFranchisePack(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	strong := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if metadata.IsSeasonFolderName(e.Name()) || metadata.IsMoviesFolderName(e.Name()) {
			continue
		}
		child := filepath.Join(dir, e.Name())
		if isStrongNestedShow(child) {
			strong++
		}
	}
	return strong >= 2
}

// isFilmPack is true when dir holds ≥2 single-video film folders and no season children
// (OVA/film packs that must not become one parent TV show).
func isFilmPack(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	films := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if metadata.IsSeasonFolderName(e.Name()) {
			return false
		}
		child := filepath.Join(dir, e.Name())
		if isSingleVideoFilmDir(child) {
			films++
		}
	}
	return films >= 2
}

// expandShowRoots returns nested show paths for a franchise pack, or [dir] otherwise.
func expandShowRoots(dir string) []string {
	if !isFranchisePack(dir) {
		return []string{dir}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{dir}
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || metadata.IsSeasonFolderName(e.Name()) || metadata.IsMoviesFolderName(e.Name()) {
			continue
		}
		child := filepath.Join(dir, e.Name())
		if isStrongNestedShow(child) {
			out = append(out, child)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return []string{dir}
	}
	return out
}

// filmPackMovies lists primary videos under a film-pack directory (each film folder + root videos).
func filmPackMovies(dir string) []string {
	var out []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			if metadata.IsSeasonFolderName(e.Name()) {
				continue
			}
			if !isSingleVideoFilmDir(full) {
				continue
			}
			_ = filepath.WalkDir(full, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() || !metadata.IsVideo(d.Name()) || metadata.IsMovieExtra(path) {
					return err
				}
				out = append(out, path)
				return nil
			})
			continue
		}
		if metadata.IsVideo(e.Name()) && !metadata.IsMovieExtra(full) {
			out = append(out, full)
		}
	}
	sort.Strings(out)
	return out
}

// isSingleVideoFilmDir is true when a tree has exactly one video, no season-child
// dirs, and no SxxEyy videos — even if a stray tvshow.nfo is present.
func isSingleVideoFilmDir(dir string) bool {
	videoCount := 0
	hasSxxEyy := false
	hasSeasonChild := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != dir && metadata.IsSeasonFolderName(d.Name()) {
				hasSeasonChild = true
			}
			return nil
		}
		if !metadata.IsVideo(d.Name()) {
			return nil
		}
		videoCount++
		if _, _, ok := metadata.ParseEpisode(d.Name()); ok {
			hasSxxEyy = true
		}
		return nil
	})
	return videoCount == 1 && !hasSeasonChild && !hasSxxEyy
}

// ingestAnimeEpisodes assigns season/episode for show trees.
// Seasons come from folder layout (Season N / OVA / irregular child dirs).
// Per season bucket: ingest ParseEpisode hits, then ResolveEpisodeLoose for
// remaining files (mixed SxxEyy + dash-numbered packs).
// Under-show Movies/ packs and root films beside seasons become season 0.
// If the show still has zero ingested episodes, number remaining videos sequentially.
func (s *Scanner) ingestAnimeEpisodes(ctx context.Context, showID int64, showPath string, paths []string) error {
	index := metadata.SeasonIndexForShow(showPath)
	bySeason := map[int][]string{}
	for _, path := range paths {
		sn := metadata.SeasonForPathWithIndex(path, showPath, index)
		bySeason[sn] = append(bySeason[sn], path)
	}
	ingested := 0
	seen := map[string]bool{}
	for seasonNum, list := range bySeason {
		sort.Strings(list)
		for _, path := range list {
			if metadata.IsEpisodeExtra(path) {
				continue
			}
			if _, _, ok := metadata.ParseEpisode(filepath.Base(path)); ok {
				_, ep, _ := metadata.ParseEpisode(filepath.Base(path))
				// Prefer folder-assigned season so S04E01 in "Cour 1" stays season 1.
				if err := s.ingestEpisodeAt(ctx, showID, path, seasonNum, ep); err != nil {
					return err
				}
				seen[path] = true
				ingested++
			}
		}
		for _, path := range list {
			if seen[path] || metadata.IsEpisodeExtra(path) {
				continue
			}
			season, ep, ok := metadata.ResolveEpisodeLoose(path, showPath)
			if !ok {
				continue
			}
			if metadata.InSeasonSubdir(path, showPath) {
				season = seasonNum
			}
			if err := s.ingestEpisodeAt(ctx, showID, path, season, ep); err != nil {
				return err
			}
			seen[path] = true
			ingested++
		}
	}
	if err := s.ingestShowFilmsAsSeason0(ctx, showID, showPath, paths, seen); err != nil {
		return err
	}
	if ingested > 0 || len(seen) > 0 {
		return nil
	}
	return s.ingestSequentialEpisodes(ctx, showID, showPath, paths)
}

// ingestShowFilmsAsSeason0 stores Movies/ pack videos and root films beside
// Season folders as season 0 specials (not separate library movie cards).
func (s *Scanner) ingestShowFilmsAsSeason0(ctx context.Context, showID int64, showPath string, paths []string, seen map[string]bool) error {
	var films []string
	for _, path := range paths {
		if seen[path] || metadata.IsEpisodeExtra(path) || !metadata.IsVideo(filepath.Base(path)) {
			continue
		}
		if isUnderShowFilm(path, showPath) {
			films = append(films, path)
		}
	}
	if len(films) == 0 {
		return nil
	}
	sort.Strings(films)
	nextEp := 1
	for _, path := range films {
		if err := s.ingestEpisodeAt(ctx, showID, path, 0, nextEp); err != nil {
			return err
		}
		seen[path] = true
		nextEp++
	}
	return nil
}

// isUnderShowFilm reports Movies/Film pack videos and root-level non-episode
// videos that sit beside Season folders (Legend of Crimson–style).
func isUnderShowFilm(path, showPath string) bool {
	showPath = filepath.Clean(showPath)
	parent := filepath.Clean(filepath.Dir(path))
	if metadata.IsMoviesFolderName(filepath.Base(parent)) && filepath.Clean(filepath.Dir(parent)) == showPath {
		return true
	}
	if parent != showPath {
		return false
	}
	if !showHasSeasonChild(showPath) {
		return false
	}
	if metadata.IsOVAFileName(path) {
		return true
	}
	if _, _, ok := metadata.ParseEpisode(filepath.Base(path)); ok {
		return false
	}
	if _, _, ok := metadata.ResolveEpisodeLoose(path, showPath); ok {
		return false
	}
	return true
}

func showHasSeasonChild(showPath string) bool {
	entries, err := os.ReadDir(showPath)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && metadata.IsSeasonFolderName(e.Name()) {
			return true
		}
	}
	return false
}

// ingestSequentialEpisodes numbers non-extra videos as season/episode by sort order
// when no filename patterns matched (typical single-cour anime rips).
func (s *Scanner) ingestSequentialEpisodes(ctx context.Context, showID int64, showPath string, paths []string) error {
	sort.Strings(paths)
	index := metadata.SeasonIndexForShow(showPath)
	nextEp := map[int]int{}
	for _, path := range paths {
		if metadata.IsEpisodeExtra(path) {
			continue
		}
		if !metadata.IsVideo(filepath.Base(path)) {
			continue
		}
		season := metadata.SeasonForPathWithIndex(path, showPath, index)
		ep := nextEp[season] + 1
		nextEp[season] = ep
		if err := s.ingestEpisodeAt(ctx, showID, path, season, ep); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scanner) ingestShow(ctx context.Context, lib *db.Library, showPath string) (int64, error) {
	showTitle := metadata.CleanEpisodeTitle(filepath.Base(showPath))
	if showTitle == "" {
		showTitle = filepath.Base(showPath)
	}
	pathTitle := showTitle
	year := 0
	plot := ""
	rating := 0.0
	if nfo := filepath.Join(showPath, "tvshow.nfo"); fileExists(nfo) {
		if n, err := metadata.ReadTVShowNFO(nfo); err == nil {
			if n.Title != "" && !metadata.MovieNFOMatchesPath(n.Title, pathTitle) {
				// Stray tvshow.nfo (title does not match the folder). Keep the
				// path-derived name, same as movies with a mismatched movie.nfo.
			} else {
				if n.Title != "" {
					showTitle = n.Title
				}
				year = n.Year
				plot = n.Plot
				rating = metadata.NumericRating(n.Rating, n.Ratings)
			}
		}
	}
	cacheShow := filepath.Join(s.StorePath, "metadata", "tv", sanitize(showTitle))
	_ = os.MkdirAll(cacheShow, 0o755)
	nfoRel := filepath.Join("metadata", "tv", sanitize(showTitle), "tvshow.nfo")
	if fileExists(filepath.Join(showPath, "tvshow.nfo")) {
		_ = metadata.CopyFile(filepath.Join(showPath, "tvshow.nfo"), filepath.Join(s.StorePath, nfoRel))
	} else {
		_ = metadata.WriteTVShowNFO(filepath.Join(s.StorePath, nfoRel), metadata.TVShowNFO{Title: showTitle, Year: year, Plot: plot})
	}
	posterRel := ""
	if p := metadata.FindSidecar(showPath, showTitle, "poster.jpg", "folder.jpg"); p != "" {
		posterRel = filepath.Join("metadata", "tv", sanitize(showTitle), "poster.jpg")
		_ = metadata.CopyFile(p, filepath.Join(s.StorePath, posterRel))
	}
	it := db.MediaItem{
		LibraryID:  lib.ID,
		Kind:       "show",
		Title:      showTitle,
		SortTitle:  showTitle,
		Year:       nullInt(year),
		Path:       showPath,
		Plot:       sql.NullString{String: plot, Valid: plot != ""},
		PosterPath: sql.NullString{String: posterRel, Valid: posterRel != ""},
		NFOPath:    sql.NullString{String: nfoRel, Valid: true},
		Rating:     sql.NullFloat64{Float64: rating, Valid: rating > 0},
		Mtime:      0,
	}
	existed, _ := s.DB.MediaItemExistsAtPath(ctx, lib.ID, showPath)
	id, err := s.DB.UpsertMediaItem(ctx, it)
	if err == nil && !existed && s.Webhooks != nil {
		it.ID = id
		s.Webhooks.DispatchItemAdded(ctx, lib.UserID, &it)
	}
	return id, err
}

// resolveTVEpisode maps a video under a TV show dir to season/episode.
// Order: SxxEyy → loose (NxM / episode number) → single-video show as S01E01.
func resolveTVEpisode(showDir, path string) (season, episode int, ok bool) {
	season, episode, ok = metadata.ParseEpisode(filepath.Base(path))
	if ok {
		return season, episode, true
	}
	season, episode, ok = metadata.ResolveEpisodeLoose(path, showDir)
	if ok {
		return season, episode, true
	}
	if countVideosUnder(showDir) == 1 {
		return 1, 1, true
	}
	return 0, 0, false
}

// countVideosUnder returns how many video files exist under root (recursive).
func countVideosUnder(root string) int {
	n := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if metadata.IsVideo(d.Name()) {
			n++
		}
		return nil
	})
	return n
}

func (s *Scanner) ingestEpisodeAt(ctx context.Context, showID int64, path string, season, episode int) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	seasonID, err := s.ensureSeason(ctx, showID, season, filepath.Dir(path))
	if err != nil {
		return err
	}
	title := metadata.CleanEpisodeTitle(filepath.Base(path))
	dir := filepath.Dir(path)
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	epPlot := ""
	runtime := 0
	nfoRel := ""
	nfoSrc := metadata.FindSidecar(dir, base, "episode.nfo", base+".nfo")
	if nfoSrc != "" {
		if n, err := metadata.ReadEpisodeNFO(nfoSrc); err == nil {
			if n.Title != "" {
				title = n.Title
			}
			epPlot = n.Plot
			runtime = n.Runtime
		}
		show, _ := s.DB.GetMediaItem(ctx, showID)
		showKey := "show"
		if show != nil {
			showKey = sanitize(show.Title)
		}
		nfoRel = filepath.Join("metadata", "tv", showKey, fmt.Sprintf("S%02d", season), base+".nfo")
		_ = metadata.CopyFile(nfoSrc, filepath.Join(s.StorePath, nfoRel))
	}
	stillRel := ""
	if p := metadata.FindSidecar(dir, base, "thumb.jpg", "thumb.png", base+"-thumb.jpg", base+".jpg", base+".png"); p != "" {
		show, _ := s.DB.GetMediaItem(ctx, showID)
		showKey := "show"
		if show != nil {
			showKey = sanitize(show.Title)
		}
		ext := filepath.Ext(p)
		if ext == "" {
			ext = ".jpg"
		}
		stillRel = filepath.Join("metadata", "tv", showKey, fmt.Sprintf("S%02d", season), base+"-thumb"+ext)
		_ = metadata.CopyFile(p, filepath.Join(s.StorePath, stillRel))
	}
	existed, _ := s.DB.EpisodeExistsAtPath(ctx, showID, path)
	id, err := s.DB.UpsertEpisode(ctx, db.Episode{
		SeasonID:       seasonID,
		ShowID:         showID,
		EpisodeNumber:  episode,
		Title:          sql.NullString{String: title, Valid: true},
		Path:           path,
		RuntimeSeconds: nullInt(runtime),
		Plot:           sql.NullString{String: epPlot, Valid: epPlot != ""},
		StillPath:      sql.NullString{String: stillRel, Valid: stillRel != ""},
		NFOPath:        sql.NullString{String: nfoRel, Valid: nfoRel != ""},
		Mtime:          info.ModTime().Unix(),
	})
	if err == nil && !existed && s.Webhooks != nil {
		ep := db.Episode{
			ID: id, SeasonID: seasonID, ShowID: showID, EpisodeNumber: episode,
			Title: sql.NullString{String: title, Valid: true}, Path: path,
		}
		show, _ := s.DB.GetMediaItem(ctx, showID)
		ownerID := s.libraryOwnerID(ctx, showID)
		s.Webhooks.DispatchEpisodeAdded(ctx, ownerID, &ep, show)
	}
	return err
}

// ensureSeason upserts season metadata from the episode's directory and show root.
func (s *Scanner) ensureSeason(ctx context.Context, showID int64, seasonNum int, episodeDir string) (int64, error) {
	seasonDir := episodeDir
	title := ""
	plot := ""
	show, _ := s.DB.GetMediaItem(ctx, showID)
	showRoot := ""
	if show != nil {
		showRoot = show.Path
	}
	nfoDirs := []string{seasonDir}
	if showRoot != "" && filepath.Clean(showRoot) != filepath.Clean(seasonDir) {
		nfoDirs = append(nfoDirs, showRoot)
	}
	for _, dir := range nfoDirs {
		if nfo := metadata.FindSeasonNFO(dir, seasonNum); nfo != "" {
			if n, err := metadata.ReadSeasonNFO(nfo); err == nil {
				if n.Title != "" {
					title = n.Title
				}
				if n.Plot != "" {
					plot = n.Plot
				}
				break
			}
		}
	}
	showKey := "show"
	if show != nil {
		showKey = sanitize(show.Title)
	}
	posterRel := ""
	posterDirs := []string{seasonDir}
	if showRoot != "" && filepath.Clean(showRoot) != filepath.Clean(seasonDir) {
		posterDirs = append(posterDirs, showRoot)
	}
	base := fmt.Sprintf("season%02d", seasonNum)
	baseAlt := fmt.Sprintf("season%d", seasonNum)
	for _, dir := range posterDirs {
		p := metadata.FindSidecar(dir, base, "poster.jpg", "folder.jpg", "poster.png", "folder.png")
		if p == "" {
			p = metadata.FindSidecar(dir, baseAlt, "poster.jpg", "folder.jpg", "poster.png", "folder.png")
		}
		// seasonNN-poster.jpg sidecar naming is covered by FindSidecar(base+"-"+name).
		if p == "" {
			for _, name := range []string{
				base + "-poster.jpg", base + "-poster.png",
				baseAlt + "-poster.jpg", baseAlt + "-poster.png",
			} {
				cand := filepath.Join(dir, name)
				if fileExists(cand) {
					p = cand
					break
				}
			}
		}
		if p != "" {
			posterRel = filepath.Join("metadata", "tv", showKey, fmt.Sprintf("S%02d", seasonNum), "poster"+filepath.Ext(p))
			_ = metadata.CopyFile(p, filepath.Join(s.StorePath, posterRel))
			break
		}
	}
	if title == "" {
		title = fmt.Sprintf("Season %d", seasonNum)
	}
	return s.DB.UpsertSeason(ctx, showID, seasonNum, title, posterRel, plot)
}

func sanitize(s string) string {
	s = strings.TrimSpace(s)
	repl := strings.NewReplacer("/", "-", "\\", "-", ":", " -", "*", "", "?", "", "\"", "", "<", "", ">", "", "|", "")
	return repl.Replace(s)
}

func titleYear(title string, year int) string {
	if year > 0 {
		return title + " (" + itoa(year) + ")"
	}
	return title
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func nullInt(y int) sql.NullInt64 {
	if y == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(y), Valid: true}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (s *Scanner) libraryOwnerID(ctx context.Context, showID int64) int64 {
	show, err := s.DB.GetMediaItem(ctx, showID)
	if err != nil || show == nil {
		return 0
	}
	var uid int64
	if err := s.DB.SQL.QueryRowContext(ctx, `SELECT user_id FROM libraries WHERE id = ?`, show.LibraryID).Scan(&uid); err != nil {
		return 0
	}
	return uid
}
