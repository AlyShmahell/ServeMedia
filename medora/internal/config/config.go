package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
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

type PluginInstalledConfig struct {
	Enabled bool           `yaml:"enabled"`
	Config  map[string]any `yaml:"config"`
}

type PluginsConfig struct {
	Enabled   bool                             `yaml:"enabled"`
	Installed map[string]PluginInstalledConfig `yaml:"installed"`
}

type IntegrationsConfig struct {
	Webhooks WebhooksConfig `yaml:"webhooks"`
	Plugins  PluginsConfig  `yaml:"plugins"`
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
		HWAccel        string `yaml:"hwaccel"`      // auto | vaapi | none
		VAAPIDevice    string `yaml:"vaapi_device"` // empty = first /dev/dri/renderD*
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
	Legal struct {
		Path string `yaml:"path"`
	} `yaml:"legal"`
	Integrations IntegrationsConfig `yaml:"integrations"`
}

func Defaults() Config {
	var c Config
	c.HTTP.Addr = ":7676"
	c.Store.Path = "/data/store"
	c.Media.Path = "/media"
	c.Legal.Path = "/legal"
	c.Transcode.Path = "/data/transcode"
	c.Transcode.MaxHeight = 2160
	c.Transcode.CRF = 23
	c.Transcode.SegmentSeconds = 6
	c.Transcode.CleanupHours = 24
	c.Transcode.HWAccel = "auto"
	c.Transcode.VAAPIDevice = ""
	c.Backup.Enabled = true
	c.Backup.Interval = 24 * time.Hour
	c.Backup.Retain = 7
	c.Backup.Dir = "/data/backups"
	c.Scan.OnStartup = true
	c.Integrations.Plugins.Enabled = true
	if c.Integrations.Plugins.Installed == nil {
		c.Integrations.Plugins.Installed = map[string]PluginInstalledConfig{}
	}
	if _, ok := c.Integrations.Plugins.Installed["providers"]; !ok {
		c.Integrations.Plugins.Installed["providers"] = PluginInstalledConfig{
			Enabled: true,
			Config: map[string]any{
				"omdb":   map[string]any{"api_key": "", "base_url": "", "rps": 1.0, "daily_limit": 1000},
				"tvmaze": map[string]any{"rps": 2.0, "daily_limit": 0},
			},
		}
	}
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

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func Load(path string) (Config, error) {
	c := Defaults()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnv(&c)
			return c, nil
		}
		return c, err
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("parse config: %w", err)
	}
	applyEnv(&c)
	c.EnsureIntegrationDefaults()
	return c, nil
}

func (c Config) Save(path string) error {
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
	if v := os.Getenv("MEDORA_LEGAL_PATH"); v != "" {
		c.Legal.Path = v
	}
	// Legacy env for OMDb key until set via plugin settings.
	if v := os.Getenv("OMDB_API_KEY"); v != "" {
		c.ensurePluginConfig("providers")
		if omdb, ok := c.Integrations.Plugins.Installed["providers"].Config["omdb"].(map[string]any); ok {
			if strAny(omdb["api_key"]) == "" {
				omdb["api_key"] = v
			}
		}
	}
	if v := os.Getenv("OMDB_BASE_URL"); v != "" {
		c.ensurePluginConfig("providers")
		if omdb, ok := c.Integrations.Plugins.Installed["providers"].Config["omdb"].(map[string]any); ok {
			if strAny(omdb["base_url"]) == "" {
				omdb["base_url"] = v
			}
		}
	}
}

func (c *Config) ensurePluginConfig(id string) {
	if c.Integrations.Plugins.Installed == nil {
		c.Integrations.Plugins.Installed = map[string]PluginInstalledConfig{}
	}
	if _, ok := c.Integrations.Plugins.Installed[id]; !ok {
		c.Integrations.Plugins.Installed[id] = PluginInstalledConfig{Enabled: true, Config: map[string]any{}}
	}
	if c.Integrations.Plugins.Installed[id].Config == nil {
		inst := c.Integrations.Plugins.Installed[id]
		inst.Config = map[string]any{}
		c.Integrations.Plugins.Installed[id] = inst
	}
}

func strAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
