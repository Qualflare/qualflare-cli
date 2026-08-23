# qualflare-cli: native shard collect + video upload Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `qualflare-cli` becomes the sole uploader for both `@qualflare/cypress` and
`@qualflare/cucumberjs` — it accepts a directory of their standardized report files, merges them
using each file's own embedded `shardIndex` (no flag needed), and resolves any pending video
attachment via the presigned-URL upload flow those reporters used to perform themselves.

**Architecture:** No changes to the merge machinery itself — `ParseTestResults` already merges
every file it's given into one `domain.Launch` regardless of flags; this plan only teaches
`collect` to accept a directory argument (expanding to its `*.json` files) and teaches the
qualflare-native parser to read `shardIndex` from the source JSON instead of relying on
`--shard`'s file-order tagging (which stays, unchanged, for every other framework). Video
resolution is a new pass between parsing and sending: walk every attachment, resolve any
`LocalVideoPath` via a presigned-URL upload using the same auth token `SendReport` already uses,
fail-open per attachment.

**Tech Stack:** Go, `resty` HTTP client, `cobra` CLI, table-driven tests.

**Spec:** `../qualflare-cypress/docs/superpowers/specs/2026-08-24-native-sharded-collect-design.md`
(authored in the sibling `qualflare-cypress` repo; covers all three repos).

## Global Constraints

- No backend (`api-service`) changes — `domain.Attachment.StorageKey`/`FileSize` map onto fields
  the server already accepts (see the qualflare-cypress plan's Task 3 doc comment, which cites the
  matching server-side `launch.Attachment` fields from the video-attachment backend work already
  merged this session).
- `--shard` is unchanged in behavior and scope — it stays the only merge-tagging mechanism for
  every non-Qualflare-format framework (JUnit XML, pytest, TestNG, ...).
- `domain.Attachment.LocalVideoPath` must never be serialized into an outgoing request — use
  `json:"-"`.
- Video upload is fail-open per attachment (skip + warn), matching the reporters' existing
  established policy. A missing/malformed report file is a hard CLI error.

---

## Task 1: `domain.Attachment` gains `StorageKey`/`FileSize`/`LocalVideoPath`; `ReportSender` gains `UploadVideo`

**Files:**
- Modify: `internal/core/domain/models.go`
- Modify: `internal/core/ports/interfaces.go`
- Modify: `internal/core/services/report_service_parse_test.go` (the `stubSender` there must keep
  satisfying `ports.ReportSender` after this change)

**Interfaces:**
- Produces: `domain.Attachment` gains `StorageKey string \`json:"storageKey,omitempty"\``,
  `FileSize int64 \`json:"fileSize,omitempty"\``, `LocalVideoPath string \`json:"-"\``.
- Produces: `ports.ReportSender` gains
  `UploadVideo(ctx context.Context, localPath, mimeType string) (storageKey string, fileSize int64, err error)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/core/domain/models_test.go` (create if it doesn't exist — check first):

```go
package domain

import (
	"encoding/json"
	"testing"
)

func TestAttachmentLocalVideoPathNeverSerializes(t *testing.T) {
	a := Attachment{Name: "clip", LocalVideoPath: "/tmp/should-not-appear.mp4"}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if got := string(b); containsSubstring(got, "should-not-appear") {
		t.Fatalf("LocalVideoPath leaked into wire JSON: %s", got)
	}
}

func TestAttachmentStorageKeyAndFileSizeSerialize(t *testing.T) {
	a := Attachment{Name: "clip", StorageKey: "case-run-attachments/proj/1.mp4", FileSize: 12345}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	got := string(b)
	if !containsSubstring(got, `"storageKey":"case-run-attachments/proj/1.mp4"`) {
		t.Fatalf("storageKey missing from wire JSON: %s", got)
	}
	if !containsSubstring(got, `"fileSize":12345`) {
		t.Fatalf("fileSize missing from wire JSON: %s", got)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/core/domain/... -run TestAttachment -v`
Expected: FAIL — compile error, `Attachment` has no `StorageKey`/`FileSize`/`LocalVideoPath` fields.

- [ ] **Step 3: Add the fields**

In `internal/core/domain/models.go`, find `type Attachment struct` (around line 304) and add:

```go
type Attachment struct {
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Content  string `json:"content,omitempty"` // Base64 encoded
	// StorageKey/FileSize mirror the server's launch.Attachment fields of the
	// same name (video attachments uploaded via the presigned-URL flow) —
	// set by report_service.go's video-resolution pass, never by a parser
	// directly.
	StorageKey string `json:"storageKey,omitempty"`
	FileSize   int64  `json:"fileSize,omitempty"`
	// LocalVideoPath is set by the qualflare-native parser only (see
	// internal/adapters/parsers/native/qualflare) when a report file
	// references a video it hasn't uploaded itself — an absolute path,
	// resolved at parse time relative to that source file's own directory.
	// Never sent to the server: report_service.go's video-resolution pass
	// consumes it and fills StorageKey/FileSize before SendReport is called.
	LocalVideoPath string `json:"-"`
}
```

- [ ] **Step 4: Widen `ports.ReportSender` and fix `stubSender`**

In `internal/core/ports/interfaces.go`, change:
```go
type ReportSender interface {
	// SendReport sends a report to the API
	SendReport(ctx context.Context, report *domain.Launch) error
}
```
to:
```go
type ReportSender interface {
	// SendReport sends a report to the API
	SendReport(ctx context.Context, report *domain.Launch) error
	// UploadVideo uploads a local video file via the presigned-URL flow
	// (POST /api/v1/attachments/upload-url, then PUT the bytes) and returns
	// the resulting storageKey and the file's byte size.
	UploadVideo(ctx context.Context, localPath, mimeType string) (storageKey string, fileSize int64, err error)
}
```

In `internal/core/services/report_service_parse_test.go`, add to `stubSender`:
```go
func (s *stubSender) UploadVideo(_ context.Context, _, _ string) (string, int64, error) {
	return "", 0, nil
}
```
Run `grep -rn "ReportSender\b" --include="*.go" .` first to find every other type asserted against
this interface (mocks, fakes) and add the same stub method to each — do not assume `stubSender` is
the only one.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/core/domain/... ./internal/core/services/... -v`
Expected: PASS, and the whole module compiles (confirming every `ReportSender` implementer was
found and fixed).

- [ ] **Step 6: Commit**

```bash
git add internal/core/domain/models.go internal/core/domain/models_test.go internal/core/ports/interfaces.go internal/core/services/report_service_parse_test.go
git commit -m "feat(domain): add Attachment.StorageKey/FileSize/LocalVideoPath, widen ReportSender for video upload"
```

---

## Task 2: `http.Client.UploadVideo` — the presigned-URL flow, ported from the reporters

**Files:**
- Modify: `internal/adapters/http/client.go`
- Test: `internal/adapters/http/client_test.go` (check for an existing test file first —
  `internal/adapters/http/*_test.go`; extend it if one exists)

**Interfaces:**
- Produces: `func (c *Client) UploadVideo(ctx context.Context, localPath, mimeType string) (storageKey string, fileSize int64, err error)`.

- [ ] **Step 1: Write the failing test**

Add a new test file `internal/adapters/http/upload_video_test.go`. Two separate `httptest.Server`s,
constructed in dependency order — the PUT-target server first, so its URL is known before the
presign server's handler is written (a single shared mux can't reference its own server's URL from
inside a handler defined before `httptest.NewServer` returns it):

```go
package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"qualflare-cli/internal/config"
)

func TestUploadVideoRoundTrip(t *testing.T) {
	var gotPutBody []byte
	var gotPutContentType string

	putServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotPutBody = buf
		gotPutContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer putServer.Close()

	presignServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uploadUrl":"` + putServer.URL + `","storageKey":"case-run-attachments/proj/1.mp4"}`))
	}))
	defer presignServer.Close()

	cfg := config.DefaultConfig()
	cfg.SetAPIEndpoint(presignServer.URL)
	cfg.SetAPIKey("test-token")
	client := NewHTTPClient(cfg)
	defer client.Close()

	dir := t.TempDir()
	videoPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(videoPath, []byte("fake-video-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	storageKey, fileSize, err := client.UploadVideo(context.Background(), videoPath, "video/mp4")
	if err != nil {
		t.Fatalf("UploadVideo error: %v", err)
	}
	if storageKey != "case-run-attachments/proj/1.mp4" {
		t.Errorf("storageKey = %q", storageKey)
	}
	if fileSize != int64(len("fake-video-bytes")) {
		t.Errorf("fileSize = %d, want %d", fileSize, len("fake-video-bytes"))
	}
	if string(gotPutBody) != "fake-video-bytes" {
		t.Errorf("PUT body = %q", gotPutBody)
	}
	if gotPutContentType != "video/mp4" {
		t.Errorf("PUT Content-Type = %q", gotPutContentType)
	}
}
```

Check `internal/config`'s real `Config` type for the exact method names (`SetAPIEndpoint`,
`SetAPIKey` used above are a guess based on the `ConfigProvider` interface's `GetAPIEndpoint`/
`GetAPIKey` — grep `internal/config/config.go` for the real setter names before finalizing this
test; adjust if they differ).

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/adapters/http/... -run TestUploadVideoRoundTrip -v`
Expected: FAIL — `UploadVideo` method doesn't exist on `*Client`.

- [ ] **Step 3: Implement `UploadVideo`**

Add to `internal/adapters/http/client.go`:

```go
type uploadURLRequest struct {
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	FileSize int64  `json:"fileSize"`
}

type uploadURLResponse struct {
	UploadURL  string `json:"uploadUrl"`
	StorageKey string `json:"storageKey"`
}

// UploadVideo uploads one local video file via the presigned-URL flow
// (POST /api/v1/attachments/upload-url -> PUT bytes), mirroring the
// @qualflare/cypress and @qualflare/cucumberjs reporters' own
// video-uploader.ts, now that neither reporter performs uploads itself.
// Single attempt, no retry on the PUT (the presign request itself still
// goes through the client's normal retry policy via c.resty) — the caller
// (report_service.go) treats any error here as fail-open: log and skip,
// never fail the whole collect.
func (c *Client) UploadVideo(ctx context.Context, localPath, mimeType string) (string, int64, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return "", 0, fmt.Errorf("stat video file: %w", err)
	}
	fileSize := info.Size()

	reqURL := c.endpoint + apiBasePath + "/attachments/upload-url"
	var presign uploadURLResponse
	resp, err := c.resty.R().
		SetContext(ctx).
		SetBody(uploadURLRequest{
			Filename: filepath.Base(localPath),
			MimeType: mimeType,
			FileSize: fileSize,
		}).
		SetResult(&presign).
		Post(reqURL)
	if err != nil {
		return "", 0, &APIError{Op: "upload-url", Message: "failed to request upload URL", Err: err}
	}
	if !resp.IsStatusSuccess() {
		return "", 0, c.buildAPIError("upload-url", resp)
	}

	body, err := os.ReadFile(localPath)
	if err != nil {
		return "", 0, fmt.Errorf("read video file: %w", err)
	}

	putResp, err := resty.New().R().
		SetContext(ctx).
		SetHeader("Content-Type", mimeType).
		SetBody(body).
		Put(presign.UploadURL)
	if err != nil {
		return "", 0, &APIError{Op: "upload-put", Message: "failed to PUT video bytes", Err: err}
	}
	if !putResp.IsStatusSuccess() {
		return "", 0, &APIError{Op: "upload-put", Message: fmt.Sprintf("PUT failed with status %d", putResp.StatusCode())}
	}

	return presign.StorageKey, fileSize, nil
}
```

Add `"os"` and `"path/filepath"` to the file's imports if not already present (check first — `os`
is likely already imported given `redactDebugLog`'s use elsewhere in the file; verify before
adding a duplicate). The bare `resty.New()` for the PUT deliberately does not reuse `c.resty` — the
PUT target is R2, not the Qualflare API, and must not carry the `QF_TOKEN` auth middleware that
`c.resty`'s request middleware attaches to every request; matching the reporters' `putObject`,
which explicitly sends no auth header on the PUT.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/adapters/http/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/http/client.go internal/adapters/http/upload_video_test.go
git commit -m "feat(http): add UploadVideo, the presigned-URL flow ported from the reporters"
```

---

## Task 3: The qualflare-native parser reads `shardIndex` and preserves `localVideoPath` via `PathAwareParser`

**Files:**
- Modify: `internal/adapters/parsers/native/qualflare/qualflare.go`
- Modify: `internal/adapters/parsers/native/qualflare/qualflare_test.go`

**Interfaces:**
- Produces: `Parser` now also implements `ports.PathAwareParser` (`ParsePath(path string) (*domain.Suite, error)`), matching the existing `xctest` parser's pattern.
- Every `domain.Case` this parser produces now carries `ShardIndex` when the source JSON has it.
- `domain.Attachment.LocalVideoPath` is populated (absolute path) instead of the old
  `StorageKey`-drop behavior — `StorageKey` is removed from this parser's own incoming JSON struct
  entirely (the reporters never emit it anymore).

- [ ] **Step 1: Write the failing tests**

Replace `TestParserDropsStorageKeyAttachmentsButKeepsInlineOnes` in `qualflare_test.go` with:

```go
func TestParserResolvesLocalVideoPathToAbsolute(t *testing.T) {
	dir := t.TempDir()
	videoFile := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(videoFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(dir, "report.json")
	jsonReport := `{"framework": "cypress", "suites": [{"name": "s", "cases": [
		{"id": "1", "name": "t", "status": "passed", "attachments": [
			{"name": "screenshot", "mimeType": "image/png", "content": "aGVsbG8="},
			{"name": "video", "mimeType": "video/mp4", "localVideoPath": "clip.mp4"}
		]}
	]}]}`
	if err := os.WriteFile(reportPath, []byte(jsonReport), 0o600); err != nil {
		t.Fatal(err)
	}

	parser := New()
	suite, err := parser.ParsePath(reportPath)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	attachments := suite.Cases[0].Attachments
	if len(attachments) != 2 {
		t.Fatalf("expected 2 attachments (inline + video), got %d", len(attachments))
	}
	video := attachments[1]
	if video.LocalVideoPath != videoFile {
		t.Errorf("expected LocalVideoPath %q (resolved relative to report.json's directory), got %q", videoFile, video.LocalVideoPath)
	}
	if attachments[0].Content != "aGVsbG8=" {
		t.Errorf("expected inline screenshot to survive unchanged, got %+v", attachments[0])
	}
}

func TestParserReadsShardIndexFromSource(t *testing.T) {
	jsonReport := `{"framework": "cypress", "suites": [{"name": "s", "cases": [
		{"id": "1", "name": "t", "status": "passed", "shardIndex": 2}
	]}]}`
	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if suite.Cases[0].ShardIndex == nil || *suite.Cases[0].ShardIndex != 2 {
		t.Errorf("expected ShardIndex 2, got %v", suite.Cases[0].ShardIndex)
	}
}

func TestParserOmitsShardIndexWhenSourceOmitsIt(t *testing.T) {
	jsonReport := `{"framework": "cypress", "suites": [{"name": "s", "cases": [
		{"id": "1", "name": "t", "status": "passed"}
	]}]}`
	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if suite.Cases[0].ShardIndex != nil {
		t.Errorf("expected nil ShardIndex, got %v", *suite.Cases[0].ShardIndex)
	}
}
```

Add `"os"` and `"path/filepath"` to the test file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/adapters/parsers/native/qualflare/... -v`
Expected: FAIL — `ParsePath` doesn't exist, `Case.ShardIndex`/`Attachment.LocalVideoPath` aren't
read from source JSON yet.

- [ ] **Step 3: Rewrite `qualflare.go`**

In the `Case` struct, add `ShardIndex *int \`json:"shardIndex,omitempty"\`` next to `IsFlaky`.

In the `Attachment` struct, replace the `StorageKey` field (and its whole doc comment explaining
why it's dropped) with:
```go
type Attachment struct {
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Content  string `json:"content,omitempty"` // Base64 encoded
	// LocalVideoPath, relative to the report file this Attachment came from —
	// resolved to an absolute path in ParsePath before it ever reaches
	// convertCase. See domain.Attachment.LocalVideoPath's doc comment.
	LocalVideoPath string `json:"localVideoPath,omitempty"`
}
```

Change `Parse` to delegate through a shared internal function that also accepts the source
directory (empty string when called via the plain `io.Reader` path, which then leaves any
`LocalVideoPath` as the raw relative string from the JSON — acceptable since `Parse` is only ever
called directly by tests exercising fields other than video, per the existing test suite; every
real collect invocation goes through `ParsePath`):

```go
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var collect Collect
	if err := json.NewDecoder(reader).Decode(&collect); err != nil {
		return nil, err
	}
	return buildSuite(collect, "")
}

// ParsePath implements ports.PathAwareParser — report_service.go calls this
// instead of Parse for this framework specifically, because a video
// attachment's localVideoPath is relative to THIS file's own directory, not
// the CLI's cwd (necessary once a merge pulls files from multiple shard
// subdirectories together — see the design spec).
func (p *Parser) ParsePath(path string) (*domain.Suite, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var collect Collect
	if err := json.NewDecoder(f).Decode(&collect); err != nil {
		return nil, err
	}
	return buildSuite(collect, filepath.Dir(path))
}

func buildSuite(collect Collect, sourceDir string) (*domain.Suite, error) {
	suite := &domain.Suite{
		Name:      "Qualflare Test Results",
		Category:  domain.Framework(collect.Framework).GetCategory(),
		Timestamp: time.Now().UTC(),
		Cases:     make([]domain.Case, 0),
	}

	var totalDurationNs int64
	for _, s := range collect.Suites {
		totalDurationNs += s.Duration
		for _, c := range s.Cases {
			suite.Cases = append(suite.Cases, convertCase(c, s.Name, sourceDir))
		}
	}
	suite.Duration = base.ParseDurationNs(totalDurationNs)
	suite.RecomputeCounts()

	return suite, nil
}
```

(Delete the old `Parse` body that built `suite` directly — it's now `buildSuite`.)

Update `convertCase`'s signature and attachment loop:
```go
func convertCase(c Case, suiteName string, sourceDir string) domain.Case {
	testCase := domain.Case{
		ID:         c.ID,
		Name:       c.Name,
		ClassName:  base.CoalesceString(c.ClassName, suiteName),
		Status:     mapStatus(c.Status),
		Duration:   base.ParseDurationNs(c.Duration),
		RetryCount: c.RetryCount,
		IsFlaky:    c.IsFlaky,
		ShardIndex: c.ShardIndex,
		Error:      c.Error,
		Priority:   domain.Severity(c.Priority),
		Tags:       c.Tags,
		Properties: c.Properties,
	}

	for _, a := range c.Attachments {
		attachment := domain.Attachment{
			Name:     a.Name,
			Path:     a.Path,
			MimeType: a.MimeType,
			Content:  a.Content,
		}
		if a.LocalVideoPath != "" && sourceDir != "" {
			attachment.LocalVideoPath = filepath.Join(sourceDir, a.LocalVideoPath)
		} else if a.LocalVideoPath != "" {
			attachment.LocalVideoPath = a.LocalVideoPath
		}
		testCase.Attachments = append(testCase.Attachments, attachment)
	}

	// ...steps loop unchanged...

	return testCase
}
```

Add `"os"` and `"path/filepath"` to the file's imports.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/adapters/parsers/native/qualflare/... -v`
Expected: PASS (all existing tests plus the 3 new ones — note `TestParserFlattensMultipleSuitesIntoOneWrapperSuite` and the other pre-existing tests call `Parse` directly, which still works via the `buildSuite(collect, "")` path).

- [ ] **Step 5: Verify `PathAwareParser` is actually picked up**

Run: `go build ./... && go vet ./...`
Expected: clean — confirms `*Parser` satisfies `ports.PathAwareParser` (a compile-time interface
check, not exercised by the unit tests above, which call `ParsePath` directly rather than through
`report_service.go`'s type-assertion). Add one more test in `qualflare_test.go` to close this gap
explicitly:

```go
func TestParserSatisfiesPathAwareParser(t *testing.T) {
	var _ ports.PathAwareParser = New()
}
```

(Add `"qualflare-cli/internal/core/ports"` to the test file's imports.) Run
`go test ./internal/adapters/parsers/native/qualflare/... -v` again to confirm this compiles and
passes.

- [ ] **Step 6: Commit**

```bash
git add internal/adapters/parsers/native/qualflare/qualflare.go internal/adapters/parsers/native/qualflare/qualflare_test.go
git commit -m "feat(parser): qualflare parser implements PathAwareParser, reads shardIndex, resolves localVideoPath"
```

---

## Task 4: `report_service.go` resolves pending video attachments before sending

**Files:**
- Modify: `internal/core/services/report_service.go`
- Test: `internal/core/services/report_service_video_test.go` (new)

**Interfaces:**
- Consumes: `ports.ReportSender.UploadVideo` (Task 1/2).
- Produces: a new unexported method `resolveVideoAttachments(ctx, *domain.Launch) []error` (returns
  per-attachment warnings, never a hard error — fail-open) called from `ProcessTestResults` between
  `ParseTestResults` and `s.sender.SendReport`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/services/report_service_video_test.go`:

```go
package services

import (
	"context"
	"errors"
	"testing"

	"qualflare-cli/internal/core/domain"
)

type videoStubSender struct {
	stubSender
	uploadCalls   []string
	uploadErr     error
	storageKeyOut string
	fileSizeOut   int64
}

func (v *videoStubSender) UploadVideo(_ context.Context, localPath, _ string) (string, int64, error) {
	v.uploadCalls = append(v.uploadCalls, localPath)
	if v.uploadErr != nil {
		return "", 0, v.uploadErr
	}
	return v.storageKeyOut, v.fileSizeOut, nil
}

func TestResolveVideoAttachmentsFillsStorageKeyAndClearsLocalPath(t *testing.T) {
	sender := &videoStubSender{storageKeyOut: "case-run-attachments/proj/1.mp4", fileSizeOut: 999}
	svc := &ReportService{sender: sender}

	launch := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{{
		Attachments: []domain.Attachment{{Name: "video", LocalVideoPath: "/tmp/clip.mp4"}},
	}}}}}

	svc.resolveVideoAttachments(context.Background(), launch)

	att := launch.Suites[0].Cases[0].Attachments[0]
	if att.StorageKey != "case-run-attachments/proj/1.mp4" {
		t.Errorf("StorageKey = %q", att.StorageKey)
	}
	if att.FileSize != 999 {
		t.Errorf("FileSize = %d", att.FileSize)
	}
	if att.LocalVideoPath != "" {
		t.Errorf("expected LocalVideoPath cleared, got %q", att.LocalVideoPath)
	}
	if len(sender.uploadCalls) != 1 || sender.uploadCalls[0] != "/tmp/clip.mp4" {
		t.Errorf("uploadCalls = %v", sender.uploadCalls)
	}
}

func TestResolveVideoAttachmentsFailsOpenOnUploadError(t *testing.T) {
	sender := &videoStubSender{uploadErr: errors.New("network down")}
	svc := &ReportService{sender: sender, warn: newTestWarnWriter(t)}

	launch := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{{
		Attachments: []domain.Attachment{
			{Name: "video", LocalVideoPath: "/tmp/clip.mp4"},
			{Name: "screenshot", Content: "aGVsbG8="},
		},
	}}}}}

	svc.resolveVideoAttachments(context.Background(), launch)

	cases := launch.Suites[0].Cases[0]
	if len(cases.Attachments) != 2 {
		t.Fatalf("expected the failed video attachment kept in place, not dropped: %+v", cases.Attachments)
	}
	if cases.Attachments[0].StorageKey != "" {
		t.Errorf("expected no StorageKey on a failed upload, got %q", cases.Attachments[0].StorageKey)
	}
	if cases.Attachments[1].Content != "aGVsbG8=" {
		t.Errorf("expected the unrelated inline attachment untouched, got %+v", cases.Attachments[1])
	}
}

func TestResolveVideoAttachmentsSkipsAttachmentsWithNoLocalPath(t *testing.T) {
	sender := &videoStubSender{}
	svc := &ReportService{sender: sender}

	launch := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{{
		Attachments: []domain.Attachment{{Name: "screenshot", Content: "aGVsbG8="}},
	}}}}}

	svc.resolveVideoAttachments(context.Background(), launch)

	if len(sender.uploadCalls) != 0 {
		t.Errorf("expected no upload calls for a non-video attachment, got %v", sender.uploadCalls)
	}
}
```

`newTestWarnWriter` needs to match whatever `s.warn`'s type actually is (`io.Writer` per the
`ReportService` struct definition) — check `report_service.go`'s `warnWriter()`/`warn` field usage
first and use a plain `&bytes.Buffer{}` or `io.Discard` if a named helper doesn't already exist
elsewhere in this package's tests; don't invent a name that collides with one that does.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/core/services/... -run TestResolveVideoAttachments -v`
Expected: FAIL — `resolveVideoAttachments` doesn't exist.

- [ ] **Step 3: Implement `resolveVideoAttachments`**

Add to `report_service.go`:

```go
// resolveVideoAttachments walks every attachment in the merged launch and
// resolves any LocalVideoPath (set by the qualflare-native parser's
// ParsePath — see internal/adapters/parsers/native/qualflare) into a real
// StorageKey/FileSize via the presigned-URL flow. Fail-open per attachment,
// matching the policy the reporters themselves used before this
// responsibility moved here: a failed upload is logged and the attachment
// is left with neither StorageKey nor LocalVideoPath resolved (effectively
// dropped server-side, since neither Content nor StorageKey ends up set) —
// it never fails the whole collect.
func (s *ReportService) resolveVideoAttachments(ctx context.Context, launch *domain.Launch) {
	for i := range launch.Suites {
		for j := range launch.Suites[i].Cases {
			attachments := launch.Suites[i].Cases[j].Attachments
			for k := range attachments {
				if attachments[k].LocalVideoPath == "" {
					continue
				}
				localPath := attachments[k].LocalVideoPath
				storageKey, fileSize, err := s.sender.UploadVideo(ctx, localPath, attachments[k].MimeType)
				if err != nil {
					fmt.Fprintf(s.warnWriter(), "skipping video attachment %q (%s): %v\n", attachments[k].Name, localPath, err)
					attachments[k].LocalVideoPath = ""
					continue
				}
				attachments[k].StorageKey = storageKey
				attachments[k].FileSize = fileSize
				attachments[k].LocalVideoPath = ""
			}
		}
	}
}
```

Check `warnWriter()`'s exact existing signature/name in this file (referenced in `tagShardsByFile`'s
call site as `s.warnWriter()`) before using it here — match it exactly, don't guess.

Wire it into `ProcessTestResults`, between parsing and sending:
```go
func (s *ReportService) ProcessTestResults(ctx context.Context, files []string, framework domain.Framework) error {
	report, err := s.ParseTestResults(ctx, files, framework)
	if err != nil {
		return err
	}

	if s.config.IsDryRun() {
		return nil
	}

	s.resolveVideoAttachments(ctx, report)

	return s.sender.SendReport(ctx, report)
}
```

(Dry-run still skips video upload entirely, matching today's "parse without sending" contract —
`--dry-run --output json` should print the report with `LocalVideoPath` still populated for
inspection, not silently attempt uploads a dry run shouldn't perform.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/core/services/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/services/report_service.go internal/core/services/report_service_video_test.go
git commit -m "feat(collect): resolve pending video attachments before sending, fail-open per attachment"
```

---

## Task 5: `collect <dir>` expands a directory argument to its `*.json` files

**Files:**
- Modify: `internal/adapters/cli/command.go`
- Modify: `internal/adapters/cli/glob_test.go`

**Interfaces:**
- Produces: a new function `expandDirectories(files []string) ([]string, error)`, called in
  `runCollect` right after `expandGlobs` and before `verifyFilesExist`.

- [ ] **Step 1: Write the failing test**

Add to `internal/adapters/cli/glob_test.go`:

```go
func TestExpandDirectories(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"shard-0.json", "shard-1.json"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A non-JSON file in the same directory must NOT be picked up.
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := expandDirectories([]string{dir})
	if err != nil {
		t.Fatalf("expandDirectories errored: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 JSON files, got %d: %v", len(out), out)
	}

	// A literal file argument passes through untouched.
	filePath := filepath.Join(dir, "shard-0.json")
	out, err = expandDirectories([]string{filePath})
	if err != nil || len(out) != 1 || out[0] != filePath {
		t.Fatalf("literal file passthrough failed: %v %v", out, err)
	}

	// An empty directory is a loud error, not a silent empty collect.
	emptyDir := t.TempDir()
	if _, err := expandDirectories([]string{emptyDir}); err == nil {
		t.Fatal("an empty directory must error")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/adapters/cli/... -run TestExpandDirectories -v`
Expected: FAIL — `expandDirectories` doesn't exist.

- [ ] **Step 3: Implement `expandDirectories`**

Add to `command.go`, near `expandGlobs`:

```go
// expandDirectories expands any argument that is a directory into the *.json
// files directly inside it (non-recursive — matches the reporters' flat
// outputDir layout), preserving order and passing a non-directory argument
// through literally. A directory with no *.json files inside is an error,
// matching expandGlobs's "no matches = loud error, not a silent empty
// upload" convention (BUG-28).
func expandDirectories(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			// Let the existing verifyFilesExist give the real "does not
			// exist" error later — this function only expands directories
			// it can actually see.
			out = append(out, p)
			continue
		}
		if !info.IsDir() {
			out = append(out, p)
			continue
		}
		matches, err := filepath.Glob(filepath.Join(p, "*.json"))
		if err != nil {
			return nil, fmt.Errorf("invalid directory %q: %w", p, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("directory %q contains no .json report files", p)
		}
		sort.Strings(matches)
		out = append(out, matches...)
	}
	return out, nil
}
```

Add `"sort"` to the file's imports if not already present.

In `runCollect`, call this right after `expandGlobs`:
```go
	files, err := expandGlobs(files)
	if err != nil {
		return err
	}
	files, err = expandDirectories(files)
	if err != nil {
		return err
	}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/adapters/cli/... -v`
Expected: PASS.

- [ ] **Step 5: Update `createCollectCommand`'s help text**

In `command.go`'s `createCollectCommand`, update the `Long`/`Example` cobra strings to mention that
a directory argument works too — e.g. add to `Example`:
```
  # Collect (and auto-merge, if the files carry their own shardIndex) every
  # report in a directory — this is what @qualflare/cypress and
  # @qualflare/cucumberjs's outputDir produces
  qf my-app collect ./qualflare-results
```

- [ ] **Step 6: Full verification pass**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all green.

- [ ] **Step 7: Commit**

```bash
git add internal/adapters/cli/command.go internal/adapters/cli/glob_test.go
git commit -m "feat(collect): accept a directory argument, expanding to its *.json files"
```

---

## Task 6: Integration test — real merge across multiple shard files with embedded `shardIndex`, plus video resolution end to end

**Files:**
- Test: `internal/adapters/cli/collect_integration_test.go` (new, or extend `collect_test.go` if it
  already runs `runCollect` end-to-end against real files on disk — read it first)

- [ ] **Step 1: Write the test**

Read `internal/adapters/cli/collect_test.go` in full first to match its existing harness (how it
constructs a `*CLI`, injects a stub sender/config, and invokes `runCollect`). Add a test that:

1. Writes two qualflare-format JSON files into one temp directory, one with
   `"shardIndex": 0` on its case and one with `"shardIndex": 1`, plus a real video file referenced
   by `localVideoPath` in one of them.
2. Runs `runCollect(ctx, []string{tempDir}, collectOptions{...})` (no `--shard`).
3. Asserts the stub sender's `SendReport` was called exactly once (one merged Launch, not two), with
   a `*domain.Launch` whose `Suites` combine cases from both files, each retaining its own
   `ShardIndex` (0 and 1, not renumbered).
4. Asserts the stub sender's `UploadVideo` was called once, and the resulting case's attachment
   carries `StorageKey`/`FileSize` with `LocalVideoPath` cleared.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/adapters/cli/... -run TestCollect -v` (adjust the run filter to the new
test's actual name)
Expected: FAIL until Tasks 1–5 are all in place — if this task is executed after them (as the plan
order implies), it should mostly pass immediately with any real gaps surfacing here.

- [ ] **Step 3: Fix any gap this surfaces**

If it fails, the failure is almost certainly a wiring gap between Tasks 1–5 (e.g. `runCollect`
never actually calls `resolveVideoAttachments`, or the merge order differs from expected) — trace
it back to the specific task above and fix it there, updating that task's own tests too if the fix
changes behavior it already covers.

- [ ] **Step 4: Run the full test suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/cli/collect_integration_test.go
git commit -m "test(collect): end-to-end directory-merge + shardIndex + video-resolution coverage"
```
