package auth

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const lockHelperEnv = "QF_LOCK_HELPER_PATH"

// TestMain lets the test binary double as the lock-holding helper subprocess.
func TestMain(m *testing.M) {
	if p := os.Getenv(lockHelperEnv); p != "" {
		err := WithFileLock(p, func() error {
			return holdCriticalSection(p + ".marker")
		})
		if err != nil {
			os.Stderr.WriteString(err.Error())
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// holdCriticalSection fails if another holder is inside the section.
func holdCriticalSection(marker string) error {
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_ = f.Close()
	time.Sleep(150 * time.Millisecond)
	return os.Remove(marker)
}

func TestWithFileLock_Serializes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qualflare", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 2 {
		wg.Go(func() {
			errs <- WithFileLock(path, func() error { return holdCriticalSection(path + ".marker") })
		})
	}
	for range 2 {
		wg.Go(func() {
			cmd := exec.Command(os.Args[0], "-test.run=NONE") //nolint:gosec // re-executing the test binary
			cmd.Env = append(os.Environ(), lockHelperEnv+"="+path)
			out, err := cmd.CombinedOutput()
			if err != nil {
				errs <- &helperError{err: err, out: string(out)}
				return
			}
			errs <- nil
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("overlap or failure: %v", err)
		}
	}

	info, err := os.Stat(path + ".lock")
	if err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 && !strings.Contains(strings.ToLower(os.Getenv("OS")), "windows") {
		t.Errorf("lock mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestWithFileLock_ReturnsFnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.toml")
	want := os.ErrInvalid
	if got := WithFileLock(path, func() error { return want }); got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	// Lock is released: a second call must not block.
	if err := WithFileLock(path, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}

type helperError struct {
	err error
	out string
}

func (e *helperError) Error() string { return e.err.Error() + ": " + e.out }
