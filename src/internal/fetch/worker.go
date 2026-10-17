package fetch

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
	"github.com/alyshmahell/servemedia/src/internal/metadata"
)

type Worker struct {
	DB        *db.DB
	Store     string
	MediaRoot string
	Meta      matchmedia.Service
	Webhooks  WebhookNotifier
}

type Opts struct {
	Persist      bool
	Overwrite    bool
	ScanJobID    int64
	QueryTitle   string
	ManualSelect bool
	ScanMode     string
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
	if err := w.matchAndFetch(ctx, lib, lib.Path, nil, opts); err != nil {
		return err
	}
	return w.stillsWithPrune(ctx, lib, opts)
}

func (w *Worker) libraryOwnerID(ctx context.Context, libraryID int64) int64 {
	if w == nil || w.DB == nil {
		return 0
	}
	var uid int64
	if err := w.DB.SQL.QueryRowContext(ctx, `SELECT user_id FROM libraries WHERE id = ?`, libraryID).Scan(&uid); err != nil {
		return 0
	}
	return uid
}

func (w *Worker) MatchItem(ctx context.Context, lib *db.Library, item *db.MediaItem, opts Opts) error {
	if item == nil || lib == nil {
		return fmt.Errorf("missing media item")
	}
	opts.ManualSelect = true
	if title := strings.TrimSpace(opts.QueryTitle); title != "" {
		return w.matchIngest(ctx, lib, item, title, opts)
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

func (w *Worker) matchIngest(ctx context.Context, lib *db.Library, item *db.MediaItem, title string, opts Opts) error {
	if w.Meta == nil {
		return fmt.Errorf("metadata service unavailable")
	}
	st, err := w.Meta.Status()
	if err != nil || !st.Ready {
		return fmt.Errorf("metadata service unavailable")
	}
	w.progress(ctx, opts.ScanJobID, 5, "Matching titles…")
	row := matchmedia.IngestRow{Title: title}
	if year := ingestYear(item, title); year != "" {
		row.Year = year
	}
	jobs, session, err := w.Meta.RunIngest(ctx, []matchmedia.IngestRow{row}, func(done, total int, name string) {
		w.reportRunProgress(ctx, opts.ScanJobID, done, total, name)
	})
	if err != nil {
		return err
	}
	if err := w.applyJobs(ctx, lib, jobs, item, opts, session); err != nil {
		return err
	}
	return w.extractMissingStills(ctx, lib, item, opts)
}

func ingestYear(item *db.MediaItem, title string) string {
	if _, y := metadata.ParseTitleYear(title); y > 0 {
		return strconv.Itoa(y)
	}
	if item != nil && item.Year.Valid && item.Year.Int64 > 0 {
		return strconv.FormatInt(item.Year.Int64, 10)
	}
	return ""
}

func (w *Worker) MatchPath(ctx context.Context, lib *db.Library, scanPath string, only *db.MediaItem, opts Opts) error {
	if err := w.matchAndFetch(ctx, lib, scanPath, only, opts); err != nil {
		return err
	}
	return w.extractMissingStills(ctx, lib, only, opts)
}

func (w *Worker) matchAndFetch(ctx context.Context, lib *db.Library, scanPath string, only *db.MediaItem, opts Opts) error {
	if w.Meta == nil {
		return fmt.Errorf("metadata service unavailable")
	}
	st, err := w.Meta.Status()
	if err != nil || !st.Ready {
		return fmt.Errorf("metadata service unavailable")
	}
	w.progress(ctx, opts.ScanJobID, 0, "scan 0/0")
	if pipe, ok := w.Meta.(scanMatcher); ok {
		return w.matchPipeline(ctx, lib, scanPath, only, opts, pipe)
	}
	jobs, session, err := w.Meta.Run(ctx, scanPath, opts.ScanMode, func(done, total int, name string) {
		w.reportRunProgress(ctx, opts.ScanJobID, done, total, name)
	})
	if err != nil {
		return err
	}
	return w.applyJobs(ctx, lib, jobs, only, opts, session)
}

type scanMatcher interface {
	Scan(ctx context.Context, path, mode string, report func(done, total int, name string)) ([]matchmedia.Job, error)
	Match(ctx context.Context, jobs []matchmedia.Job, report func(done, total int, name string), onJob func(matchmedia.Job)) ([]matchmedia.Job, error)
}

func (w *Worker) matchPipeline(ctx context.Context, lib *db.Library, scanPath string, only *db.MediaItem, opts Opts, pipe scanMatcher) error {
	opts = rescanSaves(opts)
	jobs, err := pipe.Scan(ctx, scanPath, opts.ScanMode, func(done, total int, name string) {
		w.reportRunProgress(ctx, opts.ScanJobID, done, total, name)
	})
	if err != nil {
		return err
	}
	for i := range jobs {
		if jobs[i].ID == "" {
			jobs[i].ID = matchmedia.StableID(jobs[i].Path)
		}
		if err := w.recordTreeStructure(ctx, lib, jobs[i], opts); err != nil {
			return err
		}
	}
	live := &liveProgress{jobID: opts.ScanJobID}
	q := newJobQueue()
	errCh := make(chan error, 1)
	go func() {
		errCh <- w.fetchQueued(ctx, lib, only, opts, "", live, q)
	}()
	var qmu sync.Mutex
	queued := map[string]string{}
	enqueue := func(j matchmedia.Job, requireFiles bool) {
		if j.Status != "matched" || j.Match == nil {
			return
		}
		if j.ID == "" {
			j.ID = matchmedia.StableID(j.Path)
		}
		if requireFiles && len(j.Files) == 0 {
			return
		}
		sig := jobFilesKey(j)
		qmu.Lock()
		if prev, ok := queued[j.ID]; ok && prev == sig {
			qmu.Unlock()
			return
		}
		queued[j.ID] = sig
		qmu.Unlock()
		q.push(j)
	}
	for _, j := range jobs {
		enqueue(j, true)
	}
	final := jobs
	if jobsNeedMatch(jobs) {
		final, err = pipe.Match(ctx, jobs, func(done, total int, name string) {
			live.setMatch(name, done, total)
			w.flushLive(ctx, live)
		}, func(j matchmedia.Job) {
			enqueue(j, true)
		})
		if err != nil {
			q.close()
			<-errCh
			return err
		}
	}
	for _, j := range final {
		if j.Status != "matched" || j.Match == nil {
			continue
		}
		if j.ID == "" {
			j.ID = matchmedia.StableID(j.Path)
		}
		enqueue(j, false)
	}
	qmu.Lock()
	fetchN := len(queued)
	qmu.Unlock()
	live.setFetchTotal(fetchN)
	w.flushLive(ctx, live)
	q.close()
	if err := <-errCh; err != nil {
		return err
	}
	for _, j := range final {
		if j.Status == "matched" && j.Match != nil {
			continue
		}
		if j.Status == "pending" || j.Status == "" {
			continue
		}
		if err := w.recordTree(ctx, lib, j, opts); err != nil {
			return err
		}
		if _, err := w.applyFinishedJob(ctx, lib, j, only, opts, ""); err != nil {
			return err
		}
	}
	return nil
}

func jobsNeedMatch(jobs []matchmedia.Job) bool {
	for _, j := range jobs {
		if j.Status == "pending" || j.Status == "error" {
			return true
		}
	}
	return false
}

type jobQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []matchmedia.Job
	closed bool
}

func newJobQueue() *jobQueue {
	q := &jobQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *jobQueue) push(j matchmedia.Job) {
	q.mu.Lock()
	q.items = append(q.items, j)
	q.mu.Unlock()
	q.cond.Signal()
}

func (q *jobQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.cond.Signal()
}

func (q *jobQueue) pop() (matchmedia.Job, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.items) == 0 {
		return matchmedia.Job{}, false
	}
	j := q.items[0]
	q.items = q.items[1:]
	return j, true
}

type liveProgress struct {
	mu          sync.Mutex
	jobID       int64
	matchLine   string
	fetchLine   string
	matchDone   int
	matchTotal  int
	matching    bool
	fetchDone   int
	fetchTotal  int
	fetchNFO    int
	fetchEp     int
	fetchPoster int
	fetchTitle  string
}

func (p *liveProgress) setMatch(line string, done, total int) {
	p.mu.Lock()
	p.matchLine = line
	p.matchDone = done
	p.matchTotal = total
	p.matching = true
	p.mu.Unlock()
}

func (p *liveProgress) setFetchTotal(n int) {
	p.mu.Lock()
	p.fetchTotal = n
	p.matching = false
	if p.fetchDone > 0 {
		p.fetchLine = fetchLine(p.fetchDone, p.fetchTotal, p.fetchNFO, p.fetchEp, p.fetchPoster, p.fetchTitle)
	}
	p.mu.Unlock()
}

func (p *liveProgress) noteFetch(done, nfo, episode, poster int, title string) {
	p.mu.Lock()
	p.fetchDone = done
	p.fetchNFO = nfo
	p.fetchEp = episode
	p.fetchPoster = poster
	p.fetchTitle = title
	p.fetchLine = fetchLine(done, p.fetchTotal, nfo, episode, poster, title)
	p.mu.Unlock()
}

func (p *liveProgress) snapshot() (string, int) {
	var lines []string
	if p.matchLine != "" {
		lines = append(lines, p.matchLine)
	}
	if p.fetchLine != "" {
		lines = append(lines, p.fetchLine)
	}
	msg := strings.Join(lines, "\n")
	pct := 10
	if p.matching && p.matchTotal > 0 {
		pct = 10 + p.matchDone*70/p.matchTotal
		if pct > 80 {
			pct = 80
		}
	} else if p.fetchTotal > 0 {
		pct = 80 + p.fetchDone*19/p.fetchTotal
		if pct > 99 {
			pct = 99
		}
	} else if p.fetchDone > 0 {
		pct = 80
	}
	return msg, pct
}

func fetchLine(done, total, _, episode, poster int, title string) string {
	head := fmt.Sprintf("fetch %d", done)
	if total > 0 {
		head = fmt.Sprintf("fetch %d/%d", done, total)
	}
	return fmt.Sprintf("%s nfo:%d poster:%d %s", head, episode, poster, title)
}

func (w *Worker) flushLive(ctx context.Context, live *liveProgress) {
	if live == nil || live.jobID == 0 {
		return
	}
	live.mu.Lock()
	defer live.mu.Unlock()
	msg, pct := live.snapshot()
	if msg == "" {
		return
	}
	w.progress(ctx, live.jobID, pct, msg)
}

func jobFilesKey(j matchmedia.Job) string {
	var b strings.Builder
	for _, f := range j.Files {
		b.WriteString(f.Path)
		b.WriteByte(0)
		b.WriteString(f.Season)
		b.WriteByte(0)
		b.WriteString(f.Episode)
		b.WriteByte('\n')
	}
	return b.String()
}

func (w *Worker) fetchQueued(ctx context.Context, lib *db.Library, only *db.MediaItem, opts Opts, session string, live *liveProgress, q *jobQueue) error {
	nfo, episode, poster, done := 0, 0, 0, 0
	seen := map[string]saveHit{}
	for {
		j, ok := q.pop()
		if !ok {
			return nil
		}
		if err := w.recordTree(ctx, lib, j, opts); err != nil {
			return err
		}
		hit, err := w.applyFinishedJob(ctx, lib, j, only, opts, session)
		if err != nil {
			return err
		}
		id := j.ID
		if id == "" {
			id = matchmedia.StableID(j.Path)
		}
		if prev, ok := seen[id]; ok {
			nfo += hit.nfo - prev.nfo
			episode += hit.episode - prev.episode
			poster += hit.poster - prev.poster
			if nfo < 0 {
				nfo = 0
			}
			if episode < 0 {
				episode = 0
			}
			if poster < 0 {
				poster = 0
			}
		} else {
			nfo += hit.nfo
			episode += hit.episode
			poster += hit.poster
			done++
		}
		seen[id] = hit
		title := strings.TrimSpace(j.Title)
		if j.Match != nil && strings.TrimSpace(j.Match.Title) != "" {
			title = strings.TrimSpace(j.Match.Title)
		}
		live.noteFetch(done, nfo, episode, poster, title)
		w.flushLive(ctx, live)
	}
}

func (w *Worker) applyJobs(ctx context.Context, lib *db.Library, jobs []matchmedia.Job, only *db.MediaItem, opts Opts, session string) error {
	opts = rescanSaves(opts)
	var prepared []matchmedia.Job
	for _, j := range jobs {
		if j.ID == "" {
			j.ID = matchmedia.StableID(j.Path)
		}
		if j.Status == "pending" || j.Status == "" {
			continue
		}
		if err := w.recordTree(ctx, lib, j, opts); err != nil {
			return err
		}
		prepared = append(prepared, j)
	}
	var matched []matchmedia.Job
	for _, j := range prepared {
		if j.Status == "matched" && j.Match != nil {
			matched = append(matched, j)
			continue
		}
		if _, err := w.applyFinishedJob(ctx, lib, j, only, opts, session); err != nil {
			return err
		}
	}
	total := len(matched)
	episode, poster := 0, 0
	for i, j := range matched {
		hit, err := w.applyFinishedJob(ctx, lib, j, only, opts, session)
		if err != nil {
			return err
		}
		episode += hit.episode
		poster += hit.poster
		if opts.ScanJobID != 0 && total > 0 {
			title := strings.TrimSpace(j.Title)
			if j.Match != nil && strings.TrimSpace(j.Match.Title) != "" {
				title = strings.TrimSpace(j.Match.Title)
			}
			w.reportRunProgress(ctx, opts.ScanJobID, i+1, total, fmt.Sprintf("fetch %d/%d nfo:%d poster:%d %s", i+1, total, episode, poster, title))
		}
	}
	return nil
}

func rescanSaves(opts Opts) Opts {
	switch strings.TrimSpace(opts.ScanMode) {
	case "rescan", "matchmedia":
		opts.Overwrite = true
	}
	return opts
}

func (w *Worker) reportRunProgress(ctx context.Context, jobID int64, done, total int, name string) {
	if jobID == 0 {
		return
	}
	pct := 10
	if total > 0 && done > 0 {
		pct = 10 + done*70/total
		if pct > 90 {
			pct = 90
		}
	}
	msg := strings.TrimSpace(name)
	if msg == "" {
		msg = "Matching titles…"
	}
	switch {
	case strings.HasPrefix(msg, "scan ") && total > 0:
		pct = done * 10 / total
	case strings.HasPrefix(msg, "match ") && total > 0:
		pct = 10 + done*70/total
		if pct > 80 {
			pct = 80
		}
	case (strings.HasPrefix(msg, "fetch ") || strings.HasPrefix(msg, "save ")) && total > 0:
		pct = 80 + done*19/total
		if pct > 99 {
			pct = 99
		}
	}
	w.progress(ctx, jobID, pct, msg)
}

type saveHit struct {
	nfo, episode, poster int
}

func (w *Worker) applyFinishedJob(ctx context.Context, lib *db.Library, j matchmedia.Job, only *db.MediaItem, opts Opts, session string) (saveHit, error) {
	if only != nil && !jobTouchesItem(j, only) {
		return saveHit{}, nil
	}
	it, err := w.upsertFromJob(ctx, lib.ID, j, opts.Overwrite, only == nil, opts.ScanMode)
	if err != nil {
		return saveHit{}, err
	}
	if it == nil && only != nil {
		it = only
	}
	if it == nil {
		log.Printf("matchmedia job %s path %s: no library item", j.ID, j.Path)
		return saveHit{}, nil
	}
	w.linkItemNode(ctx, lib, it, j)
	if only != nil && it.ID != only.ID && !jobTouchesItem(j, only) {
		return saveHit{}, nil
	}
	if only == nil && it.MatchSkipped() {
		return saveHit{}, nil
	}
	if changesSkipApply(opts, it, j) {
		return saveHit{}, nil
	}
	if extrasShapedJob(j) {
		return saveHit{}, nil
	}
	if err := w.applyJob(ctx, it, j, opts, session); err != nil {
		log.Printf("apply %s: %v", j.ID, err)
	}
	w.stampNodePoster(ctx, lib, it, j)
	w.stampSavedTree(ctx, lib, it)
	hit := saveHit{}
	if j.Status == "matched" && j.Match != nil {
		hit.nfo = 1
		hit.episode = episodeRecords(j)
		if it.PosterPath.Valid && strings.TrimSpace(it.PosterPath.String) != "" {
			hit.poster = 1
		}
	}
	return hit, nil
}

func episodeRecords(j matchmedia.Job) int {
	if j.Kind != "show" && j.Kind != "tv" {
		return 0
	}
	n := 0
	for _, s := range j.Catalog {
		for _, e := range s.Episodes {
			paths := 0
			if strings.TrimSpace(e.Path) != "" {
				paths++
			}
			for _, p := range e.Paths {
				p = strings.TrimSpace(p)
				if p != "" && p != e.Path {
					paths++
				}
			}
			n += paths
		}
	}
	if n == 0 {
		for _, f := range j.Files {
			if strings.TrimSpace(f.Season) != "" && strings.TrimSpace(f.Episode) != "" {
				n++
			}
		}
	}
	return n
}

func (w *Worker) ApplySelect(ctx context.Context, item *db.MediaItem, provider, id string, persist bool) error {
	if item == nil || !item.MatchMediaJobID.Valid || item.MatchMediaJobID.String == "" {
		return fmt.Errorf("no matchmedia job for this title")
	}
	session := ""
	if item.MatchMediaSessionID.Valid {
		session = strings.TrimSpace(item.MatchMediaSessionID.String)
	}
	var (
		j   matchmedia.Job
		err error
	)
	if sel, ok := w.Meta.(interface {
		SelectStored(context.Context, matchmedia.Job, string, string) (matchmedia.Job, error)
	}); ok {
		hint := DecodePickerSnapshot(item.MatchCandidates.String)
		hint.ID = item.MatchMediaJobID.String
		if hint.Path == "" {
			hint.Path = item.Path
		}
		if hint.Title == "" {
			hint.Title = item.Title
		}
		if hint.Kind == "" {
			hint.Kind = item.Kind
		}
		j, err = sel.SelectStored(ctx, hint, provider, id)
	} else {
		if session == "" {
			return fmt.Errorf("no MatchMedia job for this title — rescan to match again")
		}
		j, err = w.Meta.Select(session, item.MatchMediaJobID.String, provider, id)
	}
	if err != nil {
		return err
	}
	it, err := w.upsertFromJob(ctx, item.LibraryID, j, true, false, "")
	if err != nil {
		return err
	}
	if it == nil {
		it = item
	}
	if err := w.applyJob(ctx, it, j, Opts{Persist: persist, Overwrite: true}, session); err != nil {
		return err
	}
	if fresh, err := w.DB.GetMediaItem(ctx, it.ID); err == nil && fresh != nil {
		it = fresh
	}
	return w.stampSelectNode(ctx, it, j)
}

func (w *Worker) upsertFromJob(ctx context.Context, libraryID int64, j matchmedia.Job, overwrite, keepSkipped bool, scanMode string) (*db.MediaItem, error) {
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
	title := jobTitle(j, itemPath)
	parent, err := w.findParentShow(ctx, libraryID, j, kind, itemPath)
	if err != nil {
		return nil, err
	}
	mtime := fileMtime(itemPath)
	existing, err := w.DB.GetMediaItemByPath(ctx, libraryID, itemPath)
	if err != nil {
		return nil, err
	}
	created := existing == nil
	if existing == nil {
		if err := w.ReattachMovedPath(ctx, &db.Library{ID: libraryID}, kind, itemPath); err != nil {
			return nil, err
		}
		existing, err = w.DB.GetMediaItemByPath(ctx, libraryID, itemPath)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			created = false
		}
	}
	if keepSkipped && existing != nil && existing.MatchSkipped() {
		_ = w.DB.TouchMediaItemMtime(ctx, existing.ID, mtime)
		return w.DB.GetMediaItem(ctx, existing.ID)
	}
	if parent != nil && extrasShapedJob(j) {
		return w.attachExtrasToShow(ctx, libraryID, parent, j, files, itemPath)
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
	if created && w.Webhooks != nil {
		w.Webhooks.DispatchItemAdded(ctx, w.libraryOwnerID(ctx, libraryID), it)
	}
	known := changesKnownMatched(scanMode, overwrite, existing, j.Status)
	if kind == "show" {
		if err := ingestShowFiles(ctx, w, it.ID, files); err != nil {
			return nil, err
		}
	}
	if !known {
		if err := w.nestMediaItem(ctx, libraryID, it, parent); err != nil {
			return nil, err
		}
	}
	return w.DB.GetMediaItem(ctx, it.ID)
}

func changesKnownMatched(scanMode string, overwrite bool, existing *db.MediaItem, jobStatus string) bool {
	if scanMode != "changes" || overwrite || existing == nil {
		return false
	}
	if !existing.MetaID.Valid || strings.TrimSpace(existing.MetaID.String) == "" {
		return false
	}
	return jobStatus == "matched"
}

func changesSkipApply(opts Opts, it *db.MediaItem, j matchmedia.Job) bool {
	return changesKnownMatched(opts.ScanMode, opts.Overwrite, it, j.Status)
}

func jobTitle(j matchmedia.Job, itemPath string) string {
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
	return title
}

func extrasShapedJob(j matchmedia.Job) bool {
	return strings.EqualFold(strings.TrimSpace(j.Role), "extras")
}

func (w *Worker) findParentShow(ctx context.Context, libraryID int64, j matchmedia.Job, kind, itemPath string) (*db.MediaItem, error) {
	if extrasShapedJob(j) && j.Match != nil {
		host, err := w.DB.GetMediaItemByMeta(ctx, libraryID, j.Match.Provider, j.Match.ID)
		if err != nil {
			return nil, err
		}
		if host != nil && host.Kind == "show" && filepath.Clean(host.Path) != filepath.Clean(itemPath) {
			return host, nil
		}
	}
	if p, err := w.findAncestorShow(ctx, libraryID, itemPath); err != nil || p != nil {
		return p, err
	}
	jobRoot := strings.TrimSpace(j.Path)
	if jobRoot == "" {
		jobRoot = itemPath
	}
	if p, err := w.findAncestorShow(ctx, libraryID, jobRoot); err != nil || p != nil {
		return p, err
	}
	return w.findTitleSimilarShow(ctx, libraryID, j, itemPath)
}

func (w *Worker) findAncestorShow(ctx context.Context, libraryID int64, path string) (*db.MediaItem, error) {
	path = filepath.Clean(path)
	shows, err := w.DB.ListShows(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	var best *db.MediaItem
	bestLen := 0
	for i := range shows {
		show := &shows[i]
		root := filepath.Clean(show.Path)
		if root == path || !pathUnder(path, root) {
			continue
		}
		if best == nil || len(root) > bestLen {
			cp := *show
			best = &cp
			bestLen = len(root)
		}
	}
	return best, nil
}

func pathUnder(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	sep := string(os.PathSeparator)
	return path == root || strings.HasPrefix(path, root+sep)
}

func (w *Worker) findTitleSimilarShow(ctx context.Context, libraryID int64, j matchmedia.Job, itemPath string) (*db.MediaItem, error) {
	shows, err := w.DB.ListShows(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	jobPath := strings.TrimSpace(j.Path)
	if jobPath == "" {
		jobPath = itemPath
	}
	jobTok := metadata.NestTokens(jobTitle(j, itemPath))
	bestScore := 0
	var best *db.MediaItem
	tie := false
	for i := range shows {
		show := &shows[i]
		if filepath.Clean(show.Path) == filepath.Clean(itemPath) {
			continue
		}
		if sameCatalog(show, j) {
			continue
		}
		if pathUnder(show.Path, jobPath) {
			continue
		}
		showParent := filepath.Dir(filepath.Clean(show.Path))
		sameParent := filepath.Dir(filepath.Clean(itemPath)) == showParent
		if jobPath != "" {
			sameParent = sameParent || filepath.Dir(filepath.Clean(jobPath)) == showParent
		}
		if !pathUnder(itemPath, show.Path) && !pathUnder(jobPath, show.Path) && !sameParent {
			continue
		}
		showTok := metadata.NestTokens(show.Title)
		if !tokensPrefix(showTok, jobTok) {
			continue
		}
		score := tokenOverlap(showTok, jobTok) + len(showTok)*10
		if score > bestScore {
			cp := *show
			best = &cp
			bestScore = score
			tie = false
		} else if score == bestScore && score > 0 {
			tie = true
		}
	}
	if tie || best == nil || bestScore <= 0 {
		return nil, nil
	}
	return best, nil
}

func sameCatalog(show *db.MediaItem, j matchmedia.Job) bool {
	if show == nil || j.Match == nil {
		return false
	}
	id := strings.TrimSpace(j.Match.ID)
	if id == "" || !show.MetaID.Valid || strings.TrimSpace(show.MetaID.String) != id {
		return false
	}
	prov := strings.TrimSpace(j.Match.Provider)
	if prov == "" || !show.MetaProvider.Valid {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(show.MetaProvider.String), prov)
}

func tokensPrefix(small, big []string) bool {
	if len(small) == 0 || len(small) >= len(big) {
		return false
	}
	for i, t := range small {
		if big[i] != t {
			return false
		}
	}
	return true
}

func tokenOverlap(a, b []string) int {
	have := map[string]bool{}
	for _, t := range a {
		have[t] = true
	}
	n := 0
	for _, t := range b {
		if have[t] {
			n++
		}
	}
	return n
}

func (w *Worker) nestMediaItem(ctx context.Context, libraryID int64, it *db.MediaItem, parent *db.MediaItem) error {
	if it == nil {
		return nil
	}
	if parent != nil && parent.ID != it.ID && !pathUnder(parent.Path, it.Path) {
		if err := w.DB.SetMediaItemParent(ctx, it.ID, parent.ID); err != nil {
			return err
		}
		it.ParentID = sql.NullInt64{Int64: parent.ID, Valid: true}
		if err := w.DB.DeleteEpisodesUnderPath(ctx, parent.ID, it.Path); err != nil {
			return err
		}
	} else if parent == nil && it.ParentID.Valid {
		cur, err := w.DB.GetMediaItem(ctx, it.ParentID.Int64)
		if err != nil {
			return err
		}
		if cur == nil || !pathUnder(it.Path, cur.Path) {
			if err := w.DB.SetMediaItemParent(ctx, it.ID, 0); err != nil {
				return err
			}
			it.ParentID = sql.NullInt64{}
		}
	}
	if it.Kind != "show" {
		return nil
	}
	items, err := w.DB.ListAllMediaItems(ctx, libraryID)
	if err != nil {
		return err
	}
	root := filepath.Clean(it.Path)
	for _, other := range items {
		if other.ID == it.ID {
			continue
		}
		if !pathUnder(other.Path, root) || filepath.Clean(other.Path) == root {
			continue
		}
		if err := w.DB.SetMediaItemParent(ctx, other.ID, it.ID); err != nil {
			return err
		}
		if err := w.DB.DeleteEpisodesUnderPath(ctx, it.ID, other.Path); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) attachExtrasToShow(ctx context.Context, libraryID int64, host *db.MediaItem, j matchmedia.Job, files []matchmedia.JobFile, itemPath string) (*db.MediaItem, error) {
	existing, err := w.DB.GetMediaItemByPath(ctx, libraryID, itemPath)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.ID != host.ID {
		if err := w.DB.DeleteMediaItem(ctx, existing.ID); err != nil {
			return nil, err
		}
	}
	if jp := strings.TrimSpace(j.Path); jp != "" && filepath.Clean(jp) != filepath.Clean(itemPath) {
		if extra, err := w.DB.GetMediaItemByPath(ctx, libraryID, jp); err == nil && extra != nil && extra.ID != host.ID {
			_ = w.DB.DeleteMediaItem(ctx, extra.ID)
		}
	}
	if err := w.dropUnnumberedShowEpisodes(ctx, host.ID, files, nil); err != nil {
		return nil, err
	}
	return w.DB.GetMediaItem(ctx, host.ID)
}

func ingestShowFiles(ctx context.Context, w *Worker, showID int64, files []matchmedia.JobFile) error {
	numbered, _ := splitJobFileNumbers(files)
	if len(numbered) > 0 {
		if err := w.IngestNumberedEpisodes(ctx, showID, numbered); err != nil {
			return err
		}
	}
	return w.dropUnnumberedShowEpisodes(ctx, showID, files, numbered)
}

func (w *Worker) dropUnnumberedShowEpisodes(ctx context.Context, showID int64, files []matchmedia.JobFile, numbered []NumberedEpisode) error {
	if w == nil || w.DB == nil || showID <= 0 {
		return nil
	}
	if err := w.DB.DeleteSeasonZeroEpisodes(ctx, showID); err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, n := range numbered {
		if p := filepath.Clean(strings.TrimSpace(n.Path)); p != "" {
			keep[p] = true
		}
	}
	job := map[string]bool{}
	for _, f := range files {
		if p := filepath.Clean(strings.TrimSpace(f.Path)); p != "" {
			job[p] = true
		}
	}
	eps, err := w.DB.ListEpisodesByShow(ctx, showID)
	if err != nil {
		return err
	}
	for _, ep := range eps {
		p := filepath.Clean(ep.Path)
		if job[p] && !keep[p] {
			if err := w.DB.DeleteEpisode(ctx, ep.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func splitJobFileNumbers(files []matchmedia.JobFile) (numbered []NumberedEpisode, loose []string) {
	for _, f := range files {
		path := strings.TrimSpace(f.Path)
		if path == "" {
			continue
		}
		if season, episode, ok := jobFileNumbers(f); ok {
			numbered = append(numbered, NumberedEpisode{Path: path, Season: season, Episode: episode})
			continue
		}
		loose = append(loose, path)
	}
	return numbered, loose
}

func jobFileNumbers(f matchmedia.JobFile) (season, episode int, ok bool) {
	s, sok := parseJobInt(strings.TrimSpace(f.Season))
	e, eok := parseJobInt(strings.TrimSpace(f.Episode))
	if !sok || !eok || s <= 0 || e <= 0 {
		return 0, 0, false
	}
	return s, e, true
}

func parseJobInt(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

func expandJobFiles(j matchmedia.Job) []matchmedia.JobFile {
	var videos []matchmedia.JobFile
	for _, f := range j.Files {
		if strings.TrimSpace(f.Path) == "" || !isVideoPath(f.Path) {
			continue
		}
		videos = append(videos, f)
	}
	return videos
}

func kindFromJob(j matchmedia.Job, files []matchmedia.JobFile) string {
	k := strings.ToLower(strings.TrimSpace(j.Kind))
	if k == "movie" || k == "show" {
		return k
	}
	for _, f := range files {
		if strings.TrimSpace(f.Season) != "" || strings.TrimSpace(f.Episode) != "" {
			return "show"
		}
	}
	return "movie"
}

func jobTouchesItem(j matchmedia.Job, item *db.MediaItem) bool {
	if item == nil {
		return true
	}
	if strings.TrimSpace(j.Path) == "" {
		hasFile := false
		for _, f := range j.Files {
			if strings.TrimSpace(f.Path) != "" {
				hasFile = true
				break
			}
		}
		if !hasFile {
			return true
		}
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

func (w *Worker) applyJob(ctx context.Context, it *db.MediaItem, j matchmedia.Job, opts Opts, session string) error {
	hasMeta := it.MetaID.Valid && strings.TrimSpace(it.MetaID.String) != ""
	switch j.Status {
	case "manual", "multiple":
		if opts.ManualSelect || !(hasMeta && !opts.Overwrite) {
			return w.setJobMatch(ctx, it, session, j.ID, "manual", "", j)
		}
		return nil
	case "unmatched":
		if opts.ManualSelect || !(hasMeta && !opts.Overwrite) {
			if JobHasPickerCandidates(j) {
				return w.setJobMatch(ctx, it, session, j.ID, "manual", "", j)
			}
			return w.setJobMatch(ctx, it, session, j.ID, "unmatched", "", j)
		}
		return nil
	case "error":
		if hasMeta && !opts.Overwrite && !opts.ManualSelect {
			return nil
		}
		if JobHasPickerCandidates(j) {
			return w.setJobMatch(ctx, it, session, j.ID, "manual", "", j)
		}
		msg := strings.TrimSpace(j.Error)
		if msg == "" {
			msg = "Match failed"
		}
		return w.setJobMatch(ctx, it, session, j.ID, "error", msg, j)
	case "matched":
		if opts.ManualSelect {
			return w.setJobMatch(ctx, it, session, j.ID, "manual", "", j)
		}
		if hasMeta && !opts.Overwrite {
			return w.fillMissingArt(ctx, it, opts.Persist, session)
		}
		return w.applyMatched(ctx, it, j, opts.Persist, session)
	default:
		return nil
	}
}

func (w *Worker) setJobMatch(ctx context.Context, it *db.MediaItem, session, jobID, status, matchErr string, j matchmedia.Job) error {
	if err := w.DB.SetMatchMediaMatch(ctx, it.ID, session, jobID, status, matchErr); err != nil {
		return err
	}
	raw := ""
	if status == "manual" || JobHasPickerCandidates(j) {
		raw = EncodePickerSnapshot(j)
	}
	return w.DB.SetMatchCandidates(ctx, it.ID, raw)
}

func (w *Worker) applyMatched(ctx context.Context, it *db.MediaItem, j matchmedia.Job, persist bool, session string) error {
	cand := j.Match
	if cand == nil {
		return fmt.Errorf("unmatched")
	}
	cat := matchmedia.Catalog{
		Provider: cand.Provider, ID: cand.ID, Title: cand.Title, Year: cand.Year,
		Synopsis: cand.Synopsis, Poster: cand.Poster, Seasons: j.Catalog,
	}
	needFetch := len(j.Catalog) == 0 && strings.TrimSpace(cand.Synopsis) == "" && strings.TrimSpace(cand.Poster) == ""
	if needFetch && w.Meta != nil {
		if fetched, err := w.Meta.Catalog(session, cand.Provider, cand.ID); err == nil {
			cat = fetched
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
	return w.DB.SetMatchMediaMatch(ctx, it.ID, session, j.ID, "matched", "")
}

func (w *Worker) applyMovie(ctx context.Context, it *db.MediaItem, cat matchmedia.Catalog, persist bool, session string) error {
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
	shareShow := metadata.DirHasTVShowNFO(it.Path)
	if persist {
		if bareOK {
			_ = metadata.CopyFile(storeNFO, filepath.Join(mediaDir, "movie.nfo"))
		} else {
			if !shareShow {
				metadata.QuarantineServeMediaRejected(filepath.Join(mediaDir, "movie.nfo"))
				metadata.QuarantineServeMediaRejected(filepath.Join(mediaDir, "poster.jpg"))
			}
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
	if posterRel == "" && !shareShow {
		if p := metadata.FindSidecar(mediaDir, base, "poster.jpg", "poster.png", "folder.jpg", "folder.png"); p != "" {
			dst := filepath.Join(cacheDir, "poster"+filepath.Ext(p))
			if err := metadata.CopyFile(p, dst); err == nil {
				posterRel = filepath.Join("metadata", "movies", cacheKey, filepath.Base(dst))
			}
		}
	} else if posterRel == "" {
		for _, ext := range []string{".jpg", ".png", ".webp"} {
			cand := filepath.Join(mediaDir, base+"-poster"+ext)
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				dst := filepath.Join(cacheDir, "poster"+ext)
				if err := metadata.CopyFile(cand, dst); err == nil {
					posterRel = filepath.Join("metadata", "movies", cacheKey, filepath.Base(dst))
				}
				break
			}
		}
	}
	return w.DB.UpdateMediaItemMeta(ctx, it.ID, nfo.Title, nfo.Year, nfo.Plot, posterRel, "", nfoRel, 0, cat.Provider, cat.ID)
}

func (w *Worker) applyShow(ctx context.Context, it *db.MediaItem, cat matchmedia.Catalog, persist bool, session string) error {
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
	p := w.writeImage(cacheDir, "poster", strings.TrimSpace(cat.Poster), session)
	if p == "" {
		p = w.writeImage(cacheDir, "poster", catalogSeasonPosterURL(cat), session)
	}
	if p != "" {
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
		catalogSeasonTitle := ""
		plot := ""
		sposter := ""
		if cs != nil {
			catalogSeasonTitle = cs.Title
			plot = cs.Synopsis
			sposter = cs.Poster
		}
		stitle := seasonDisplayTitle(season.SeasonNumber, catalogSeasonTitle, title)
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
			ce := resolveCatalogEpisode(cat, season.SeasonNumber, ep.EpisodeNumber, ep.Path)
			w.applyEpisode(ctx, it, &season, &ep, ce, persist, session)
		}
	}
	if posterRel == "" {
		seasons, _ = w.DB.ListSeasons(ctx, it.ID)
		if srcRel := firstSeasonPosterRel(seasons); srcRel != "" {
			src := filepath.Join(w.Store, srcRel)
			ext := filepath.Ext(src)
			if ext == "" {
				ext = ".jpg"
			}
			dst := filepath.Join(cacheDir, "poster"+ext)
			if err := metadata.CopyFile(src, dst); err == nil {
				posterRel = filepath.Join("metadata", "tv", cacheKey, filepath.Base(dst))
				if persist {
					_ = metadata.CopyFile(dst, filepath.Join(it.Path, filepath.Base(dst)))
				}
				_ = w.DB.UpdateMediaItemMeta(ctx, it.ID, nfo.Title, nfo.Year, nfo.Plot, posterRel, "", nfoRel, 0, cat.Provider, cat.ID)
				it.PosterPath = sql.NullString{String: posterRel, Valid: true}
			}
		}
	}
	return nil
}

func catalogSeasonPosterURL(cat matchmedia.Catalog) string {
	if s := cat.FindSeason(1); s != nil {
		if u := strings.TrimSpace(s.Poster); u != "" {
			return u
		}
	}
	var s0 string
	for i := range cat.Seasons {
		u := strings.TrimSpace(cat.Seasons[i].Poster)
		if u == "" {
			continue
		}
		n := atoiYear(cat.Seasons[i].Number)
		if n == 1 {
			return u
		}
		if n == 0 {
			if s0 == "" {
				s0 = u
			}
			continue
		}
		return u
	}
	return s0
}

func firstSeasonPosterRel(seasons []db.Season) string {
	var s0, other string
	for _, se := range seasons {
		if !se.PosterPath.Valid {
			continue
		}
		p := strings.TrimSpace(se.PosterPath.String)
		if p == "" {
			continue
		}
		switch se.SeasonNumber {
		case 1:
			return p
		case 0:
			if s0 == "" {
				s0 = p
			}
		default:
			if other == "" {
				other = p
			}
		}
	}
	if other != "" {
		return other
	}
	return s0
}

func (w *Worker) applyEpisode(ctx context.Context, show *db.MediaItem, season *db.Season, ep *db.Episode, ce *matchmedia.Episode, persist bool, session string) {
	mediaDir := filepath.Dir(ep.Path)
	base := strings.TrimSuffix(filepath.Base(ep.Path), filepath.Ext(ep.Path))
	showKey := metadata.SanitizePathSegment(show.Title)
	cacheDir := filepath.Join(w.Store, "metadata", "tv", showKey, fmt.Sprintf("S%02d", season.SeasonNumber))
	_ = os.MkdirAll(cacheDir, 0o755)
	filenameTitle := episodeDisplayTitle(ep.Path, ep.EpisodeNumber, nil)
	catalogTitle := ""
	if ce != nil {
		catalogTitle = strings.TrimSpace(ce.Title)
	}
	existing := strings.TrimSpace(ep.Title.String)
	title := catalogTitle
	if title == "" || strings.EqualFold(title, filenameTitle) {
		if existing != "" {
			title = existing
		} else if title == "" {
			title = filenameTitle
		}
	}
	plot := ""
	stillURL := ""
	if ce != nil {
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

func emptyPath(ns sql.NullString) bool {
	return !ns.Valid || strings.TrimSpace(ns.String) == ""
}

func catalogSessions(it *db.MediaItem, jobSession string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	if it != nil && it.MatchMediaSessionID.Valid {
		add(it.MatchMediaSessionID.String)
	}
	add(jobSession)
	return out
}

func (w *Worker) fillMissingArt(ctx context.Context, it *db.MediaItem, persist bool, session string) error {
	if it == nil || !it.MetaProvider.Valid || !it.MetaID.Valid {
		return nil
	}
	provider := strings.TrimSpace(it.MetaProvider.String)
	metaID := strings.TrimSpace(it.MetaID.String)
	var cat matchmedia.Catalog
	usedSess := ""
	if w.Meta != nil {
		for _, sess := range catalogSessions(it, session) {
			c, err := w.Meta.Catalog(sess, provider, metaID)
			if err != nil {
				log.Printf("catalog %s/%s session %s: %v", provider, metaID, sess, err)
				continue
			}
			cat = c
			usedSess = sess
			break
		}
	}
	if usedSess != "" {
		switch it.Kind {
		case "movie":
			w.fillMissingMoviePoster(ctx, it, cat, persist, usedSess)
		case "show":
			w.fillMissingShowPosters(ctx, it, cat, persist, usedSess)
			seasons, _ := w.DB.ListSeasons(ctx, it.ID)
			for _, season := range seasons {
				eps, _ := w.DB.ListEpisodes(ctx, season.ID)
				for i := range eps {
					if eps[i].StillPath.Valid && eps[i].StillPath.String != "" {
						continue
					}
					ce := resolveCatalogEpisode(cat, season.SeasonNumber, eps[i].EpisodeNumber, eps[i].Path)
					w.applyEpisode(ctx, it, &season, &eps[i], ce, persist, usedSess)
				}
			}
		}
	}
	if got, err := w.DB.GetMediaItem(ctx, it.ID); err == nil && got != nil {
		it.PosterPath = got.PosterPath
	}
	if emptyPath(it.PosterPath) {
		w.installLocalCatalogPoster(ctx, it, persist)
	}
	return nil
}

func matchmediaCatalogRoot(store string) string {
	return filepath.Join(filepath.Dir(store), "matchmedia", "catalog")
}

func findLocalCatalogPoster(store, provider, id string) string {
	provider = strings.TrimSpace(provider)
	id = strings.TrimSpace(id)
	if provider == "" || id == "" {
		return ""
	}
	ents, err := os.ReadDir(matchmediaCatalogRoot(store))
	if err != nil {
		return ""
	}
	prefix := "[" + provider + "-" + id + "]"
	for _, e := range ents {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		dir := filepath.Join(matchmediaCatalogRoot(store), e.Name())
		for _, name := range []string{"poster.jpg", "poster.png", "poster.webp"} {
			p := filepath.Join(dir, name)
			st, err := os.Stat(p)
			if err == nil && st.Size() > 0 {
				return p
			}
		}
	}
	return ""
}

func (w *Worker) installLocalCatalogPoster(ctx context.Context, it *db.MediaItem, persist bool) {
	if it == nil || !emptyPath(it.PosterPath) {
		return
	}
	prov, pid := "", ""
	if it.MetaProvider.Valid {
		prov = it.MetaProvider.String
	}
	if it.MetaID.Valid {
		pid = it.MetaID.String
	}
	src := findLocalCatalogPoster(w.Store, prov, pid)
	if src == "" {
		return
	}
	title := it.Title
	year := 0
	if it.Year.Valid {
		year = int(it.Year.Int64)
	}
	var cacheDir, posterRel string
	switch it.Kind {
	case "movie":
		cacheKey := metadata.SanitizePathSegment(metadata.TitleYear(title, year))
		cacheDir = filepath.Join(w.Store, "metadata", "movies", cacheKey)
		posterRel = filepath.Join("metadata", "movies", cacheKey, filepath.Base(src))
	default:
		cacheKey := metadata.SanitizePathSegment(title)
		cacheDir = filepath.Join(w.Store, "metadata", "tv", cacheKey)
		posterRel = filepath.Join("metadata", "tv", cacheKey, filepath.Base(src))
	}
	_ = os.MkdirAll(cacheDir, 0o755)
	dst := filepath.Join(cacheDir, filepath.Base(src))
	if err := metadata.CopyFile(src, dst); err != nil {
		log.Printf("copy catalog poster %s: %v", src, err)
		return
	}
	nfoRel := ""
	if it.NFOPath.Valid {
		nfoRel = it.NFOPath.String
	}
	if err := w.DB.UpdateMediaItemMeta(ctx, it.ID, title, year, "", posterRel, "", nfoRel, 0, prov, pid); err != nil {
		log.Printf("store catalog poster %s: %v", posterRel, err)
		return
	}
	it.PosterPath = sql.NullString{String: posterRel, Valid: true}
	if persist {
		switch it.Kind {
		case "movie":
			mediaDir := filepath.Dir(it.Path)
			base := strings.TrimSuffix(filepath.Base(it.Path), filepath.Ext(it.Path))
			if metadata.PreferBareMovieSidecar(it.Path) {
				_ = metadata.CopyFile(dst, filepath.Join(mediaDir, filepath.Base(dst)))
			} else {
				_ = metadata.CopyFile(dst, filepath.Join(mediaDir, base+"-poster"+filepath.Ext(dst)))
			}
		default:
			_ = metadata.CopyFile(dst, filepath.Join(it.Path, filepath.Base(dst)))
		}
	}
}

func (w *Worker) fillMissingMoviePoster(ctx context.Context, it *db.MediaItem, cat matchmedia.Catalog, persist bool, session string) {
	if !emptyPath(it.PosterPath) {
		return
	}
	title := strings.TrimSpace(cat.Title)
	if title == "" {
		title = it.Title
	}
	year := atoiYear(cat.Year)
	if year == 0 && it.Year.Valid {
		year = int(it.Year.Int64)
	}
	cacheKey := metadata.SanitizePathSegment(metadata.TitleYear(title, year))
	cacheDir := filepath.Join(w.Store, "metadata", "movies", cacheKey)
	_ = os.MkdirAll(cacheDir, 0o755)
	p := w.writeImage(cacheDir, "poster", strings.TrimSpace(cat.Poster), session)
	if p == "" {
		return
	}
	posterRel := filepath.Join("metadata", "movies", cacheKey, filepath.Base(p))
	if persist {
		mediaDir := filepath.Dir(it.Path)
		base := strings.TrimSuffix(filepath.Base(it.Path), filepath.Ext(it.Path))
		if metadata.PreferBareMovieSidecar(it.Path) {
			_ = metadata.CopyFile(p, filepath.Join(mediaDir, filepath.Base(p)))
		} else {
			_ = metadata.CopyFile(p, filepath.Join(mediaDir, base+"-poster"+filepath.Ext(p)))
		}
	}
	nfoRel := ""
	if it.NFOPath.Valid {
		nfoRel = it.NFOPath.String
	}
	_ = w.DB.UpdateMediaItemMeta(ctx, it.ID, title, year, cat.Synopsis, posterRel, "", nfoRel, 0, cat.Provider, cat.ID)
}

func (w *Worker) fillMissingShowPosters(ctx context.Context, it *db.MediaItem, cat matchmedia.Catalog, persist bool, session string) {
	title := strings.TrimSpace(cat.Title)
	if title == "" {
		title = it.Title
	}
	year := atoiYear(cat.Year)
	if year == 0 && it.Year.Valid {
		year = int(it.Year.Int64)
	}
	cacheKey := metadata.SanitizePathSegment(title)
	cacheDir := filepath.Join(w.Store, "metadata", "tv", cacheKey)
	_ = os.MkdirAll(cacheDir, 0o755)
	nfoRel := ""
	if it.NFOPath.Valid {
		nfoRel = it.NFOPath.String
	} else {
		nfoRel = filepath.Join("metadata", "tv", cacheKey, "tvshow.nfo")
	}
	if emptyPath(it.PosterPath) {
		p := w.writeImage(cacheDir, "poster", strings.TrimSpace(cat.Poster), session)
		if p == "" {
			p = w.writeImage(cacheDir, "poster", catalogSeasonPosterURL(cat), session)
		}
		if p != "" {
			posterRel := filepath.Join("metadata", "tv", cacheKey, filepath.Base(p))
			if persist {
				_ = metadata.CopyFile(p, filepath.Join(it.Path, filepath.Base(p)))
			}
			_ = w.DB.UpdateMediaItemMeta(ctx, it.ID, title, year, cat.Synopsis, posterRel, "", nfoRel, 0, cat.Provider, cat.ID)
			it.PosterPath = sql.NullString{String: posterRel, Valid: true}
		}
	}
	seasons, _ := w.DB.ListSeasons(ctx, it.ID)
	for _, season := range seasons {
		if !emptyPath(season.PosterPath) {
			continue
		}
		cs := cat.FindSeason(season.SeasonNumber)
		sposter := ""
		if cs != nil {
			sposter = cs.Poster
		}
		sdir := filepath.Join(w.Store, "metadata", "tv", cacheKey, fmt.Sprintf("S%02d", season.SeasonNumber))
		_ = os.MkdirAll(sdir, 0o755)
		posterRel := ""
		if p := w.writeImage(sdir, "poster", sposter, session); p != "" {
			posterRel = filepath.Join("metadata", "tv", cacheKey, fmt.Sprintf("S%02d", season.SeasonNumber), filepath.Base(p))
			if persist {
				mediaDir := it.Path
				eps, _ := w.DB.ListEpisodes(ctx, season.ID)
				if len(eps) > 0 {
					mediaDir = filepath.Dir(eps[0].Path)
				}
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
		_ = w.DB.UpdateSeasonMeta(ctx, season.ID, "", "", posterRel, cat.Provider, cat.ID)
	}
	if emptyPath(it.PosterPath) {
		seasons, _ = w.DB.ListSeasons(ctx, it.ID)
		if srcRel := firstSeasonPosterRel(seasons); srcRel != "" {
			src := filepath.Join(w.Store, srcRel)
			ext := filepath.Ext(src)
			if ext == "" {
				ext = ".jpg"
			}
			dst := filepath.Join(cacheDir, "poster"+ext)
			if err := metadata.CopyFile(src, dst); err == nil {
				posterRel := filepath.Join("metadata", "tv", cacheKey, filepath.Base(dst))
				if persist {
					_ = metadata.CopyFile(dst, filepath.Join(it.Path, filepath.Base(dst)))
				}
				_ = w.DB.UpdateMediaItemMeta(ctx, it.ID, title, year, cat.Synopsis, posterRel, "", nfoRel, 0, cat.Provider, cat.ID)
				it.PosterPath = sql.NullString{String: posterRel, Valid: true}
			}
		}
	}
}

func (w *Worker) writeImage(dir, baseName, imageURL, session string) string {
	if imageURL == "" || dir == "" || w.Meta == nil {
		return ""
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		img, ext, err := w.Meta.DownloadURL(imageURL, session)
		if err != nil {
			lastErr = err
			if attempt < 2 {
				time.Sleep(150 * time.Millisecond)
			}
			continue
		}
		if ext == "" {
			ext = ".jpg"
		}
		path, err := metadata.WriteBytesBesideDir(dir, baseName+ext, img)
		if err != nil {
			lastErr = err
			return ""
		}
		return path
	}
	if lastErr != nil {
		log.Printf("writeImage %s: %v", imageURL, lastErr)
	}
	return ""
}

func seasonDisplayTitle(seasonNum int, catalogTitle, showTitle string) string {
	def := fmt.Sprintf("Season %d", seasonNum)
	if seasonNum == 0 {
		def = "Specials"
	}
	ct := strings.TrimSpace(catalogTitle)
	if ct == "" || strings.EqualFold(ct, strings.TrimSpace(showTitle)) {
		return def
	}
	return ct
}

func episodeDisplayTitle(path string, epNum int, ce *matchmedia.Episode) string {
	if ce != nil {
		if t := strings.TrimSpace(ce.Title); t != "" {
			return t
		}
	}
	cleaned := metadata.CleanEpisodeTitle(filepath.Base(path))
	if isMeaningfulEpisodeTitle(cleaned) {
		return cleaned
	}
	return fmt.Sprintf("Episode %d", epNum)
}

func isMeaningfulEpisodeTitle(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "episode ") {
		if _, err := strconv.Atoi(strings.TrimSpace(low[len("episode "):])); err == nil {
			return false
		}
	}
	if _, err := strconv.Atoi(s); err == nil {
		return false
	}
	return !bareEpisodeCode(s)
}

func bareEpisodeCode(s string) bool {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	if len(s) < 4 || s[0] != 's' {
		return false
	}
	i := 1
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 1 || i >= len(s) || s[i] != 'e' {
		return false
	}
	i++
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return start < i && i == len(s)
}

func resolveCatalogEpisode(cat matchmedia.Catalog, seasonNum, epNum int, filePath string) *matchmedia.Episode {
	if titled := findSpecialsCatalogEpisode(cat, filePath); titled != nil {
		return titled
	}
	ce := cat.FindEpisode(seasonNum, epNum)
	if ce == nil {
		return nil
	}
	if seasonNum != 0 {
		return ce
	}
	cleaned := metadata.CleanEpisodeTitle(filepath.Base(filePath))
	if isMeaningfulEpisodeTitle(cleaned) && strings.TrimSpace(ce.Title) != "" &&
		metadata.AliasKey(ce.Title) != metadata.AliasKey(cleaned) {
		return nil
	}
	return ce
}

func findSpecialsCatalogEpisode(cat matchmedia.Catalog, filePath string) *matchmedia.Episode {
	want := metadata.AliasKey(metadata.CleanEpisodeTitle(filepath.Base(filePath)))
	if want == "" {
		return nil
	}
	for i := range cat.Seasons {
		s := &cat.Seasons[i]
		if !catalogSeasonIsSpecials(s) {
			continue
		}
		for j := range s.Episodes {
			if metadata.AliasKey(s.Episodes[j].Title) == want {
				return &s.Episodes[j]
			}
		}
	}
	return nil
}

func catalogSeasonIsSpecials(s *matchmedia.Season) bool {
	if s == nil {
		return false
	}
	n := strings.TrimSpace(s.Number)
	if n == "" || strings.EqualFold(n, "specials") || strings.EqualFold(n, "special") || strings.EqualFold(n, "ova") {
		return true
	}
	parsed, err := strconv.Atoi(n)
	return err == nil && parsed == 0
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
