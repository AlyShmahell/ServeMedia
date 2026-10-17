package matchmedia

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/config"
	"gopkg.in/yaml.v3"
)

func within(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if root == string(os.PathSeparator) {
		return path == root || filepath.IsAbs(path)
	}
	if path == root {
		return true
	}
	prefix := root + string(os.PathSeparator)
	return strings.HasPrefix(path, prefix)
}

func overlayDest(dataDir string) string {
	return filepath.Join(dataDir, "config", "overlay.yaml")
}

func loadYAMLMap(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func resolveSeedPath() (string, error) {
	cfg := config.Config{}
	bundled := cfg.MatchMediaShare()
	seed := filepath.Join(bundled, "config", "default.yaml")
	dataDir, err := config.MatchMediaXDGDataDir()
	if err != nil {
		return "", err
	}
	xdgSeed := filepath.Join(dataDir, "config", "default.yaml")
	if _, err := os.Stat(xdgSeed); err == nil {
		return xdgSeed, nil
	}
	if _, err := os.Stat(seed); err != nil {
		return "", fmt.Errorf("matchmedia seed: %w", err)
	}
	return seed, nil
}

// SeedMap is an empty overlay patch. Provider API keys are kept separately.
func SeedMap() (map[string]any, error) {
	return map[string]any{}, nil
}

// APIKeyPatch keeps TMDb and OMDb keys from an overlay document.
func APIKeyPatch(m map[string]any) map[string]any {
	out := map[string]any{}
	providers, _ := m["providers"].(map[string]any)
	kept := map[string]any{}
	for _, name := range []string{"tmdb", "omdb"} {
		spec, _ := providers[name].(map[string]any)
		if spec == nil {
			continue
		}
		key, _ := spec["api_key"].(string)
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		kept[name] = map[string]any{"api_key": key}
	}
	if len(kept) > 0 {
		out["providers"] = kept
	}
	return out
}

func sidecarDocument(m map[string]any) bool {
	if m == nil {
		return false
	}
	if _, ok := m["browse_roots"]; ok {
		return true
	}
	if _, ok := m["scan"]; ok {
		return true
	}
	if _, ok := m["session"]; ok {
		return true
	}
	if fmt.Sprint(m["version"]) == "0.1.0" {
		return true
	}
	if httpMap, ok := m["http"].(map[string]any); ok {
		if _, ok := httpMap["addr"]; ok {
			return true
		}
	}
	if group, ok := m["group"].(map[string]any); ok {
		if _, ok := group["extras"]; ok {
			return true
		}
	}
	return false
}

func scrubSidecarFile(path string) error {
	m, err := loadYAMLMap(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !sidecarDocument(m) {
		return nil
	}
	body, err := yaml.Marshal(APIKeyPatch(m))
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

// RemoveOverlay deletes MatchMedia overlay.yaml if present.
func RemoveOverlay() error {
	dataDir, err := config.MatchMediaXDGDataDir()
	if err != nil {
		return err
	}
	err = os.Remove(overlayDest(dataDir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func seedXDG(srcHome, dataDir string) error {
	if err := os.MkdirAll(filepath.Join(dataDir, "config"), 0o755); err != nil {
		return err
	}
	for _, rel := range []string{
		filepath.Join("config", "default.yaml"),
		"public",
	} {
		src := filepath.Join(srcHome, rel)
		dst := filepath.Join(dataDir, rel)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if _, err := os.Stat(src); err != nil {
			if rel == filepath.Join("config", "default.yaml") {
				return fmt.Errorf("matchmedia seed %s: %w", rel, err)
			}
			continue
		}
		if err := copyTree(src, dst); err != nil {
			return fmt.Errorf("matchmedia seed %s: %w", rel, err)
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode().Perm())
	}
	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, fi.Mode().Perm())
	})
}

func writeOverlay(dataDir, seedPath string) error {
	path := overlayDest(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	out := map[string]any{}
	if seed, err := loadYAMLMap(seedPath); err == nil {
		out = seed
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("matchmedia seed: %w", err)
	}
	seedKinds := groupKinds(out)
	if existing, err := loadYAMLMap(path); err == nil {
		mergeOverlay(out, existing)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("matchmedia overlay: %w", err)
	}
	if extra := strings.TrimSpace(os.Getenv("SERVEMEDIA_MATCHMEDIA_OVERLAY")); extra != "" {
		more, err := loadYAMLMap(extra)
		if err != nil {
			return fmt.Errorf("matchmedia overlay: %w", err)
		}
		mergeOverlay(out, more)
	}
	setGroupKinds(out, seedKinds)
	delete(out, "data_dir")
	delete(out, "browse_root")
	body, err := yaml.Marshal(out)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func mergeOverlay(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				mergeOverlay(dm, sm)
				dst[k] = dm
				continue
			}
		}
		dst[k] = v
	}
}

func groupKinds(cfg map[string]any) any {
	g, ok := cfg["group"].(map[string]any)
	if !ok {
		return nil
	}
	return g["kinds"]
}

func setGroupKinds(cfg map[string]any, kinds any) {
	if kinds == nil {
		return
	}
	g, ok := cfg["group"].(map[string]any)
	if !ok {
		cfg["group"] = map[string]any{"kinds": kinds}
		return
	}
	g["kinds"] = kinds
}
