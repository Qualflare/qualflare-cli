package services

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
)

func inlinePNG(content string) domain.Attachment {
	return domain.Attachment{
		Name:     "shot.png",
		MimeType: "image/png",
		Content:  base64.StdEncoding.EncodeToString([]byte(content)),
	}
}

func offloadSvc(sender *videoStubSender) *ReportService {
	return &ReportService{sender: sender, config: config.DefaultConfig(), warn: io.Discard}
}

func launchWithAttachments(atts ...domain.Attachment) *domain.Launch {
	return &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{{Attachments: atts}}}}}
}

func TestOffload_ReplacesInlineContentWithAStorageKey(t *testing.T) {
	sender := &videoStubSender{}
	svc := offloadSvc(sender)
	l := launchWithAttachments(inlinePNG("PNGBYTES"))

	svc.offloadInlineAttachments(context.Background(), l)

	got := l.Suites[0].Cases[0].Attachments[0]
	if got.Content != "" {
		t.Errorf("content must be cleared, got %q", got.Content)
	}
	if got.StorageKey == "" {
		t.Error("storageKey must be set")
	}
	if got.FileSize != int64(len("PNGBYTES")) {
		t.Errorf("FileSize = %d, want the DECODED byte length", got.FileSize)
	}
	if len(sender.attachmentCalls) != 1 || sender.attachmentCalls[0].size != len("PNGBYTES") {
		t.Errorf("expected one upload of the decoded bytes, got %+v", sender.attachmentCalls)
	}
}

// The case this whole change exists for. Each shard honours its own 750KB inline
// budget, but collect merges them into ONE request — eleven shards assemble a
// body past /collect's 10MB limit and the server answers 413, losing everything.
func TestOffload_TwelveShardsWorthOfAttachmentsLeaveTheBody(t *testing.T) {
	const perShard = 750 * 1024
	sender := &videoStubSender{}
	svc := offloadSvc(sender)

	suites := make([]domain.Suite, 0, 12)
	for i := 0; i < 12; i++ {
		// distinct content per shard, or dedupe would mask the problem
		blob := strings.Repeat(string(rune('a'+i)), perShard)
		suites = append(suites, domain.Suite{Cases: []domain.Case{{
			Attachments: []domain.Attachment{inlinePNG(blob)},
		}}})
	}
	l := &domain.Launch{Suites: suites}

	before := inlineBytes(l)
	svc.offloadInlineAttachments(context.Background(), l)
	after := inlineBytes(l)

	if before < 10<<20 {
		t.Fatalf("fixture should exceed the 10MB body limit to be meaningful; got %d bytes", before)
	}
	if after != 0 {
		t.Errorf("all attachments should have left the body; %d bytes still inline", after)
	}
	if len(sender.attachmentCalls) != 12 {
		t.Errorf("expected 12 uploads, got %d", len(sender.attachmentCalls))
	}
}

// One screenshot attached to several cases is one object in storage.
func TestOffload_IdenticalContentUploadsOnce(t *testing.T) {
	sender := &videoStubSender{}
	svc := offloadSvc(sender)
	same := inlinePNG("IDENTICAL")
	l := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{
		{Attachments: []domain.Attachment{same}},
		{Attachments: []domain.Attachment{same}},
		{Attachments: []domain.Attachment{same}},
	}}}}

	svc.offloadInlineAttachments(context.Background(), l)

	if len(sender.attachmentCalls) != 1 {
		t.Errorf("identical payloads should upload once, got %d calls", len(sender.attachmentCalls))
	}
	for i, c := range l.Suites[0].Cases {
		if c.Attachments[0].StorageKey == "" || c.Attachments[0].Content != "" {
			t.Errorf("case %d not resolved: %+v", i, c.Attachments[0])
		}
	}
}

// Fail-open: a failed upload must never cost the user their screenshot, and must
// never fail the collect. Worst case we are back to inlining that one file.
func TestOffload_FailedUploadLeavesTheAttachmentInline(t *testing.T) {
	sender := &videoStubSender{attachmentErr: errors.New("network down")}
	warn := &strings.Builder{}
	svc := &ReportService{sender: sender, config: config.DefaultConfig(), warn: warn}
	l := launchWithAttachments(inlinePNG("PNGBYTES"))

	svc.offloadInlineAttachments(context.Background(), l)

	got := l.Suites[0].Cases[0].Attachments[0]
	if got.Content == "" {
		t.Error("a failed upload must leave the content inline, not drop the attachment")
	}
	if got.StorageKey != "" {
		t.Errorf("no storageKey should be claimed on failure, got %q", got.StorageKey)
	}
	if !strings.Contains(warn.String(), "could not be uploaded") {
		t.Errorf("the failure should be reported; got %q", warn.String())
	}
	// The count alone sends the reader to the server logs for something the
	// client already knows. This was found the hard way: diagnosing a real 400
	// meant patching a temporary print into the binary, which a user cannot do.
	if !strings.Contains(warn.String(), "network down") {
		t.Errorf("the warning must name the underlying cause; got %q", warn.String())
	}
}

// An attachment already carrying a storageKey (a video resolved moments earlier)
// must not be touched.
func TestOffload_LeavesResolvedAndEmptyAttachmentsAlone(t *testing.T) {
	sender := &videoStubSender{}
	svc := offloadSvc(sender)
	l := launchWithAttachments(
		domain.Attachment{Name: "clip", StorageKey: "already-there"},
		domain.Attachment{Name: "bare"},
	)

	svc.offloadInlineAttachments(context.Background(), l)

	if len(sender.attachmentCalls) != 0 {
		t.Errorf("nothing should have uploaded, got %+v", sender.attachmentCalls)
	}
	if l.Suites[0].Cases[0].Attachments[0].StorageKey != "already-there" {
		t.Error("an existing storageKey must be preserved")
	}
}

func inlineBytes(l *domain.Launch) int {
	n := 0
	for i := range l.Suites {
		for j := range l.Suites[i].Cases {
			for _, a := range l.Suites[i].Cases[j].Attachments {
				n += len(a.Content)
			}
		}
	}
	return n
}

// Reporters name attachments for humans. Playwright's screenshots arrive as
// "screenshot" with no extension, while the server cross-checks the extension
// against the declared MIME and rejects a mismatch — so passing the display name
// through would 400 every real screenshot, fall back to inlining, and leave the
// feature quietly doing nothing.
func TestOffload_SendsAMimeDerivedFilenameNotTheDisplayName(t *testing.T) {
	sender := &videoStubSender{}
	svc := offloadSvc(sender)
	l := launchWithAttachments(domain.Attachment{
		Name:     "screenshot", // exactly what Playwright emits
		MimeType: "image/png",
		Content:  base64.StdEncoding.EncodeToString([]byte("PNGBYTES")),
	})

	svc.offloadInlineAttachments(context.Background(), l)

	if len(sender.attachmentCalls) != 1 {
		t.Fatalf("expected one upload, got %+v", sender.attachmentCalls)
	}
	if got := sender.attachmentCalls[0].name; got != "screenshot.png" {
		t.Errorf("filename = %q, want %q — the server requires the extension to match the MIME", got, "screenshot.png")
	}
}

func TestOffloadFilename_DerivesFromMimeAndSurvivesOddNames(t *testing.T) {
	for _, tc := range []struct{ name, mime, want string }{
		{"screenshot", "image/png", "screenshot.png"},
		{"shot.png", "image/png", "shot.png"},     // already correct, not doubled
		{"photo.jpeg", "image/jpeg", "photo.jpg"}, // normalised to one spelling
		{"anim", "image/gif", "anim.gif"},
		{"", "image/png", "attachment.png"}, // no name at all
		{"a/b/c.png", "image/png", "c.png"}, // path components stripped
	} {
		got, ok := offloadFilename(tc.name, tc.mime)
		if !ok || got != tc.want {
			t.Errorf("offloadFilename(%q, %q) = (%q, %v), want %q", tc.name, tc.mime, got, ok, tc.want)
		}
	}
}

// text/plain logs and text/markdown error-context are not types this endpoint
// accepts. Attempting them would spend a presign round-trip each to earn a 400
// and a warning — worse than the inlining it was meant to avoid.
func TestOffload_LeavesNonUploadableTypesInline(t *testing.T) {
	sender := &videoStubSender{}
	warn := &strings.Builder{}
	svc := &ReportService{sender: sender, config: config.DefaultConfig(), warn: warn}
	l := launchWithAttachments(
		domain.Attachment{Name: "note", MimeType: "text/plain",
			Content: base64.StdEncoding.EncodeToString([]byte("a log line"))},
		domain.Attachment{Name: "error-context", MimeType: "text/markdown",
			Content: base64.StdEncoding.EncodeToString([]byte("# context"))},
	)

	svc.offloadInlineAttachments(context.Background(), l)

	if len(sender.attachmentCalls) != 0 {
		t.Errorf("no upload should be attempted for these types, got %+v", sender.attachmentCalls)
	}
	for _, a := range l.Suites[0].Cases[0].Attachments {
		if a.Content == "" {
			t.Errorf("%s must stay inline", a.Name)
		}
	}
	if warn.String() != "" {
		t.Errorf("leaving them inline is normal, not a warning; got %q", warn.String())
	}
}

// --- Storage full / workspace frozen ---------------------------------------------

// The server's own words, from the backend's 402 refusals.
const (
	storageFullMsg = "Storage is full (1.2 GB of 1.0 GB). Results are still saved, but new files are not. " +
		"Delete old runs, wait for run files older than 30 days to expire, or upgrade the plan."
	frozenMsg = "This workspace is larger than its plan allows (4 of 3 members, 2 of 2 projects), so " +
		"nothing new can be created. Remove members or projects until it fits, or upgrade the plan."
)

// codedErr stands in for the HTTP client's *APIError: the services package
// only ever sees it through ports.CodedError.
type codedErr struct{ code, msg string }

func (e *codedErr) Error() string        { return "request upload slot: " + e.msg }
func (e *codedErr) ErrorCode() string    { return e.code }
func (e *codedErr) ErrorMessage() string { return e.msg }

func storageFull() error { return &codedErr{code: ports.CodeStorageExceeded, msg: storageFullMsg} }
func frozen() error      { return &codedErr{code: ports.CodeWorkspaceFrozen, msg: frozenMsg} }

// refusingSender accepts its first acceptFirst upload-slot requests (videos
// and attachments counted together) and answers every later one with err.
type refusingSender struct {
	stubSender
	mu              sync.Mutex
	acceptFirst     int
	err             error
	videoCalls      int
	attachmentCalls int
}

func (r *refusingSender) answer() (string, int64, error) {
	if r.videoCalls+r.attachmentCalls <= r.acceptFirst {
		return fmt.Sprintf("key-%d", r.videoCalls+r.attachmentCalls), 1, nil
	}
	return "", 0, r.err
}

func (r *refusingSender) UploadAttachment(_ context.Context, _ []byte, _, _ string) (string, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attachmentCalls++
	return r.answer()
}

func (r *refusingSender) UploadVideo(_ context.Context, _, _ string) (string, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.videoCalls++
	return r.answer()
}

func refusalSvc(sender *refusingSender) (*ReportService, *strings.Builder) {
	warn := &strings.Builder{}
	return &ReportService{sender: sender, config: uploadsVideo(), warn: warn}, warn
}

// distinctScreenshots spreads n screenshots with distinct content (identical
// content is one request) over cases of five, so a removal has neighbours whose
// indexes it could shift.
func distinctScreenshots(n int) *domain.Launch {
	var cases []domain.Case
	for i := 0; i < n; i++ {
		if i%5 == 0 {
			cases = append(cases, domain.Case{})
		}
		c := &cases[len(cases)-1]
		c.Attachments = append(c.Attachments, inlinePNG(fmt.Sprintf("PNG-%d", i)))
	}
	return &domain.Launch{Suites: []domain.Suite{{Cases: cases}}}
}

func distinctVideos(n int) []domain.Attachment {
	out := make([]domain.Attachment, n)
	for i := range out {
		out[i] = domain.Attachment{
			Name:         fmt.Sprintf("clip-%d", i),
			LocalPath:    fmt.Sprintf("/tmp/clip-%d.mp4", i),
			ArtifactKind: domain.ArtifactKindVideo,
		}
	}
	return out
}

func allAttachments(l *domain.Launch) []domain.Attachment {
	var out []domain.Attachment
	for i := range l.Suites {
		for j := range l.Suites[i].Cases {
			out = append(out, l.Suites[i].Cases[j].Attachments...)
		}
	}
	return out
}

func storageFullLine(n int) string {
	return fmt.Sprintf("storage is full: %d file(s) were not uploaded; results are still sent. %s\n", n, storageFullMsg)
}

// When storage is full the server removes every inline file from /collect and
// says so; sending the screenshots inline would only spend the body budget.
func TestOffload_StorageFull_DropsScreenshotsAndStopsAsking(t *testing.T) {
	sender := &refusingSender{err: storageFull()}
	svc, warn := refusalSvc(sender)
	l := distinctScreenshots(20)

	svc.offloadInlineAttachments(context.Background(), l)

	if got := allAttachments(l); len(got) != 0 {
		t.Errorf("every refused screenshot must be removed from its case, %d remain: %+v", len(got), got)
	}
	if sender.attachmentCalls > attachmentUploadConcurrency {
		t.Errorf("asked for %d slots after the refusal; at most the %d already in flight may be asked",
			sender.attachmentCalls, attachmentUploadConcurrency)
	}
	if got, want := warn.String(), storageFullLine(20); got != want {
		t.Errorf("output =\n%q\nwant exactly\n%q", got, want)
	}
	if strings.Contains(warn.String(), "left inline") {
		t.Errorf("a refusal is not a failure to report per file; got %q", warn.String())
	}
}

// Anything that is not one of the two refusals keeps today's fail-open path.
func TestOffload_OtherFailureStillInlines(t *testing.T) {
	sender := &refusingSender{err: errors.New("network down")}
	svc, warn := refusalSvc(sender)
	l := distinctScreenshots(3)

	svc.offloadInlineAttachments(context.Background(), l)

	got := allAttachments(l)
	if len(got) != 3 {
		t.Fatalf("a plain failure must keep every attachment, got %d", len(got))
	}
	for _, a := range got {
		if a.Content == "" {
			t.Errorf("%s lost its inline content", a.Name)
		}
	}
	if !strings.Contains(warn.String(), "could not be uploaded") {
		t.Errorf("the failure should be reported as today; got %q", warn.String())
	}
	if strings.Contains(warn.String(), "storage is full") {
		t.Errorf("a plain failure is not a full workspace; got %q", warn.String())
	}
}

func TestOffload_StorageFull_KeepsWhatWasUploadedBeforeIt(t *testing.T) {
	sender := &refusingSender{acceptFirst: 2, err: storageFull()}
	svc, warn := refusalSvc(sender)
	l := distinctScreenshots(20)

	svc.offloadInlineAttachments(context.Background(), l)

	got := allAttachments(l)
	if len(got) != 2 {
		t.Fatalf("the 2 accepted uploads should remain, got %d: %+v", len(got), got)
	}
	for _, a := range got {
		if a.StorageKey == "" || a.Content != "" {
			t.Errorf("a remaining attachment must be the uploaded one: %+v", a)
		}
	}
	if got, want := warn.String(), storageFullLine(18); got != want {
		t.Errorf("output =\n%q\nwant exactly\n%q", got, want)
	}
}

func TestOffload_StorageFull_IdenticalContentCountsEachAttachment(t *testing.T) {
	sender := &refusingSender{err: storageFull()}
	svc, warn := refusalSvc(sender)
	same := inlinePNG("IDENTICAL")
	l := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{
		{Attachments: []domain.Attachment{same}},
		{Attachments: []domain.Attachment{same}},
		{Attachments: []domain.Attachment{same}},
	}}}}

	svc.offloadInlineAttachments(context.Background(), l)

	if sender.attachmentCalls != 1 {
		t.Errorf("identical content is one request, got %d", sender.attachmentCalls)
	}
	if got := allAttachments(l); len(got) != 0 {
		t.Errorf("every attachment sharing the refused content must go, %d remain", len(got))
	}
	if got, want := warn.String(), storageFullLine(3); got != want {
		t.Errorf("output =\n%q\nwant exactly\n%q", got, want)
	}
}

func TestOffload_StorageFull_LeavesOtherInlineTypesAlone(t *testing.T) {
	sender := &refusingSender{err: storageFull()}
	svc, _ := refusalSvc(sender)
	// The screenshot first, so its removal shifts the text attachment's index.
	l := launchWithAttachments(
		inlinePNG("PNGBYTES"),
		domain.Attachment{Name: "log", MimeType: "text/plain",
			Content: base64.StdEncoding.EncodeToString([]byte("a log line"))},
	)

	svc.offloadInlineAttachments(context.Background(), l)

	got := l.Suites[0].Cases[0].Attachments
	if len(got) != 1 || got[0].Name != "log" {
		t.Fatalf("only the text attachment should remain, got %+v", got)
	}
	if got[0].Content == "" {
		t.Error("the text attachment must keep its inline content")
	}
}

// Frozen: the send that follows is refused with the full reason, so nothing is
// said here, and the screenshots are left as they were.
func TestOffload_Frozen_StopsAskingAndPrintsNothing(t *testing.T) {
	sender := &refusingSender{err: frozen()}
	svc, warn := refusalSvc(sender)
	l := distinctScreenshots(20)

	svc.offloadInlineAttachments(context.Background(), l)

	if sender.attachmentCalls > attachmentUploadConcurrency {
		t.Errorf("asked for %d slots after the refusal; at most the %d already in flight may be asked",
			sender.attachmentCalls, attachmentUploadConcurrency)
	}
	if warn.String() != "" {
		t.Errorf("frozen prints nothing here; got %q", warn.String())
	}
	got := allAttachments(l)
	if len(got) != 20 {
		t.Fatalf("frozen must leave every screenshot in place, got %d", len(got))
	}
	for _, a := range got {
		if a.Content == "" || a.StorageKey != "" {
			t.Errorf("frozen must leave the screenshot inline and untouched: %+v", a)
		}
	}
}

func TestResolveArtifacts_StorageFull_OneSummaryLine(t *testing.T) {
	sender := &refusingSender{err: storageFull()}
	svc, warn := refusalSvc(sender)
	l := launchWithAttachments(distinctVideos(5)...)

	svc.resolveArtifactAttachments(context.Background(), l)

	if sender.videoCalls != 1 {
		t.Errorf("no slot may be asked after the refusal, got %d requests", sender.videoCalls)
	}
	if got, want := warn.String(), storageFullLine(5); got != want {
		t.Errorf("output =\n%q\nwant exactly\n%q", got, want)
	}
	if strings.Contains(warn.String(), "skipping") {
		t.Errorf("no per-file line on a refusal; got %q", warn.String())
	}
}

func TestResolveArtifacts_Frozen_StopsAskingAndPrintsNothing(t *testing.T) {
	sender := &refusingSender{err: frozen()}
	svc, warn := refusalSvc(sender)
	l := launchWithAttachments(distinctVideos(3)...)

	svc.resolveArtifactAttachments(context.Background(), l)

	if sender.videoCalls != 1 {
		t.Errorf("no slot may be asked after the refusal, got %d requests", sender.videoCalls)
	}
	if warn.String() != "" {
		t.Errorf("frozen prints nothing here; got %q", warn.String())
	}
	got := allAttachments(l)
	if len(got) != 3 {
		t.Fatalf("the videos are kept as placeholders, got %d", len(got))
	}
	for _, a := range got {
		if a.StorageKey != "" || a.LocalPath != "" {
			t.Errorf("want a name-only placeholder, got %+v", a)
		}
	}
}

// twoVideosThreeScreenshots is a run with both kinds enabled: the videos go
// through resolveArtifactAttachments, the screenshots through the offload.
func twoVideosThreeScreenshots() *domain.Launch {
	l := distinctScreenshots(3)
	l.Suites[0].Cases = append(l.Suites[0].Cases, domain.Case{Attachments: distinctVideos(2)})
	return l
}

// In a run the two steps share the refusal: the second asks for nothing after
// the first was refused, and the line is printed once, with the total.
func TestUploadSteps_StorageFull_OneLineForBothSteps(t *testing.T) {
	sender := &refusingSender{err: storageFull()}
	svc, warn := refusalSvc(sender)
	l := twoVideosThreeScreenshots()

	svc.uploadFiles(context.Background(), l)

	if sender.videoCalls != 1 {
		t.Errorf("the video uploader should be asked once, got %d", sender.videoCalls)
	}
	if sender.attachmentCalls != 0 {
		t.Errorf("the attachment uploader must not be asked after the refusal, got %d", sender.attachmentCalls)
	}
	if got, want := warn.String(), storageFullLine(5); got != want {
		t.Errorf("output =\n%q\nwant exactly one line\n%q", got, want)
	}
	got := allAttachments(l)
	if len(got) != 2 {
		t.Fatalf("only the 2 video placeholders should remain, got %d: %+v", len(got), got)
	}
	for _, a := range got {
		if !strings.HasPrefix(a.Name, "clip-") || a.StorageKey != "" || a.LocalPath != "" || a.ArtifactKind != "" {
			t.Errorf("want a name-only video placeholder, got %+v", a)
		}
	}
}

func TestUploadSteps_NothingRefused_PrintsNothing(t *testing.T) {
	sender := &refusingSender{acceptFirst: 1 << 30}
	svc, warn := refusalSvc(sender)
	l := twoVideosThreeScreenshots()

	svc.uploadFiles(context.Background(), l)

	if warn.String() != "" {
		t.Errorf("a run where every upload succeeds prints nothing; got %q", warn.String())
	}
	for _, a := range allAttachments(l) {
		if a.StorageKey == "" {
			t.Errorf("every attachment should be uploaded: %+v", a)
		}
	}
}
