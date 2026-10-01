package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alyshmahell/servemedia/internal/backup"
	"github.com/alyshmahell/servemedia/internal/config"
	"github.com/alyshmahell/servemedia/internal/db"
	"github.com/alyshmahell/servemedia/internal/fetch"
	"github.com/alyshmahell/servemedia/internal/media"
	"github.com/alyshmahell/servemedia/internal/matchmedia"
	"github.com/alyshmahell/servemedia/internal/server"
	"github.com/alyshmahell/servemedia/internal/watchdog"
	"github.com/alyshmahell/servemedia/web"
)

func main() {
	configPath := flag.String("config", "", "path to default.yaml")
	doPrepare := flag.Bool("prepare", false, "fetch third-party vendor if missing, then exit")
	flag.Parse()
	path := *configPath
	if path == "" {
		var err error
		path, err = config.DefaultConfigPath()
		if err != nil {
			log.Fatal(err)
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		log.Fatal(err)
	}
	media.SetRoot(cfg.ExeDir, cfg.Transcode.FFmpeg)
	if *doPrepare {
		if _, err := os.Stat(cfg.MatchMediaBin()); err != nil {
			log.Fatal(err)
		}
		if err := thirdParty(cfg); err != nil {
			log.Fatal(err)
		}
		return
	}

	_ = os.MkdirAll(cfg.Store.Path, 0o755)
	_ = os.MkdirAll(filepath.Join(cfg.Store.Path, "metadata", "movies"), 0o755)
	_ = os.MkdirAll(filepath.Join(cfg.Store.Path, "metadata", "tv"), 0o755)
	_ = os.MkdirAll(cfg.Transcode.Path, 0o755)
	_ = os.MkdirAll(cfg.Backup.Dir, 0o755)

	uiURL := publicURL(cfg.HTTP.Addr)
	if ln, err := net.Listen("tcp", cfg.HTTP.Addr); err != nil {
		if isAddrInUse(err) {
			openBrowser(uiURL)
			return
		}
		log.Fatal(err)
	} else {
		_ = ln.Close()
	}

	var procMu sync.Mutex
	var proc *matchmedia.Proc
	p, err := matchmedia.Start(cfg.MatchMedia.Addr)
	if err != nil {
		log.Printf("matchmedia: %v (metadata may be unavailable)", err)
	}
	proc = p

	var database *db.DB
	openDB := func() error {
		dbPath := filepath.Join(cfg.Store.Path, "servemedia.db")
		var err error
		database, err = db.Open(dbPath)
		return err
	}
	if err := openDB(); err != nil {
		log.Fatal(err)
	}
	if err := database.FailRunningScanJobs(context.Background(), "Interrupted (server restarted)"); err != nil {
		log.Printf("clear stale scan jobs: %v", err)
	}

	tr := media.NewManager(cfg)
	bak := &backup.Service{Cfg: &cfg, DB: database, StorePath: cfg.Store.Path, DataRoot: filepath.Dir(cfg.Store.Path)}
	meta := &matchmedia.Client{Base: "http://" + cfg.MatchMedia.Addr}

	var srv *server.Server
	reopen := func() error {
		cfg2, err := config.Load(path)
		if err != nil {
			return err
		}
		cfg = cfg2
		media.SetRoot(cfg.ExeDir, cfg.Transcode.FFmpeg)
		if err := openDB(); err != nil {
			return err
		}
		bak.DB = database
		bak.Cfg = &cfg
		bak.StorePath = cfg.Store.Path
		if srv != nil {
			srv.DB = database
			srv.Cfg = &cfg
			srv.Backup = bak
			srv.Meta = meta
			srv.RefreshFetchConfig()
			if srv.Webhooks != nil {
				srv.Webhooks.Refresh(&cfg)
			}
		}
		bak.StartScheduler(context.Background())
		return nil
	}

	srv, err = server.New(&cfg, database, bak, tr, web.FS, meta, reopen)
	if err != nil {
		log.Fatal(err)
	}
	srv.SetRestartMeta(func() error {
		procMu.Lock()
		defer procMu.Unlock()
		if proc != nil {
			_ = proc.Stop()
			proc = nil
		}
		p, err := matchmedia.Start(cfg.MatchMedia.Addr)
		if err != nil {
			return err
		}
		proc = p
		return nil
	})
	var shutdownOnce sync.Once
	shutdownCh := make(chan struct{})
	requestShutdown := func() {
		shutdownOnce.Do(func() { close(shutdownCh) })
	}
	wd := watchdog.New(time.Duration(cfg.Watchdog.TTLSeconds)*time.Second, tr, requestShutdown)
	srv.Watchdog = wd
	go wd.Run()
	srv.MigrateLegacyWebhooks(context.Background())
	bak.StartScheduler(context.Background())

	if cfg.Scan.OnStartup {
		go func() {
			time.Sleep(2 * time.Second)
			st, err := meta.Status()
			if err != nil || !st.Ready {
				log.Printf("scan.on_startup: MatchMedia not ready, skip")
				return
			}
			libs, err := database.ListAllLibraries(context.Background())
			if err != nil {
				return
			}
			for i := range libs {
				lib := libs[i]
				jobID, err := database.CreateScanJob(context.Background(), lib.ID)
				if err != nil {
					continue
				}
				opts := fetch.Opts{ScanJobID: jobID, ScanMode: "nfo"}
				if err := srv.Fetch.MatchLibrary(context.Background(), &lib, opts); err != nil {
					log.Printf("scan.on_startup library %d: %v", lib.ID, err)
					_ = database.UpdateScanJob(context.Background(), jobID, "error", 100, err.Error())
					continue
				}
				_ = database.UpdateScanJob(context.Background(), jobID, "done", 100, "Complete")
			}
		}()
	}

	httpSrv := &http.Server{Addr: cfg.HTTP.Addr, Handler: srv.Router()}
	ln, err := net.Listen("tcp", cfg.HTTP.Addr)
	if err != nil {
		if isAddrInUse(err) {
			procMu.Lock()
			if proc != nil {
				_ = proc.Stop()
			}
			procMu.Unlock()
			openBrowser(uiURL)
			return
		}
		log.Fatal(err)
	}
	go func() {
		log.Printf("servemedia listening on %s", cfg.HTTP.Addr)
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	openBrowser(uiURL)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
	case <-shutdownCh:
		log.Printf("watchdog: all tabs closed, shutting down")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	procMu.Lock()
	if proc != nil {
		_ = proc.Stop()
	}
	procMu.Unlock()
	if err := database.FailRunningScanJobs(context.Background(), "Interrupted (server shutting down)"); err != nil {
		log.Printf("fail running scan jobs: %v", err)
	}
	_ = database.Close()
}

func publicURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1:7676"
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func shouldOpenBrowser() bool {
	if strings.TrimSpace(os.Getenv("SERVEMEDIA_NO_BROWSER")) != "" {
		return false
	}
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

func openBrowser(rawURL string) {
	if !shouldOpenBrowser() {
		return
	}
	if err := exec.Command("xdg-open", rawURL).Start(); err == nil {
		return
	}
	_ = exec.Command("gio", "open", rawURL).Start()
}

func isAddrInUse(err error) bool {
	var op *net.OpError
	if errors.As(err, &op) {
		err = op.Err
	}
	var sys *os.SyscallError
	if errors.As(err, &sys) {
		err = sys.Err
	}
	return errors.Is(err, syscall.EADDRINUSE) || strings.Contains(strings.ToLower(err.Error()), "address already in use")
}

func thirdParty(cfg config.Config) error {
	if strings.TrimSpace(cfg.Vendor.HTMXURL) == "" {
		return fmt.Errorf("vendor URLs missing from config")
	}
	flatpak := strings.TrimSpace(os.Getenv("FLATPAK_ID")) != "" || strings.TrimSpace(os.Getenv("APPIMAGE")) != ""
	vdir := cfg.VendorDir()
	if err := os.MkdirAll(filepath.Join(vdir, "video.js"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(vdir, "codemirror"), 0o755); err != nil {
		return err
	}
	if !flatpak {
		if err := os.MkdirAll(filepath.Join(vdir, "ffmpeg"), 0o755); err != nil {
			return err
		}
	}
	gets := []struct {
		url, dest string
	}{
		{cfg.Vendor.HTMXURL, filepath.Join(vdir, "htmx.min.js")},
		{cfg.Vendor.HTMXLicenseURL, filepath.Join(vdir, "htmx.min.js.LICENSE")},
		{cfg.Vendor.VideoJSJSURL, filepath.Join(vdir, "video.js", "video.min.js")},
		{cfg.Vendor.VideoJSCSSURL, filepath.Join(vdir, "video.js", "video-js.min.css")},
		{cfg.Vendor.VideoJSLicenseURL, filepath.Join(vdir, "video.js", "LICENSE")},
		{cfg.Vendor.HLSURL, filepath.Join(vdir, "hls.min.js")},
		{cfg.Vendor.HLSLicenseURL, filepath.Join(vdir, "hls.min.js.LICENSE")},
		{cfg.Vendor.CodeMirrorJSURL, filepath.Join(vdir, "codemirror", "codemirror.min.js")},
		{cfg.Vendor.CodeMirrorCSSURL, filepath.Join(vdir, "codemirror", "codemirror.min.css")},
		{cfg.Vendor.CodeMirrorYAMLURL, filepath.Join(vdir, "codemirror", "yaml.min.js")},
		{cfg.Vendor.CodeMirrorLicenseURL, filepath.Join(vdir, "codemirror", "LICENSE")},
		{cfg.Vendor.FFmpegLicenseURL, filepath.Join(vdir, "ffmpeg", "LICENSE")},
	}
	for _, g := range gets {
		if g.url == "" {
			continue
		}
		if flatpak && strings.Contains(filepath.ToSlash(g.dest), "/ffmpeg/") {
			continue
		}
		if fileOK(g.dest) {
			continue
		}
		if err := download(g.url, g.dest); err != nil {
			return fmt.Errorf("%s: %w", g.dest, err)
		}
	}
	if flatpak {
		_ = media.EnsureVAAPIProbeClip()
		return nil
	}
	ffmpeg := filepath.Join(vdir, "ffmpeg", "ffmpeg")
	ffprobe := filepath.Join(vdir, "ffmpeg", "ffprobe")
	if !fileOK(ffmpeg) || !fileOK(ffprobe) {
		return fmt.Errorf("vendor/ffmpeg missing; rebuild dist (builder compiles ffmpeg)")
	}
	if err := media.EnsureVAAPIProbeClip(); err != nil {
		return fmt.Errorf("vaapi probe clip: %w", err)
	}
	return nil
}

func download(rawURL, dest string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "servemedia-prepare")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func fileOK(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Size() > 0
}
