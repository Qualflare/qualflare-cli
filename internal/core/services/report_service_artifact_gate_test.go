package services

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
)

// gateSvc builds a service that additionally opts into the given kinds. With no
// kinds it leaves the DEFAULTS in place (images on, heavy kinds off) rather
// than declining everything -- an empty set is "none", which is a different
// thing and has its own helper below.
func gateSvc(sender *videoStubSender, kinds ...string) (*ReportService, *bytes.Buffer) {
	cfg := config.DefaultConfig()
	if len(kinds) > 0 {
		set := map[string]bool{}
		for _, k := range kinds {
			set[k] = true
		}
		cfg.SetUploadArtifacts(set)
	}
	warn := &bytes.Buffer{}
	return &ReportService{sender: sender, config: cfg, warn: warn}, warn
}

// gateSvcNone is --upload-artifacts=none: a non-nil empty set, declining every
// kind including the ones that default to on.
func gateSvcNone(sender *videoStubSender) (*ReportService, *bytes.Buffer) {
	cfg := config.DefaultConfig()
	cfg.SetUploadArtifacts(map[string]bool{})
	warn := &bytes.Buffer{}
	return &ReportService{sender: sender, config: cfg, warn: warn}, warn
}

func inlineImage(name string) domain.Attachment {
	// "aGVsbG8=" is "hello"; the bytes do not matter, the MIME type does.
	return domain.Attachment{Name: name, MimeType: "image/png", Content: "aGVsbG8="}
}

func inlineText(name string) domain.Attachment {
	return domain.Attachment{Name: name, MimeType: "text/plain", Content: "aGVsbG8="}
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
// Heavy kinds only. Images are NOT part of "nothing by default" -- see
// TestArtifactGate_InlineImagesUploadByDefault.
func TestArtifactGate_UploadsNoHeavyArtifactsByDefault(t *testing.T) {
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

// An on-disk screenshot uploads without anyone passing a flag. Screenshots were
// always delivered back when inlining was their only route, so requiring
// --upload-artifacts=image would silently stop delivering them for every
// existing user.
func TestArtifactGate_ImagesUploadByDefault(t *testing.T) {
	sender := &videoStubSender{storageKeyOut: "k", fileSizeOut: 7}
	svc, _ := gateSvc(sender)

	launch := launchWith(domain.Attachment{
		Name: "shot", LocalPath: "/tmp/shot.png", ArtifactKind: domain.ArtifactKindImage,
	})
	svc.resolveArtifactAttachments(context.Background(), launch)

	if len(sender.uploadCalls) != 1 {
		t.Fatalf("image should upload with no flag, got %v", sender.uploadCalls)
	}
	att := launch.Suites[0].Cases[0].Attachments[0]
	if att.StorageKey != "k" {
		t.Errorf("StorageKey = %q, want the uploaded key", att.StorageKey)
	}
}

// --upload-artifacts=video must not turn screenshots off as a side effect. That
// is what makes named kinds additive rather than a replacement.
func TestArtifactGate_AskingForVideoKeepsImages(t *testing.T) {
	sender := &videoStubSender{storageKeyOut: "k", fileSizeOut: 1}
	svc, _ := gateSvc(sender, domain.ArtifactKindVideo)

	launch := launchWith(video(), domain.Attachment{
		Name: "shot", LocalPath: "/tmp/shot.png", ArtifactKind: domain.ArtifactKindImage,
	})
	svc.resolveArtifactAttachments(context.Background(), launch)

	if len(launch.Suites[0].Cases[0].Attachments) != 2 {
		t.Errorf("both video and image should survive, got %d attachments",
			len(launch.Suites[0].Cases[0].Attachments))
	}
	if len(sender.uploadCalls) != 2 {
		t.Errorf("expected 2 uploads, got %v", sender.uploadCalls)
	}
}

// "none" has to reach the INLINE images too. A reporter older than the
// localImagePath contract still base64-inlines its screenshots, so gating only
// the on-disk path would leave --upload-artifacts=none delivering exactly the
// images it was told not to -- and inside the /collect body at that.
func TestArtifactGate_NoneDropsInlineImagesToo(t *testing.T) {
	sender := &videoStubSender{}
	svc, warn := gateSvcNone(sender)

	launch := launchWith(inlineImage("shot"), inlineText("log"))
	svc.offloadInlineAttachments(context.Background(), launch)

	atts := launch.Suites[0].Cases[0].Attachments
	if len(atts) != 1 {
		t.Fatalf("the image should be dropped and the log kept, got %d attachments", len(atts))
	}
	if atts[0].Name != "log" {
		t.Errorf("kept the wrong attachment: %q", atts[0].Name)
	}
	// Dropped, not blanked: the server persists a row from Name alone, so a
	// stripped attachment would be an undownloadable placeholder in the UI.
	if len(sender.uploadCalls) != 0 {
		t.Errorf("nothing should upload when images are declined, got %v", sender.uploadCalls)
	}
	if !strings.Contains(warn.String(), "declined") {
		t.Errorf("the drop should be reported, got %q", warn.String())
	}
}

// A text attachment has no artifact kind and was never gateable. Declining
// images must not take it with them.
func TestArtifactGate_NoneLeavesNonImageInlineAttachmentsAlone(t *testing.T) {
	sender := &videoStubSender{}
	svc, _ := gateSvcNone(sender)

	launch := launchWith(inlineText("log"))
	svc.offloadInlineAttachments(context.Background(), launch)

	if len(launch.Suites[0].Cases[0].Attachments) != 1 {
		t.Error("a text attachment is not an image and must survive --upload-artifacts=none")
	}
}
