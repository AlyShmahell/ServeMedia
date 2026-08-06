package watchdog

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type Supervisor struct {
	cfg Config
	mu  sync.Mutex

	lastPulse time.Time
	hasPulse  bool

	cmd     *exec.Cmd
	startMu sync.Mutex
}

func NewSupervisor(cfg Config) *Supervisor {
	return &Supervisor{cfg: cfg}
}

func (s *Supervisor) Pulse(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastPulse = now
	s.hasPulse = true
}

func (s *Supervisor) Tick(ctx context.Context) {
	now := time.Now()
	s.mu.Lock()
	pulseAge := time.Duration(0)
	active := false
	if s.hasPulse {
		pulseAge = now.Sub(s.lastPulse)
		active = pulseAge < s.cfg.PulseIdle
	}
	s.mu.Unlock()

	up := medoraUp(ctx, s.cfg.HealthURL)
	switch {
	case active && !up:
		log.Printf("watchdog: pulse active (%v ago), starting medora", pulseAge)
		if err := s.startMedora(); err != nil {
			log.Printf("watchdog: start medora: %v", err)
		}
	case !active && s.hasPulse && up:
		log.Printf("watchdog: pulse idle (%v), stopping medora", pulseAge)
		if err := s.stopMedora(); err != nil {
			log.Printf("watchdog: stop medora: %v", err)
		}
	}
}

func (s *Supervisor) Status(ctx context.Context) (medoraRunning bool, pulseActive bool) {
	s.mu.Lock()
	last := s.lastPulse
	has := s.hasPulse
	s.mu.Unlock()
	if has {
		pulseActive = time.Since(last) < s.cfg.PulseIdle
	}
	return medoraUp(ctx, s.cfg.HealthURL), pulseActive
}

func medoraUp(ctx context.Context, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (s *Supervisor) startMedora() error {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	s.mu.Lock()
	running := s.cmd != nil && s.cmd.Process != nil && s.cmd.ProcessState == nil
	s.mu.Unlock()
	if running || medoraUp(context.Background(), s.cfg.HealthURL) {
		return nil
	}

	cmd := exec.Command(s.cfg.MedoraBin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), "MEDORA_HTTP_ADDR="+s.cfg.MedoraInternalAddr)
	if err := cmd.Start(); err != nil {
		return err
	}

	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()

	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		if s.cmd == cmd {
			s.cmd = nil
		}
		s.mu.Unlock()
		if err != nil {
			log.Printf("watchdog: medora exited: %v", err)
		}
	}()
	return nil
}

func (s *Supervisor) StopMedora() error {
	return s.stopMedora()
}

func (s *Supervisor) stopMedora() error {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	s.mu.Lock()
	cmd := s.cmd
	s.cmd = nil
	s.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		pgid, err := syscall.Getpgid(cmd.Process.Pid)
		if err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
		} else {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			} else {
				_ = cmd.Process.Kill()
			}
		}
	}
	return nil
}
