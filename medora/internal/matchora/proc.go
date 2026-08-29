package matchora

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

type Proc struct {
	cmd *exec.Cmd
}

func Start(exeDir, dataDir, addr, browseRoot string, prepare bool) (*Proc, error) {
	bin := filepath.Join(exeDir, "tools", "matchora", "matchora")
	if _, err := os.Stat(bin); err != nil {
		return nil, fmt.Errorf("matchora binary: %w", err)
	}
	matchoraHome := filepath.Join(exeDir, "tools", "matchora")
	if err := writeOverlay(matchoraHome, dataDir, addr, browseRoot); err != nil {
		return nil, err
	}
	base := "http://" + addr
	if prepare {
		cmd := exec.Command(bin, "--prepare")
		cmd.Dir = matchoraHome
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("matchora --prepare: %w", err)
		}
		return nil, nil
	}
	if healthy(base) {
		stopExisting(bin)
		deadline := time.Now().Add(8 * time.Second)
		for healthy(base) && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
	}
	cmd := exec.Command(bin)
	cmd.Dir = matchoraHome
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
			log.Printf("matchora exited: %v", err)
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
	return nil, fmt.Errorf("matchora did not become healthy on %s", addr)
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

func writeOverlay(matchoraHome, dataDir, addr, browseRoot string) error {
	overlayDir := filepath.Join(matchoraHome, "data")
	if err := os.MkdirAll(overlayDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	if strings.TrimSpace(browseRoot) == "" {
		browseRoot = "/"
	}
	out := map[string]any{}
	if extra := strings.TrimSpace(os.Getenv("MEDORA_MATCHORA_OVERLAY")); extra != "" {
		more, err := os.ReadFile(extra)
		if err != nil {
			return fmt.Errorf("matchora overlay: %w", err)
		}
		if err := yaml.Unmarshal(more, &out); err != nil {
			return fmt.Errorf("matchora overlay: %w", err)
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
	return os.WriteFile(filepath.Join(overlayDir, "config.yaml"), body, 0o644)
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
