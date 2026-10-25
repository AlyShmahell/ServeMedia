package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type runtimeDir struct {
	Dir      string
	Sock     string
	URLFile  string
	PortFile string
}

func runtimePaths() (runtimeDir, error) {
	base := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	if base == "" {
		base = strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	}
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return runtimeDir{}, err
		}
		base = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(base, "servemedia")
	return runtimeDir{
		Dir:      dir,
		Sock:     filepath.Join(dir, "servemedia.sock"),
		URLFile:  filepath.Join(dir, "http.url"),
		PortFile: filepath.Join(dir, "port"),
	}, nil
}

func (r runtimeDir) ensure() error {
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(r.Dir, 0o700)
}

func (r runtimeDir) write(loopbackURL, port string) error {
	if err := r.ensure(); err != nil {
		return err
	}
	if err := writePrivate(r.URLFile, loopbackURL+"\n"); err != nil {
		return err
	}
	return writePrivate(r.PortFile, port+"\n")
}

func writePrivate(path, body string) error {
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func (r runtimeDir) cleanup() {
	_ = os.Remove(r.Sock)
	_ = os.Remove(r.URLFile)
	_ = os.Remove(r.PortFile)
}

func listenAddr(host, port string) (string, error) {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if port == "" {
		port = "7676"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return "", fmt.Errorf("invalid port %q", strings.TrimSpace(port))
	}
	return net.JoinHostPort(host, strconv.Itoa(n)), nil
}

func listenTCP(host, port string) (net.Listener, error) {
	explicit := strings.TrimSpace(port) != ""
	addr, err := listenAddr(host, port)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err == nil || explicit {
		return ln, err
	}
	log.Printf("port 7676: %v; listening on an ephemeral port", err)
	fallback, ferr := listenAddr(host, "0")
	if ferr != nil {
		return nil, ferr
	}
	return net.Listen("tcp", fallback)
}

func loopbackURL(addr net.Addr) (url, port string, err error) {
	_, port, err = net.SplitHostPort(addr.String())
	if err != nil || port == "" {
		return "", "", fmt.Errorf("listen address %v", addr)
	}
	return "http://127.0.0.1:" + port, port, nil
}

func probeRunning(r runtimeDir) (string, bool) {
	if strings.TrimSpace(r.Sock) == "" {
		return "", false
	}
	if _, err := os.Stat(r.Sock); err != nil {
		return "", false
	}
	client := &http.Client{
		Timeout: 400 * time.Millisecond,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 200 * time.Millisecond}
				return d.DialContext(ctx, "unix", r.Sock)
			},
		},
	}
	resp, err := client.Get("http://servemedia/healthz")
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	b, err := os.ReadFile(r.URLFile)
	if err != nil {
		return "", true
	}
	return strings.TrimSpace(string(b)), true
}

func discardStaleSocket(r runtimeDir) {
	if _, ok := probeRunning(r); ok {
		return
	}
	_ = os.Remove(r.Sock)
}
