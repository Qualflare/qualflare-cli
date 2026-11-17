//go:build windows

package auth

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// WithFileLock runs fn while holding an exclusive lock on path+".lock", so
// concurrent qf processes serialise their read-modify-write of the store. The
// lock is released when fn returns. The parent directory is created (0700) if
// missing, as on a fresh install.
func WithFileLock(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, fileMode) //nolint:gosec // path derives from the config location
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := windows.Handle(f.Fd())
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol); err != nil {
		return fmt.Errorf("lock %s.lock: %w", path, err)
	}
	defer func() { _ = windows.UnlockFileEx(h, 0, 1, 0, ol) }()
	return fn()
}
