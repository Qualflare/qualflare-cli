package services

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"

	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
)

func inline(name, content string) domain.Attachment {
	return domain.Attachment{
		Name:     name,
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
	l := launchWithAttachments(inline("shot.png", "PNGBYTES"))

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
			Attachments: []domain.Attachment{inline("shot.png", blob)},
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
	same := inline("shot.png", "IDENTICAL")
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
	l := launchWithAttachments(inline("shot.png", "PNGBYTES"))

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

func TestWarnIfBodyLooksTooLarge_OnlyWhenItActuallyIs(t *testing.T) {
	quiet := &strings.Builder{}
	(&ReportService{warn: quiet}).warnIfBodyLooksTooLarge(launchWithAttachments(inline("s.png", "small")))
	if quiet.String() != "" {
		t.Errorf("a small body should warn about nothing, got %q", quiet.String())
	}

	loud := &strings.Builder{}
	(&ReportService{warn: loud}).warnIfBodyLooksTooLarge(
		launchWithAttachments(inline("s.png", strings.Repeat("x", 9<<20))))
	if !strings.Contains(loud.String(), "10MB") {
		t.Errorf("a body near the limit should say so; got %q", loud.String())
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
