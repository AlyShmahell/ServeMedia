package plugins

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alyshmahell/medora/internal/config"
	"github.com/alyshmahell/medora-plugin-sdk/install"
	"github.com/alyshmahell/medora-plugin-sdk/manifest"
)

const (
	DefaultDataDir    = "/data/plugins"
	DefaultBundledDir = "/usr/share/medora/plugins/bundled"
	DefaultRunDir     = "/data/run/plugins"
)

// Installed describes a plugin on disk.
type Installed struct {
	Manifest manifest.Plugin
	Dir      string
	Bundled  bool
	Enabled  bool
}

// Manager discovers, installs, and runs plugin processes.
type Manager struct {
	Cfg        *config.Config
	DataDir    string
	BundledDir string
	RunDir     string

	mu        sync.Mutex
	processes map[string]*exec.Cmd
	installed map[string]Installed
}

func NewManager(cfg *config.Config) *Manager {
	dataDir := DefaultDataDir
	bundled := DefaultBundledDir
	runDir := DefaultRunDir
	return &Manager{
		Cfg:        cfg,
		DataDir:    dataDir,
		BundledDir: bundled,
		RunDir:     runDir,
		processes:  map[string]*exec.Cmd{},
		installed:  map[string]Installed{},
	}
}

func (m *Manager) Refresh(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Cfg = cfg
}

// Start seeds bundled plugins, scans disk, and starts enabled plugins.
func (m *Manager) Start() error {
	if err := SeedBundled(m.BundledDir, m.DataDir); err != nil {
		return err
	}
	if err := m.scanInstalled(); err != nil {
		return err
	}
	return m.startEnabled()
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cmd := range m.processes {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
		delete(m.processes, id)
	}
}

func (m *Manager) List() []Installed {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Installed, 0, len(m.installed))
	for _, p := range m.installed {
		out = append(out, p)
	}
	return out
}

func (m *Manager) Get(id string) (Installed, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.installed[id]
	return p, ok
}

// MetadataSocket returns the unix socket for the enabled metadata plugin.
func (m *Manager) MetadataSocket() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.installed {
		if p.Manifest.Type != "metadata" || !p.Enabled {
			continue
		}
		return filepath.Join(m.RunDir, p.Manifest.ID+".sock")
	}
	return ""
}

// Rescan reloads installed plugins from disk and seeds bundled zips when none are present.
func (m *Manager) Rescan() error {
	if needsSeed(m.DataDir) {
		if err := SeedBundled(m.BundledDir, m.DataDir); err != nil {
			return err
		}
	}
	return m.scanInstalled()
}

func needsSeed(dataDir string) bool {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return true
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if fileExists(filepath.Join(dataDir, e.Name(), "plugin.yaml")) {
			return false
		}
	}
	return true
}

func (m *Manager) scanInstalled() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.installed = map[string]Installed{}
	entries, err := os.ReadDir(m.DataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(m.DataDir, 0o755)
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(m.DataDir, e.Name())
		plug, err := manifest.ParseFile(filepath.Join(dir, "plugin.yaml"))
		if err != nil {
			continue
		}
		enabled := m.isEnabled(plug.ID)
		m.installed[plug.ID] = Installed{
			Manifest: plug,
			Dir:      dir,
			Bundled:  fileExists(filepath.Join(dir, ".bundled")),
			Enabled:  enabled,
		}
	}
	return nil
}

func (m *Manager) isEnabled(id string) bool {
	if m.Cfg == nil || !m.Cfg.Integrations.Plugins.Enabled {
		return id == "providers"
	}
	if inst, ok := m.Cfg.Integrations.Plugins.Installed[id]; ok {
		return inst.Enabled
	}
	return id == "providers"
}

func (m *Manager) startEnabled() error {
	m.mu.Lock()
	ids := make([]string, 0, len(m.installed))
	for id, p := range m.installed {
		if p.Enabled {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		if err := m.startPlugin(id); err != nil {
			return fmt.Errorf("start plugin %s: %w", id, err)
		}
	}
	return nil
}

func (m *Manager) startPlugin(id string) error {
	m.mu.Lock()
	p, ok := m.installed[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("plugin %q not installed", id)
	}
	if err := m.writeRuntimeConfig(p); err != nil {
		return err
	}
	bin := filepath.Join(p.Dir, p.Manifest.Binary)
	socket := filepath.Join(m.RunDir, id+".sock")
	_ = os.Remove(socket)
	_ = os.MkdirAll(m.RunDir, 0o755)
	cmd := exec.Command(bin)
	cmd.Dir = p.Dir
	cmd.Env = append(os.Environ(),
		"MEDORA_PLUGIN_RUNTIME_CONFIG="+filepath.Join(p.Dir, "plugin.runtime.json"),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := waitForSocket(socket, 20*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	m.mu.Lock()
	m.processes[id] = cmd
	m.mu.Unlock()
	go m.waitProcess(id, cmd)
	return nil
}

func (m *Manager) waitProcess(id string, cmd *exec.Cmd) {
	_ = cmd.Wait()
	m.mu.Lock()
	if cur, ok := m.processes[id]; ok && cur == cmd {
		delete(m.processes, id)
	}
	m.mu.Unlock()
}

func (m *Manager) writeRuntimeConfig(p Installed) error {
	socket := filepath.Join(m.RunDir, p.Manifest.ID+".sock")
	stateDir := filepath.Join(p.Dir, "state")
	cfgMap := map[string]any{}
	if m.Cfg != nil {
		if inst, ok := m.Cfg.Integrations.Plugins.Installed[p.Manifest.ID]; ok && inst.Config != nil {
			cfgMap = inst.Config
		}
	}
	runtime := map[string]any{
		"socket":    socket,
		"state_dir": stateDir,
	}
	for k, v := range cfgMap {
		runtime[k] = v
	}
	if _, ok := runtime["omdb"]; !ok {
		runtime["omdb"] = map[string]any{"api_key": "", "base_url": "", "rps": 1.0, "daily_limit": 1000}
	}
	if _, ok := runtime["tvmaze"]; !ok {
		runtime["tvmaze"] = map[string]any{"rps": 2.0, "daily_limit": 0}
	}
	b, err := json.Marshal(runtime)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(p.Dir, "plugin.runtime.json"), b, 0o644)
}

// Reload stops and restarts one plugin (after config or install change).
func (m *Manager) Reload(id string) error {
	m.stopPlugin(id)
	if err := m.scanInstalled(); err != nil {
		return err
	}
	m.mu.Lock()
	p, ok := m.installed[id]
	enabled := ok && p.Enabled
	m.mu.Unlock()
	if !enabled {
		return nil
	}
	return m.startPlugin(id)
}

func (m *Manager) stopPlugin(id string) {
	m.mu.Lock()
	cmd := m.processes[id]
	delete(m.processes, id)
	m.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		time.Sleep(200 * time.Millisecond)
		_ = cmd.Process.Kill()
	}
}

// InstallZip installs or upgrades a plugin from a zip archive.
func (m *Manager) InstallZip(r io.ReaderAt, size int64, bundled bool) (manifest.Plugin, error) {
	tmp, err := os.MkdirTemp(m.DataDir, ".install-")
	if err != nil {
		return manifest.Plugin{}, err
	}
	defer os.RemoveAll(tmp)
	plug, err := install.ExtractZip(r, size, tmp)
	if err != nil {
		return manifest.Plugin{}, err
	}
	dest := filepath.Join(m.DataDir, plug.ID)
	if existing, err := install.ReadManifestVersion(dest); err == nil {
		if plug.Version <= existing.Version {
			return manifest.Plugin{}, fmt.Errorf("plugin %q version %d is not newer than installed %d", plug.ID, plug.Version, existing.Version)
		}
		m.stopPlugin(plug.ID)
		_ = os.RemoveAll(dest)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return manifest.Plugin{}, err
	}
	_ = os.Chmod(filepath.Join(dest, plug.Binary), 0o755)
	if bundled {
		_ = os.WriteFile(filepath.Join(dest, ".bundled"), []byte("1"), 0o644)
	}
	if err := m.scanInstalled(); err != nil {
		return plug, err
	}
	if m.isEnabled(plug.ID) {
		if err := m.startPlugin(plug.ID); err != nil {
			return plug, err
		}
	}
	return plug, nil
}

// InstallZipBytes is a helper for HTTP uploads.
func (m *Manager) InstallZipBytes(b []byte, bundled bool) (manifest.Plugin, error) {
	return m.InstallZip(bytes.NewReader(b), int64(len(b)), bundled)
}

func SeedBundled(bundledDir, dataDir string) error {
	_ = os.MkdirAll(dataDir, 0o755)
	entries, err := os.ReadDir(bundledDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	mgr := &Manager{DataDir: dataDir, BundledDir: bundledDir}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		path := filepath.Join(bundledDir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			return err
		}
		var plug manifest.Plugin
		for _, f := range zr.File {
			if filepath.Base(f.Name) != "plugin.yaml" {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			data, _ := io.ReadAll(rc)
			rc.Close()
			plug, err = manifest.Parse(data)
			if err != nil {
				return err
			}
			break
		}
		if plug.ID == "" {
			continue
		}
		dest := filepath.Join(dataDir, plug.ID)
		existing, err := install.ReadManifestVersion(dest)
		if err == nil && existing.Version >= plug.Version {
			continue
		}
		if _, err := mgr.InstallZipBytes(b, true); err != nil && !strings.Contains(err.Error(), "not newer") {
			return err
		}
	}
	return nil
}

func waitForSocket(socket string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if st, err := os.Stat(socket); err == nil && st.Mode()&os.ModeSocket != 0 {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("plugin socket %s not ready", socket)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
