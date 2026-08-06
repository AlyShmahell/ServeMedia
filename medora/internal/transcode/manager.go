package transcode

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alyshmahell/medora/internal/config"
	"github.com/alyshmahell/medora/internal/media"
)

type Pipeline int

const (
	PipelineSoftware Pipeline = iota
	PipelineVAAPIHybrid
	PipelineVAAPIFull
	PipelineVAAPIFull10Bit
)

func (p Pipeline) String() string {
	switch p {
	case PipelineVAAPIFull:
		return "vaapi_full"
	case PipelineVAAPIFull10Bit:
		return "vaapi_full_10bit"
	case PipelineVAAPIHybrid:
		return "vaapi_hybrid"
	default:
		return "software"
	}
}

type Manager struct {
	Cfg         config.Config
	mu          sync.Mutex
	jobs        map[string]*Job
	activeByKey map[string]string
	useVAAPI    bool
	vaapiDev    string
}

type Job struct {
	ID         string
	OwnerKey   string
	Source     string
	AudioIndex int
	Height     int
	StartAt    int
	OutputDir  string
	Status     string
	Pipeline   string
	LastAccess time.Time
	cmd        *exec.Cmd
	cancel     context.CancelFunc
}

func NewManager(cfg config.Config) *Manager {
	m := &Manager{
		Cfg:         cfg,
		jobs:        map[string]*Job{},
		activeByKey: map[string]string{},
	}
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

	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").CombinedOutput()
	if err != nil || !bytes.Contains(out, []byte("h264_vaapi")) {
		log.Printf("transcode hwaccel=software (h264_vaapi unavailable)")
		return
	}

	devs := listRenderNodes()
	if dev := strings.TrimSpace(m.Cfg.Transcode.VAAPIDevice); dev != "" {
		devs = []string{dev}
	}
	if len(devs) == 0 {
		if mode == "vaapi" {
			log.Printf("transcode hwaccel=vaapi requested but no render node found; using software")
		} else {
			log.Printf("transcode hwaccel=software (no /dev/dri/renderD* nodes)")
		}
		return
	}

	statFails := 0
	for _, dev := range devs {
		if _, err := os.Stat(dev); err != nil {
			log.Printf("transcode hwaccel: skip %s (stat: %v)", dev, err)
			statFails++
			continue
		}
		ok, partial, err := tryVAAPIDevice(dev)
		if !ok {
			log.Printf("transcode hwaccel: skip %s (smoke failed: %v)", dev, err)
			continue
		}
		if partial {
			log.Printf("transcode hwaccel: %s passed minimal smoke only (full hybrid smoke failed); will try hybrid encode", dev)
		}
		m.useVAAPI = true
		m.vaapiDev = dev
		log.Printf("transcode hwaccel=vaapi device=%s", dev)
		if clip := vaapiProbeClipPath(); clip != "" {
			if p, err := media.Ffprobe(clip); err == nil && is10BitPixFmt(p.VideoPixFmt()) {
				if err := vaapiSmokeTest10BitDirect(dev, clip); err == nil {
					log.Printf("transcode hwaccel: %s 10-bit direct GPU convert OK (scale_vaapi)", dev)
				} else {
					log.Printf("transcode hwaccel: %s 10-bit direct GPU convert unavailable (%v); will use hwdownload fallback", dev, err)
				}
			}
		}
		return
	}

	if statFails == len(devs) {
		log.Printf("transcode hwaccel: render nodes present but not accessible; add host user to render/video group or set group_add in compose (see medora/.env.example)")
	}
	log.Printf("transcode hwaccel=software (no render node passed vaapi smoke test)")
}

// HWAccelStatus returns a short description of the active transcode path.
func (m *Manager) HWAccelStatus() string {
	if m.useVAAPI && m.vaapiDev != "" {
		return "vaapi " + m.vaapiDev
	}
	return "software"
}

// ActiveTranscodeStatus returns the pipeline of the most recent running job, or "".
func (m *Manager) ActiveTranscodeStatus() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best *Job
	for _, j := range m.jobs {
		if j.Status != "running" {
			continue
		}
		if best == nil || j.LastAccess.After(best.LastAccess) {
			best = j
		}
	}
	if best == nil || best.Pipeline == "" {
		return ""
	}
	return best.Pipeline
}

func listRenderNodes() []string {
	matches, err := filepath.Glob("/dev/dri/renderD*")
	if err != nil || len(matches) == 0 {
		return nil
	}
	sort.Strings(matches)
	return matches
}

func is10BitPixFmt(pixFmt string) bool {
	pixFmt = strings.ToLower(strings.TrimSpace(pixFmt))
	return strings.Contains(pixFmt, "10") || strings.Contains(pixFmt, "p010")
}

func selectInitialPipeline(useVAAPI bool, pixFmt string) Pipeline {
	if !useVAAPI {
		return PipelineSoftware
	}
	// 8-bit and 10-bit both try full GPU decode first; 10-bit falls back to hwdownload chain.
	return PipelineVAAPIFull
}

func fallbackPipeline(p Pipeline, pixFmt string) Pipeline {
	switch p {
	case PipelineVAAPIFull:
		if is10BitPixFmt(pixFmt) {
			return PipelineVAAPIFull10Bit
		}
		return PipelineVAAPIHybrid
	case PipelineVAAPIFull10Bit:
		return PipelineVAAPIHybrid
	case PipelineVAAPIHybrid:
		return PipelineSoftware
	default:
		return PipelineSoftware
	}
}

func videoPixFmt(source, hint string) string {
	if hint != "" {
		return hint
	}
	if p, err := media.Ffprobe(source); err == nil {
		return p.VideoPixFmt()
	}
	return ""
}

func tryVAAPIDevice(dev string) (ok bool, partial bool, err error) {
	if err := vaapiSmokeTestFull(dev); err != nil {
		if err2 := vaapiSmokeTestMinimal(dev); err2 == nil {
			return true, true, err
		}
		return false, false, err
	}
	if err := vaapiSmokeTestFullHW(dev); err != nil {
		return true, true, err
	}
	if clip := vaapiProbeClipPath(); clip != "" {
		if err := vaapiRealFileSmoke(dev, clip); err != nil {
			return false, false, fmt.Errorf("real-file smoke: %w", err)
		}
	}
	return true, false, nil
}

func vaapiProbeClipPath() string {
	const path = "/usr/share/medora/vaapi-probe.mkv"
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		return path
	}
	return ""
}

func vaapiRealFileSmoke(dev, source string) error {
	pixFmt := ""
	if p, err := media.Ffprobe(source); err == nil {
		pixFmt = p.VideoPixFmt()
	}
	pipeline := selectInitialPipeline(true, pixFmt)
	if pipeline == PipelineSoftware {
		pipeline = PipelineVAAPIHybrid
	}
	args := ffmpegArgsForPipeline(dev, source, pipeline, -1, 0, 0, config.Defaults())
	args = append([]string{"-hide_banner", "-loglevel", "error"}, args...)
	args = append(args, "-frames:v", "30", "-f", "null", "-")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	return runFFmpegSmoke(cmd)
}

func vaapiSmokeTestFull(dev string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	smoke := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-init_hw_device", "vaapi=va:"+dev,
		"-filter_hw_device", "va",
		"-f", "lavfi", "-i", "color=c=black:s=320x240:d=0.2",
		"-vf", "format=nv12,hwupload,scale_vaapi=format=nv12",
		"-c:v", "h264_vaapi",
		"-f", "null", "-",
	)
	return runFFmpegSmoke(smoke)
}

func vaapiSmokeTest10BitDirect(dev, source string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	smoke := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-init_hw_device", "vaapi=va:"+dev,
		"-filter_hw_device", "va",
		"-hwaccel", "vaapi", "-hwaccel_device", "va", "-hwaccel_output_format", "vaapi",
		"-i", source,
		"-vf", "scale_vaapi=format=nv12",
		"-c:v", "h264_vaapi",
		"-frames:v", "30",
		"-f", "null", "-",
	)
	return runFFmpegSmoke(smoke)
}

func vaapiSmokeTestFullHW(dev string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	smoke := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-init_hw_device", "vaapi=va:"+dev,
		"-filter_hw_device", "va",
		"-hwaccel", "vaapi", "-hwaccel_device", "va", "-hwaccel_output_format", "vaapi",
		"-f", "lavfi", "-i", "color=c=black:s=320x240:d=0.2",
		"-vf", "scale_vaapi=format=nv12",
		"-c:v", "h264_vaapi",
		"-f", "null", "-",
	)
	return runFFmpegSmoke(smoke)
}

func vaapiSmokeTestMinimal(dev string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	smoke := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-init_hw_device", "vaapi=va:"+dev,
		"-f", "lavfi", "-i", "color=c=black:s=320x240:d=0.2",
		"-vf", "format=nv12,hwupload",
		"-c:v", "h264_vaapi",
		"-f", "null", "-",
	)
	return runFFmpegSmoke(smoke)
}

func runFFmpegSmoke(cmd *exec.Cmd) error {
	var smokeErr bytes.Buffer
	cmd.Stderr = &smokeErr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(smokeErr.String())
		if len(msg) > 500 {
			msg = msg[len(msg)-500:]
		}
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

// Start begins an HLS transcode. ownerKey identifies the client (e.g. user+IP);
// a new start cancels any prior job for the same key. pixFmt may be empty to probe source.
func (m *Manager) Start(ctx context.Context, ownerKey, source string, audioIndex, height int, startAt float64, pixFmt string) (*Job, error) {
	startSec := 0
	if startAt > 0 {
		startSec = int(startAt)
	}

	m.mu.Lock()
	if ownerKey != "" {
		if id, ok := m.activeByKey[ownerKey]; ok {
			m.removeJobLocked(id)
		}
	}
	m.mu.Unlock()

	if j := m.getActive(source, audioIndex, height, startSec); j != nil {
		if err := m.WaitPlaylist(ctx, j, 30*time.Second); err != nil {
			return nil, err
		}
		return j, nil
	}

	pixFmt = videoPixFmt(source, pixFmt)
	pipeline := selectInitialPipeline(m.useVAAPI, pixFmt)

	for {
		j, err := m.startJob(source, audioIndex, height, startSec, ownerKey, pipeline)
		if err != nil {
			return nil, err
		}
		if err := m.WaitPlaylist(ctx, j, 30*time.Second); err != nil {
			next := fallbackPipeline(pipeline, pixFmt)
			if next == pipeline {
				return nil, err
			}
			log.Printf("WARN transcode: vaapi fallback for job %s pipeline=%s (%v); retrying %s", j.ID, pipeline, err, next)
			m.mu.Lock()
			m.removeJobLocked(j.ID)
			m.mu.Unlock()
			pipeline = next
			continue
		}
		return j, nil
	}
}

// Cancel stops the active transcode for ownerKey, if any.
func (m *Manager) Cancel(ownerKey string) {
	if ownerKey == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.activeByKey[ownerKey]; ok {
		m.removeJobLocked(id)
	}
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

func (m *Manager) startJob(source string, audioIndex, height, startSec int, ownerKey string, pipeline Pipeline) (*Job, error) {
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.Source == source && j.AudioIndex == audioIndex && j.Height == height && j.StartAt == startSec && (j.Status == "running" || j.Status == "ready") {
			j.LastAccess = time.Now()
			if ownerKey != "" {
				m.activeByKey[ownerKey] = j.ID
				j.OwnerKey = ownerKey
			}
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
		ID: id, OwnerKey: ownerKey, Source: source, AudioIndex: audioIndex, Height: height, StartAt: startSec, OutputDir: out,
		Status: "running", Pipeline: pipeline.String(), LastAccess: time.Now(), cancel: cancel,
	}
	m.jobs[id] = j
	if ownerKey != "" {
		m.activeByKey[ownerKey] = id
	}
	args := m.ffmpegArgs(source, out, pipeline, audioIndex, height, startSec)
	log.Printf("transcode pipeline=%s job=%s", pipeline, id)
	log.Printf("ffmpeg %s: %s", id, strings.Join(args, " "))
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

func (m *Manager) removeJobLocked(id string) {
	j, ok := m.jobs[id]
	if !ok {
		return
	}
	if j.cancel != nil {
		j.cancel()
	} else if j.cmd != nil && j.cmd.Process != nil {
		_ = j.cmd.Process.Kill()
	}
	outDir := j.OutputDir
	if j.OwnerKey != "" && m.activeByKey[j.OwnerKey] == id {
		delete(m.activeByKey, j.OwnerKey)
	}
	delete(m.jobs, id)
	go func() { _ = os.RemoveAll(outDir) }()
}

func (m *Manager) ffmpegArgs(source, out string, pipeline Pipeline, audioIndex, height, startSec int) []string {
	return ffmpegArgsForPipeline(m.vaapiDev, source, pipeline, audioIndex, height, startSec, m.Cfg, out)
}

func ffmpegArgsForPipeline(vaapiDev, source string, pipeline Pipeline, audioIndex, height, startSec int, cfg config.Config, out ...string) []string {
	seg := cfg.Transcode.SegmentSeconds
	if seg <= 0 {
		seg = 6
	}
	forceKF := fmt.Sprintf("expr:gte(t,n_forced*%d)", seg)
	audioMap := "0:a:0?"
	if audioIndex >= 0 {
		audioMap = fmt.Sprintf("0:%d", audioIndex)
	}

	var hls []string
	if len(out) > 0 && out[0] != "" {
		hls = []string{
			"-c:a", "aac", "-ac", "2",
			"-f", "hls",
			"-hls_time", fmt.Sprintf("%d", seg),
			"-hls_list_size", "0",
			"-hls_playlist_type", "event",
			"-hls_flags", "independent_segments",
			"-hls_segment_filename", filepath.Join(out[0], "seg_%03d.ts"),
			filepath.Join(out[0], "master.m3u8"),
		}
	}

	ss := []string{}
	if startSec > 0 {
		ss = []string{"-ss", fmt.Sprintf("%d", startSec)}
	}

	switch pipeline {
	case PipelineVAAPIFull, PipelineVAAPIFull10Bit, PipelineVAAPIHybrid:
		if vaapiDev == "" {
			pipeline = PipelineSoftware
			break
		}
		vf := vaapiVideoFilter(pipeline, height)
		args := []string{"-y",
			"-init_hw_device", "vaapi=va:" + vaapiDev,
			"-filter_hw_device", "va",
		}
		args = append(args, ss...)
		if pipeline == PipelineVAAPIHybrid {
			args = append(args,
				"-i", source,
				"-map", "0:v:0", "-map", audioMap,
				"-vf", vf,
			)
		} else {
			args = append(args,
				"-hwaccel", "vaapi", "-hwaccel_device", "va", "-hwaccel_output_format", "vaapi",
				"-i", source,
				"-map", "0:v:0", "-map", audioMap,
				"-vf", vf,
			)
		}
		args = append(args,
			"-c:v", "h264_vaapi",
			"-profile:v", "high",
			"-force_key_frames", forceKF,
		)
		if cfg.Transcode.CRF > 0 {
			args = append(args, "-qp", fmt.Sprintf("%d", cfg.Transcode.CRF))
		} else {
			args = append(args, "-b:v", "5M")
		}
		return append(args, hls...)
	}

	crf := cfg.Transcode.CRF
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

func vaapiVideoFilter(pipeline Pipeline, height int) string {
	scale := "scale_vaapi=format=nv12"
	if height > 0 {
		scale = fmt.Sprintf("scale_vaapi=w=-2:h=%d:format=nv12", height)
	}
	switch pipeline {
	case PipelineVAAPIFull10Bit:
		return "hwdownload,format=p010le,format=nv12,hwupload," + scale
	case PipelineVAAPIFull:
		return scale
	default:
		vf := "format=nv12,hwupload," + scale
		return vf
	}
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
				m.removeJobLocked(id)
			}
		}
		m.mu.Unlock()
	}
}
