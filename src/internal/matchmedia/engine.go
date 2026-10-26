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
	"github.com/alyshmahell/matchmedia/src/mod/sidecar"
	"github.com/alyshmahell/servemedia/src/internal/config"
	"gopkg.in/yaml.v3"
)

// Engine calls MatchMedia in-process. API keys live in the XDG overlay.
type Engine struct {
	mu      sync.Mutex
	cfg     *mmconfig.Config
	fetch   *mmfetch.Client
	overlay string
	jobs    map[string]Job
}

func Open() (*Engine, error) {
	dataDir, err := config.MatchMediaDir()
	if err != nil {
		return nil, err
	}
	overlay := overlayDest(dataDir)
	if err := scrubSidecarFile(overlay); err != nil {
		return nil, err
	}
	if extra := strings.TrimSpace(os.Getenv("SERVEMEDIA_MATCHMEDIA_OVERLAY")); extra != "" {
		if err := mergeExtraOverlay(overlay, extra); err != nil {
			return nil, err
		}
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
	arg, err := overlayArg(e.overlay)
	if err != nil {
		return err
	}
	cfg, err := mmconfig.New(arg)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.cfg = cfg
	e.fetch = mmfetch.New(cfg)
	e.mu.Unlock()
	return nil
}

func overlayArg(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return path, nil
}

func (e *Engine) config() *mmconfig.Config {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg
}

func (e *Engine) fetcher() *mmfetch.Client {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.fetch
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
	for i := range jobs {
		if jobs[i].ID == "" {
			jobs[i].ID = stableJobID(jobs[i].Path)
		}
	}
	e.remember(jobs)
	return jobs, nil
}

// Match searches jobs that are still pending or in error. onJob runs for each finished job.
func (e *Engine) Match(ctx context.Context, jobs []Job, report func(done, total int, name string), onJob func(Job)) ([]Job, error) {
	cfg := e.config()
	if cfg == nil {
		return nil, fmt.Errorf("metadata service unavailable")
	}
	if !needsProviderMatch(jobs) {
		return jobs, nil
	}
	client := e.fetcher()
	if client == nil {
		return nil, fmt.Errorf("metadata service unavailable")
	}
	var mu sync.Mutex
	var matched, sidecar, replay, manual, unmatched, errored int
	var lastDone, lastTotal int
	var lastTitle string
	out := match.Collect(match.RunProgress(ctx, client, jobs, func(done, total int, j match.Job) {
		if onJob != nil {
			onJob(j)
		}
		if report == nil {
			return
		}
		mu.Lock()
		tallyMatch(j, &matched, &sidecar, &replay, &manual, &unmatched, &errored)
		lastDone, lastTotal = done, total
		lastTitle = j.Title
		msg := matchProgress(done, total, matched, manual, unmatched, errored, sidecar, replay, 0, j.Title)
		mu.Unlock()
		report(done, total, msg)
	}, nil))
	if report != nil && lastTotal > 0 {
		folded := 0
		if lastTotal > len(out) {
			folded = lastTotal - len(out)
		}
		report(lastDone, lastTotal, matchProgress(lastDone, lastTotal, matched, manual, unmatched, errored, sidecar, replay, folded, lastTitle))
	}
	e.remember(out)
	return out, nil
}

func tallyMatch(j match.Job, matched, sidecar, replay, manual, unmatched, errored *int) {
	if match.ChildFail(j) {
		return
	}
	if j.Status == "matched" {
		switch j.Ranker {
		case "sidecar":
			*sidecar++
			return
		case "replay":
			*replay++
			return
		}
	}
	switch j.Status {
	case "matched":
		*matched++
	case "manual":
		*manual++
	case "unmatched":
		*unmatched++
	case "error":
		*errored++
	}
}

func matchProgress(done, total, matched, manual, unmatched, errored, sidecar, replay, folded int, title string) string {
	return fmt.Sprintf("match %d/%d matched:%d manual:%d unmatched:%d error:%d sidecar:%d replay:%d folded:%d %s", done, total, matched, manual, unmatched, errored, sidecar, replay, folded, title)
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
	client := e.fetcher()
	if client == nil {
		return nil, "", fmt.Errorf("metadata service unavailable")
	}
	jobs := ingest.Jobs(in)
	out := match.Collect(match.RunProgress(ctx, client, jobs, func(done, total int, j match.Job) {
		if report != nil {
			report(done, total, j.Title)
		}
	}, nil))
	e.remember(out)
	return out, "", nil
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
	client := e.fetcher()
	if client == nil {
		return Job{}, fmt.Errorf("metadata service unavailable")
	}
	out, err := match.Select(ctx, client, hint, provider, id)
	if err != nil {
		return Job{}, err
	}
	e.remember([]Job{out})
	return out, nil
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
	client := e.fetcher()
	if client == nil {
		return nil, "", fmt.Errorf("metadata service unavailable")
	}
	body, ctype, err := client.Bytes(context.Background(), u, "")
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
	return configDocument(e.config())
}

func removeEmptyDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return
	}
	_ = os.Remove(dir)
}

func (e *Engine) SetConfig(patch map[string]any) error {
	if patch == nil {
		patch = map[string]any{}
	}
	base, err := defaultDocument()
	if err != nil {
		return err
	}
	delta := overlayDiff(base, patch)
	if len(delta) == 0 {
		if err := os.Remove(e.overlay); err != nil && !os.IsNotExist(err) {
			return err
		}
		removeEmptyDir(filepath.Dir(e.overlay))
		return e.Reload()
	}
	if err := os.MkdirAll(filepath.Dir(e.overlay), 0o755); err != nil {
		return err
	}
	body, err := yamlMarshal(delta)
	if err != nil {
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

// SidecarComplete reports a Changes job that already has its title NFO, poster,
// and, for a show, an NFO beside every episode. Those jobs are not fetched.
func SidecarComplete(j Job) bool {
	return sidecar.Complete(j, true)
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
		c.Attrs = copyAttrs(j.Match.Attrs)
	}
	return c
}

func copyAttrs(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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
