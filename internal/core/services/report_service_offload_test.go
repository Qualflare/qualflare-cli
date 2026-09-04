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
