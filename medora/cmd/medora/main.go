package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/alyshmahell/medora/internal/backup"
	"github.com/alyshmahell/medora/internal/config"
	"github.com/alyshmahell/medora/internal/db"
	"github.com/alyshmahell/medora/internal/scanner"
	"github.com/alyshmahell/medora/internal/server"
	"github.com/alyshmahell/medora/internal/transcode"
	"github.com/alyshmahell/medora/web"
)

func main() {
	storePath := envOr("MEDORA_STORE_PATH", "/data/store")
	cfgPath := filepath.Join(storePath, "config.yaml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	if cfg.Store.Path == "" {
		cfg.Store.Path = storePath
	}
	_ = os.MkdirAll(cfg.Store.Path, 0o755)
	_ = os.MkdirAll(filepath.Join(cfg.Store.Path, "metadata", "movies"), 0o755)
	_ = os.MkdirAll(filepath.Join(cfg.Store.Path, "metadata", "tv"), 0o755)
	_ = os.MkdirAll(cfg.Transcode.Path, 0o755)
	_ = os.MkdirAll(cfg.Backup.Dir, 0o755)

	var database *db.DB
	openDB := func() error {
		dbPath := filepath.Join(cfg.Store.Path, "medora.db")
		if err := migrateLegacyDB(dbPath); err != nil {
			return err
		}
		var err error
		database, err = db.Open(dbPath)
		return err
	}
	if err := openDB(); err != nil {
		log.Fatal(err)
	}

	sc := &scanner.Scanner{DB: database, StorePath: cfg.Store.Path, MediaRoot: cfg.Media.Path}
	tr := transcode.NewManager(cfg)
	bak := &backup.Service{Cfg: &cfg, DB: database, StorePath: cfg.Store.Path, DataRoot: filepath.Dir(cfg.Store.Path)}

	var srv *server.Server
	reopen := func() error {
		cfg2, err := config.Load(filepath.Join(cfg.Store.Path, "config.yaml"))
		if err != nil {
			return err
		}
		cfg = cfg2
		if err := openDB(); err != nil {
			return err
		}
		sc.DB = database
		sc.StorePath = cfg.Store.Path
		bak.DB = database
		bak.Cfg = &cfg
		bak.StorePath = cfg.Store.Path
		if srv != nil {
			srv.DB = database
			srv.Cfg = &cfg
			srv.Backup = bak
			srv.Scanner = sc
			srv.RefreshFetchConfig()
		}
		bak.StartScheduler(context.Background())
		return nil
	}

	srv, err = server.New(&cfg, database, bak, sc, tr, web.FS, reopen)
	if err != nil {
		log.Fatal(err)
	}
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
	go func() {
		log.Printf("medora listening on %s", cfg.HTTP.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	_ = database.Close()
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// migrateLegacyDB renames finlet.db → medora.db (and sidecars) once if needed.
func migrateLegacyDB(dbPath string) error {
	if _, err := os.Stat(dbPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	legacy := filepath.Join(filepath.Dir(dbPath), "finlet.db")
	if _, err := os.Stat(legacy); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.Rename(legacy, dbPath); err != nil {
		return err
	}
	for _, suf := range []string{"-wal", "-shm"} {
		old, neu := legacy+suf, dbPath+suf
		if _, err := os.Stat(old); err == nil {
			_ = os.Rename(old, neu)
		}
	}
	log.Printf("migrated store database finlet.db → medora.db")
	return nil
}
