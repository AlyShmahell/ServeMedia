package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alyshmahell/servemedia/src/internal/backup"
	"github.com/alyshmahell/servemedia/src/internal/config"
	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/fetch"
	"github.com/alyshmahell/servemedia/src/internal/listen"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
	"github.com/alyshmahell/servemedia/src/internal/media"
	"github.com/alyshmahell/servemedia/src/internal/server"
	"github.com/alyshmahell/servemedia/src/internal/watchdog"
	"github.com/alyshmahell/servemedia/src/web"
)

func main() {
	configPath := flag.String("config", "", "path to default.yaml")
	hostFlag := flag.String("host", "", "bind address (default all interfaces)")
	portFlag := flag.String("port", "", "bind port (default 0)")
	webkitFlag := flag.Bool("webkit", false, "open a WebKit window on 127.0.0.1 and an ephemeral port")
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

	_ = os.MkdirAll(cfg.Store.Path, 0o755)
	_ = os.MkdirAll(cfg.Transcode.Path, 0o755)
	_ = os.MkdirAll(cfg.Backup.Dir, 0o755)

	rt, err := runtimePaths()
	if err != nil {
		log.Fatal(err)
	}
	if url, ok := probeRunning(rt); ok {
		if url != "" {
			if err := present(*webkitFlag, url); err != nil {
				log.Fatal(err)
			}
		}
		return
	}

	var meta matchmedia.Service
	if opened, err := matchmedia.Open(); err != nil {
		log.Printf("matchmedia: %v (metadata may be unavailable)", err)
	} else {
		meta = opened
	}

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
	if *webkitFlag {
		if _, err := database.EnsureOverlord(context.Background()); err != nil {
			log.Fatal(err)
		}
	}
	if err := database.FailRunningScanJobs(context.Background(), "Interrupted (server restarted)"); err != nil {
		log.Printf("clear stale scan jobs: %v", err)
	}

	tr := media.NewManager(cfg)
	bak := &backup.Service{Cfg: &cfg, DB: database, StorePath: cfg.Store.Path, DataRoot: filepath.Dir(cfg.Store.Path)}

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
		if *webkitFlag {
			if _, err := database.EnsureOverlord(context.Background()); err != nil {
				return err
			}
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
	srv.Overlord = *webkitFlag
	srv.SetRestartMeta(func() error {
		if meta == nil {
			opened, err := matchmedia.Open()
			if err != nil {
				return err
			}
			meta = opened
			srv.Meta = meta
			srv.RefreshFetchConfig()
			return nil
		}
		return meta.Reload()
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
			if meta == nil {
				log.Printf("scan.on_startup: MatchMedia not ready, skip")
				return
			}
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

	handler := srv.Router()
	uiSup := listen.NewSupervisor()
	netSup := listen.NewSupervisor()
	uiSup.SetHandler(server.WithLoopback(handler))
	netSup.SetHandler(handler)

	var uiURL string
	var actual listen.Addr
	lanUp := false
	if *webkitFlag {
		ln, err := listenTCP("127.0.0.1", "0")
		if err != nil {
			log.Fatal(err)
		}
		uiAddr, err := uiSup.Use(ln)
		if err != nil {
			log.Fatal(err)
		}
		uiURL = uiAddr.OpenURL()
		actual = uiAddr
		if h, p, ok := savedNetwork(cfg); ok {
			bound, note, err := netSup.Bind(h, p)
			if err != nil {
				log.Printf("network: %v", err)
			} else {
				lanUp = true
				actual = bound
				if note != "" {
					log.Printf("network: %s", note)
				}
				wd.SetHoldShutdown(true)
				log.Printf("network listening on %s", bound.OpenURL())
			}
		}
	} else {
		flagHost := strings.TrimSpace(*hostFlag)
		flagPort := strings.TrimSpace(*portFlag)
		var err error
		if flagHost != "" || flagPort != "" {
			ln, lerr := listenTCP(flagHost, flagPort)
			if lerr != nil {
				log.Fatal(lerr)
			}
			actual, err = netSup.Use(ln)
		} else if h, p, ok := savedNetwork(cfg); ok {
			var note string
			actual, note, err = netSup.Bind(h, p)
			if err == nil && note != "" {
				log.Printf("network: %s", note)
			}
		} else {
			ln, lerr := listenTCP("", "")
			if lerr != nil {
				log.Fatal(lerr)
			}
			actual, err = netSup.Use(ln)
		}
		if err != nil {
			log.Fatal(err)
		}
		uiURL = actual.OpenURL()
	}
	if actual.Port == "" || uiURL == "" {
		log.Fatal("no listen address")
	}
	if err := rt.ensure(); err != nil {
		log.Fatal(err)
	}
	publishPort := actual.Port
	if *webkitFlag {
		if ui, ok := uiSup.Current(); ok {
			publishPort = ui.Port
		}
	}
	if err := rt.write(uiURL, publishPort); err != nil {
		log.Fatal(err)
	}
	if srv.Webhooks != nil {
		srv.Webhooks.SetLoopbackURL(uiURL)
	}
	log.Printf("servemedia listening on %s", uiURL)
	flagOverride := !*webkitFlag && (strings.TrimSpace(*hostFlag) != "" || strings.TrimSpace(*portFlag) != "")
	view := server.NetworkBind{Host: actual.Host, Port: actual.Port, URL: actual.OpenURL(), LanUp: lanUp}
	loopbackShown := ""
	if *webkitFlag {
		loopbackShown = uiURL
	}
	desktop := *webkitFlag
	srv.SetNetwork(desktop, loopbackShown, flagOverride, view, func(host string, port int, disable bool) (server.NetworkBind, error) {
		return applyNetwork(netSup, uiSup, wd, rt, desktop, host, port, disable)
	})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	if openUI(*webkitFlag, uiURL, func(rawURL string) error {
		return runWebKit(rawURL, func() { go watchQuit(sig, shutdownCh) })
	}, openBrowser) {
		select {
		case <-sig:
		case <-shutdownCh:
			log.Printf("watchdog: all tabs closed, shutting down")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = uiSup.Shutdown(ctx)
	_ = netSup.Shutdown(ctx)
	rt.cleanup()
	if err := database.FailRunningScanJobs(context.Background(), "Interrupted (server shutting down)"); err != nil {
		log.Printf("fail running scan jobs: %v", err)
	}
	_ = database.Close()
}

func savedNetwork(cfg config.Config) (host, port string, ok bool) {
	if cfg.Network == nil || cfg.Network.Port < 1 || cfg.Network.Port > 65535 {
		return "", "", false
	}
	return strings.TrimSpace(cfg.Network.Host), strconv.Itoa(cfg.Network.Port), true
}

func applyNetwork(netSup, uiSup *listen.Supervisor, wd *watchdog.Watchdog, rt runtimeDir, desktop bool, host string, port int, disable bool) (server.NetworkBind, error) {
	if desktop {
		if disable {
			netSup.Stop()
			wd.SetHoldShutdown(false)
			cur, ok := uiSup.Current()
			if !ok {
				return server.NetworkBind{}, fmt.Errorf("ui listener is down")
			}
			return server.NetworkBind{Host: cur.Host, Port: cur.Port, URL: cur.OpenURL()}, nil
		}
		bound, note, err := netSup.Bind(host, strconv.Itoa(port))
		if err != nil {
			if cur, ok := netSup.Current(); ok {
				return server.NetworkBind{Host: cur.Host, Port: cur.Port, URL: cur.OpenURL(), Note: note, LanUp: true}, err
			}
			ui, _ := uiSup.Current()
			return server.NetworkBind{Host: ui.Host, Port: ui.Port, URL: ui.OpenURL()}, err
		}
		wd.SetHoldShutdown(true)
		return server.NetworkBind{Host: bound.Host, Port: bound.Port, URL: bound.OpenURL(), Note: note, LanUp: true}, nil
	}
	var bound listen.Addr
	var note string
	var err error
	if disable {
		bound, note, err = netSup.Bind("", "7676")
	} else {
		bound, note, err = netSup.Bind(host, strconv.Itoa(port))
	}
	if err != nil {
		if cur, ok := netSup.Current(); ok {
			return server.NetworkBind{Host: cur.Host, Port: cur.Port, URL: cur.OpenURL(), Note: note}, err
		}
		return server.NetworkBind{}, err
	}
	if werr := rt.write(bound.OpenURL(), bound.Port); werr != nil {
		log.Printf("network url file: %v", werr)
	}
	return server.NetworkBind{Host: bound.Host, Port: bound.Port, URL: bound.OpenURL(), Note: note}, nil
}

// webkitBind forces the window onto loopback and a kernel-chosen port.
// --host and --port do not apply in that mode.
func webkitBind(webkit bool, host, port string) (string, string) {
	if webkit {
		return "127.0.0.1", "0"
	}
	return host, port
}

func uiUsesWebKit(webkit bool) bool {
	return webkit
}

// present opens the page. A WebKit window replaces the system browser.
// If that window cannot be created, the browser opens and this process exits.
func present(webkit bool, rawURL string) error {
	openUI(webkit, rawURL, func(rawURL string) error {
		return runWebKit(rawURL, func() {
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sig
				requestQuit()
			}()
		})
	}, openBrowser)
	return nil
}

// openUI opens a WebKit window when webkit is set. A window error opens the
// browser instead. The returned bool is true when the caller must keep the
// process alive until a signal or the watchdog.
func openUI(webkit bool, rawURL string, window func(string) error, browser func(string)) bool {
	if !webkit {
		browser(rawURL)
		return true
	}
	if err := window(rawURL); err != nil {
		log.Printf("webkit: %v; opening a browser", err)
		browser(rawURL)
		return true
	}
	return false
}

func watchQuit(sig <-chan os.Signal, shutdownCh <-chan struct{}) {
	select {
	case <-sig:
	case <-shutdownCh:
		log.Printf("watchdog: all tabs closed, shutting down")
	}
	requestQuit()
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
