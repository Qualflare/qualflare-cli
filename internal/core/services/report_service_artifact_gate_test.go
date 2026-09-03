package services

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
)

// gateSvc builds a service whose config opts into exactly the given kinds.
func gateSvc(sender *videoStubSender, kinds ...string) (*ReportService, *bytes.Buffer) {
	cfg := config.DefaultConfig()
	set := map[string]bool{}
	for _, k := range kinds {
		set[k] = true
	}
	cfg.SetUploadArtifacts(set)
	warn := &bytes.Buffer{}
	return &ReportService{sender: sender, config: cfg, warn: warn}, warn
}

func launchWith(atts ...domain.Attachment) *domain.Launch {
	return &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{{Attachments: atts}}}}}
}

func video() domain.Attachment {
	return domain.Attachment{Name: "clip", LocalPath: "/tmp/clip.mp4", ArtifactKind: domain.ArtifactKindVideo}
}

func trace() domain.Attachment {
	return domain.Attachment{Name: "trace", LocalPath: "/tmp/trace.zip", ArtifactKind: domain.ArtifactKindTrace}
}

// The default. Videos uploaded automatically before this gate existed, and a
// video is the largest thing in a report by an order of magnitude.
func TestArtifactGate_UploadsNothingByDefault(t *testing.T) {
	sender := &videoStubSender{storageKeyOut: "k", fileSizeOut: 1}
	svc, _ := gateSvc(sender)

	svc.resolveArtifactAttachments(context.Background(), launchWith(video(), trace()))

	if len(sender.uploadCalls) != 0 {
		t.Errorf("nothing should upload without --upload-artifacts, got %v", sender.uploadCalls)
	}
}

// THE load-bearing one. The server persists an attachment row from Name alone —
// it does not filter out one with both Content and StorageKey empty — so a
// gated-out artifact left in the payload becomes an undownloadable placeholder
// in the UI. It must be dropped instead.
func TestArtifactGate_SkippedArtifactIsDroppedNotLeftAsAPlaceholder(t *testing.T) {
	sender := &videoStubSender{}
	svc, _ := gateSvc(sender)

	launch := launchWith(video(), domain.Attachment{Name: "screenshot", Content: "base64"})
	svc.resolveArtifactAttachments(context.Background(), launch)

	got := launch.Suites[0].Cases[0].Attachments
	if len(got) != 1 {
		t.Fatalf("expected only the screenshot to survive, got %d: %+v", len(got), got)
	}
	if got[0].Name != "screenshot" {
		t.Errorf("the wrong attachment survived: %+v", got[0])
	}
}

func TestArtifactGate_UploadsOnlyTheKindsAskedFor(t *testing.T) {
	sender := &videoStubSender{storageKeyOut: "k", fileSizeOut: 1}
	svc, _ := gateSvc(sender, domain.ArtifactKindVideo)

	launch := launchWith(video(), trace())
	svc.resolveArtifactAttachments(context.Background(), launch)

	if len(sender.uploadCalls) != 1 || sender.uploadCalls[0] != "/tmp/clip.mp4" {
		t.Fatalf("only the video should upload, got %v", sender.uploadCalls)
	}
	got := launch.Suites[0].Cases[0].Attachments
	if len(got) != 1 || got[0].Name != "clip" {
		t.Errorf("the gated-out trace should be dropped, leaving the video: %+v", got)
	}
}

func TestArtifactGate_UploadsBothKindsWhenBothAsked(t *testing.T) {
	sender := &videoStubSender{storageKeyOut: "k", fileSizeOut: 1}
	svc, _ := gateSvc(sender, domain.ArtifactKindVideo, domain.ArtifactKindTrace)

	svc.resolveArtifactAttachments(context.Background(), launchWith(video(), trace()))

	if len(sender.uploadCalls) != 2 {
		t.Errorf("both kinds should upload, got %v", sender.uploadCalls)
	}
}

// Silence would be the worst outcome of the gate: videos used to upload
// automatically, so an upgrader loses them and must be told why.
func TestArtifactGate_WarnsWithTheCountAndTheExactFlag(t *testing.T) {
	sender := &videoStubSender{}
	svc, warn := gateSvc(sender)

	svc.resolveArtifactAttachments(context.Background(), launchWith(video(), video(), trace()))

	msg := warn.String()
	for _, want := range []string{"2 video", "1 trace", "--upload-artifacts=trace,video"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message should contain %q; got:\n%s", want, msg)
		}
	}
}

func TestArtifactGate_SaysNothingWhenThereIsNothingToSkip(t *testing.T) {
	sender := &videoStubSender{}
	svc, warn := gateSvc(sender)

	svc.resolveArtifactAttachments(context.Background(),
		launchWith(domain.Attachment{Name: "screenshot", Content: "base64"}))

	if warn.String() != "" {
		t.Errorf("a run with no artifacts should print nothing, got %q", warn.String())
	}
}

// A failed upload keeps the placeholder deliberately — a fault is worth seeing,
// a deliberate skip is not. This pins that the two paths stay distinct.
func TestArtifactGate_FailedUploadStillKeepsTheAttachment(t *testing.T) {
	sender := &videoStubSender{uploadErr: context.Canceled}
	svc, _ := gateSvc(sender, domain.ArtifactKindVideo)

	launch := launchWith(video())
	svc.resolveArtifactAttachments(context.Background(), launch)

	if len(launch.Suites[0].Cases[0].Attachments) != 1 {
		t.Error("a FAILED upload must not drop the attachment; only a gated-out one is dropped")
	}
}
