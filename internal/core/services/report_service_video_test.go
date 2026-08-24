package services

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
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
	svc := &ReportService{sender: sender, warn: io.Discard}

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

// Fix 4b: the same local video file referenced by more than one attachment (e.g. a
// spec-level recording attached to multiple test cases) must be uploaded exactly once,
// with every attachment reusing the first upload's StorageKey/FileSize.
func TestResolveVideoAttachmentsUploadsSameLocalPathOnlyOnce(t *testing.T) {
	sender := &videoStubSender{storageKeyOut: "case-run-attachments/proj/shared.mp4", fileSizeOut: 4242}
	svc := &ReportService{sender: sender}

	launch := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{
		{Name: "a", Attachments: []domain.Attachment{{Name: "spec video", LocalVideoPath: "/tmp/shared.mp4"}}},
		{Name: "b", Attachments: []domain.Attachment{{Name: "spec video", LocalVideoPath: "/tmp/shared.mp4"}}},
	}}}}

	svc.resolveVideoAttachments(context.Background(), launch)

	if len(sender.uploadCalls) != 1 || sender.uploadCalls[0] != "/tmp/shared.mp4" {
		t.Fatalf("expected exactly 1 UploadVideo call for a video shared by 2 attachments, got %v", sender.uploadCalls)
	}
	for _, c := range launch.Suites[0].Cases {
		att := c.Attachments[0]
		if att.StorageKey != "case-run-attachments/proj/shared.mp4" || att.FileSize != 4242 {
			t.Errorf("case %q: expected the memoized upload result, got StorageKey=%q FileSize=%d", c.Name, att.StorageKey, att.FileSize)
		}
		if att.LocalVideoPath != "" {
			t.Errorf("case %q: expected LocalVideoPath cleared, got %q", c.Name, att.LocalVideoPath)
		}
	}
}

// ctxCapturingVideoSender records ctx.Err() at the moment UploadVideo is invoked, so a
// test can assert the ctx resolveVideoAttachments hands it was NOT already expired/canceled
// — i.e. that it is not derived from an outer deadline the caller controls.
type ctxCapturingVideoSender struct {
	stubSender
	uploadCalls    []string
	capturedCtxErr error
	storageKeyOut  string
	fileSizeOut    int64
}

func (v *ctxCapturingVideoSender) UploadVideo(ctx context.Context, localPath, _ string) (string, int64, error) {
	v.uploadCalls = append(v.uploadCalls, localPath)
	v.capturedCtxErr = ctx.Err()
	return v.storageKeyOut, v.fileSizeOut, nil
}

// slowParser sleeps briefly during Parse before returning its canned suite. Used only
// to deterministically simulate, without any dependency on real network/upload timing,
// "the --timeout budget got consumed by parsing before video resolution had a chance to
// run" — ParseTestResults's per-file ctx.Done() check runs BEFORE parseFile is called,
// so a short outer deadline that elapses DURING the sleep still lets parsing complete
// normally; it is only expired by the time control reaches resolveVideoAttachments.
type slowParser struct {
	delay time.Duration
	suite *domain.Suite
}

func (p *slowParser) Parse(r io.Reader) (*domain.Suite, error) {
	if _, err := io.ReadAll(r); err != nil {
		return nil, err
	}
	time.Sleep(p.delay)
	s := *p.suite
	s.Cases = append([]domain.Case(nil), p.suite.Cases...)
	return &s, nil
}
func (p *slowParser) GetFramework() domain.Framework    { return domain.FrameworkJUnit }
func (p *slowParser) SupportedFileExtensions() []string { return []string{".xml"} }

// Fix 2 regression test: report_service.go's ProcessTestResults must give
// resolveVideoAttachments a ctx that is NOT derived from the caller's own deadline
// (runCollect's context.WithTimeout(ctx, opts.timeout), default 30s), because
// context.WithTimeout always resolves to the EARLIER of a parent and child deadline —
// composing UploadVideo's own 5-minute timeout with an outer ctx that has already
// expired by the time video resolution runs would silently defeat it. This proves the
// wiring specifically: it gives ProcessTestResults an outer ctx whose deadline elapses
// WHILE parsing is in flight (simulating --timeout's budget being consumed by parsing,
// exactly as report_service.go's own comment describes), and asserts the video upload
// is still attempted with a ctx that is not itself already Done by the time it reaches
// UploadVideo.
func TestProcessTestResultsGivesVideoUploadAnIndependentContext(t *testing.T) {
	dir := t.TempDir()
	f := writeFile(t, dir, "r.xml", "<x/>")

	suite := onePassing(domain.FrameworkJUnit)
	suite.Cases[0].Attachments = []domain.Attachment{{Name: "video", LocalVideoPath: "/tmp/clip.mp4"}}
	parser := &slowParser{delay: 100 * time.Millisecond, suite: suite}
	fac := &stubFactory{parsers: map[domain.Framework]ports.Parser{domain.FrameworkJUnit: parser}}

	sender := &ctxCapturingVideoSender{storageKeyOut: "k", fileSizeOut: 1}
	svc := NewReportService(fac, sender, config.DefaultConfig())
	svc.warn = io.Discard

	// A deadline much shorter than the parser's artificial delay: by the time
	// parsing finishes and resolveVideoAttachments would run, this ctx is Done.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := svc.ProcessTestResults(ctx, []string{f}, domain.FrameworkJUnit); err != nil {
		t.Fatalf("ProcessTestResults() = %v", err)
	}

	if len(sender.uploadCalls) != 1 {
		t.Fatalf("expected UploadVideo to be attempted once despite the outer ctx expiring during parsing, got %d calls", len(sender.uploadCalls))
	}
	if sender.capturedCtxErr != nil {
		t.Errorf("UploadVideo's ctx.Err() = %v, want nil — resolveVideoAttachments must not derive its ctx from the caller's (now-expired) outer ctx", sender.capturedCtxErr)
	}
}
