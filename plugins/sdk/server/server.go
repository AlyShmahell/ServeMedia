package server

import (
	"net"
	"net/rpc"
	"os"
	"path/filepath"

	"github.com/alyshmahell/medora-plugin-sdk/rpcapi"
)

// MetadataBackend is implemented by metadata plugins (e.g. providers cascade).
type MetadataBackend interface {
	Status() rpcapi.StatusReply
	ListProviders() []rpcapi.ProviderInfo
	LookupMovie(title string, year int, libraryType string, durationMinutes int) (*rpcapi.Result, error)
	LookupShow(title string, year int, libraryType string, excludeIDs ...string) (*rpcapi.Result, error)
	LookupSeason(showTitle string, season int, libraryType, showProvider, showProviderID string) (*rpcapi.Result, error)
	LookupEpisode(showTitle string, season, episode int, libraryType, showProvider, showProviderID string) (*rpcapi.Result, error)
}

// Service is the net/rpc receiver for a metadata plugin.
type Service struct {
	Backend MetadataBackend
}

func (s *Service) Status(_ *rpcapi.StatusArgs, reply *rpcapi.StatusReply) error {
	*reply = s.Backend.Status()
	return nil
}

func (s *Service) ListProviders(_ *rpcapi.ListProvidersArgs, reply *rpcapi.ListProvidersReply) error {
	reply.Providers = s.Backend.ListProviders()
	return nil
}

func (s *Service) LookupMovie(args *rpcapi.LookupMovieArgs, reply *rpcapi.LookupReply) error {
	r, err := s.Backend.LookupMovie(args.Title, args.Year, args.LibraryType, args.DurationMinutes)
	if err != nil {
		return err
	}
	reply.Result = *r
	return nil
}

func (s *Service) LookupShow(args *rpcapi.LookupShowArgs, reply *rpcapi.LookupReply) error {
	r, err := s.Backend.LookupShow(args.Title, args.Year, args.LibraryType, args.ExcludeProviderIDs...)
	if err != nil {
		return err
	}
	reply.Result = *r
	return nil
}

func (s *Service) LookupSeason(args *rpcapi.LookupSeasonArgs, reply *rpcapi.LookupReply) error {
	r, err := s.Backend.LookupSeason(args.ShowTitle, args.Season, args.LibraryType, args.ShowProvider, args.ShowProviderID)
	if err != nil {
		return err
	}
	reply.Result = *r
	return nil
}

func (s *Service) LookupEpisode(args *rpcapi.LookupEpisodeArgs, reply *rpcapi.LookupReply) error {
	r, err := s.Backend.LookupEpisode(args.ShowTitle, args.Season, args.Episode, args.LibraryType, args.ShowProvider, args.ShowProviderID)
	if err != nil {
		return err
	}
	reply.Result = *r
	return nil
}

// ListenAndServe registers the service and serves on a Unix socket.
func ListenAndServe(socketPath string, svc *Service) error {
	_ = os.Remove(socketPath)
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return err
	}
	if err := rpc.RegisterName(rpcapi.ServiceName, svc); err != nil {
		return err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	_ = os.Chmod(socketPath, 0o660)
	rpc.Accept(ln)
	return nil
}
