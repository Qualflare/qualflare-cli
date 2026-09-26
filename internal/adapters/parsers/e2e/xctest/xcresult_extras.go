package xctest

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"qualflare-cli/internal/adapters/parsers/shared/toolrun"
	"qualflare-cli/internal/core/domain"
)

// Everything xcresulttool exposes beyond the case list and its activities:
// per-attempt retry history, and attachments with their bytes.
//
// Both were already in the bundle. The retry history needed one subcommand
// nobody was calling, and the attachments needed an export whose manifest is
// keyed by test — which is why this is a parser change and not an in-process
// reporter for iOS.
//
// A rule runs through this whole file: READ NOTHING THAT IS TRANSLATED.
// xcresulttool localizes its human-facing strings, which is not obvious until
// you see it — on a Turkish-locale machine the same bundle reports
// `"duration": "0,75s"` and names its repetitions "First Run" and "Retry 1".
// Only numeric and structural fields (durationInSeconds, nodeType,
// nodeIdentifier, result) are safe; anything else would work in CI and break
// on a developer's laptop.

// Attachments are inlined up to these limits and take the artifact route
// beyond them. The budget is per parse, matching the JVM reporters' own 8 MiB
// so one family-wide number governs how much rides in a report body.
const (
	maxInlineAttachmentBytes = 1 << 20 // 1 MiB for any single attachment
	maxInlineTotalBytes      = 8 << 20 // 8 MiB across the whole run
)

// testDetailsResponse mirrors `xcresulttool get test-results test-details`.
type testDetailsResponse struct {
	TestIdentifier string           `json:"testIdentifier"`
	TestResult     string           `json:"testResult"`
	TestRuns       []testDetailNode `json:"testRuns"`
}

type testDetailNode struct {
	Name              string           `json:"name"`
	NodeType          string           `json:"nodeType"`
	NodeIdentifier    string           `json:"nodeIdentifier"`
	Result            string           `json:"result"`
	DurationInSeconds float64          `json:"durationInSeconds"`
	Children          []testDetailNode `json:"children"`
}

// manifestEntry mirrors one element of the manifest.json that
// `xcresulttool export attachments` writes beside the exported files.
type manifestEntry struct {
	TestIdentifier string               `json:"testIdentifier"`
	Attachments    []manifestAttachment `json:"attachments"`
}

type manifestAttachment struct {
	ExportedFileName           string `json:"exportedFileName"`
	SuggestedHumanReadableName string `json:"suggestedHumanReadableName"`
	IsAssociatedWithFailure    bool   `json:"isAssociatedWithFailure"`
	RepetitionNumber           int    `json:"repetitionNumber"`
	DeviceName                 string `json:"deviceName"`
}

// attemptsForTest returns one attempt per repetition, or nil when the test ran
// once. Best-effort like the activities call: a test whose details cannot be
// read keeps its case-level result.
func attemptsForTest(ctx context.Context, bundlePath, testID string) ([]domain.Attempt, error) {
	if testID == "" {
		return nil, nil
	}
	out, err := toolrun.Run(ctx, "xcrun", "xcresulttool", "get", "test-results", "test-details",
		"--path", bundlePath, "--test-id", testID, "--compact")
	if err != nil {
		return nil, err
	}
	var resp testDetailsResponse
	if err := decodeXCResultJSON(out, &resp); err != nil {
		return nil, err
	}
	return attemptsFromTestRuns(resp.TestRuns), nil
}

// attemptsFromTestRuns finds Repetition nodes anywhere in the tree.
//
// Searching rather than indexing matters: the shape depends on whether the run
// used repetitions at all. With them, testRuns holds Repetition nodes
// directly; without them it holds the DEVICE that ran the test, whose child is
// a Test Plan Configuration. Reading testRuns[0] as an attempt would invent
// one for every ordinary test in every suite.
//
// Fewer than two repetitions means there is no history to tell, so nothing is
// reported — a single attempt says only what the case already says.
func attemptsFromTestRuns(runs []testDetailNode) []domain.Attempt {
	var repetitions []testDetailNode
	var walk func(nodes []testDetailNode)
	walk = func(nodes []testDetailNode) {
		for _, n := range nodes {
			if n.NodeType == "Repetition" {
				repetitions = append(repetitions, n)
				continue // a repetition's children are its runs, not more repetitions
			}
			walk(n.Children)
		}
	}
	walk(runs)

	if len(repetitions) < 2 {
		return nil
	}

	attempts := make([]domain.Attempt, 0, len(repetitions))
	for i, r := range repetitions {
		attempts = append(attempts, domain.Attempt{
			// Numbered by position, not by nodeIdentifier: the identifier is
			// a string whose meaning is xcresulttool's, and the wire wants a
			// 1-based sequence.
			Number:   i + 1,
			Status:   mapTestResult(r.Result),
			Duration: time.Duration(r.DurationInSeconds * float64(time.Second)),
			Message:  failureMessageFrom(r.Children),
		})
	}
	return attempts
}

// failureMessageFrom pulls the message off a repetition's Test Case Run child.
// The text is kept verbatim, including any "failed - " prefix: that prefix is
// xcresulttool's own wording and may itself be translated, so stripping it
// would be one more thing that behaves differently by locale.
func failureMessageFrom(children []testDetailNode) string {
	var messages []string
	for _, c := range children {
		if c.NodeType == "Test Case Run" && c.Name != "" {
			messages = append(messages, c.Name)
		}
	}
	return strings.Join(messages, "\n")
}

// retrySummary derives the two flags the wire carries alongside the attempts.
// Flaky means it ended green after being red — a test that failed every time
// is simply failed, however many attempts it took.
func retrySummary(attempts []domain.Attempt) (retries int, flaky bool) {
	if len(attempts) < 2 {
		return 0, false
	}
	retries = len(attempts) - 1
	last := attempts[len(attempts)-1]
	if last.Status != domain.StatusPassed {
		return retries, false
	}
	for _, a := range attempts[:len(attempts)-1] {
		if a.Status == domain.StatusFailed || a.Status == domain.StatusError {
			return retries, true
		}
	}
	return retries, false
}

// exportAttachments runs the export once for the whole bundle and returns the
// attachments keyed by test identifier.
//
// Once per run, not once per test: the export writes every attachment and one
// manifest, so asking per test would re-export the lot each time.
//
// The temp directory is deliberately not cleaned up here. The files outlive
// the parse — report_service resolves LocalPath and uploads them well after
// this returns — so deleting them here would delete the attachments before
// they are sent. They are small, and the OS reaps its temp directory.
func exportAttachments(ctx context.Context, bundlePath string) (map[string][]domain.Attachment, error) {
	dir, err := os.MkdirTemp("", "qf-xcresult-attachments-")
	if err != nil {
		return nil, err
	}
	if _, err := toolrun.Run(ctx, "xcrun", "xcresulttool", "export", "attachments",
		"--path", bundlePath, "--output-path", dir); err != nil {
		return nil, err
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("reading the export manifest: %w", err)
	}
	return attachmentsFromManifest(manifest, dir)
}

// attachmentsFromManifest maps the manifest onto domain attachments, reading
// each exported file from dir.
//
// Two routes, chosen by what the thing is:
//
//   - An image takes the artifact route (LocalPath + ArtifactKindImage), which
//     report_service uploads. Images are the kind that uploads BY DEFAULT, and
//     a screenshot is the reason to read attachments at all.
//   - Anything else small is inlined as base64. Calling it a trace would make
//     it opt-in, so a plain text attachment would silently vanish for everyone
//     who never passed --upload-artifacts. Past the size limits it becomes a
//     trace after all, because a large file has no business in a report body.
func attachmentsFromManifest(manifest []byte, dir string) (map[string][]domain.Attachment, error) {
	var entries []manifestEntry
	if err := decodeXCResultJSON(manifest, &entries); err != nil {
		return nil, fmt.Errorf("decoding the export manifest: %w", err)
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}

	byTest := make(map[string][]domain.Attachment, len(entries))
	var inlined int64
	for _, e := range entries {
		for _, a := range e.Attachments {
			if a.ExportedFileName == "" {
				continue
			}
			path := filepath.Join(absDir, a.ExportedFileName)
			info, err := os.Stat(path)
			if err != nil {
				// The manifest named a file the export did not write. Losing
				// one attachment is better than losing the run.
				continue
			}

			att := domain.Attachment{
				Name:     cleanAttachmentName(a.SuggestedHumanReadableName),
				MimeType: mimeTypeForFile(path),
			}
			if att.Name == "" {
				att.Name = a.ExportedFileName
			}

			switch {
			case strings.HasPrefix(att.MimeType, "image/"):
				att.LocalPath = path
				att.ArtifactKind = domain.ArtifactKindImage
			case info.Size() <= maxInlineAttachmentBytes && inlined+info.Size() <= maxInlineTotalBytes:
				content, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				att.Content = base64.StdEncoding.EncodeToString(content)
				inlined += info.Size()
			default:
				att.LocalPath = path
				att.ArtifactKind = domain.ArtifactKindTrace
			}

			byTest[e.TestIdentifier] = append(byTest[e.TestIdentifier], att)
		}
	}
	return byTest, nil
}

// mangledAttachmentName matches how XCTest names an attachment on disk:
// the name the test gave it, then an index, then a UUID.
var mangledAttachmentName = regexp.MustCompile(`^(.+)_\d+_[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}(\.[^.]+)?$`)

// cleanAttachmentName recovers the name the test actually used.
//
// XCTest stores `screenshot.png` as
// `screenshot_0_D7579D4F-66F9-4664-9204-EF82E212000F.png`, and a report full
// of UUIDs is materially worse to read. This is cosmetic only: anything that
// does not match the mangling is returned untouched, so a wrong guess can
// never lose the real name.
func cleanAttachmentName(name string) string {
	m := mangledAttachmentName.FindStringSubmatch(name)
	if m == nil {
		return name
	}
	return m[1] + m[2]
}

// mimeTypeForFile maps the exported extension. Deliberately a small explicit
// table rather than mime.TypeByExtension, whose answers depend on the host's
// own mime database — the same bundle would classify differently on different
// machines, which is the locale bug in another costume.
func mimeTypeForFile(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".heic":
		return "image/heic"
	case ".mov":
		return "video/quicktime"
	case ".mp4":
		return "video/mp4"
	case ".txt", ".log":
		return "text/plain"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	default:
		return "application/octet-stream"
	}
}
