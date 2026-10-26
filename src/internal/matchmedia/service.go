package matchmedia

import "context"

// Service is the metadata engine the fetch worker and settings page call.
// Scan, Match, and SelectStored are the only match path. Run remains for a
// full walk-and-match, and the HTTP Client implements the same methods so
// existing stub servers still stand in for an engine.
type Service interface {
	Status() (Status, error)
	Scan(ctx context.Context, path, mode string, report func(done, total int, name string)) ([]Job, error)
	Match(ctx context.Context, jobs []Job, report func(done, total int, name string), onJob func(Job)) ([]Job, error)
	SelectStored(ctx context.Context, hint Job, provider, id string) (Job, error)
	Run(ctx context.Context, path, mode string, report func(done, total int, name string)) ([]Job, string, error)
	RunIngest(ctx context.Context, rows []IngestRow, report func(done, total int, name string)) ([]Job, string, error)
	Select(session, jobID, provider, id string) (Job, error)
	Job(session, id string) (Job, error)
	Catalog(session, provider, id string) (Catalog, error)
	DownloadURL(u, session string) ([]byte, string, error)
	ResolveURL(u, session string) string
	SecretsStatus() (map[string]bool, error)
	SetSecrets(updates map[string]string) error
	Config() (map[string]any, error)
	SetConfig(patch map[string]any) error
	Reload() error
}
