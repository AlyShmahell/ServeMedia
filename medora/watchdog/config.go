package watchdog

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Addr               string
	MedoraBin          string
	MedoraInternalAddr string
	HealthURL          string
	ProxyURL           string
	PulseIdle          time.Duration
	Tick               time.Duration
	SessionTTL         time.Duration
	PulseHint          time.Duration
	WakePoll           time.Duration
	Autostart          bool
}

func LoadConfig() Config {
	internal := envOr("MEDORA_INTERNAL_ADDR", ":7678")
	return Config{
		Addr:               envOr("WATCHDOG_ADDR", ":7676"),
		MedoraBin:          envOr("MEDORA_BIN", "/usr/local/bin/medora"),
		MedoraInternalAddr: internal,
		HealthURL:          envOr("MEDORA_HEALTH_URL", internalHealthURL(internal)),
		ProxyURL:           envOr("MEDORA_PROXY_URL", internalBaseURL(internal)),
		PulseIdle:          envDuration("PULSE_IDLE", 60*time.Second),
		Tick:               envDuration("WATCHDOG_TICK", 10*time.Second),
		SessionTTL:         envDuration("SESSION_TTL", 24*time.Hour),
		PulseHint:          envDuration("PULSE_INTERVAL", 30*time.Second),
		WakePoll:           envDuration("WAKE_POLL_INTERVAL", 750*time.Millisecond),
		Autostart:          os.Getenv("WATCHDOG_AUTOSTART") == "1" || strings.EqualFold(os.Getenv("WATCHDOG_AUTOSTART"), "true"),
	}
}

func internalHealthURL(addr string) string {
	return internalBaseURL(addr) + "/healthz"
}

func internalBaseURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = ":7678"
	}
	if strings.HasPrefix(addr, ":") {
		return fmt.Sprintf("http://127.0.0.1%s", addr)
	}
	if strings.Contains(addr, "://") {
		return strings.TrimSuffix(addr, "/")
	}
	return "http://" + addr
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
