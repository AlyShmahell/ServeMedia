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

type MatchoraConfig struct {
	Addr string `yaml:"addr"`
	URL  string `yaml:"url"`
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
	Integrations IntegrationsConfig `yaml:"integrations"`
	Matchora     MatchoraConfig     `yaml:"matchora"`
	Vendor       VendorConfig       `yaml:"vendor"`
	Version      string             `yaml:"version"`

	ExeDir      string `yaml:"-"`
	ConfigPath  string `yaml:"-"`
	OverlayPath string `yaml:"-"`
}

func ExeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe), nil
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
	return filepath.Join(base, p)
}

func Defaults() Config {
	var c Config
	c.HTTP.Addr = ":7676"
	c.Store.Path = "data/store"
	c.Transcode.Path = "data/transcode"
	c.Transcode.MaxHeight = 2160
	c.Transcode.CRF = 23
	c.Transcode.SegmentSeconds = 6
	c.Transcode.CleanupHours = 24
	c.Transcode.HWAccel = "auto"
	c.Backup.Enabled = true
	c.Backup.Interval = 24 * time.Hour
	c.Backup.Retain = 7
	c.Backup.Dir = "data/backups"
	c.Scan.OnStartup = true
	c.Matchora.Addr = "127.0.0.1:7680"
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
		path = filepath.Join(root, "config", "default.yaml")
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
	c.resolvePaths()
	overlay := filepath.Join(root, "data", "config.yaml")
	c.OverlayPath = overlay
	if b, err := os.ReadFile(overlay); err == nil && len(b) > 0 {
		if err := yaml.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("parse overlay: %w", err)
		}
		c.ExeDir = root
		c.ConfigPath = path
		c.OverlayPath = overlay
		c.resolvePaths()
	}
	c.Version = seedVersion
	applyEnv(&c)
	c.ExeDir = root
	c.resolvePaths()
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

func (c *Config) resolvePaths() {
	base := c.ExeDir
	c.Store.Path = resolvePath(base, c.Store.Path, "data/store")
	c.Transcode.Path = resolvePath(base, c.Transcode.Path, "data/transcode")
	c.Backup.Dir = resolvePath(base, c.Backup.Dir, "data/backups")
	parts := SplitMediaPaths(c.Media.Path)
	if len(parts) > 0 {
		for i, p := range parts {
			if !filepath.IsAbs(p) {
				parts[i] = resolvePath(base, p, "")
			} else {
				parts[i] = filepath.Clean(p)
			}
		}
		c.Media.Path = strings.Join(parts, ",")
	}
	if strings.TrimSpace(c.Matchora.Addr) == "" {
		c.Matchora.Addr = "127.0.0.1:7680"
	}
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
	if v := os.Getenv("MEDORA_HTTP_ADDR"); v != "" {
		c.HTTP.Addr = v
	}
	if v := os.Getenv("MEDORA_STORE_PATH"); v != "" {
		c.Store.Path = v
	}
	if v := os.Getenv("MEDORA_MEDIA_PATH"); v != "" {
		c.Media.Path = v
	}
	if v := os.Getenv("MEDORA_FFMPEG"); v != "" {
		c.Transcode.FFmpeg = v
	}
}

func (c Config) OverlaySavePath() string {
	if strings.TrimSpace(c.OverlayPath) != "" {
		return c.OverlayPath
	}
	return filepath.Join(c.ExeDir, "data", "config.yaml")
}

func (c Config) MatchoraBin() string {
	return filepath.Join(c.ExeDir, "tools", "matchora", "matchora")
}

func (c Config) VendorDir() string {
	return filepath.Join(c.ExeDir, "vendor")
}
