package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/alyshmahell/medora-plugin-providers/internal/cascade"
	"github.com/alyshmahell/medora-plugin-providers/internal/omdb"
	"github.com/alyshmahell/medora-plugin-providers/internal/ratelimit"
	"github.com/alyshmahell/medora-plugin-providers/internal/tvmaze"
	"github.com/alyshmahell/medora-plugin-sdk/server"
)

// RuntimeConfig is written by medora before starting the plugin process.
type RuntimeConfig struct {
	Socket   string         `json:"socket"`
	StateDir string         `json:"state_dir"`
	OMDb     OMDbConfig     `json:"omdb"`
	TVmaze   TVmazeConfig   `json:"tvmaze"`
	Cascade  map[string]any `json:"cascade"`
}

type OMDbConfig struct {
	APIKey     string  `json:"api_key"`
	BaseURL    string  `json:"base_url"`
	RPS        float64 `json:"rps"`
	DailyLimit int     `json:"daily_limit"`
}

type TVmazeConfig struct {
	RPS        float64 `json:"rps"`
	DailyLimit int     `json:"daily_limit"`
}

func main() {
	cfgPath := os.Getenv("MEDORA_PLUGIN_RUNTIME_CONFIG")
	if cfgPath == "" {
		cfgPath = "/data/plugins/providers/plugin.runtime.json"
	}
	cfg, err := loadRuntimeConfig(cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	if cfg.Socket == "" {
		cfg.Socket = "/data/run/plugins/providers.sock"
	}
	if cfg.StateDir == "" {
		cfg.StateDir = "/data/plugins/providers/state"
	}
	_ = os.MkdirAll(cfg.StateDir, 0o755)
	_ = os.MkdirAll(filepath.Dir(cfg.Socket), 0o755)

	tvRPS := cfg.TVmaze.RPS
	if tvRPS <= 0 {
		tvRPS = 2
	}
	omRPS := cfg.OMDb.RPS
	if omRPS <= 0 {
		omRPS = 1
	}
	omDaily := cfg.OMDb.DailyLimit
	if omDaily == 0 {
		omDaily = 1000
	}

	cas := &cascade.Cascade{
		TVmaze: &tvmaze.Client{
			Limiter: ratelimit.New("tvmaze", tvRPS, cfg.TVmaze.DailyLimit, cfg.StateDir),
		},
		OMDb: &omdb.Client{
			APIKey:  cfg.OMDb.APIKey,
			BaseURL: cfg.OMDb.BaseURL,
			Limiter: ratelimit.New("omdb", omRPS, omDaily, cfg.StateDir),
		},
	}

	log.Printf("providers plugin listening on %s", cfg.Socket)
	if err := server.ListenAndServe(cfg.Socket, &server.Service{Backend: cas}); err != nil {
		log.Fatal(err)
	}
}

func loadRuntimeConfig(path string) (RuntimeConfig, error) {
	var cfg RuntimeConfig
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}
