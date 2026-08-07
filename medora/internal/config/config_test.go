package config_test

import (
	"testing"

	"github.com/alyshmahell/medora/internal/config"
)

func TestEnsureIntegrationDefaults(t *testing.T) {
	cfg := config.Defaults()
	if !cfg.EnsureIntegrationDefaults() {
		t.Fatal("expected defaults to be generated")
	}
	if cfg.Integrations.Webhooks.ServerID == "" {
		t.Fatal("missing server id")
	}
	if cfg.EnsureIntegrationDefaults() {
		t.Fatal("second call should not change")
	}
}
