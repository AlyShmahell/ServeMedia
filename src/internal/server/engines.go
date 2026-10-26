package server

import (
	"net/http"
	"sort"
	"strconv"
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

func settingsEnginesTab(r *http.Request) string {
	if strings.TrimSpace(r.URL.Query().Get("tab")) == "ffmpeg" {
		return "ffmpeg"
	}
	return "matchmedia"
}

func (s *Server) enginesSettingsData() map[string]any {
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
	return map[string]any{
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
	}
}

func (s *Server) handleSettingsEngines(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, settingsEnginesTab(r), s.enginesSettingsData())
}

func (s *Server) handleSaveEngines(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.FormValue("ffmpeg_settings") == "1" {
		s.saveFFmpegSettings(w, r)
		return
	}
	if s.Meta != nil {
		raw := r.FormValue("overlay_yaml")
		secrets := secretUpdates(r)
		if strings.TrimSpace(raw) != "" || len(secrets) > 0 {
			cur, err := s.Meta.Config()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var patch map[string]any
			if strings.TrimSpace(raw) != "" {
				if err := yaml.Unmarshal([]byte(raw), &patch); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			} else {
				patch = cur
			}
			if patch == nil {
				patch = map[string]any{}
			}
			if err := s.Meta.SetConfig(mergeProviderKeys(cur, patch, secrets)); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
	}
	s.finishEngines(w, r, "matchmedia")
}

func secretUpdates(r *http.Request) map[string]string {
	updates := map[string]string{}
	for k := range r.PostForm {
		if !strings.HasPrefix(k, "secret_") {
			continue
		}
		name := strings.TrimPrefix(k, "secret_")
		if name == "" {
			continue
		}
		if v := strings.TrimSpace(r.FormValue(k)); v != "" {
			updates[name] = v
		}
	}
	return updates
}

// mergeProviderKeys keeps api_key values the YAML editor omitted, then applies
// non-blank secret fields. A blank secret leaves the stored key in place.
func mergeProviderKeys(cur, patch map[string]any, secrets map[string]string) map[string]any {
	if patch == nil {
		patch = map[string]any{}
	}
	curProviders, _ := cur["providers"].(map[string]any)
	patchProviders, _ := patch["providers"].(map[string]any)
	if patchProviders == nil {
		patchProviders = map[string]any{}
	}
	for name, raw := range curProviders {
		spec, _ := raw.(map[string]any)
		if spec == nil {
			continue
		}
		key, _ := spec["api_key"].(string)
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		pSpec, ok := patchProviders[name].(map[string]any)
		if !ok || pSpec == nil {
			continue
		}
		if existing, _ := pSpec["api_key"].(string); strings.TrimSpace(existing) == "" {
			pSpec["api_key"] = key
			patchProviders[name] = pSpec
		}
	}
	for name, key := range secrets {
		name = strings.TrimSpace(name)
		key = strings.TrimSpace(key)
		if name == "" || key == "" {
			continue
		}
		pSpec, _ := patchProviders[name].(map[string]any)
		if pSpec == nil {
			pSpec = map[string]any{}
		}
		pSpec["api_key"] = key
		patchProviders[name] = pSpec
	}
	if len(patchProviders) > 0 {
		patch["providers"] = patchProviders
	}
	return patch
}

func (s *Server) saveFFmpegSettings(w http.ResponseWriter, r *http.Request) {
	crf, err := strconv.Atoi(strings.TrimSpace(r.FormValue("crf")))
	if err != nil || crf <= 0 {
		http.Error(w, "crf must be a positive number", http.StatusBadRequest)
		return
	}
	height, err := strconv.Atoi(strings.TrimSpace(r.FormValue("max_height")))
	if err != nil || height <= 0 {
		http.Error(w, "max height must be a positive number", http.StatusBadRequest)
		return
	}
	segment, err := strconv.Atoi(strings.TrimSpace(r.FormValue("segment_seconds")))
	if err != nil || segment <= 0 {
		http.Error(w, "segment seconds must be a positive number", http.StatusBadRequest)
		return
	}
	cleanup, err := strconv.Atoi(strings.TrimSpace(r.FormValue("cleanup_hours")))
	if err != nil || cleanup <= 0 {
		http.Error(w, "cleanup hours must be a positive number", http.StatusBadRequest)
		return
	}
	hw := strings.ToLower(strings.TrimSpace(r.FormValue("hwaccel")))
	switch hw {
	case "auto", "vaapi", "none":
	default:
		http.Error(w, "hwaccel must be auto, vaapi, or none", http.StatusBadRequest)
		return
	}
	s.Cfg.Transcode.CRF = crf
	s.Cfg.Transcode.MaxHeight = height
	s.Cfg.Transcode.SegmentSeconds = segment
	s.Cfg.Transcode.CleanupHours = cleanup
	s.Cfg.Transcode.HWAccel = hw
	if err := s.Cfg.Save(s.Cfg.OverlaySavePath()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.Transcode != nil {
		s.Transcode.UseConfig(*s.Cfg)
	}
	s.finishEngines(w, r, "ffmpeg")
}

func (s *Server) finishEngines(w http.ResponseWriter, r *http.Request, tab string) {
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	s.finishSettings(w, r, tab, s.enginesSettingsData())
}

func (s *Server) handleRestoreEngines(w http.ResponseWriter, r *http.Request) {
	patch := map[string]any{}
	if s.Meta != nil {
		if cur, err := s.Meta.Config(); err == nil {
			patch = matchmedia.APIKeyPatch(cur)
		}
		if err := s.Meta.SetConfig(patch); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	s.finishEngines(w, r, "matchmedia")
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
	http.Redirect(w, r, "/", http.StatusFound)
}
