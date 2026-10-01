package matchmedia

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alyshmahell/servemedia/internal/config"
	"gopkg.in/yaml.v3"
)

type Proc struct {
	cmd *exec.Cmd
}

func Start(addr string) (*Proc, error) {
	cfg := config.Config{}
	bin := cfg.MatchMediaBin()
	if _, err := os.Stat(bin); err != nil {
		return nil, fmt.Errorf("matchmedia binary: %w", err)
	}
	bundled := cfg.MatchMediaShare()
	seed := filepath.Join(bundled, "config", "default.yaml")
	if _, err := os.Stat(seed); err != nil {
		return nil, fmt.Errorf("matchmedia seed: %w", err)
	}
	dataDir, err := config.MatchMediaXDGDataDir()
	if err != nil {
		return nil, err
	}
	if err := seedXDG(bundled, dataDir); err != nil {
		return nil, err
	}
	xdgSeed := filepath.Join(dataDir, "config", "default.yaml")
	if _, err := os.Stat(xdgSeed); err == nil {
		seed = xdgSeed
	}
	if err := writeOverlay(dataDir, seed); err != nil {
		return nil, err
	}
	base := "http://" + addr
	if healthy(base) {
		stopExisting(bin)
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if !healthy(base) && !addrInUse(addr) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	cmd := exec.Command(bin, "-config", seed)
	cmd.Dir = filepath.Dir(bin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = childSysProcAttr()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &Proc{cmd: cmd}
	go func() {
		err := cmd.Wait()
		if err != nil {
			log.Printf("matchmedia exited: %v", err)
		}
	}()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		if healthy(base) {
			return p, nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	_ = p.Stop()
	return nil, fmt.Errorf("matchmedia did not become healthy on %s", addr)
}

func (p *Proc) Stop() error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	pid := p.cmd.Process.Pid
	pgid, err := syscall.Getpgid(pid)
	termGroup := err == nil

	signalGroup := func(sig syscall.Signal) {
		if termGroup {
			_ = syscall.Kill(-pgid, sig)
			return
		}
		_ = p.cmd.Process.Signal(sig)
	}

	signalGroup(syscall.SIGTERM)
	if waitProcessGone(pid, 3*time.Second) {
		return nil
	}
	signalGroup(syscall.SIGKILL)
	if waitProcessGone(pid, time.Second) {
		return nil
	}
	return fmt.Errorf("matchmedia pid %d still alive after SIGKILL", pid)
}

func waitProcessGone(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return syscall.Kill(pid, 0) != nil
}

func within(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if root == string(os.PathSeparator) {
		return path == root || filepath.IsAbs(path)
	}
	if path == root {
		return true
	}
	prefix := root + string(os.PathSeparator)
	return strings.HasPrefix(path, prefix)
}

func overlayDest(dataDir string) string {
	return filepath.Join(dataDir, "config", "overlay.yaml")
}

func loadYAMLMap(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func resolveSeedPath() (string, error) {
	cfg := config.Config{}
	bundled := cfg.MatchMediaShare()
	seed := filepath.Join(bundled, "config", "default.yaml")
	dataDir, err := config.MatchMediaXDGDataDir()
	if err != nil {
		return "", err
	}
	xdgSeed := filepath.Join(dataDir, "config", "default.yaml")
	if _, err := os.Stat(xdgSeed); err == nil {
		return xdgSeed, nil
	}
	if _, err := os.Stat(seed); err != nil {
		return "", fmt.Errorf("matchmedia seed: %w", err)
	}
	return seed, nil
}

// SeedMap is MatchMedia default.yaml as a generic map (for Restore default).
func SeedMap() (map[string]any, error) {
	path, err := resolveSeedPath()
	if err != nil {
		return nil, err
	}
	return loadYAMLMap(path)
}

// RemoveOverlay deletes MatchMedia overlay.yaml if present.
func RemoveOverlay() error {
	dataDir, err := config.MatchMediaXDGDataDir()
	if err != nil {
		return err
	}
	err = os.Remove(overlayDest(dataDir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func seedXDG(srcHome, dataDir string) error {
	if err := os.MkdirAll(filepath.Join(dataDir, "config"), 0o755); err != nil {
		return err
	}
	for _, rel := range []string{
		filepath.Join("config", "default.yaml"),
		"public",
	} {
		src := filepath.Join(srcHome, rel)
		dst := filepath.Join(dataDir, rel)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if _, err := os.Stat(src); err != nil {
			if rel == filepath.Join("config", "default.yaml") {
				return fmt.Errorf("matchmedia seed %s: %w", rel, err)
			}
			continue
		}
		if err := copyTree(src, dst); err != nil {
			return fmt.Errorf("matchmedia seed %s: %w", rel, err)
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode().Perm())
	}
	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, fi.Mode().Perm())
	})
}

func writeOverlay(dataDir, seedPath string) error {
	path := overlayDest(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	out := map[string]any{}
	if seed, err := loadYAMLMap(seedPath); err == nil {
		out = seed
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("matchmedia seed: %w", err)
	}
	if existing, err := loadYAMLMap(path); err == nil {
		mergeOverlay(out, existing)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("matchmedia overlay: %w", err)
	}
	if extra := strings.TrimSpace(os.Getenv("SERVEMEDIA_MATCHMEDIA_OVERLAY")); extra != "" {
		more, err := loadYAMLMap(extra)
		if err != nil {
			return fmt.Errorf("matchmedia overlay: %w", err)
		}
		mergeOverlay(out, more)
	}
	delete(out, "data_dir")
	delete(out, "browse_root")
	body, err := yaml.Marshal(out)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func mergeOverlay(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				mergeOverlay(dm, sm)
				dst[k] = dm
				continue
			}
		}
		dst[k] = v
	}
}

func stopExisting(bin string) {
	want, err := filepath.EvalSymlinks(bin)
	if err != nil {
		want = bin
	}
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err != nil {
			continue
		}
		exe = strings.TrimSuffix(exe, " (deleted)")
		resolved, err := filepath.EvalSymlinks(exe)
		if err != nil {
			resolved = exe
		}
		if resolved != want && exe != want && exe != bin {
			continue
		}
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
}

func healthy(base string) bool {
	req, err := http.NewRequest(http.MethodGet, base+"/health", nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

func addrInUse(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
