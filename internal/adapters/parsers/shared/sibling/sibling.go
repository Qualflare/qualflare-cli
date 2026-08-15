// Package sibling looks up a companion file living next to a primary report
// file — for a framework whose report format is split across more than one
// file on disk (e.g. Maestro's commands.json next to its JUnit XML).
package sibling

import (
	"os"
	"path/filepath"
)

// Read looks for a file named name in the same directory as path (the
// primary input a parser was given) and returns its contents. ok is false
// with a nil error when the sibling simply doesn't exist — the expected
// common case a caller should treat as silent fallback, not a warning. Any
// other error (permissions, name is itself a directory, I/O failure) is
// returned via err so a caller can choose to surface it.
func Read(path, name string) (content []byte, ok bool, err error) {
	sibPath := filepath.Join(filepath.Dir(path), name)
	content, err = os.ReadFile(sibPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return content, true, nil
}
