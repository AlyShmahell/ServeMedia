package backup

import (
	"archive/tar"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alyshmahell/medora/internal/config"
	"github.com/alyshmahell/medora/internal/db"
	"github.com/klauspost/compress/zstd"
)

type Service struct {
	Cfg       *config.Config
	DB        *db.DB
	StorePath string
	DataRoot  string // parent of store (usually /data)

	mu        sync.Mutex
	running   bool
	lastMsg   string
	lastOK    bool
	lastAt    time.Time
	cancelSched context.CancelFunc
}

type Status struct {
	Running bool
	LastMsg string
	LastOK  bool
	LastAt  time.Time
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{Running: s.running, LastMsg: s.lastMsg, LastOK: s.lastOK, LastAt: s.lastAt}
}

func (s *Service) StartScheduler(parent context.Context) {
	if s.cancelSched != nil {
		s.cancelSched()
	}
	if !s.Cfg.Backup.Enabled || s.Cfg.Backup.Interval <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancelSched = cancel
	go func() {
		t := time.NewTicker(s.Cfg.Backup.Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := s.RunBackup(ctx); err != nil {
					log.Printf("periodic backup: %v", err)
				}
			}
		}
	}()
}

func (s *Service) RunBackup(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("backup already running")
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	err := s.DB.WithLock(func() error {
		return s.doBackup(ctx)
	})
	s.mu.Lock()
	s.lastAt = time.Now().UTC()
	if err != nil {
		s.lastOK = false
		s.lastMsg = err.Error()
	} else {
		s.lastOK = true
		s.lastMsg = "ok"
	}
	s.mu.Unlock()
	return err
}

func (s *Service) doBackup(ctx context.Context) error {
	_ = ctx
	dir := s.Cfg.Backup.Dir
	if dir == "" {
		dir = "/data/backups"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmpDB := filepath.Join(dir, ".medora-backup-tmp.db")
	_ = os.Remove(tmpDB)
	if err := vacuumInto(s.DB.SQL, tmpDB); err != nil {
		return fmt.Errorf("sqlite snapshot: %w", err)
	}
	defer os.Remove(tmpDB)

	ts := time.Now().UTC().Format("20060102T150405")
	final := filepath.Join(dir, "medora-metadata-"+ts+".tar.zst")
	partial := final + ".partial"
	f, err := os.Create(partial)
	if err != nil {
		return err
	}
	zw, err := zstd.NewWriter(f)
	if err != nil {
		f.Close()
		return err
	}
	tw := tar.NewWriter(zw)

	err = filepath.Walk(s.StorePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(s.StorePath), path)
		if err != nil {
			return err
		}
		// store/...
		if !strings.HasPrefix(rel, "store") {
			rel = filepath.Join("store", filepath.Base(s.StorePath))
			if path != s.StorePath {
				inner, _ := filepath.Rel(s.StorePath, path)
				rel = filepath.Join("store", inner)
			} else {
				rel = "store"
			}
		}
		rel = filepath.ToSlash(rel)
		if info.IsDir() {
			hdr, _ := tar.FileInfoHeader(info, "")
			hdr.Name = rel + "/"
			return tw.WriteHeader(hdr)
		}
		// replace live db with snapshot
		if filepath.Base(path) == "medora.db" || strings.HasSuffix(path, "medora.db-wal") || strings.HasSuffix(path, "medora.db-shm") {
			if filepath.Base(path) != "medora.db" {
				return nil
			}
			st, err := os.Stat(tmpDB)
			if err != nil {
				return err
			}
			hdr, err := tar.FileInfoHeader(st, "")
			if err != nil {
				return err
			}
			hdr.Name = "store/medora.db"
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			in, err := os.Open(tmpDB)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(tw, in)
			in.Close()
			return copyErr
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, in)
		in.Close()
		return copyErr
	})
	if err != nil {
		tw.Close()
		zw.Close()
		f.Close()
		_ = os.Remove(partial)
		return err
	}
	if err := tw.Close(); err != nil {
		zw.Close()
		f.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(partial, final); err != nil {
		return err
	}
	return s.applyRetention(dir)
}

func vacuumInto(sqlDB *sql.DB, dest string) error {
	_, err := sqlDB.Exec(`VACUUM INTO ?`, dest)
	return err
}

func (s *Service) applyRetention(dir string) error {
	retain := s.Cfg.Backup.Retain
	if retain <= 0 {
		retain = 7
	}
	matches, err := filepath.Glob(filepath.Join(dir, "medora-metadata-*.tar.zst"))
	if err != nil {
		return err
	}
	sort.Strings(matches)
	for len(matches) > retain {
		_ = os.Remove(matches[0])
		matches = matches[1:]
	}
	return nil
}

func (s *Service) List() ([]string, error) {
	dir := s.Cfg.Backup.Dir
	matches, err := filepath.Glob(filepath.Join(dir, "medora-metadata-*.tar.zst"))
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	var names []string
	seen := map[string]bool{}
	for _, m := range matches {
		base := filepath.Base(m)
		if seen[base] {
			continue
		}
		seen[base] = true
		names = append(names, base)
	}
	return names, nil
}

func (s *Service) Delete(name string) error {
	name = filepath.Base(name)
	ok := strings.HasPrefix(name, "medora-metadata-") && strings.HasSuffix(name, ".tar.zst")
	if !ok {
		return fmt.Errorf("invalid backup name")
	}
	return os.Remove(filepath.Join(s.Cfg.Backup.Dir, name))
}

// Restore extracts archive into store via store.restoring swap.
// reopen is called after swap to reopen DB and reload config.
func (s *Service) Restore(ctx context.Context, name string, reopen func() error) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("job already running")
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	name = filepath.Base(name)
	src := filepath.Join(s.Cfg.Backup.Dir, name)
	restoring := s.StorePath + ".restoring"
	old := s.StorePath + ".old"
	_ = os.RemoveAll(restoring)
	_ = os.RemoveAll(old)

	if err := extractArchive(src, filepath.Dir(s.StorePath), restoring); err != nil {
		_ = os.RemoveAll(restoring)
		return err
	}
	// extractArchive writes to <parent>/store when archive has store/ prefix into restoring path
	// We extract so that restoring contains the store contents.
	if err := s.DB.Close(); err != nil {
		return err
	}
	if err := os.Rename(s.StorePath, old); err != nil {
		_ = reopen()
		return err
	}
	if err := os.Rename(restoring, s.StorePath); err != nil {
		_ = os.Rename(old, s.StorePath)
		_ = reopen()
		return err
	}
	if err := reopen(); err != nil {
		// try rollback
		_ = os.RemoveAll(s.StorePath)
		_ = os.Rename(old, s.StorePath)
		_ = reopen()
		return err
	}
	_ = os.RemoveAll(old)
	s.mu.Lock()
	s.lastOK = true
	s.lastMsg = "restored " + name
	s.lastAt = time.Now().UTC()
	s.mu.Unlock()
	return nil
}

func extractArchive(archive, dataParent, restoringStore string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := zstd.NewReader(f)
	if err != nil {
		return err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	_ = os.MkdirAll(restoringStore, 0o755)
	foundDB, foundCfg := false, false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(hdr.Name)
		if !strings.HasPrefix(name, "store/") && name != "store" {
			continue
		}
		rel := strings.TrimPrefix(name, "store/")
		target := filepath.Join(restoringStore, rel)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(restoringStore)) {
			return fmt.Errorf("invalid path in archive")
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			out.Close()
			if err != nil {
				return err
			}
			if rel == "medora.db" {
				foundDB = true
			}
			if rel == "config.yaml" {
				foundCfg = true
			}
		}
	}
	_ = dataParent
	if !foundDB || !foundCfg {
		return fmt.Errorf("archive missing medora.db or config.yaml")
	}
	return nil
}
