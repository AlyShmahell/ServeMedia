package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

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
	Providers struct {
		Socket string `yaml:"socket"`
	} `yaml:"providers"`
	Legal struct {
		Path string `yaml:"path"`
	} `yaml:"legal"`
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
	c.Providers.Socket = "/data/run/providers.sock"
	return c
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
	if v := os.Getenv("MEDORA_PROVIDERS_SOCKET"); v != "" {
		c.Providers.Socket = v
	}
	if v := os.Getenv("MEDORA_LEGAL_PATH"); v != "" {
		c.Legal.Path = v
	}
}
