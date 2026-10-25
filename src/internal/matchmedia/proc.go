package matchmedia

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	mmconfig "github.com/alyshmahell/matchmedia/src/mod/config"
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
	return filepath.Join(dataDir, "overlay.yaml")
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
	dataDir, err := config.MatchMediaDir()
	if err != nil {
		return err
	}
	err = os.Remove(overlayDest(dataDir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// mergeExtraOverlay folds an extra overlay file onto path, creating path when needed.
func mergeExtraOverlay(path, extra string) error {
	base, err := loadYAMLMap(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if base == nil {
		base = map[string]any{}
	}
	more, err := loadYAMLMap(extra)
	if err != nil {
		return fmt.Errorf("matchmedia overlay: %w", err)
	}
	mergeOverlay(base, more)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := yaml.Marshal(base)
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

// configDocument is the resolved MatchMedia config, without runtime data_dir
// or empty values, so the settings box shows the default plus any overlay.
func configDocument(cfg *mmconfig.Config) (map[string]any, error) {
	if cfg == nil {
		return map[string]any{}, nil
	}
	clone := *cfg
	clone.DataDir = ""
	b, err := yaml.Marshal(&clone)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	delete(m, "data_dir")
	pruned, _ := pruneEmpty(m).(map[string]any)
	if pruned == nil {
		pruned = map[string]any{}
	}
	return pruned, nil
}

func defaultDocument() (map[string]any, error) {
	cfg, err := mmconfig.New("")
	if err != nil {
		return nil, err
	}
	return configDocument(cfg)
}

// overlayDiff keeps edited branches that differ from the MatchMedia default.
// Keys present only in the default stay out, so a later default change still applies.
func overlayDiff(base, edited map[string]any) map[string]any {
	out := map[string]any{}
	for k, ev := range edited {
		if k == "data_dir" {
			continue
		}
		bv, ok := base[k]
		if !ok {
			out[k] = ev
			continue
		}
		em, eok := ev.(map[string]any)
		bm, bok := bv.(map[string]any)
		if eok && bok {
			child := overlayDiff(bm, em)
			if len(child) > 0 {
				out[k] = child
			}
			continue
		}
		if !yamlEqual(bv, ev) {
			out[k] = ev
		}
	}
	return out
}

func pruneEmpty(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range t {
			if k == "data_dir" {
				continue
			}
			p := pruneEmpty(val)
			if p == nil {
				continue
			}
			if m, ok := p.(map[string]any); ok && len(m) == 0 {
				continue
			}
			out[k] = p
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		if len(t) == 0 {
			return nil
		}
		return t
	case string:
		if strings.TrimSpace(t) == "" {
			return nil
		}
		return t
	case bool:
		if !t {
			return nil
		}
		return t
	case int:
		if t == 0 {
			return nil
		}
		return t
	case int64:
		if t == 0 {
			return nil
		}
		return t
	case uint64:
		if t == 0 {
			return nil
		}
		return t
	case float64:
		if t == 0 {
			return nil
		}
		return t
	case float32:
		if t == 0 {
			return nil
		}
		return t
	default:
		return v
	}
}

func yamlEqual(a, b any) bool {
	if am, ok := a.(map[string]any); ok {
		bm, ok := b.(map[string]any)
		if !ok || len(am) != len(bm) {
			return false
		}
		for k, av := range am {
			if !yamlEqual(av, bm[k]) {
				return false
			}
		}
		return true
	}
	if as, ok := a.([]any); ok {
		bs, ok := b.([]any)
		if !ok || len(as) != len(bs) {
			return false
		}
		for i := range as {
			if !yamlEqual(as[i], bs[i]) {
				return false
			}
		}
		return true
	}
	af, aok := asFloat(a)
	bf, bok := asFloat(b)
	if aok || bok {
		return aok && bok && af == bf
	}
	return reflect.DeepEqual(a, b)
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}
