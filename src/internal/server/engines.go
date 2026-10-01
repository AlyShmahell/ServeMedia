package server

import (
	"net/http"
	"sort"
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
	"gopkg.in/yaml.v3"
)

func (s *Server) enginesStatus() (ready bool, version, reason string) {
	if s.Meta == nil {
		return false, "", "MatchMedia unavailable"
	}
	st, err := s.Meta.Status()
	if err != nil || !st.Ready {
		reason = st.DisabledReason
		if reason == "" {
			reason = "MatchMedia unavailable"
		}
		return false, st.Version, reason
	}
	return true, st.Version, ""
}

func (s *Server) handleEnginesStatus(w http.ResponseWriter, r *http.Request) {
	ready, _, _ := s.enginesStatus()
	s.render(w, r, "partials/engines_status.html", map[string]any{
		"Ready": ready,
	})
}

func (s *Server) handleSettingsEngines(w http.ResponseWriter, r *http.Request) {
	tab := strings.TrimSpace(r.URL.Query().Get("tab"))
	if tab != "ffmpeg" {
		tab = "matchmedia"
	}
	ready, ver, reason := s.enginesStatus()
	secrets := map[string]bool{}
	var secretKeys []string
	overlayYAML := ""
	var overlayErr string
	if s.Meta != nil {
		if st, err := s.Meta.SecretsStatus(); err == nil {
			secrets = st
			for k := range st {
				secretKeys = append(secretKeys, k)
			}
			sort.Strings(secretKeys)
		}
		if cfg, err := s.Meta.Config(); err == nil {
			b, err := yaml.Marshal(cfg)
			if err != nil {
				overlayErr = err.Error()
			} else {
				overlayYAML = string(b)
			}
		} else {
			overlayErr = err.Error()
		}
	}
	hw := ""
	active := ""
	hwOn := false
	if s.Transcode != nil {
		hw = s.Transcode.HWAccelStatus()
		active = s.Transcode.ActiveTranscodeStatus()
		hwOn = strings.TrimSpace(hw) != "" && !strings.EqualFold(hw, "software")
	}
	s.render(w, r, "settings_engines.html", map[string]any{
		"Tab":             tab,
		"Ready":           ready,
		"MetaVersion":     ver,
		"Reason":          reason,
		"Secrets":         secrets,
		"SecretKeys":      secretKeys,
		"OverlayYAML":     overlayYAML,
		"OverlayError":    overlayErr,
		"Config":          s.Cfg,
		"TranscodeHW":     hw,
		"TranscodeHWOn":   hwOn,
		"ActiveTranscode": active,
	})
}

func (s *Server) handleSaveEngines(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if s.Meta != nil {
		updates := map[string]string{}
		for k := range r.PostForm {
			if !strings.HasPrefix(k, "secret_") {
				continue
			}
			key := strings.TrimPrefix(k, "secret_")
			if key == "" {
				continue
			}
			if v := strings.TrimSpace(r.FormValue(k)); v != "" {
				updates[key] = v
			}
		}
		if len(updates) > 0 {
			if err := s.Meta.SetSecrets(updates); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		raw := r.FormValue("overlay_yaml")
		if strings.TrimSpace(raw) != "" {
			var patch map[string]any
			if err := yaml.Unmarshal([]byte(raw), &patch); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if patch == nil {
				patch = map[string]any{}
			}
			if err := s.Meta.SetConfig(patch); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
	}
	http.Redirect(w, r, "/settings/engines", http.StatusFound)
}

func (s *Server) handleRestoreEngines(w http.ResponseWriter, r *http.Request) {
	if err := matchmedia.RemoveOverlay(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	seed, err := matchmedia.SeedMap()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.Meta != nil {
		if err := s.Meta.SetConfig(seed); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	http.Redirect(w, r, "/settings/engines", http.StatusFound)
}

func (s *Server) handleRestartEngines(w http.ResponseWriter, r *http.Request) {
	if s.restartMeta == nil {
		http.Error(w, "MatchMedia restart unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.restartMeta(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		s.handleEnginesStatus(w, r)
		return
	}
	http.Redirect(w, r, "/settings/engines", http.StatusFound)
}
