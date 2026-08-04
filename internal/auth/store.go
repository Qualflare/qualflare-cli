// Package auth manages per-project credential storage backed by a TOML file.
package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

const (
	configDirName  = "qualflare"
	configFileName = "config.toml"
	fileMode       = 0o600
	dirMode        = 0o700
	schemaVersion  = 1
)

var (
	ErrNotFound      = errors.New("identifier not found")
	ErrAlreadyExists = errors.New("identifier already exists")
)

// Identifier holds the credentials saved for one project alias.
type Identifier struct {
	Token string `toml:"token"`
}

type fileFormat struct {
	SchemaVersion int                   `toml:"schema_version"`
	Identifiers   map[string]Identifier `toml:"identifiers"`
}

// Store is an in-memory view of the on-disk credentials file. Mutations
// only persist after Save.
type Store struct {
	path string
	data fileFormat
}

// DefaultPath returns the platform-specific path to config.toml under
// os.UserConfigDir.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(dir, configDirName, configFileName), nil
}

// Load reads the store at path. A missing file returns an empty store
// (not an error). A malformed file returns a wrapped error.
func Load(path string) (*Store, error) {
	s := &Store{
		path: path,
		data: fileFormat{
			SchemaVersion: schemaVersion,
			Identifiers:   map[string]Identifier{},
		},
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	// Warn (don't fail — that would lock the user out) if the token file is
	// group/world-accessible, so a 0644 file doesn't silently expose the token (SEC-05).
	if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm()&0o077 != 0 {
		fmt.Fprintf(os.Stderr, "warning: %s is group/world-accessible (mode %04o); run `chmod 600 %s` to protect your API token\n", path, info.Mode().Perm(), path)
	}
	if len(b) == 0 {
		return s, nil
	}
	var parsed fileFormat
	if _, err := toml.Decode(string(b), &parsed); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if parsed.Identifiers == nil {
		parsed.Identifiers = map[string]Identifier{}
	}
	if parsed.SchemaVersion == 0 {
		parsed.SchemaVersion = schemaVersion
	}
	s.data = parsed
	return s, nil
}

// Path returns the file path the store was loaded from.
func (s *Store) Path() string { return s.path }

// Get returns the token for id and whether it exists.
func (s *Store) Get(id string) (string, bool) {
	v, ok := s.data.Identifiers[id]
	return v.Token, ok
}

// Has reports whether id is present.
func (s *Store) Has(id string) bool {
	_, ok := s.data.Identifiers[id]
	return ok
}

// Set inserts or replaces the token for id.
func (s *Store) Set(id, token string) {
	s.data.Identifiers[id] = Identifier{Token: token}
}

// Delete removes id. Returns ErrNotFound if absent.
func (s *Store) Delete(id string) error {
	if _, ok := s.data.Identifiers[id]; !ok {
		return ErrNotFound
	}
	delete(s.data.Identifiers, id)
	return nil
}

// List returns all identifiers sorted alphabetically.
func (s *Store) List() []string {
	out := make([]string, 0, len(s.data.Identifiers))
	for k := range s.data.Identifiers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Save atomically writes the store to disk. Creates the parent directory
// (0700) if missing and writes the file with 0600 perms.
func (s *Store) Save() error {
	if s.path == "" {
		return errors.New("store has no path")
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	if s.data.SchemaVersion == 0 {
		s.data.SchemaVersion = schemaVersion
	}
	if s.data.Identifiers == nil {
		s.data.Identifiers = map[string]Identifier{}
	}

	tmp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if err := os.Chmod(tmpPath, fileMode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}

	enc := toml.NewEncoder(tmp)
	if err := enc.Encode(s.data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("encode toml: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
