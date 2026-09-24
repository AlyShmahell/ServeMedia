package matchmedia

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// MatchMedia v0.0.8 merges this file before {data_dir}/config.yaml.
const flatpakRunOverlay = "/run/matchmedia/config.yaml"

type Proc struct {
	cmd *exec.Cmd
}

func Start(exeDir, dataDir, addr, browseRoot string) (*Proc, error) {
	srcHome := filepath.Join(exeDir, "tools", "matchmedia")
	bin := filepath.Join(srcHome, "matchmedia")
	if _, err := os.Stat(bin); err != nil {
		return nil, fmt.Errorf("matchmedia binary: %w", err)
	}
	matchmediaHome := srcHome
	if strings.TrimSpace(os.Getenv("APPIMAGE")) != "" {
		home, err := prepareAppImageHome(srcHome, dataDir)
		if err != nil {
			return nil, err
		}
		matchmediaHome = home
		bin = filepath.Join(matchmediaHome, "matchmedia")
	}
	_ = os.RemoveAll(filepath.Join(matchmediaHome, "vendor"))
	if err := writeOverlay(matchmediaHome, dataDir, addr, browseRoot); err != nil {
		return nil, err
	}
	base := "http://" + addr
	if healthy(base) {
		stopExisting(bin)
		deadline := time.Now().Add(8 * time.Second)
		for healthy(base) && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
	}
	cmd := exec.Command(bin)
	cmd.Dir = matchmediaHome
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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
	pgid, err := syscall.Getpgid(p.cmd.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
	}
	return nil
}

// CommonRoot is a directory that contains every path (or "/" if they are disjoint).
func CommonRoot(paths []string) string {
	var cleaned []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		cleaned = append(cleaned, filepath.Clean(p))
	}
	if len(cleaned) == 0 {
		return "/"
	}
	root := cleaned[0]
	for _, p := range cleaned[1:] {
		for root != string(os.PathSeparator) && root != "." && !within(root, p) {
			parent := filepath.Dir(root)
			if parent == root {
				return string(os.PathSeparator)
			}
			root = parent
		}
	}
	if root == "." {
		return string(os.PathSeparator)
	}
	return root
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

func overlayDest(matchmediaHome, _ string) string {
	if strings.TrimSpace(os.Getenv("FLATPAK_ID")) != "" {
		return flatpakRunOverlay
	}
	return filepath.Join(matchmediaHome, "data", "config.yaml")
}

func prepareAppImageHome(srcHome, dataDir string) (string, error) {
	dest := filepath.Join(filepath.Dir(filepath.Dir(dataDir)), "matchmedia-home")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	for _, name := range []string{"matchmedia", "config", "public"} {
		src := filepath.Join(srcHome, name)
		if _, err := os.Stat(src); err != nil {
			if name == "matchmedia" {
				return "", fmt.Errorf("matchmedia binary: %w", err)
			}
			continue
		}
		if err := copyTree(src, filepath.Join(dest, name)); err != nil {
			return "", fmt.Errorf("matchmedia home %s: %w", name, err)
		}
	}
	if err := os.Chmod(filepath.Join(dest, "matchmedia"), 0o755); err != nil {
		return "", err
	}
	return dest, nil
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

func writeOverlay(matchmediaHome, dataDir, addr, browseRoot string) error {
	path := overlayDest(matchmediaHome, dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	if strings.TrimSpace(browseRoot) == "" {
		browseRoot = "/"
	}
	out := map[string]any{}
	if extra := strings.TrimSpace(os.Getenv("SERVEMEDIA_MATCHMEDIA_OVERLAY")); extra != "" {
		more, err := os.ReadFile(extra)
		if err != nil {
			return fmt.Errorf("matchmedia overlay: %w", err)
		}
		if err := yaml.Unmarshal(more, &out); err != nil {
			return fmt.Errorf("matchmedia overlay: %w", err)
		}
		if out == nil {
			out = map[string]any{}
		}
	}
	mergeOverlay(out, map[string]any{
		"http":        map[string]any{"addr": addr},
		"data_dir":    dataDir,
		"browse_root": browseRoot,
	})
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
