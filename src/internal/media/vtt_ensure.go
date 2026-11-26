package media

import (
	"context"
	"os"
	"path/filepath"
	"sync"
)

// vttFlight is one in-progress WebVTT publish for a cache path.
// refs counts callers still waiting. The write is canceled only when the
// last caller leaves, and the flight is then dropped so a later request
// starts a fresh attempt instead of joining a canceled one.
type vttFlight struct {
	path   string
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	err    error
	refs   int
}

var (
	vttMu      sync.Mutex
	vttFlights = map[string]*vttFlight{}
	// vttBeforeCancelLock, when set, runs after a caller gives up and before
	// the flight mutex is taken. It must not acquire vttMu. Tests use it to
	// finish the flight in that window.
	vttBeforeCancelLock func()
)

func vttReady(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

// ensureVTTFile runs write at most once per outPath until a non-empty file exists.
// write receives a private temp path and must write the complete file there.
// The temp is renamed onto outPath only after write returns nil.
// Canceling ctx drops this caller's ref. write's context is canceled only
// when no caller is still waiting.
func ensureVTTFile(ctx context.Context, outPath string, write func(ctx context.Context, tmp string) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if vttReady(outPath) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	vttMu.Lock()
	if f, ok := vttFlights[outPath]; ok {
		f.refs++
		vttMu.Unlock()
		return waitVTT(ctx, f)
	}
	flightCtx, cancel := context.WithCancel(context.Background())
	f := &vttFlight{
		path:   outPath,
		ctx:    flightCtx,
		cancel: cancel,
		done:   make(chan struct{}),
		refs:   1,
	}
	vttFlights[outPath] = f
	vttMu.Unlock()

	go func() {
		defer func() {
			vttMu.Lock()
			if vttFlights[outPath] == f {
				delete(vttFlights, outPath)
			}
			vttMu.Unlock()
			close(f.done)
		}()
		f.err = produceVTT(f.ctx, outPath, write)
	}()
	return waitVTT(ctx, f)
}

func produceVTT(ctx context.Context, outPath string, write func(ctx context.Context, tmp string) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if vttReady(outPath) {
		return nil
	}
	dir := filepath.Dir(outPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(outPath)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := write(ctx, name); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, outPath); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

func waitVTT(ctx context.Context, f *vttFlight) error {
	select {
	case <-f.done:
		return f.err
	case <-ctx.Done():
		if vttBeforeCancelLock != nil {
			vttBeforeCancelLock()
		}
		vttMu.Lock()
		select {
		case <-f.done:
			vttMu.Unlock()
			return f.err
		default:
		}
		f.refs--
		if f.refs == 0 {
			if vttFlights[f.path] == f {
				delete(vttFlights, f.path)
			}
			f.cancel()
		}
		vttMu.Unlock()
		return ctx.Err()
	}
}
