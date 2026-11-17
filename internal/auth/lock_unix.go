//go:build !windows

package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// WithFileLock runs fn while holding an exclusive lock on path+".lock", so
// concurrent qf processes serialise their read-modify-write of the store. The
// lock is released when fn returns. The parent directory is created (0700) if
// missing, as on a fresh install.
func WithFileLock(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil { //nolint:gosec // path derives from the config location
		return fmt.Errorf("create config dir: %w", err)
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, fileMode) //nolint:gosec // path derives from the config location
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	defer func() { _ = f.Close() }()
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX) //nolint:gosec // fd fits in int
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("lock %s.lock: %w", path, err)
	}
	defer func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }() //nolint:gosec // fd fits in int
	return fn()
}
