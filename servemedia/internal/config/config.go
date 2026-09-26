package config

import (
	"fmt"
	"os"
	"path/filepath"
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

type MatchMediaConfig struct {
	Addr    string `yaml:"addr"`
	Version string `yaml:"version"`
	URL     string `yaml:"url"`
}

type VendorConfig struct {
	HTMXURL           string `yaml:"htmx_url"`
	HTMXLicenseURL    string `yaml:"htmx_license_url"`
	VideoJSJSURL      string `yaml:"videojs_js_url"`
	VideoJSCSSURL     string `yaml:"videojs_css_url"`
	VideoJSLicenseURL string `yaml:"videojs_license_url"`
	HLSURL            string `yaml:"hls_url"`
	HLSLicenseURL     string `yaml:"hls_license_url"`
	FFmpegSrcURL      string `yaml:"ffmpeg_src_url"`
	X264SrcURL        string `yaml:"x264_src_url"`
	FFmpegLicenseURL  string `yaml:"ffmpeg_license_url"`
}

type Config struct {
	HTTP struct {
		Addr string `yaml:"addr"`
	} `yaml:"http"`
	Store struct {
		Path string `yaml:"path"`
	} `yaml:"store"`
	Media struct {
		Path string `yaml:"path"`
	} `yaml:"media"`
	Transcode struct {
		Path           string `yaml:"path"`
		MaxHeight      int    `yaml:"max_height"`
		CRF            int    `yaml:"crf"`
		SegmentSeconds int    `yaml:"segment_seconds"`
		CleanupHours   int    `yaml:"cleanup_hours"`
		HWAccel        string `yaml:"hwaccel"`
		VAAPIDevice    string `yaml:"vaapi_device"`
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
	MatchMedia   MatchMediaConfig   `yaml:"matchmedia"`
	Vendor       VendorConfig       `yaml:"vendor"`
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

// CacheRoot is $XDG_CACHE_HOME/servemedia (or $HOME/.cache/servemedia).
func CacheRoot() (string, error) {
	return cacheDirDefault()
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

func MatchMediaXDGDataDir() (string, error) {
	return appDir("matchmedia", "XDG_DATA_HOME", filepath.Join(".local", "share"))
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
	c.HTTP.Addr = ":7676"
	c.Store.Path = "store"
	c.Media.Path = "/mnt,/media,$XDG_VIDEOS_DIR,$XDG_MUSIC_DIR"
	c.Transcode.Path = "transcode"
	c.Transcode.MaxHeight = 2160
	c.Transcode.CRF = 23
	c.Transcode.SegmentSeconds = 6
	c.Transcode.CleanupHours = 24
	c.Transcode.HWAccel = "auto"
	c.Backup.Enabled = true
	c.Backup.Interval = 24 * time.Hour
	c.Backup.Retain = 7
	c.Backup.Dir = "backups"
	c.Scan.OnStartup = true
	c.Watchdog.TTLSeconds = 60
	c.MatchMedia.Addr = "127.0.0.1:7680"
	return c
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
		if err := c.resolvePaths(); err != nil {
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
	if strings.TrimSpace(c.MatchMedia.Addr) == "" {
		c.MatchMedia.Addr = "127.0.0.1:7680"
	}
	return nil
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
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func applyEnv(c *Config) {
	if v := os.Getenv("SERVEMEDIA_HTTP_ADDR"); v != "" {
		c.HTTP.Addr = v
	}
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
	dir, err := MatchMediaXDGDataDir()
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
func (c Config) MatchMediaShare() string {
	if inFlatpak() {
		return "/app/tools/matchmedia"
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
