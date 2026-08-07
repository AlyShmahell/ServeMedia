package plugins

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strconv"
)

// PluginPanel is a pre-rendered plugin settings block for the integrations page.
type PluginPanel struct {
	ID            string
	Name          string
	Type          string
	Version       int
	Bundled       bool
	Enabled       bool
	SettingsHTML  template.HTML
	RenderError   string
}

// SettingsView is passed to plugin settings templates.
type SettingsView struct {
	TemplateName string
	Manifest     ManifestView
	Bundled     bool
	Enabled     bool
	OmdbAPIKey  string
	OmdbBaseURL string
	OmdbRPS     string
	OmdbDaily   string
	TvmazeRPS   string
	TvmazeDaily string
}

type ManifestView struct {
	ID      string
	Name    string
	Type    string
	Version int
}

// PluginSettingsView builds template data for a plugin settings fragment.
func PluginSettingsView(p Installed, cfg map[string]any, enabled bool) SettingsView {
	v := SettingsView{
		TemplateName: fmt.Sprintf("plugin_%s_settings.html", p.Manifest.ID),
		Manifest: ManifestView{
			ID: p.Manifest.ID, Name: p.Manifest.Name, Type: p.Manifest.Type, Version: p.Manifest.Version,
		},
		Bundled: p.Bundled,
		Enabled: enabled,
		OmdbRPS: "1",
		TvmazeRPS: "2",
		OmdbDaily: "1000",
		TvmazeDaily: "0",
	}
	if omdb, ok := cfg["omdb"].(map[string]any); ok {
		v.OmdbAPIKey = strAny(omdb["api_key"])
		v.OmdbBaseURL = strAny(omdb["base_url"])
		if s := strAny(omdb["rps"]); s != "" {
			v.OmdbRPS = s
		}
		if s := strAny(omdb["daily_limit"]); s != "" {
			v.OmdbDaily = s
		}
	}
	if tv, ok := cfg["tvmaze"].(map[string]any); ok {
		if s := strAny(tv["rps"]); s != "" {
			v.TvmazeRPS = s
		}
		if s := strAny(tv["daily_limit"]); s != "" {
			v.TvmazeDaily = s
		}
	}
	return v
}

func strAny(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	default:
		return fmt.Sprint(v)
	}
}

// ParsePluginTemplates merges plugin settings.html files into the root template.
func ParsePluginTemplates(root *template.Template, dataDir string) error {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dataDir, e.Name(), "settings.html")
		if st, err := os.Stat(path); err != nil || st.IsDir() {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := root.Parse(string(b)); err != nil {
			return fmt.Errorf("plugin %s settings.html: %w", e.Name(), err)
		}
	}
	return nil
}

// BuildPluginPanels pre-renders each plugin settings template.
func BuildPluginPanels(t *template.Template, views []SettingsView) []PluginPanel {
	panels := make([]PluginPanel, 0, len(views))
	for _, view := range views {
		panel := PluginPanel{
			ID:      view.Manifest.ID,
			Name:    view.Manifest.Name,
			Type:    view.Manifest.Type,
			Version: view.Manifest.Version,
			Bundled: view.Bundled,
			Enabled: view.Enabled,
		}
		if t == nil || view.TemplateName == "" {
			panel.RenderError = "Settings template unavailable"
			panels = append(panels, panel)
			continue
		}
		if t.Lookup(view.TemplateName) == nil {
			panel.RenderError = fmt.Sprintf("Settings template %q not loaded", view.TemplateName)
			panels = append(panels, panel)
			continue
		}
		var buf bytes.Buffer
		if err := t.ExecuteTemplate(&buf, view.TemplateName, view); err != nil {
			panel.RenderError = err.Error()
		} else {
			panel.SettingsHTML = template.HTML(buf.String())
		}
		panels = append(panels, panel)
	}
	return panels
}
