package matchmedia

import "context"

// Service is the metadata engine the fetch worker and settings page call.
// The in-process Engine is the production implementation. Client remains for
// tests that still speak the old HTTP job protocol.
type Service interface {
	Status() (Status, error)
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
