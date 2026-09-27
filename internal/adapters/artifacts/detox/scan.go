package detox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"qualflare-cli/internal/core/domain"
)

// ErrNoArtifactDirectories marks the one case where a directory holding zero
// subdirectories is not a user error: a Detox run that wrote no per-test
// artifacts at all, because every test passed and screenshots and video were
// off. ResolveRunDir still fails loudly for this case (Task 3's rule: an
// empty directory is far more likely to be a wrong path than an artifact-free
// run), but wraps the failure in this sentinel so Attach can tell the two
// apart and treat a genuinely artifact-free run as a no-op instead of an
// error.
var ErrNoArtifactDirectories = errors.New("no artifact directories")

// ResolveRunDir turns whatever the user passed into the single run directory
// to scan.
//
// Detox writes <rootDir>/<configuration>.<timestamp>/, so --artifacts-dir may
// reasonably name either level.
//
// Classification logic:
// - If any child name carries a status glyph (✓ or ✗), the directory is a run; return it as given.
// - Else if any grandchild name carries a glyph, the directory is a root; return its newest child.
// - Else return the directory as given (safe fallback for all-skipped runs).
//
// "Newest" is by modification time rather than by parsing the timestamp out of
// the name: the name's format is Detox's to change, and the filesystem already
// knows the answer.
func ResolveRunDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("reading the artifacts directory: %w", err)
	}

	// Collect actual directories (following symlinks) by name.
	var subdirs []string
	for _, e := range entries {
		fullPath := filepath.Join(dir, e.Name())
		info, err := os.Stat(fullPath)
		if err != nil {
			// Skip entries that can't be stat'd (broken symlinks, etc.)
			continue
		}
		if info.IsDir() {
			subdirs = append(subdirs, e.Name())
		}
	}

	// No directories at all is an error.
	if len(subdirs) == 0 {
		return "", fmt.Errorf("%s holds no directories: it is neither a Detox artifacts root nor a run within one: %w", dir, ErrNoArtifactDirectories)
	}

	// Level 1: Check if any child has a glyph → this is a run.
	for _, child := range subdirs {
		if hasStatusGlyph(child) {
			return dir, nil
		}
	}

	// Level 2: Check if any grandchild has a glyph → this is a root.
	for _, child := range subdirs {
		childPath := filepath.Join(dir, child)
		grandentries, err := os.ReadDir(childPath)
		if err != nil {
			continue
		}
		for _, ge := range grandentries {
			if hasStatusGlyph(ge.Name()) {
				// Found a glyphed grandchild, return the newest child (by mtime).
				newest, newestMod := "", int64(-1)
				for _, c := range subdirs {
					cPath := filepath.Join(dir, c)
					cInfo, err := os.Stat(cPath)
					if err != nil {
						continue
					}
					if mod := cInfo.ModTime().UnixNano(); mod > newestMod {
						newest, newestMod = cPath, mod
					}
				}
				if newest == "" {
					return "", fmt.Errorf("could not read any run directory under %s", dir)
				}
				return newest, nil
			}
		}
	}

	// Fallback: return the directory as given.
	return dir, nil
}

func hasStatusGlyph(name string) bool {
	return strings.HasPrefix(name, "✓ ") || strings.HasPrefix(name, "✗ ")
}

// maxInvocationsProbed bounds how many retries we look for per case. Detox
// suffixes invocation 2 onward, and a suite retrying a single test more than
// this is pathological; probing is cheap (a map lookup) but unbounded probing
// is still a loop with no reason to stop.
const maxInvocationsProbed = 10

// Attach matches each per-test directory under dir to a case in launch and
// appends its files as attachments.
//
// Matching computes forward: for every case, every plausible directory name is
// computed and looked up. The reverse — parsing a directory name back into a
// test name — cannot work, because the transformation is lossy: a test named
// "a/b" and one named "a_b" both produce "a_b".
//
// Every status glyph is tried per case, not just the one the case ended on: a
// test that failed and was retried green has a "✗ " directory for its first
// invocation and a "✓ … (2)" for its second.
//
// unreadable is returned separately from unmatched: a directory that matched
// no case by name and a directory that matched a case but could not be read
// (permission denied, say) are different problems with different fixes, and
// folding them into one list makes a mode bit look like a naming bug.
func Attach(launch *domain.Launch, dir string) (matched int, unmatched, unreadable []string, err error) {
	runDir, err := ResolveRunDir(dir)
	if err != nil {
		// A run that legitimately wrote no per-test artifacts (every test
		// passed, screenshots and video both off) looks identical on disk to
		// an empty directory. ResolveRunDir cannot tell those apart from a
		// wrong path, so it errors; Attach can, because it also knows the
		// caller is willing to report zero matches as a warning rather than
		// treat it as fatal.
		if errors.Is(err, ErrNoArtifactDirectories) {
			return 0, nil, nil, nil
		}
		return 0, nil, nil, err
	}
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("reading the run directory: %w", err)
	}

	remaining := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			remaining[e.Name()] = true
		}
	}

	for s := range launch.Suites {
		for c := range launch.Suites[s].Cases {
			cs := &launch.Suites[s].Cases[c]
			// Detox builds its directory names from the test's fullName, and
			// which FIELD carries fullName depends on which parser produced the
			// report — so every plausible field is tried rather than one being
			// chosen. See candidateMatchKeys: picking a single field shipped a
			// feature that matched nothing at all for @qualflare/jest reports,
			// whose IDs are file-qualified.
			matchKey, keyFound := firstMatchingKey(candidateMatchKeys(cs), remaining)
			if !keyFound {
				continue
			}
			found := false
			for invocation := 1; invocation <= maxInvocationsProbed; invocation++ {
				for _, status := range []string{"passed", "failed", ""} {
					name := DirectoryName(matchKey, status, invocation)
					// Two case names that sanitize to the same directory name
					// (e.g. "a/b" and "a_b"), or the same name repeated across
					// suites, collide here: whichever case is processed first
					// claims the directory, and the other finds nothing left
					// to match. That is the best a forward-only algorithm can
					// do given the lossy name transform (see the doc comment
					// above) — accepted, not an oversight.
					if !remaining[name] {
						continue
					}
					delete(remaining, name)
					atts, attErr := attachmentsIn(filepath.Join(runDir, name), invocation)
					if attErr != nil {
						unreadable = append(unreadable, name)
						continue
					}
					cs.Attachments = append(cs.Attachments, atts...)
					found = true
				}
			}
			if found {
				matched++
			}
		}
	}

	unmatched = make([]string, 0, len(remaining))
	for name := range remaining {
		unmatched = append(unmatched, name)
	}
	sort.Strings(unmatched)
	sort.Strings(unreadable)
	return matched, unmatched, unreadable, nil
}

// candidateMatchKeys returns the strings Detox might have built this case's
// directory name from, most-qualified first.
//
// There is one join key in the design — the test's fullName — but no single
// Case field holds it across every parser, and assuming one did is what broke
// this feature end to end:
//
//   - This CLI's own Jest parser (parsers/.../jest.go) puts fullName in ID.
//   - @qualflare/jest, and every reporter emitting qualflare-json, puts
//     fullName in Name and a FILE-QUALIFIED "<file>#<fullName>" in ID.
//
// Matching on ID alone therefore computed `✗ e2e/login.test.js#Login signs in`
// for a directory Detox had named `✗ Login signs in`, and matched nothing — for
// the reporter @qualflare/detox actually ships. Matching on Name alone fails
// the other way, on a parser whose Name is only the short title.
//
// Trying each candidate does not weaken the forward-only design: every
// candidate is still an exact computed lookup, never a reverse-engineered
// comparison. The third candidate is the tail of a '#'-qualified ID, which
// covers a parser that qualifies the ID without also setting Name to fullName.
func candidateMatchKeys(cs *domain.Case) []string {
	keys := make([]string, 0, 3)
	add := func(k string) {
		if k == "" {
			return
		}
		for _, existing := range keys {
			if existing == k {
				return
			}
		}
		keys = append(keys, k)
	}

	// Name first: it is fullName for qualflare-json, which is the format every
	// Qualflare reporter writes, and the one a Detox user actually produces.
	add(cs.Name)
	add(cs.ID)
	if idx := strings.LastIndex(cs.ID, "#"); idx >= 0 {
		add(cs.ID[idx+1:])
	}
	return keys
}

// firstMatchingKey returns the first candidate with at least one directory on
// disk, without consuming it.
//
// Choosing the key BEFORE collecting is what keeps a retried test's
// invocations together: probing key-by-key inside the collection loop would let
// invocation 1 match on Name and invocation 2 on ID, and worse, would let a case
// claim a directory belonging to a different case through its weaker candidate.
func firstMatchingKey(keys []string, remaining map[string]bool) (string, bool) {
	for _, key := range keys {
		for invocation := 1; invocation <= maxInvocationsProbed; invocation++ {
			for _, status := range []string{"passed", "failed", ""} {
				if remaining[DirectoryName(key, status, invocation)] {
					return key, true
				}
			}
		}
	}
	return "", false
}

// attachmentsIn reads one per-test directory. The invocation is folded into
// each name rather than into a separate field, because artifacts attach to the
// CASE: domain.Attempt has no attachment field, and adding one would be a
// server change.
func attachmentsIn(dir string, invocation int) ([]domain.Attachment, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}

	var atts []domain.Attachment
	for _, e := range entries {
		// os.Stat, not e.IsDir(): the latter does not follow symlinks (it
		// reports the link itself), so a symlink to a directory would be
		// treated as a file and attached as an artifact. ResolveRunDir already
		// uses os.Stat for the same reason (see its own comment) — this keeps
		// the two consistent.
		info, statErr := os.Stat(filepath.Join(dir, e.Name()))
		if statErr != nil {
			// Unreadable/broken entry: skip rather than fail the whole
			// directory over one bad entry.
			continue
		}
		if info.IsDir() {
			continue
		}
		name := e.Name()
		if invocation > 1 {
			name = fmt.Sprintf("%s (attempt %d)", name, invocation)
		}
		kind, mime := kindForArtifact(e.Name())
		atts = append(atts, domain.Attachment{
			Name:         name,
			MimeType:     mime,
			LocalPath:    filepath.Join(abs, e.Name()),
			ArtifactKind: kind,
		})
	}
	return atts, nil
}

// kindForArtifact maps Detox's five artifact types onto the kinds
// --upload-artifacts gates. Images upload by default; video and traces are
// opt-in, video because it is the largest thing in a report by an order of
// magnitude and logs because a device log from a real app is the artifact most
// likely to carry customer data.
func kindForArtifact(filename string) (kind, mime string) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png":
		return domain.ArtifactKindImage, "image/png"
	case ".jpg", ".jpeg":
		return domain.ArtifactKindImage, "image/jpeg"
	case ".mp4":
		return domain.ArtifactKindVideo, "video/mp4"
	case ".log":
		return domain.ArtifactKindTrace, "text/plain"
	case ".dtxrec", ".dtxplain", ".viewhierarchy":
		// Same result as the default arm (ArtifactKindTrace,
		// application/octet-stream). Kept explicit, not merged, to name Detox's
		// other artifact types rather than let them read as forgotten: .dtxrec
		// and .dtxplain are its instruments recordings, .viewhierarchy its UI
		// hierarchy snapshots.
		//
		// The extension WAS ".uihierarchy" here, which Detox never writes — the
		// design doc's artifact table said so and this encoded it faithfully.
		// IosUIHierarchyPlugin.js writes `${key}.viewhierarchy`. Nothing broke,
		// because the default arm returns the same pair, which is exactly why a
		// wrong-but-harmless value like that survives review: it is only
		// findable by reading the producer, not by testing the consumer.
		return domain.ArtifactKindTrace, "application/octet-stream"
	default:
		return domain.ArtifactKindTrace, "application/octet-stream"
	}
}
