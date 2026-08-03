package transcode

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alyshmahell/medora/internal/config"
)

type Manager struct {
	Cfg      config.Config
	mu       sync.Mutex
	jobs     map[string]*Job
	useVAAPI bool
	vaapiDev string
}

type Job struct {
	ID         string
	Source     string
	AudioIndex int // absolute ffprobe stream index; -1 = first audio (0:a:0?)
	Height     int // target encode height; 0 = no vertical scale
	StartAt    int // input seek seconds (floored); 0 = from start
	OutputDir  string
	Status     string
	LastAccess time.Time
	cmd        *exec.Cmd
	cancel     context.CancelFunc
}

func NewManager(cfg config.Config) *Manager {
	m := &Manager{Cfg: cfg, jobs: map[string]*Job{}}
	m.probeVAAPI()
	go m.cleanupLoop()
	return m
}

func (m *Manager) probeVAAPI() {
	mode := strings.ToLower(strings.TrimSpace(m.Cfg.Transcode.HWAccel))
	if mode == "" {
		mode = "auto"
	}
	if mode == "none" {
		log.Printf("transcode hwaccel=software")
		return
	}

	dev := strings.TrimSpace(m.Cfg.Transcode.VAAPIDevice)
	if dev == "" {
		dev = findRenderNode()
	}
	if dev == "" {
		if mode == "vaapi" {
			log.Printf("transcode hwaccel=vaapi requested but no render node found; using software")
		} else {
			log.Printf("transcode hwaccel=software")
		}
		return
	}
	if _, err := os.Stat(dev); err != nil {
		log.Printf("transcode hwaccel=software (vaapi device %s: %v)", dev, err)
		return
	}

	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").CombinedOutput()
	if err != nil || !bytes.Contains(out, []byte("h264_vaapi")) {
		log.Printf("transcode hwaccel=software (h264_vaapi unavailable)")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// AMD VAAPI encode requires at least 256x128; 64x64 falsely fails the probe.
	smoke := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-init_hw_device", "vaapi=va:"+dev,
		"-f", "lavfi", "-i", "color=c=black:s=320x240:d=0.2",
		"-vf", "format=nv12,hwupload",
		"-c:v", "h264_vaapi",
		"-f", "null", "-",
	)
	var smokeErr bytes.Buffer
	smoke.Stderr = &smokeErr
	if err := smoke.Run(); err != nil {
		msg := strings.TrimSpace(smokeErr.String())
		if len(msg) > 500 {
			msg = msg[len(msg)-500:]
		}
		if msg != "" {
			log.Printf("transcode hwaccel=software (vaapi smoke failed on %s: %v)\n%s", dev, err, msg)
		} else {
			log.Printf("transcode hwaccel=software (vaapi smoke failed on %s: %v)", dev, err)
		}
		return
	}

	m.useVAAPI = true
	m.vaapiDev = dev
	log.Printf("transcode hwaccel=vaapi device=%s", dev)
}

func findRenderNode() string {
	matches, err := filepath.Glob("/dev/dri/renderD*")
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// Start begins an HLS transcode for source using the given absolute audio stream
// index (-1 maps the first audio track), optional target height (0 = no scale),
// and optional input seek startAt (floored to whole seconds; 0 = from start).
// The caller's ctx is only used for the wait-for-playlist deadline; ffmpeg runs
// detached from the HTTP request.
func (m *Manager) Start(ctx context.Context, source string, audioIndex, height int, startAt float64) (*Job, error) {
	startSec := 0
	if startAt > 0 {
		startSec = int(startAt)
	}
	if j := m.getActive(source, audioIndex, height, startSec); j != nil {
		if err := m.WaitPlaylist(ctx, j, 30*time.Second); err != nil {
			return nil, err
		}
		return j, nil
	}

	useVAAPI := m.useVAAPI
	j, err := m.startJob(source, audioIndex, height, startSec, useVAAPI)
	if err != nil {
		return nil, err
	}
	if err := m.WaitPlaylist(ctx, j, 30*time.Second); err != nil {
		if useVAAPI {
			log.Printf("vaapi job %s failed before playlist (%v); retrying software", j.ID, err)
			j, err = m.startJob(source, audioIndex, height, startSec, false)
			if err != nil {
				return nil, err
			}
			if err := m.WaitPlaylist(ctx, j, 30*time.Second); err != nil {
				return nil, err
			}
			return j, nil
		}
		return nil, err
	}
	return j, nil
}

func (m *Manager) getActive(source string, audioIndex, height, startSec int) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.Source == source && j.AudioIndex == audioIndex && j.Height == height && j.StartAt == startSec && (j.Status == "running" || j.Status == "ready") {
			j.LastAccess = time.Now()
			return j
		}
	}
	return nil
}

func (m *Manager) startJob(source string, audioIndex, height, startSec int, vaapi bool) (*Job, error) {
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.Source == source && j.AudioIndex == audioIndex && j.Height == height && j.StartAt == startSec && (j.Status == "running" || j.Status == "ready") {
			j.LastAccess = time.Now()
			m.mu.Unlock()
			return j, nil
		}
	}

	id := fmt.Sprintf("%d", time.Now().UnixNano())
	out := filepath.Join(m.Cfg.Transcode.Path, id)
	if err := os.MkdirAll(out, 0o755); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	jobCtx, cancel := context.WithCancel(context.Background())
	j := &Job{
		ID: id, Source: source, AudioIndex: audioIndex, Height: height, StartAt: startSec, OutputDir: out,
		Status: "running", LastAccess: time.Now(), cancel: cancel,
	}
	m.jobs[id] = j
	args := m.ffmpegArgs(source, out, vaapi, audioIndex, height, startSec)
	cmd := exec.CommandContext(jobCtx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	j.cmd = cmd
	m.mu.Unlock()

	go func() {
		err := cmd.Run()
		m.mu.Lock()
		defer m.mu.Unlock()
		if err != nil {
			j.Status = "error"
			msg := stderr.String()
			if len(msg) > 2000 {
				msg = msg[len(msg)-2000:]
			}
			log.Printf("ffmpeg %s: %v\n%s", id, err, msg)
		} else {
			j.Status = "ready"
		}
	}()
	return j, nil
}

func (m *Manager) ffmpegArgs(source, out string, vaapi bool, audioIndex, height, startSec int) []string {
	seg := m.Cfg.Transcode.SegmentSeconds
	if seg <= 0 {
		seg = 6
	}
	forceKF := fmt.Sprintf("expr:gte(t,n_forced*%d)", seg)
	audioMap := "0:a:0?"
	if audioIndex >= 0 {
		audioMap = fmt.Sprintf("0:%d", audioIndex)
	}
	hls := []string{
		"-c:a", "aac", "-ac", "2",
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", seg),
		"-hls_list_size", "0",
		"-hls_playlist_type", "event",
		"-hls_flags", "independent_segments",
		"-hls_segment_filename", filepath.Join(out, "seg_%03d.ts"),
		filepath.Join(out, "master.m3u8"),
	}

	ss := []string{}
	if startSec > 0 {
		ss = []string{"-ss", fmt.Sprintf("%d", startSec)}
	}

	if vaapi && m.vaapiDev != "" {
		vf := "scale_vaapi=format=nv12"
		if height > 0 {
			vf = fmt.Sprintf("scale_vaapi=w=-2:h=%d:format=nv12", height)
		}
		args := []string{"-y"}
		args = append(args, ss...)
		args = append(args,
			"-hwaccel", "vaapi",
			"-hwaccel_device", m.vaapiDev,
			"-hwaccel_output_format", "vaapi",
			"-i", source,
			"-map", "0:v:0", "-map", audioMap,
			"-vf", vf,
			"-c:v", "h264_vaapi",
			"-profile:v", "high",
			"-force_key_frames", forceKF,
		)
		if m.Cfg.Transcode.CRF > 0 {
			args = append(args, "-qp", fmt.Sprintf("%d", m.Cfg.Transcode.CRF))
		} else {
			args = append(args, "-b:v", "5M")
		}
		return append(args, hls...)
	}

	crf := m.Cfg.Transcode.CRF
	if crf <= 0 {
		crf = 23
	}
	args := []string{"-y"}
	args = append(args, ss...)
	args = append(args, "-i", source, "-map", "0:v:0", "-map", audioMap)
	if height > 0 {
		args = append(args, "-vf", fmt.Sprintf("scale=-2:%d", height))
	}
	args = append(args,
		"-pix_fmt", "yuv420p",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", fmt.Sprintf("%d", crf),
		"-profile:v", "high",
		"-force_key_frames", forceKF,
	)
	return append(args, hls...)
}

// WaitPlaylist blocks until master.m3u8 exists, the job errors, ctx is done, or timeout.
func (m *Manager) WaitPlaylist(ctx context.Context, j *Job, timeout time.Duration) error {
	if j == nil {
		return fmt.Errorf("nil job")
	}
	master := filepath.Join(j.OutputDir, "master.m3u8")
	if st, err := os.Stat(master); err == nil && st.Size() > 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		st := j.Status
		m.mu.Unlock()
		if st == "error" {
			return fmt.Errorf("ffmpeg failed for job %s", j.ID)
		}
		if info, err := os.Stat(master); err == nil && info.Size() > 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for HLS playlist")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *Manager) Get(id string) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.jobs[id]
	if j != nil {
		j.LastAccess = time.Now()
	}
	return j
}

func (m *Manager) cleanupLoop() {
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for range t.C {
		hours := m.Cfg.Transcode.CleanupHours
		if hours <= 0 {
			hours = 24
		}
		cut := time.Now().Add(-time.Duration(hours) * time.Hour)
		m.mu.Lock()
		for id, j := range m.jobs {
			if j.LastAccess.Before(cut) {
				if j.cancel != nil {
					j.cancel()
				} else if j.cmd != nil && j.cmd.Process != nil {
					_ = j.cmd.Process.Kill()
				}
				_ = os.RemoveAll(j.OutputDir)
				delete(m.jobs, id)
			}
		}
		m.mu.Unlock()
	}
}
