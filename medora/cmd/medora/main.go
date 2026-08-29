package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alyshmahell/medora/internal/backup"
	"github.com/alyshmahell/medora/internal/config"
	"github.com/alyshmahell/medora/internal/db"
	"github.com/alyshmahell/medora/internal/ffbin"
	"github.com/alyshmahell/medora/internal/matchora"
	"github.com/alyshmahell/medora/internal/prepare"
	"github.com/alyshmahell/medora/internal/scanner"
	"github.com/alyshmahell/medora/internal/server"
	"github.com/alyshmahell/medora/internal/transcode"
	"github.com/alyshmahell/medora/internal/version"
	"github.com/alyshmahell/medora/web"
)

func main() {
	exeDir, err := config.ExeDir()
	if err != nil {
		log.Fatal(err)
	}
	configPath := flag.String("config", "", "path to default.yaml")
	doPrepare := flag.Bool("prepare", false, "fetch third-party vendor if missing, install matchora llama.cpp, then exit")
	flag.Parse()
	path := *configPath
	if path == "" {
		path = filepath.Join(exeDir, "config", "default.yaml")
	}
	cfg, err := config.Load(path)
	if err != nil {
		log.Fatal(err)
	}
	ffbin.SetRoot(cfg.ExeDir, cfg.Transcode.FFmpeg)
	version.Init(cfg.ExeDir)

	matchoraData := filepath.Join(cfg.ExeDir, "data", "matchora")
	if *doPrepare {
		if _, err := os.Stat(cfg.MatchoraBin()); err != nil {
			log.Fatal(err)
		}
		if err := prepare.ThirdParty(cfg); err != nil {
			log.Fatal(err)
		}
		ffbin.SetRoot(cfg.ExeDir, cfg.Transcode.FFmpeg)
		if _, err := matchora.Start(cfg.ExeDir, matchoraData, cfg.Matchora.Addr, matchora.CommonRoot(cfg.MediaRoots()), true); err != nil {
			log.Fatal(err)
		}
		return
	}

	_ = os.MkdirAll(cfg.Store.Path, 0o755)
	_ = os.MkdirAll(filepath.Join(cfg.Store.Path, "metadata", "movies"), 0o755)
	_ = os.MkdirAll(filepath.Join(cfg.Store.Path, "metadata", "tv"), 0o755)
	_ = os.MkdirAll(cfg.Transcode.Path, 0o755)
	_ = os.MkdirAll(cfg.Backup.Dir, 0o755)

	proc, err := matchora.Start(cfg.ExeDir, matchoraData, cfg.Matchora.Addr, matchora.CommonRoot(cfg.MediaRoots()), false)
	if err != nil {
		log.Printf("matchora: %v (metadata may be unavailable)", err)
	}

	var database *db.DB
	openDB := func() error {
		dbPath := filepath.Join(cfg.Store.Path, "medora.db")
		var err error
		database, err = db.Open(dbPath)
		return err
	}
	if err := openDB(); err != nil {
		log.Fatal(err)
	}

	sc := &scanner.Scanner{DB: database, StorePath: cfg.Store.Path, MediaRoot: cfg.PrimaryMediaRoot()}
	tr := transcode.NewManager(cfg)
	bak := &backup.Service{Cfg: &cfg, DB: database, StorePath: cfg.Store.Path, DataRoot: filepath.Dir(cfg.Store.Path)}
	meta := &matchora.Client{Base: "http://" + cfg.Matchora.Addr}

	var srv *server.Server
	reopen := func() error {
		cfg2, err := config.Load(path)
		if err != nil {
			return err
		}
		cfg = cfg2
		ffbin.SetRoot(cfg.ExeDir, cfg.Transcode.FFmpeg)
		if err := openDB(); err != nil {
			return err
		}
		sc.DB = database
		sc.StorePath = cfg.Store.Path
		sc.MediaRoot = cfg.PrimaryMediaRoot()
		bak.DB = database
		bak.Cfg = &cfg
		bak.StorePath = cfg.Store.Path
		if srv != nil {
			srv.DB = database
			srv.Cfg = &cfg
			srv.Backup = bak
			srv.Scanner = sc
			srv.Meta = meta
			srv.RefreshFetchConfig()
			if srv.Webhooks != nil {
				srv.Webhooks.Refresh(&cfg)
			}
			sc.Webhooks = srv
		}
		bak.StartScheduler(context.Background())
		return nil
	}

	srv, err = server.New(&cfg, database, bak, sc, tr, web.FS, meta, reopen)
	if err != nil {
		log.Fatal(err)
	}
	srv.MigrateLegacyWebhooks(context.Background())
	sc.Webhooks = srv
	bak.StartScheduler(context.Background())

	if cfg.Scan.OnStartup {
		go func() {
			time.Sleep(2 * time.Second)
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
				sc.ScanLibrary(context.Background(), &lib, jobID)
			}
		}()
	}

	httpSrv := &http.Server{Addr: cfg.HTTP.Addr, Handler: srv.Router()}
	uiURL := publicURL(cfg.HTTP.Addr)
	ln, err := net.Listen("tcp", cfg.HTTP.Addr)
	if err != nil {
		if isAddrInUse(err) {
			openBrowser(uiURL)
			return
		}
		log.Fatal(err)
	}
	go func() {
		log.Printf("medora listening on %s", cfg.HTTP.Addr)
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	openBrowser(uiURL)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	if proc != nil {
		_ = proc.Stop()
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
	if strings.TrimSpace(os.Getenv("MEDORA_NO_BROWSER")) != "" {
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
