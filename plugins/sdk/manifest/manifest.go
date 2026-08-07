package manifest

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var idRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// Plugin is the plugin.yaml manifest.
type Plugin struct {
	ID      string         `yaml:"id"`
	Name    string         `yaml:"name"`
	Type    string         `yaml:"type"`
	Version int            `yaml:"version"`
	Binary  string         `yaml:"binary"`
	Config  map[string]any `yaml:"config"`
}

func ParseFile(path string) (Plugin, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Plugin{}, err
	}
	return Parse(b)
}

func Parse(b []byte) (Plugin, error) {
	var p Plugin
	if err := yaml.Unmarshal(b, &p); err != nil {
		return Plugin{}, fmt.Errorf("parse plugin.yaml: %w", err)
	}
	return p, Validate(p)
}

func Validate(p Plugin) error {
	p.ID = strings.TrimSpace(p.ID)
	p.Name = strings.TrimSpace(p.Name)
	p.Type = strings.TrimSpace(p.Type)
	p.Binary = strings.TrimSpace(p.Binary)
	if p.ID == "" {
		return fmt.Errorf("plugin.yaml: id is required")
	}
	if !idRe.MatchString(p.ID) {
		return fmt.Errorf("plugin.yaml: invalid id %q", p.ID)
	}
	if p.Name == "" {
		return fmt.Errorf("plugin.yaml: name is required")
	}
	if p.Type == "" {
		return fmt.Errorf("plugin.yaml: type is required")
	}
	if p.Version <= 0 {
		return fmt.Errorf("plugin.yaml: version must be positive")
	}
	if p.Binary == "" {
		return fmt.Errorf("plugin.yaml: binary is required")
	}
	if strings.Contains(p.Binary, "/") || strings.Contains(p.Binary, "..") {
		return fmt.Errorf("plugin.yaml: invalid binary name")
	}
	return nil
}

// SafeExtractPath returns dest path for a zip member or error on zip slip.
func SafeExtractPath(destDir, name string) (string, error) {
	name = strings.TrimPrefix(name, "/")
	if name == "" || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid zip entry %q", name)
	}
	clean := strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "./")
	if clean == "" || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("invalid zip entry %q", name)
	}
	return strings.TrimSuffix(destDir, "/") + "/" + clean, nil
}
