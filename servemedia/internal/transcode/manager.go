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

	"github.com/alyshmahell/servemedia/internal/config"
	"github.com/alyshmahell/servemedia/internal/ffbin"
	"github.com/alyshmahell/servemedia/internal/media"
)

type Pipeline int

const (
	PipelineSoftware Pipeline = iota
	PipelineVAAPIHybrid
	PipelineVAAPIFull
	PipelineVAAPIFull10Bit
	PipelineVAAPIFullAV110
)

func (p Pipeline) String() string {
	switch p {
	case PipelineVAAPIFull:
		return "vaapi_full"
	case PipelineVAAPIFull10Bit:
		return "vaapi_full_10bit"
	case PipelineVAAPIFullAV110:
		return "vaapi_full_av1_10"
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
	vaapiCodec  string // "h264" or "av1"
	vaapiAV110  bool
}

type Job struct {
	ID         string
	OwnerKey   string
	Tabs       map[string]struct{}
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

	ensureLibVADriversPath()

	out, err := exec.Command(ffbin.FFmpeg(), "-hide_banner", "-encoders").CombinedOutput()
	hasH264 := err == nil && bytes.Contains(out, []byte("h264_vaapi"))
	hasAV1 := err == nil && bytes.Contains(out, []byte("av1_vaapi"))
	if err != nil || (!hasH264 && !hasAV1) {
		log.Printf("transcode hwaccel=software (vaapi encoders unavailable)")
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
		ok, partial, codec, err := tryVAAPIDevice(dev)
		if !ok {
			if err != nil {
				log.Printf("transcode hwaccel: skip %s: %v", dev, err)
			} else {
				log.Printf("transcode hwaccel: skip %s (vaapi smoke failed; using software if no other device)", dev)
			}
			continue
		}
		if codec == "" {
			codec = "h264"
		}
		m.useVAAPI = true
		m.vaapiDev = dev
		m.vaapiCodec = codec
		if codec == "av1" {
			log.Printf("transcode hwaccel=vaapi device=%s codec=av1 (h264 encode unavailable)", dev)
			if err := vaapiSmokeTestAV110(dev); err == nil {
				m.vaapiAV110 = true
				log.Printf("transcode hwaccel: %s av1 10-bit encode OK", dev)
			} else {
				log.Printf("transcode hwaccel: %s av1 10-bit encode unavailable (%v)", dev, err)
			}
		} else {
			log.Printf("transcode hwaccel=vaapi device=%s", dev)
			if partial {
				if err != nil {
					log.Printf("transcode hwaccel: %s using hybrid encode (%v)", dev, err)
				} else {
					log.Printf("transcode hwaccel: %s passed minimal smoke only (full hybrid smoke failed); will try hybrid encode", dev)
				}
			}
		}
		if codec != "av1" {
			if clip := vaapiProbeClipPath(); clip != "" {
				if p, err := media.Ffprobe(clip); err == nil && is10BitPixFmt(p.VideoPixFmt()) {
					if err := vaapiSmokeTest10BitDirect(dev, clip); err == nil {
						log.Printf("transcode hwaccel: %s 10-bit direct GPU convert OK (scale_vaapi)", dev)
					} else {
						log.Printf("transcode hwaccel: %s 10-bit direct GPU convert unavailable (%v); will use hwdownload fallback", dev, err)
					}
				}
			}
		}
		return
	}

	if statFails == len(devs) {
		log.Printf("transcode hwaccel: render nodes present but not accessible; add host user to the render and video groups")
	}
	log.Printf("transcode hwaccel=software (no render node passed vaapi smoke test)")
}

// HWAccelStatus returns a short description of the active transcode path.
func (m *Manager) HWAccelStatus() string {
	if m.useVAAPI && m.vaapiDev != "" {
		if m.vaapiCodec == "av1" {
			s := "vaapi " + m.vaapiDev + " av1"
			if m.vaapiAV110 {
				s += " av1_10"
			}
			return s
		}
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

func joinExistingLibVADriverDirs(dirs ...string) string {
	var found []string
	for _, dir := range dirs {
		st, err := os.Stat(dir)
		if err == nil && st.IsDir() {
			found = append(found, dir)
		}
	}
	return strings.Join(found, ":")
}

func ensureLibVADriversPath() {
	if os.Getenv("LIBVA_DRIVERS_PATH") != "" {
		return
	}
	path := joinExistingLibVADriverDirs(
		"/usr/lib64/dri-nonfree",
		"/usr/lib64/dri-freeworld",
		"/usr/lib64/dri",
		"/usr/lib/dri-nonfree",
		"/usr/lib/dri-freeworld",
		"/usr/lib/dri",
	)
	if path == "" {
		return
	}
	_ = os.Setenv("LIBVA_DRIVERS_PATH", path)
	log.Printf("transcode hwaccel: LIBVA_DRIVERS_PATH=%s", path)
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

func selectInitialPipeline(useVAAPI bool, pixFmt, codec string, allowAV110 bool) Pipeline {
	if !useVAAPI {
		return PipelineSoftware
	}
	if allowAV110 && codec == "av1" && is10BitPixFmt(pixFmt) {
		return PipelineVAAPIFullAV110
	}
	return PipelineVAAPIFull
}

func fallbackPipeline(p Pipeline, pixFmt string) Pipeline {
	switch p {
	case PipelineVAAPIFullAV110:
		return PipelineVAAPIFull
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

func tryVAAPIDevice(dev string) (ok bool, partial bool, codec string, err error) {
	if err := vaapiSmokeTestFull(dev); err != nil {
		if err2 := vaapiSmokeTestMinimal(dev); err2 == nil {
			return true, true, "h264", err
		}
		if err3 := vaapiSmokeTestAV1(dev); err3 == nil {
			return true, true, "av1", err
		}
		return false, false, "", err
	}
	if err := vaapiSmokeTestFullHW(dev); err != nil {
		return true, true, "h264", err
	}
	if clip := vaapiProbeClipPath(); clip != "" {
		if err := vaapiRealFileSmoke(dev, clip); err != nil {
			return true, true, "h264", fmt.Errorf("real-file smoke: %w", err)
		}
	}
	return true, false, "h264", nil
}

func vaapiProbeClipPath() string {
	const path = "/usr/share/servemedia/vaapi-probe.mkv"
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
	pipeline := selectInitialPipeline(true, pixFmt, "h264", false)
	if pipeline == PipelineSoftware {
		pipeline = PipelineVAAPIHybrid
	}
	args := ffmpegArgsForPipeline(dev, "h264", source, pipeline, -1, 0, 0, config.Defaults())
	args = append([]string{"-hide_banner", "-loglevel", "error"}, args...)
	args = append(args, "-frames:v", "30", "-f", "null", "-")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffbin.FFmpeg(), args...)
	return runFFmpegSmoke(cmd)
}

func vaapiSmokeTestFull(dev string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	smoke := exec.CommandContext(ctx, ffbin.FFmpeg(),
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
	smoke := exec.CommandContext(ctx, ffbin.FFmpeg(),
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
	smoke := exec.CommandContext(ctx, ffbin.FFmpeg(),
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
	smoke := exec.CommandContext(ctx, ffbin.FFmpeg(),
		"-hide_banner", "-loglevel", "error",
		"-init_hw_device", "vaapi=va:"+dev,
		"-f", "lavfi", "-i", "color=c=black:s=320x240:d=0.2",
		"-vf", "format=nv12,hwupload",
		"-c:v", "h264_vaapi",
		"-f", "null", "-",
	)
	return runFFmpegSmoke(smoke)
}

func vaapiSmokeTestAV1(dev string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	smoke := exec.CommandContext(ctx, ffbin.FFmpeg(),
		"-hide_banner", "-loglevel", "error",
		"-init_hw_device", "vaapi=va:"+dev,
		"-filter_hw_device", "va",
		"-f", "lavfi", "-i", "color=c=black:s=640x360:d=0.2",
		"-vf", "format=nv12,hwupload",
		"-c:v", "av1_vaapi",
		"-f", "null", "-",
	)
	return runFFmpegSmoke(smoke)
}

func vaapiSmokeTestAV110(dev string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	smoke := exec.CommandContext(ctx, ffbin.FFmpeg(),
		"-hide_banner", "-loglevel", "error",
		"-init_hw_device", "vaapi=va:"+dev,
		"-filter_hw_device", "va",
		"-f", "lavfi", "-i", "color=c=black:s=640x360:d=0.2",
		"-vf", "format=p010le,hwupload",
		"-c:v", "av1_vaapi",
		"-f", "null", "-",
	)
	return runFFmpegSmoke(smoke)
}

func runFFmpegSmoke(cmd *exec.Cmd) error {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			if len(msg) > 400 {
				msg = msg[:400]
			}
			return fmt.Errorf("%w: %s", err, msg)
		}
	}
	return err
}

// Start begins an HLS transcode. ownerKey is the viewer tab id (or a fallback client key).
// A new start for the same key drops that viewer from its previous job. A matching running
// job is reused and this viewer is attached. pixFmt may be empty to probe source.
// force8Bit skips 10-bit AV1 encode so a browser reject can re-enter the 8-bit cascade.
func (m *Manager) Start(ctx context.Context, ownerKey, source string, audioIndex, height int, startAt float64, pixFmt string, force8Bit bool) (*Job, error) {
	startSec := 0
	if startAt > 0 {
		startSec = int(startAt)
	}

	m.mu.Lock()
	if ownerKey != "" {
		if id, ok := m.activeByKey[ownerKey]; ok {
			if j := m.jobs[id]; j != nil && jobMatches(j, source, audioIndex, height, startSec) && !skipAV110Reuse(j, force8Bit) {
				j.LastAccess = time.Now()
				m.attachTabLocked(j, ownerKey)
				m.mu.Unlock()
				if err := m.WaitPlaylist(ctx, j, 30*time.Second); err != nil {
					return nil, err
				}
				return j, nil
			}
			m.detachTabLocked(ownerKey)
		}
	}
	if j := m.findActiveLocked(source, audioIndex, height, startSec); j != nil && !skipAV110Reuse(j, force8Bit) {
		j.LastAccess = time.Now()
		m.attachTabLocked(j, ownerKey)
		m.mu.Unlock()
		if err := m.WaitPlaylist(ctx, j, 30*time.Second); err != nil {
			return nil, err
		}
		return j, nil
	}
	m.mu.Unlock()

	pixFmt = videoPixFmt(source, pixFmt)
	allowAV110 := m.vaapiAV110 && !force8Bit
	pipeline := selectInitialPipeline(m.useVAAPI, pixFmt, m.vaapiCodec, allowAV110)

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

// Cancel drops ownerKey from its job and stops the transcode when no viewers remain.
func (m *Manager) Cancel(ownerKey string) {
	m.ReleaseTab(ownerKey)
}

// ReleaseTab drops tabID from its job and stops the transcode when no viewers remain.
func (m *Manager) ReleaseTab(tabID string) {
	if tabID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.detachTabLocked(tabID)
}

// StopAll cancels every running transcode.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.jobs))
	for id := range m.jobs {
		ids = append(ids, id)
	}
	for _, id := range ids {
		m.removeJobLocked(id)
	}
}

func jobMatches(j *Job, source string, audioIndex, height, startSec int) bool {
	return j.Source == source && j.AudioIndex == audioIndex && j.Height == height && j.StartAt == startSec && (j.Status == "running" || j.Status == "ready")
}

func skipAV110Reuse(j *Job, force8Bit bool) bool {
	return force8Bit && j != nil && j.Pipeline == PipelineVAAPIFullAV110.String()
}

func (m *Manager) findActiveLocked(source string, audioIndex, height, startSec int) *Job {
	for _, j := range m.jobs {
		if jobMatches(j, source, audioIndex, height, startSec) {
			return j
		}
	}
	return nil
}

func (m *Manager) attachTabLocked(j *Job, tabID string) {
	if j == nil || tabID == "" {
		return
	}
	if j.Tabs == nil {
		j.Tabs = map[string]struct{}{}
	}
	j.Tabs[tabID] = struct{}{}
	j.OwnerKey = tabID
	m.activeByKey[tabID] = j.ID
}

func (m *Manager) detachTabLocked(tabID string) {
	id, ok := m.activeByKey[tabID]
	if !ok {
		return
	}
	delete(m.activeByKey, tabID)
	j := m.jobs[id]
	if j == nil {
		return
	}
	delete(j.Tabs, tabID)
	if j.OwnerKey == tabID {
		j.OwnerKey = ""
		for other := range j.Tabs {
			j.OwnerKey = other
			break
		}
	}
	if len(j.Tabs) == 0 {
		m.removeJobLocked(id)
	}
}

func (m *Manager) startJob(source string, audioIndex, height, startSec int, ownerKey string, pipeline Pipeline) (*Job, error) {
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.Source == source && j.AudioIndex == audioIndex && j.Height == height && j.StartAt == startSec && (j.Status == "running" || j.Status == "ready") {
			if pipeline != PipelineVAAPIFullAV110 && j.Pipeline == PipelineVAAPIFullAV110.String() {
				continue
			}
			j.LastAccess = time.Now()
			m.attachTabLocked(j, ownerKey)
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
		Status: "running", Pipeline: pipeline.String(), LastAccess: time.Now(), cancel: cancel,
		Tabs: map[string]struct{}{},
	}
	m.jobs[id] = j
	m.attachTabLocked(j, ownerKey)
	args := m.ffmpegArgs(source, out, pipeline, audioIndex, height, startSec)
	log.Printf("transcode pipeline=%s job=%s", pipeline, id)
	log.Printf("ffmpeg %s: %s", id, strings.Join(args, " "))
	cmd := exec.CommandContext(jobCtx, ffbin.FFmpeg(), args...)
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
	for tab, jid := range m.activeByKey {
		if jid == id {
			delete(m.activeByKey, tab)
		}
	}
	delete(m.jobs, id)
	go func() { _ = os.RemoveAll(outDir) }()
}

func (m *Manager) ffmpegArgs(source, out string, pipeline Pipeline, audioIndex, height, startSec int) []string {
	return ffmpegArgsForPipeline(m.vaapiDev, m.vaapiCodec, source, pipeline, audioIndex, height, startSec, m.Cfg, out)
}

func ffmpegArgsForPipeline(vaapiDev, vaapiCodec, source string, pipeline Pipeline, audioIndex, height, startSec int, cfg config.Config, out ...string) []string {
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
		segName := "seg_%03d.ts"
		hls = []string{
			"-c:a", "aac", "-ac", "2",
			"-f", "hls",
			"-hls_time", fmt.Sprintf("%d", seg),
			"-hls_list_size", "0",
			"-hls_playlist_type", "event",
			"-hls_flags", "independent_segments",
		}
		if vaapiCodec == "av1" {
			segName = "seg_%03d.m4s"
			hls = append(hls, "-hls_segment_type", "fmp4")
		}
		hls = append(hls,
			"-hls_segment_filename", filepath.Join(out[0], segName),
			filepath.Join(out[0], "master.m3u8"),
		)
	}

	ss := []string{}
	if startSec > 0 {
		ss = []string{"-ss", fmt.Sprintf("%d", startSec)}
	}

	switch pipeline {
	case PipelineVAAPIFull, PipelineVAAPIFull10Bit, PipelineVAAPIFullAV110, PipelineVAAPIHybrid:
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
		vcodec := "h264_vaapi"
		vextra := []string{"-profile:v", "high"}
		if vaapiCodec == "av1" {
			vcodec = "av1_vaapi"
			vextra = nil
		}
		args = append(args, "-c:v", vcodec)
		args = append(args, vextra...)
		args = append(args, "-force_key_frames", forceKF)
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
	format := "nv12"
	if pipeline == PipelineVAAPIFullAV110 {
		format = "p010"
	}
	scale := "scale_vaapi=format=" + format
	if height > 0 {
		scale = fmt.Sprintf("scale_vaapi=w=-2:h=%d:format=%s", height, format)
	}
	switch pipeline {
	case PipelineVAAPIFull10Bit:
		return "hwdownload,format=p010le,format=nv12,hwupload," + scale
	case PipelineVAAPIFull, PipelineVAAPIFullAV110:
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
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		idleCut := time.Now().Add(-10 * time.Minute)
		hours := m.Cfg.Transcode.CleanupHours
		if hours <= 0 {
			hours = 24
		}
		ageCut := time.Now().Add(-time.Duration(hours) * time.Hour)
		m.mu.Lock()
		for id, j := range m.jobs {
			if j.LastAccess.Before(idleCut) || j.LastAccess.Before(ageCut) {
				m.removeJobLocked(id)
			}
		}
		m.mu.Unlock()
	}
}
