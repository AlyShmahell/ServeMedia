package matchmedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	mmconfig "github.com/alyshmahell/matchmedia/src/mod/config"
	mmfetch "github.com/alyshmahell/matchmedia/src/mod/fetch"
	"github.com/alyshmahell/matchmedia/src/mod/ingest"
	"github.com/alyshmahell/matchmedia/src/mod/match"
	"github.com/alyshmahell/matchmedia/src/mod/scan"
	"github.com/alyshmahell/servemedia/src/internal/config"
	"gopkg.in/yaml.v3"
)

// Engine calls MatchMedia in-process. API keys live in the XDG overlay.
type Engine struct {
	mu      sync.Mutex
	cfg     *mmconfig.Config
	overlay string
	jobs    map[string]Job
}

func Open() (*Engine, error) {
	dataDir, err := config.MatchMediaXDGDataDir()
	if err != nil {
		return nil, err
	}
	overlay := overlayDest(dataDir)
	if err := os.MkdirAll(filepath.Dir(overlay), 0o755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(overlay); os.IsNotExist(err) {
		if err := os.WriteFile(overlay, []byte("{}\n"), 0o644); err != nil {
			return nil, err
		}
	}
	if extra := strings.TrimSpace(os.Getenv("SERVEMEDIA_MATCHMEDIA_OVERLAY")); extra != "" {
		base, err := loadYAMLMap(overlay)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		more, err := loadYAMLMap(extra)
		if err != nil {
			return nil, fmt.Errorf("matchmedia overlay: %w", err)
		}
		if base == nil {
			base = map[string]any{}
		}
		mergeOverlay(base, more)
		body, err := yamlMarshal(base)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(overlay, body, 0o644); err != nil {
			return nil, err
		}
	}
	if err := scrubSidecarFile(overlay); err != nil {
		return nil, err
	}
	e := &Engine{overlay: overlay, jobs: map[string]Job{}}
	if err := e.Reload(); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *Engine) Reload() error {
	if e == nil {
		return fmt.Errorf("metadata service unavailable")
	}
	cfg, err := mmconfig.New(e.overlay)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.cfg = cfg
	e.mu.Unlock()
	return nil
}

func (e *Engine) config() *mmconfig.Config {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg
}

func (e *Engine) Status() (Status, error) {
	cfg := e.config()
	if cfg == nil {
		return Status{DisabledReason: "metadata service unavailable"}, fmt.Errorf("metadata service unavailable")
	}
	return Status{Ready: true, Version: strings.TrimSpace(cfg.Version)}, nil
}

func (e *Engine) Run(ctx context.Context, path, mode string, report func(done, total int, name string)) ([]Job, string, error) {
	jobs, err := e.Scan(ctx, path, mode, report)
	if err != nil {
		return nil, "", err
	}
	out, err := e.Match(ctx, jobs, report, nil)
	if err != nil {
		return nil, "", err
	}
	return out, "", nil
}

// Scan walks the library and returns the jobs for that mode. It does not search providers.
func (e *Engine) Scan(ctx context.Context, path, mode string, report func(done, total int, name string)) ([]Job, error) {
	cfg := e.config()
	if cfg == nil {
		return nil, fmt.Errorf("metadata service unavailable")
	}
	jobs, err := scan.RunProgress(cfg, path, mapMode(mode), func(done, total int, name string) {
		if report != nil {
			report(done, total, fmt.Sprintf("scan %d/%d %s", done, total, name))
		}
	})
	if err != nil {
		return nil, err
	}
	local := convertJobs(jobs)
	e.remember(local)
	return local, nil
}

// Match searches jobs that are still pending or in error. onJob runs for each finished job.
func (e *Engine) Match(ctx context.Context, jobs []Job, report func(done, total int, name string), onJob func(Job)) ([]Job, error) {
	cfg := e.config()
	if cfg == nil {
		return nil, fmt.Errorf("metadata service unavailable")
	}
	in := make([]match.Job, len(jobs))
	for i := range jobs {
		in[i] = toMM(jobs[i])
	}
	if !needsProviderMatch(in) {
		return jobs, nil
	}
	var mu sync.Mutex
	var matched, manual, unmatched, errored int
	out := match.RunProgress(ctx, cfg, in, func(done, total int, j match.Job) {
		local := fromMM(j)
		if onJob != nil {
			onJob(local)
		}
		if report == nil {
			return
		}
		mu.Lock()
		if !match.ChildFail(j) {
			switch j.Status {
			case "matched":
				matched++
			case "manual":
				manual++
			case "unmatched":
				unmatched++
			case "error":
				errored++
			}
		}
		msg := fmt.Sprintf("match %d/%d matched:%d manual:%d unmatched:%d error:%d %s", done, total, matched, manual, unmatched, errored, j.Title)
		mu.Unlock()
		report(done, total, msg)
	})
	local := convertJobs(out)
	e.remember(local)
	return local, nil
}

func (e *Engine) RunIngest(ctx context.Context, rows []IngestRow, report func(done, total int, name string)) ([]Job, string, error) {
	cfg := e.config()
	if cfg == nil {
		return nil, "", fmt.Errorf("metadata service unavailable")
	}
	in := make([]ingest.Row, len(rows))
	for i, row := range rows {
		in[i] = ingest.Row{Title: row.Title, Year: row.Year, Type: row.Type}
	}
	jobs := ingest.Jobs(in)
	out := match.RunProgress(ctx, cfg, jobs, func(done, total int, j match.Job) {
		if report != nil {
			report(done, total, j.Title)
		}
	})
	local := convertJobs(out)
	e.remember(local)
	return local, "", nil
}

func (e *Engine) Select(session, jobID, provider, id string) (Job, error) {
	e.mu.Lock()
	hint, ok := e.jobs[jobID]
	e.mu.Unlock()
	if !ok {
		return Job{}, fmt.Errorf("no MatchMedia job for this title — rescan to match again")
	}
	return e.SelectStored(context.Background(), hint, provider, id)
}

// SelectStored ranks a previously stored job. The library no longer reloads a session.
func (e *Engine) SelectStored(ctx context.Context, hint Job, provider, id string) (Job, error) {
	cfg := e.config()
	if cfg == nil {
		return Job{}, fmt.Errorf("metadata service unavailable")
	}
	out, err := match.Select(ctx, cfg, toMM(hint), provider, id)
	if err != nil {
		return Job{}, err
	}
	j := fromMM(out)
	e.remember([]Job{j})
	return j, nil
}

func (e *Engine) Job(session, id string) (Job, error) {
	e.mu.Lock()
	j, ok := e.jobs[id]
	e.mu.Unlock()
	if !ok {
		return Job{}, fmt.Errorf("matchmedia job %s not found", id)
	}
	return j, nil
}

func (e *Engine) Catalog(session, provider, id string) (Catalog, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, j := range e.jobs {
		if j.Match == nil || j.Match.Provider != provider || j.Match.ID != id {
			continue
		}
		return catalogFromJob(j), nil
	}
	return Catalog{}, fmt.Errorf("catalog %s/%s not found", provider, id)
}

func (e *Engine) DownloadURL(u, session string) ([]byte, string, error) {
	u = e.ResolveURL(u, session)
	if u == "" || !strings.Contains(u, "://") {
		return nil, "", fmt.Errorf("empty image URL")
	}
	cfg := e.config()
	if cfg == nil {
		return nil, "", fmt.Errorf("metadata service unavailable")
	}
	body, ctype, err := mmfetch.Bytes(context.Background(), cfg, u, "")
	if err != nil {
		return nil, "", err
	}
	return body, extFromType(ctype), nil
}

func (e *Engine) ResolveURL(u, session string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return u
}

func (e *Engine) SecretsStatus() (map[string]bool, error) {
	cfg := e.config()
	out := map[string]bool{"tmdb": false, "omdb": false}
	if cfg == nil {
		return out, nil
	}
	if p, ok := cfg.Providers["tmdb"]; ok && strings.TrimSpace(p.APIKey) != "" {
		out["tmdb"] = true
	}
	if p, ok := cfg.Providers["omdb"]; ok && strings.TrimSpace(p.APIKey) != "" {
		out["omdb"] = true
	}
	return out, nil
}

func (e *Engine) SetSecrets(updates map[string]string) error {
	cur, err := e.Config()
	if err != nil {
		return err
	}
	providers, _ := cur["providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	for name, key := range updates {
		name = strings.TrimSpace(name)
		if name == "" || strings.TrimSpace(key) == "" {
			continue
		}
		spec, _ := providers[name].(map[string]any)
		if spec == nil {
			spec = map[string]any{}
		}
		spec["api_key"] = key
		providers[name] = spec
	}
	cur["providers"] = providers
	return e.SetConfig(cur)
}

func (e *Engine) Config() (map[string]any, error) {
	m, err := loadYAMLMap(e.overlay)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	return m, nil
}

func (e *Engine) SetConfig(patch map[string]any) error {
	if patch == nil {
		patch = map[string]any{}
	}
	body, err := yamlMarshal(patch)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(e.overlay), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(e.overlay, body, 0o644); err != nil {
		return err
	}
	return e.Reload()
}

func (e *Engine) remember(jobs []Job) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.jobs == nil {
		e.jobs = map[string]Job{}
	}
	for _, j := range jobs {
		if j.ID != "" {
			e.jobs[j.ID] = j
		}
	}
}

func needsProviderMatch(jobs []match.Job) bool {
	for _, j := range jobs {
		if j.Status == "pending" || j.Status == "error" {
			return true
		}
	}
	return false
}

func mapMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "nfo", "local":
		return scan.ModeNFO
	case "changes", "matchmedia-changes":
		return scan.ModeChanges
	case "missing":
		return scan.ModeMissing
	case "rescan", "matchmedia":
		return scan.ModeRescan
	default:
		return scan.ModeRescan
	}
}

func convertJobs(in []match.Job) []Job {
	out := make([]Job, len(in))
	for i := range in {
		out[i] = fromMM(in[i])
	}
	return out
}

func fromMM(j match.Job) Job {
	out := Job{
		ID:         j.ID,
		Source:     j.Source,
		Title:      j.Title,
		Year:       j.Year,
		Parent:     j.Parent,
		Path:       j.Path,
		Kind:       j.Kind,
		Role:       j.Role,
		CatalogFor: j.CatalogFor,
		Status:     j.Status,
		Error:      j.Error,
		Ranker:     j.Ranker,
	}
	if out.ID == "" {
		out.ID = stableJobID(j.Path)
	}
	out.Files = make([]JobFile, len(j.Files))
	for i, f := range j.Files {
		out.Files[i] = JobFile{Path: f.Path, Season: f.Season, Episode: f.Episode}
	}
	if j.Match != nil {
		c := fromHit(*j.Match)
		out.Match = &c
	}
	out.Candidates = make([]Candidate, len(j.Candidates))
	for i, c := range j.Candidates {
		out.Candidates[i] = fromHit(c)
	}
	out.Catalog = make([]Season, len(j.Catalog))
	for i, s := range j.Catalog {
		out.Catalog[i] = fromSeason(s)
	}
	return out
}

func toMM(j Job) match.Job {
	out := match.Job{
		ID:         j.ID,
		Source:     j.Source,
		Title:      j.Title,
		Year:       j.Year,
		Parent:     j.Parent,
		Path:       j.Path,
		Kind:       j.Kind,
		Role:       j.Role,
		CatalogFor: j.CatalogFor,
		Status:     j.Status,
		Error:      j.Error,
		Ranker:     j.Ranker,
	}
	out.Files = make([]match.JobFile, len(j.Files))
	for i, f := range j.Files {
		out.Files[i] = match.JobFile{Path: f.Path, Season: f.Season, Episode: f.Episode}
	}
	if j.Match != nil {
		h := toHit(*j.Match)
		out.Match = &h
	}
	out.Candidates = make([]match.Candidate, len(j.Candidates))
	for i, c := range j.Candidates {
		out.Candidates[i] = toHit(c)
	}
	out.Catalog = make([]match.CatalogSeason, len(j.Catalog))
	for i, s := range j.Catalog {
		out.Catalog[i] = toSeason(s)
	}
	return out
}

func fromHit(h match.Candidate) Candidate {
	return Candidate{
		Provider: h.Provider,
		ID:       h.ID,
		Title:    h.Title,
		Year:     h.Year,
		Score:    h.Score,
		Synopsis: h.Synopsis,
		Poster:   h.Poster,
		Jaccard:  h.Jaccard,
	}
}

func toHit(c Candidate) match.Candidate {
	return match.Candidate{
		Provider: c.Provider,
		ID:       c.ID,
		Title:    c.Title,
		Year:     c.Year,
		Score:    c.Score,
		Synopsis: c.Synopsis,
		Poster:   c.Poster,
		Jaccard:  c.Jaccard,
	}
}

func fromSeason(s match.CatalogSeason) Season {
	out := Season{
		Number:   s.Number,
		Title:    s.Title,
		Synopsis: s.Synopsis,
		Poster:   s.Poster,
		Year:     s.Year,
	}
	out.Episodes = make([]Episode, len(s.Episodes))
	for i, e := range s.Episodes {
		out.Episodes[i] = Episode{
			Number: e.Number, Title: e.Title, Synopsis: e.Synopsis,
			Poster: e.Poster, Year: e.Year, Path: e.Path, Paths: append([]string(nil), e.Paths...),
		}
	}
	return out
}

func toSeason(s Season) match.CatalogSeason {
	out := match.CatalogSeason{
		Number: s.Number, Title: s.Title, Synopsis: s.Synopsis, Poster: s.Poster, Year: s.Year,
	}
	out.Episodes = make([]match.CatalogEpisode, len(s.Episodes))
	for i, e := range s.Episodes {
		out.Episodes[i] = match.CatalogEpisode{
			Number: e.Number, Title: e.Title, Synopsis: e.Synopsis,
			Poster: e.Poster, Year: e.Year, Path: e.Path, Paths: append([]string(nil), e.Paths...),
		}
	}
	return out
}

func catalogFromJob(j Job) Catalog {
	c := Catalog{Seasons: j.Catalog}
	if j.Match != nil {
		c.Provider = j.Match.Provider
		c.ID = j.Match.ID
		c.Title = j.Match.Title
		c.Year = j.Match.Year
		c.Synopsis = j.Match.Synopsis
		c.Poster = j.Match.Poster
	}
	return c
}

// StableID is the scan job id derived from a path. Scan leaves ID empty.
func StableID(path string) string {
	return stableJobID(path)
}

func stableJobID(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:8])
}

func extFromType(ctype string) string {
	switch {
	case strings.Contains(ctype, "png"):
		return ".png"
	case strings.Contains(ctype, "webp"):
		return ".webp"
	default:
		return ".jpg"
	}
}

func yamlMarshal(v any) ([]byte, error) {
	return yaml.Marshal(v)
}
