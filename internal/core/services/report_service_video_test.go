package services

import (
	"context"
	"errors"
	"io"
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
