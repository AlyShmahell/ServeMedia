package providers

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/rpc"
	"path"
	"strings"
	"time"

	"github.com/alyshmahell/medora/providers/rpcapi"
)

// Client talks to medora-providers over a Unix socket. Provider-agnostic.
type Client struct {
	Socket string
	HTTP   *http.Client // for downloading artwork URLs
}

func (c *Client) dial() (*rpc.Client, error) {
	if strings.TrimSpace(c.Socket) == "" {
		return nil, fmt.Errorf("providers socket not configured")
	}
	conn, err := net.DialTimeout("unix", c.Socket, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return rpc.NewClient(conn), nil
}

func (c *Client) call(method string, args, reply any) error {
	cli, err := c.dial()
	if err != nil {
		return err
	}
	defer cli.Close()
	return cli.Call(rpcapi.ServiceName+"."+method, args, reply)
}

func (c *Client) Status() (rpcapi.StatusReply, error) {
	var reply rpcapi.StatusReply
	err := c.call("Status", &rpcapi.StatusArgs{}, &reply)
	return reply, err
}

func (c *Client) LookupMovie(title string, year int, libraryType string, durationMinutes int) (rpcapi.Result, error) {
	var reply rpcapi.LookupReply
	err := c.call("LookupMovie", &rpcapi.LookupMovieArgs{
		Title: title, Year: year, LibraryType: libraryType, DurationMinutes: durationMinutes,
	}, &reply)
	return reply.Result, err
}

func (c *Client) LookupShow(title string, year int, libraryType string, excludeProviderIDs ...string) (rpcapi.Result, error) {
	var reply rpcapi.LookupReply
	err := c.call("LookupShow", &rpcapi.LookupShowArgs{
		Title: title, Year: year, LibraryType: libraryType, ExcludeProviderIDs: excludeProviderIDs,
	}, &reply)
	return reply.Result, err
}

func (c *Client) LookupSeason(showTitle string, season int, libraryType, showProvider, showProviderID string) (rpcapi.Result, error) {
	var reply rpcapi.LookupReply
	err := c.call("LookupSeason", &rpcapi.LookupSeasonArgs{
		ShowTitle: showTitle, Season: season, LibraryType: libraryType,
		ShowProvider: showProvider, ShowProviderID: showProviderID,
	}, &reply)
	return reply.Result, err
}

func (c *Client) LookupEpisode(showTitle string, season, episode int, libraryType, showProvider, showProviderID string) (rpcapi.Result, error) {
	var reply rpcapi.LookupReply
	err := c.call("LookupEpisode", &rpcapi.LookupEpisodeArgs{
		ShowTitle: showTitle, Season: season, Episode: episode, LibraryType: libraryType,
		ShowProvider: showProvider, ShowProviderID: showProviderID,
	}, &reply)
	return reply.Result, err
}

// DownloadURL fetches an absolute image URL (generic HTTP GET).
func (c *Client) DownloadURL(u string) ([]byte, string, error) {
	if u == "" {
		return nil, "", fmt.Errorf("empty image URL")
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	res, err := hc.Get(u)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, "", err
	}
	if res.StatusCode >= 300 {
		return nil, "", fmt.Errorf("image download %s", res.Status)
	}
	ext := path.Ext(strings.Split(u, "?")[0])
	if ext == "" {
		ext = ".jpg"
	}
	return b, ext, nil
}
