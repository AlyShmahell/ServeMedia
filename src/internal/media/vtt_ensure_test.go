package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubtitleCachePathsStayStable(t *testing.T) {
	sum := sha256.Sum256([]byte("/videos/a.mkv"))
	prefix := hex.EncodeToString(sum[:12])
	if got, want := SubtitleVTTPath("/cache", "/videos/a.mkv", 3), filepath.Join("/cache", "subs", prefix+"-3.vtt"); got != want {
		t.Fatalf("subtitle path %s want %s", got, want)
	}
	side := sha256.Sum256([]byte("/videos/a.en.srt"))
	if got, want := SidecarVTTPath("/cache", "/videos/a.en.srt"), filepath.Join("/cache", "subs", "sidecar-"+hex.EncodeToString(side[:12])+".vtt"); got != want {
		t.Fatalf("sidecar path %s want %s", got, want)
	}
}

func TestEnsureVTTFileCacheHitSkipsWriter(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ready.vtt")
	body := []byte("WEBVTT\n\nready\n")
	if err := os.WriteFile(out, body, 0o644); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	err := ensureVTTFile(context.Background(), out, func(context.Context, string) error {
		calls.Add(1)
		return errors.New("should not write")
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("writer calls %d", calls.Load())
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("cache rewritten: %q", got)
	}
}

func TestEnsureVTTFileEmptyCacheIsRewritten(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "empty.vtt")
	if err := os.WriteFile(out, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	body := []byte("WEBVTT\n\nfilled\n")
	err := ensureVTTFile(context.Background(), out, func(_ context.Context, tmp string) error {
		return os.WriteFile(tmp, body, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %q", got)
	}
}

func TestEnsureVTTFileSingleFlight(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "shared.vtt")
	body := []byte("WEBVTT\n\nshared\n")
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	defer finish()
	var tmpName string
	write := func(_ context.Context, tmp string) error {
		if calls.Add(1) == 1 {
			tmpName = tmp
			close(entered)
		}
		<-release
		return os.WriteFile(tmp, body, 0o644)
	}
	errCh := make(chan error, 2)
	go func() { errCh <- ensureVTTFile(context.Background(), out, write) }()
	go func() { errCh <- ensureVTTFile(context.Background(), out, write) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not start")
	}
	waitFlightRefs(t, out, 2)
	finish()
	for i := 0; i < 2; i++ {
		if err := waitTestErr(t, errCh); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("writer calls %d", calls.Load())
	}
	if tmpName == out+".tmp" || filepath.Dir(tmpName) != dir {
		t.Fatalf("temp %s", tmpName)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(tmpName); !os.IsNotExist(err) {
		t.Fatalf("temp left behind: %v", err)
	}
}

func TestEnsureVTTFileSecondCallDoesNotWrite(t *testing.T) {
	out := filepath.Join(t.TempDir(), "once.vtt")
	body := []byte("WEBVTT\n\nonce\n")
	var calls atomic.Int32
	write := func(_ context.Context, tmp string) error {
		calls.Add(1)
		return os.WriteFile(tmp, body, 0o644)
	}
	if err := ensureVTTFile(context.Background(), out, write); err != nil {
		t.Fatal(err)
	}
	if err := ensureVTTFile(context.Background(), out, write); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("writer calls %d", calls.Load())
	}
}

func TestEnsureVTTFileFailedWriteLeavesNoFileAndRetries(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "retry.vtt")
	var calls atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	write := func(_ context.Context, tmp string) error {
		calls.Add(1)
		if fail.Load() {
			return errors.New("boom")
		}
		return os.WriteFile(tmp, []byte("WEBVTT\n\nok\n"), 0o644)
	}
	err := ensureVTTFile(context.Background(), out, write)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("published after failure: %v", statErr)
	}
	if entries, readErr := os.ReadDir(dir); readErr != nil || len(entries) != 0 {
		t.Fatalf("temps left: %v %v", entries, readErr)
	}
	fail.Store(false)
	if err := ensureVTTFile(context.Background(), out, write); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("writer calls %d", calls.Load())
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("WEBVTT\n\nok\n")) {
		t.Fatalf("got %q", got)
	}
}

func TestEnsureVTTFileErrorReachesEveryWaiter(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bad.vtt")
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	defer finish()
	write := func(context.Context, string) error {
		close(entered)
		<-release
		return errors.New("boom")
	}
	errCh := make(chan error, 2)
	go func() { errCh <- ensureVTTFile(context.Background(), out, write) }()
	go func() { errCh <- ensureVTTFile(context.Background(), out, write) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not start")
	}
	waitFlightRefs(t, out, 2)
	finish()
	for i := 0; i < 2; i++ {
		err := waitTestErr(t, errCh)
		if err == nil || err.Error() != "boom" {
			t.Fatalf("err %v", err)
		}
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("published failed extract")
	}
}

func TestEnsureVTTFileCancelOnlyCallerRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "cancel.vtt")
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- ensureVTTFile(ctx, out, func(ctx context.Context, tmp string) error {
			close(entered)
			<-ctx.Done()
			if err := os.WriteFile(tmp, []byte("WEBVTT\n\nnope\n"), 0o644); err != nil {
				return err
			}
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not start")
	}
	cancel()
	err := waitTestErr(t, errCh)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	waitDirEmpty(t, dir)
}

func TestEnsureVTTFileCancelOneOfTwoKeepsTheOther(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "stay.vtt")
	body := []byte("WEBVTT\n\nstayed\n")
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	defer finish()
	var calls atomic.Int32
	var writerCanceled atomic.Bool
	write := func(ctx context.Context, tmp string) error {
		if calls.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-release:
		case <-ctx.Done():
			writerCanceled.Store(true)
			return ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			writerCanceled.Store(true)
			return err
		}
		return os.WriteFile(tmp, body, 0o644)
	}
	err1ch := make(chan error, 1)
	err2ch := make(chan error, 1)
	go func() { err1ch <- ensureVTTFile(ctx1, out, write) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not start")
	}
	go func() { err2ch <- ensureVTTFile(context.Background(), out, write) }()
	waitFlightRefs(t, out, 2)
	cancel1()
	if err := waitTestErr(t, err1ch); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled caller: %v", err)
	}
	waitFlightRefs(t, out, 1)
	finish()
	if err := waitTestErr(t, err2ch); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("writer calls %d", calls.Load())
	}
	if writerCanceled.Load() {
		t.Fatal("writer canceled while a caller remained")
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %q", got)
	}
}

func TestEnsureVTTFileCanceledCallerDoesNotJoin(t *testing.T) {
	out := filepath.Join(t.TempDir(), "busy.vtt")
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	defer finish()
	body := []byte("WEBVTT\n\nbusy\n")
	errCh := make(chan error, 1)
	go func() {
		errCh <- ensureVTTFile(context.Background(), out, func(_ context.Context, tmp string) error {
			close(entered)
			<-release
			return os.WriteFile(tmp, body, 0o644)
		})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ensureVTTFile(ctx, out, func(context.Context, string) error {
		t.Error("canceled caller started a writer")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	waitFlightRefs(t, out, 1)
	finish()
	if err := waitTestErr(t, errCh); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %q", got)
	}
}

func TestEnsureVTTFileDistinctPathsRunTogether(t *testing.T) {
	dir := t.TempDir()
	path1 := filepath.Join(dir, "a.vtt")
	path2 := filepath.Join(dir, "b.vtt")
	var started sync.WaitGroup
	started.Add(2)
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	defer finish()
	var n1, n2 atomic.Int32
	errCh := make(chan error, 2)
	go func() {
		errCh <- ensureVTTFile(context.Background(), path1, func(_ context.Context, tmp string) error {
			n1.Add(1)
			started.Done()
			<-release
			return os.WriteFile(tmp, []byte("one"), 0o644)
		})
	}()
	go func() {
		errCh <- ensureVTTFile(context.Background(), path2, func(_ context.Context, tmp string) error {
			n2.Add(1)
			started.Done()
			<-release
			return os.WriteFile(tmp, []byte("two"), 0o644)
		})
	}()
	done := make(chan struct{})
	go func() {
		started.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("distinct paths did not run together")
	}
	finish()
	for i := 0; i < 2; i++ {
		if err := waitTestErr(t, errCh); err != nil {
			t.Fatal(err)
		}
	}
	if n1.Load() != 1 || n2.Load() != 1 {
		t.Fatalf("calls %d %d", n1.Load(), n2.Load())
	}
	for _, tc := range []struct{ path, body string }{{path1, "one"}, {path2, "two"}} {
		got, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.body {
			t.Fatalf("%s got %q", tc.path, got)
		}
	}
}

func TestWaitVTTPrefersFinishedFlight(t *testing.T) {
	f := &vttFlight{
		path: filepath.Join(t.TempDir(), "done.vtt"),
		done: make(chan struct{}),
		refs: 1,
		err:  errors.New("kept"),
		cancel: func() {
			t.Error("canceled a finished flight")
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	vttBeforeCancelLock = func() { close(f.done) }
	t.Cleanup(func() { vttBeforeCancelLock = nil })
	err := waitVTT(ctx, f)
	if !errors.Is(err, f.err) {
		t.Fatalf("err %v", err)
	}
	if f.refs != 1 {
		t.Fatalf("refs %d", f.refs)
	}
}

func TestEnsureSidecarVTTCopiesAndReusesCache(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "movie.en.vtt")
	body := []byte("WEBVTT\n\nfrom sidecar\n")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.vtt")
	if err := os.WriteFile(out, []byte("WEBVTT\n\ncached\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSidecarVTT(context.Background(), src, out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("WEBVTT\n\ncached\n")) {
		t.Fatalf("cache rewritten: %q", got)
	}
	if err := os.Remove(out); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- EnsureSidecarVTT(context.Background(), src, out)
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err = os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %q", got)
	}
	if err := EnsureSidecarVTT(context.Background(), src, out); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("second read %q", got)
	}
}

func waitTestErr(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
	return nil
}

func waitFlightRefs(t *testing.T, path string, n int) {
	t.Helper()
	waitUntil(t, func() bool {
		vttMu.Lock()
		defer vttMu.Unlock()
		f := vttFlights[path]
		return f != nil && f.refs == n
	})
}

func waitDirEmpty(t *testing.T, dir string) {
	t.Helper()
	waitUntil(t, func() bool {
		entries, err := os.ReadDir(dir)
		return err == nil && len(entries) == 0
	})
}

func waitUntil(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met")
}
