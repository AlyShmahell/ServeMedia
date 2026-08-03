package server

import (
	"net"
	"net/rpc"
	"os"
	"path/filepath"

	"github.com/alyshmahell/medora/providers/cascade"
	"github.com/alyshmahell/medora/providers/rpcapi"
)

// Service is the net/rpc receiver for the providers sidecar.
type Service struct {
	Cascade *cascade.Cascade
}

func (s *Service) Status(_ *rpcapi.StatusArgs, reply *rpcapi.StatusReply) error {
	*reply = s.Cascade.Status()
	return nil
}

func (s *Service) LookupMovie(args *rpcapi.LookupMovieArgs, reply *rpcapi.LookupReply) error {
	r, err := s.Cascade.LookupMovie(args.Title, args.Year, args.LibraryType, args.DurationMinutes)
	if err != nil {
		return err
	}
	reply.Result = *r
	return nil
}

func (s *Service) LookupShow(args *rpcapi.LookupShowArgs, reply *rpcapi.LookupReply) error {
	r, err := s.Cascade.LookupShow(args.Title, args.Year, args.LibraryType, args.ExcludeProviderIDs...)
	if err != nil {
		return err
	}
	reply.Result = *r
	return nil
}

func (s *Service) LookupSeason(args *rpcapi.LookupSeasonArgs, reply *rpcapi.LookupReply) error {
	r, err := s.Cascade.LookupSeason(args.ShowTitle, args.Season, args.LibraryType, args.ShowProvider, args.ShowProviderID)
	if err != nil {
		return err
	}
	reply.Result = *r
	return nil
}

func (s *Service) LookupEpisode(args *rpcapi.LookupEpisodeArgs, reply *rpcapi.LookupReply) error {
	r, err := s.Cascade.LookupEpisode(args.ShowTitle, args.Season, args.Episode, args.LibraryType, args.ShowProvider, args.ShowProviderID)
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
