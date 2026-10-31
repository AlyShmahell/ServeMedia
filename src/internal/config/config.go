package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

const appName = "servemedia"

type WebhookHeader struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}

type WebhookDestination struct {
	Name              string          `yaml:"name"`
	URL               string          `yaml:"url"`
	Enabled           bool            `yaml:"enabled"`
	NotificationTypes []string        `yaml:"notification_types"`
	ItemTypes         []string        `yaml:"item_types"`
	Headers           []WebhookHeader `yaml:"headers"`
	Template          string          `yaml:"template"`
}

type WebhooksConfig struct {
	Enabled      bool                 `yaml:"enabled"`
	ServerURL    string               `yaml:"server_url"`
	ServerID     string               `yaml:"server_id"`
	APIKey       string               `yaml:"api_key"`
	Destinations []WebhookDestination `yaml:"destinations"`
}

type IntegrationsConfig struct {
	Webhooks WebhooksConfig `yaml:"webhooks"`
}

type Config struct {
	Store struct {
		Path string `yaml:"path"`
	} `yaml:"store"`
	Media struct {
		Path string `yaml:"path"`
	} `yaml:"media"`
	Transcode struct {
		Path           string `yaml:"path"`
		Heights        []int  `yaml:"heights,omitempty"`
		MaxHeight      int    `yaml:"max_height,omitempty"`
		CRF            int    `yaml:"crf"`
		SegmentSeconds int    `yaml:"segment_seconds"`
		CleanupHours   int    `yaml:"cleanup_hours"`
		HWAccel        string `yaml:"hwaccel"`
		FFmpeg         string `yaml:"ffmpeg"`
	} `yaml:"transcode"`
	Backup struct {
		Enabled  bool          `yaml:"enabled"`
		Interval time.Duration `yaml:"interval"`
		Retain   int           `yaml:"retain"`
		Dir      string        `yaml:"dir"`
	} `yaml:"backup"`
	Scan struct {
		OnStartup bool `yaml:"on_startup"`
	} `yaml:"scan"`
	Watchdog struct {
		TTLSeconds int `yaml:"ttl_seconds"`
	} `yaml:"watchdog"`
	Integrations IntegrationsConfig `yaml:"integrations"`
	Version      string             `yaml:"version"`

	ExeDir      string `yaml:"-"`
	ConfigPath  string `yaml:"-"`
	OverlayPath string `yaml:"-"`
}

func xdgBase(envKey, homeRel string) (string, error) {
	if v := strings.TrimSpace(os.Getenv(envKey)); filepath.IsAbs(v) {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" || !filepath.IsAbs(home) {
		return "", fmt.Errorf("%s unset and HOME unavailable", envKey)
	}
	return filepath.Join(home, homeRel), nil
}

func appDir(name, envKey, homeRel string) (string, error) {
	base, err := xdgBase(envKey, homeRel)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, name), nil
}

func dataDirDefault() (string, error) {
	return appDir(appName, "XDG_DATA_HOME", filepath.Join(".local", "share"))
}

func cacheDirDefault() (string, error) {
	return appDir(appName, "XDG_CACHE_HOME", ".cache")
}

func inFlatpak() bool {
	return strings.TrimSpace(os.Getenv("FLATPAK_ID")) != ""
}

func dataRoot() (string, error) {
	if home := strings.TrimSpace(os.Getenv("SERVEMEDIA_HOME")); home != "" {
		return filepath.Clean(home), nil
	}
	return dataDirDefault()
}

// ExeDir is the payload root: SERVEMEDIA_ROOT, else the XDG data dir.
func ExeDir() (string, error) {
	if root := strings.TrimSpace(os.Getenv("SERVEMEDIA_ROOT")); root != "" {
		return filepath.Clean(root), nil
	}
	return dataDirDefault()
}

// DataRoot is the writable data dir: SERVEMEDIA_HOME, else the XDG data dir.
func DataRoot() (string, error) {
	return dataRoot()
}

func DefaultConfigPath() (string, error) {
	dir, err := ExeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config", "default.yaml"), nil
}

func overlayPath() (string, error) {
	dir, err := dataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config", "overlay.yaml"), nil
}

// MatchMediaDir is the linked MatchMedia data directory under ServeMedia's own data root.
func MatchMediaDir() (string, error) {
	root, err := dataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "matchmedia"), nil
}

func resolvePath(base, p, fallback string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		p = fallback
	}
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return p
	}
	if base == "" {
		return p
	}
	return filepath.Join(base, p)
}

func Defaults() Config {
	var c Config
	c.Store.Path = "store"
	c.Media.Path = "/mnt,/media,$XDG_VIDEOS_DIR,$XDG_MUSIC_DIR"
	c.Transcode.Path = "transcode"
	c.Transcode.Heights = StandardHeights()
	c.Transcode.MaxHeight = 2160
	c.Transcode.CRF = 23
	c.Transcode.SegmentSeconds = 6
	c.Transcode.CleanupHours = 24
	c.Transcode.HWAccel = "auto"
	c.Backup.Enabled = false
	c.Backup.Interval = 24 * time.Hour
	c.Backup.Retain = 7
	c.Backup.Dir = "backups"
	c.Scan.OnStartup = false
	c.Watchdog.TTLSeconds = 60
	return c
}

// StandardHeights is the ladder offered as toggles in settings.
func StandardHeights() []int {
	return []int{2160, 1440, 1080, 720, 480, 360, 240, 144}
}

// NormalizeHeights keeps positive unique heights, highest first.
func NormalizeHeights(in []int) []int {
	seen := map[int]struct{}{}
	out := make([]int, 0, len(in))
	for _, h := range in {
		if h <= 0 {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] > out[j] })
	return out
}

// HeightsFromLegacyMax turns a saved max height into the allowlist it used to imply.
// A value that is not a standard rung is kept as a custom height.
func HeightsFromLegacyMax(max int) []int {
	if max <= 0 {
		return StandardHeights()
	}
	var out []int
	matched := false
	for _, h := range StandardHeights() {
		if h == max {
			matched = true
		}
		if h <= max {
			out = append(out, h)
		}
	}
	if !matched {
		out = append(out, max)
	}
	return NormalizeHeights(out)
}

func applyOverlayHeights(c *Config, overlay []byte) error {
	var probe struct {
		Transcode struct {
			Heights   *[]int `yaml:"heights"`
			MaxHeight *int   `yaml:"max_height"`
		} `yaml:"transcode"`
	}
	if err := yaml.Unmarshal(overlay, &probe); err != nil {
		return fmt.Errorf("parse overlay heights: %w", err)
	}
	if probe.Transcode.Heights != nil {
		c.Transcode.Heights = NormalizeHeights(*probe.Transcode.Heights)
		return nil
	}
	if probe.Transcode.MaxHeight != nil {
		c.Transcode.Heights = HeightsFromLegacyMax(*probe.Transcode.MaxHeight)
	}
	return nil
}

func (c *Config) EnsureIntegrationDefaults() bool {
	changed := false
	if c.Integrations.Webhooks.ServerID == "" {
		c.Integrations.Webhooks.ServerID = uuid.NewString()
		changed = true
	}
	return changed
}

func Load(path string) (Config, error) {
	path = strings.TrimSpace(path)
	c := Defaults()
	root, err := ExeDir()
	if err != nil {
		return c, err
	}
	c.ExeDir = root
	if path == "" {
		path, err = DefaultConfigPath()
		if err != nil {
			return c, err
		}
	}
	c.ConfigPath = path
	if b, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("parse config: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return c, err
	}
	seedVersion := strings.TrimSpace(c.Version)
	c.ExeDir = root
	c.pinInternalPaths()
	if err := c.resolvePaths(); err != nil {
		return c, err
	}
	overlay, err := overlayPath()
	if err != nil {
		return c, err
	}
	c.OverlayPath = overlay
	if b, err := os.ReadFile(overlay); err == nil && len(b) > 0 {
		if err := yaml.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("parse overlay: %w", err)
		}
		c.ExeDir = root
		c.ConfigPath = path
		c.OverlayPath = overlay
		c.pinInternalPaths()
		if err := c.resolvePaths(); err != nil {
			return c, err
		}
		if err := applyOverlayHeights(&c, b); err != nil {
			return c, err
		}
	}
	c.Version = seedVersion
	applyEnv(&c)
	c.ExeDir = root
	if err := c.resolvePaths(); err != nil {
		return c, err
	}
	c.EnsureIntegrationDefaults()
	return c, nil
}

func SplitMediaPaths(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (c Config) MediaRoots() []string {
	roots := SplitMediaPaths(c.Media.Path)
	if len(roots) == 0 {
		return []string{"/media"}
	}
	cleaned := make([]string, len(roots))
	for i, r := range roots {
		cleaned[i] = filepath.Clean(r)
	}
	return cleaned
}

func (c Config) PrimaryMediaRoot() string {
	return c.MediaRoots()[0]
}

func (c *Config) resolvePaths() error {
	data, err := dataRoot()
	if err != nil {
		return err
	}
	cache, err := cacheDirDefault()
	if err != nil {
		return err
	}
	c.Store.Path = resolvePath(data, c.Store.Path, "store")
	c.Transcode.Path = resolvePath(cache, c.Transcode.Path, "transcode")
	c.Backup.Dir = resolvePath(data, c.Backup.Dir, "backups")
	parts := SplitMediaPaths(c.Media.Path)
	if len(parts) > 0 {
		for i, p := range parts {
			parts[i] = expandMediaEntry(p)
			if parts[i] != "" && !filepath.IsAbs(parts[i]) {
				parts[i] = resolvePath(data, parts[i], "")
			}
		}
		var kept []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" && p != "." {
				kept = append(kept, filepath.Clean(p))
			}
		}
		c.Media.Path = strings.Join(kept, ",")
	}
	return nil
}

// pinInternalPaths drops store, transcode cache, and backup locations that
// arrived from YAML. Those paths are code defaults, resolved afterward.
// SERVEMEDIA_STORE_PATH is applied later and is left alone.
func (c *Config) pinInternalPaths() {
	c.Store.Path = "store"
	c.Transcode.Path = "transcode"
	c.Backup.Dir = "backups"
}

func expandMediaEntry(entry string) string {
	entry = strings.Trim(strings.TrimSpace(entry), `"'`)
	switch entry {
	case "$XDG_VIDEOS_DIR", "${XDG_VIDEOS_DIR}":
		return userMediaDir("XDG_VIDEOS_DIR", "Videos")
	case "$XDG_MUSIC_DIR", "${XDG_MUSIC_DIR}":
		return userMediaDir("XDG_MUSIC_DIR", "Music")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" || !filepath.IsAbs(home) {
		home = ""
	}
	if home != "" {
		entry = strings.ReplaceAll(entry, "${HOME}", home)
		entry = strings.ReplaceAll(entry, "$HOME", home)
	}
	return entry
}

func userMediaDir(key, fallback string) string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" || !filepath.IsAbs(home) {
		return ""
	}
	raw := userDirValue(key)
	if raw == "" {
		return filepath.Join(home, fallback)
	}
	raw = strings.ReplaceAll(raw, "${HOME}", home)
	raw = strings.ReplaceAll(raw, "$HOME", home)
	if filepath.IsAbs(raw) {
		return raw
	}
	return filepath.Join(home, raw)
}

func userDirValue(key string) string {
	base, err := xdgBase("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(base, "user-dirs.dirs"))
	if err != nil {
		return ""
	}
	prefix := key + "="
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.HasPrefix(line, prefix) {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		return strings.Trim(val, `"'`)
	}
	return ""
}

type settingsOverlay struct {
	Media *struct {
		Path string `yaml:"path"`
	} `yaml:"media,omitempty"`
	Transcode struct {
		Heights        []int  `yaml:"heights"`
		CRF            int    `yaml:"crf"`
		SegmentSeconds int    `yaml:"segment_seconds"`
		CleanupHours   int    `yaml:"cleanup_hours"`
		HWAccel        string `yaml:"hwaccel"`
	} `yaml:"transcode"`
	Backup struct {
		Enabled  bool          `yaml:"enabled"`
		Interval time.Duration `yaml:"interval"`
		Retain   int           `yaml:"retain"`
	} `yaml:"backup"`
	Scan struct {
		OnStartup bool `yaml:"on_startup"`
	} `yaml:"scan"`
	Watchdog struct {
		TTLSeconds int `yaml:"ttl_seconds"`
	} `yaml:"watchdog"`
	Integrations IntegrationsConfig `yaml:"integrations"`
}

func (c Config) settingsDocument(mediaPath string) settingsOverlay {
	var doc settingsOverlay
	if strings.TrimSpace(mediaPath) != "" {
		doc.Media = &struct {
			Path string `yaml:"path"`
		}{Path: mediaPath}
	}
	doc.Transcode.Heights = NormalizeHeights(c.Transcode.Heights)
	doc.Transcode.CRF = c.Transcode.CRF
	doc.Transcode.SegmentSeconds = c.Transcode.SegmentSeconds
	doc.Transcode.CleanupHours = c.Transcode.CleanupHours
	doc.Transcode.HWAccel = c.Transcode.HWAccel
	doc.Backup.Enabled = c.Backup.Enabled
	doc.Backup.Interval = c.Backup.Interval
	doc.Backup.Retain = c.Backup.Retain
	doc.Scan.OnStartup = c.Scan.OnStartup
	doc.Watchdog.TTLSeconds = c.Watchdog.TTLSeconds
	doc.Integrations = c.Integrations
	return doc
}

func overlayMediaPath(path string) string {
	b, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return ""
	}
	var prev struct {
		Media struct {
			Path string `yaml:"path"`
		} `yaml:"media"`
	}
	if err := yaml.Unmarshal(b, &prev); err != nil {
		return ""
	}
	return strings.TrimSpace(prev.Media.Path)
}

func (c Config) Save(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		path = c.OverlayPath
	}
	if path == "" {
		return fmt.Errorf("no overlay path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(c.settingsDocument(overlayMediaPath(path)))
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func applyEnv(c *Config) {
	if v := os.Getenv("SERVEMEDIA_STORE_PATH"); v != "" {
		c.Store.Path = v
	}
	if v := os.Getenv("SERVEMEDIA_MEDIA_PATH"); v != "" {
		c.Media.Path = v
	}
	if v := os.Getenv("SERVEMEDIA_FFMPEG"); v != "" {
		c.Transcode.FFmpeg = v
	}
}

func (c Config) OverlaySavePath() string {
	if strings.TrimSpace(c.OverlayPath) != "" {
		return c.OverlayPath
	}
	path, err := overlayPath()
	if err != nil {
		return ""
	}
	return path
}

func (c Config) MatchMediaDataDir() string {
	dir, err := MatchMediaDir()
	if err != nil {
		return ""
	}
	return dir
}

func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

// MatchMediaBin is the MatchMedia binary: sibling of servemedia under .local/bin,
// or /app/tools/matchmedia/matchmedia in Flatpak.
func (c Config) MatchMediaBin() string {
	if inFlatpak() {
		return "/app/tools/matchmedia/matchmedia"
	}
	if dir := executableDir(); dir != "" {
		return filepath.Join(dir, "matchmedia")
	}
	return "matchmedia"
}

// MatchMediaShare is the bundled MatchMedia share tree (.local/share/matchmedia),
// or /app/tools/matchmedia in Flatpak.
// When SERVEMEDIA_ROOT is set (AppImage hook), share is the sibling matchmedia
// directory under the same parent as the servemedia payload.
func (c Config) MatchMediaShare() string {
	if inFlatpak() {
		return "/app/tools/matchmedia"
	}
	if root := strings.TrimSpace(os.Getenv("SERVEMEDIA_ROOT")); root != "" {
		return filepath.Clean(filepath.Join(filepath.Dir(root), "matchmedia"))
	}
	if dir := executableDir(); dir != "" {
		return filepath.Clean(filepath.Join(dir, "..", "share", "matchmedia"))
	}
	return ""
}

func (c Config) MatchMediaSeed() string {
	return filepath.Join(c.MatchMediaShare(), "config", "default.yaml")
}

func (c Config) VendorDir() string {
	return filepath.Join(c.ExeDir, "vendor")
}
