package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"qualflare-cli/internal/adapters/parsers/factory"
	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/services"
)

// collectIntegrationSender is a ports.ReportSender stub for the end-to-end
// directory-collect test below. Unlike stubReportService above (which stubs
// out the whole ReportService and never exercises real parsing/merging/
// video-resolution), this stubs only the network boundary — SendReport and
// UploadVideo — so runCollect drives the real services.ReportService, the
// same one cmd/main.go wires up, through its actual parse-merge-resolve
// pipeline.
type collectIntegrationSender struct {
	sendReportCalls []*domain.Launch
	uploadCalls     []struct{ localPath, mimeType string }

	uploadStorageKey string
	uploadFileSize   int64
}

func (s *collectIntegrationSender) SendReport(_ context.Context, report *domain.Launch) error {
	s.sendReportCalls = append(s.sendReportCalls, report)
	return nil
}

func (s *collectIntegrationSender) UploadAttachment(_ context.Context, data []byte, _, _ string) (string, int64, error) {
	return "att-key", int64(len(data)), nil
}

func (s *collectIntegrationSender) UploadVideo(_ context.Context, localPath, mimeType string) (string, int64, error) {
	s.uploadCalls = append(s.uploadCalls, struct{ localPath, mimeType string }{localPath, mimeType})
	return s.uploadStorageKey, s.uploadFileSize, nil
}

// writeQualflareReport writes one qualflare-native Collect JSON file — the
// exact shape @qualflare/cypress's outputFile mode writes to disk per shard
// — into dir. The single case it contains carries shardIndex and, when
// videoPath is non-empty, a video attachment referencing it via
// localVideoPath (relative to dir, matching how the reporter itself would
// write it).
func writeQualflareReport(t *testing.T, dir, filename, caseID string, shardIndex int, videoPath string) {
	t.Helper()

	attachments := ""
	if videoPath != "" {
		rel, err := filepath.Rel(dir, videoPath)
		if err != nil {
			t.Fatal(err)
		}
		attachments = fmt.Sprintf(`, "attachments": [{"name": "video", "mimeType": "video/mp4", "localVideoPath": %q}]`, rel)
	}

	content := fmt.Sprintf(`{
		"framework": "cypress",
		"metadata": {},
		"suites": [{
			"name": "spec.cy.ts",
			"duration": 1000000000,
			"cases": [{
				"id": %q, "name": "test %s", "status": "passed",
				"duration": 500000000, "shardIndex": %d%s
			}]
		}]
	}`, caseID, caseID, shardIndex, attachments)

	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestRunCollect_DirectoryMergePreservesShardIndexAndResolvesVideo is the
// end-to-end proof that Tasks 1-5 are wired together correctly: `qf collect
// <dir>` on a directory of qualflare-native shard files merges them into one
// launch (one SendReport call, not one per file), keeps each case's own
// embedded shardIndex intact (no --shard flag is passed, so tagShardsByFile
// never runs — the values here come straight from the qualflare parser
// reading each case's own "shardIndex" field), and resolves the one video
// attachment through the real ReportService.resolveVideoAttachments pass
// into StorageKey/FileSize with LocalPath cleared.
//
// The two files' embedded shardIndex values (9 and 4) are deliberately
// non-sequential and deliberately inverted relative to the files'
// alphabetical/glob-processing order (shard-0.json is processed first but
// carries shardIndex 9; shard-1.json is processed second but carries
// shardIndex 4). If a regression ever reintroduced file-encounter-order
// tagging (tagShardsByFile's --shard behavior) in place of reading each
// case's own embedded field, it would stamp 0 and 1 here — a value mismatch
// the assertions below would catch. Matching file order to shardIndex order
// would let exactly that regression pass unnoticed.
func TestRunCollect_DirectoryMergePreservesShardIndexAndResolvesVideo(t *testing.T) {
	dir := t.TempDir()

	videoPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(videoPath, []byte("fake video bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	writeQualflareReport(t, dir, "shard-0.json", "case-0", 9, "")
	writeQualflareReport(t, dir, "shard-1.json", "case-1", 4, videoPath)

	sender := &collectIntegrationSender{
		uploadStorageKey: "case-run-attachments/proj/clip.mp4",
		uploadFileSize:   12345,
	}
	cfg := config.DefaultConfig()
	cfg.APIKey = "qf_token"
	cfg.Quiet = true
	svc := services.NewReportService(factory.NewParserFactory(), sender, cfg)
	c := NewCLI(svc, cfg, nil, nil, nil)

	// This test asserts the video RESOLUTION path, which only runs for a kind
	// the user opted into — the gate defaults to uploading nothing. The gate's
	// own behaviour is covered in collect_artifact_gate_test.go.
	opts := baseOpts()
	opts.uploadArtifacts = "video"
	if err := c.runCollect(context.Background(), []string{dir}, opts); err != nil {
		t.Fatalf("runCollect() = %v", err)
	}

	if len(sender.sendReportCalls) != 1 {
		t.Fatalf("SendReport called %d times, want 1 (one merged launch, not one per shard file)", len(sender.sendReportCalls))
	}
	launch := sender.sendReportCalls[0]

	// Collect every case across every suite in the merged launch, keyed by
	// ID, so the assertions below don't assume a particular suite ordering.
	casesByID := map[string]domain.Case{}
	for _, suite := range launch.Suites {
		for _, tc := range suite.Cases {
			casesByID[tc.ID] = tc
		}
	}
	if len(casesByID) != 2 {
		t.Fatalf("expected 2 distinct cases across the merged launch's suites, got %d: %+v", len(casesByID), casesByID)
	}

	case0, ok := casesByID["case-0"]
	if !ok {
		t.Fatal("case-0 missing from the merged launch")
	}
	if case0.ShardIndex == nil || *case0.ShardIndex != 9 {
		t.Errorf("case-0 ShardIndex = %v, want 9 (its own embedded value, not its file's processing order)", case0.ShardIndex)
	}

	case1, ok := casesByID["case-1"]
	if !ok {
		t.Fatal("case-1 missing from the merged launch")
	}
	if case1.ShardIndex == nil || *case1.ShardIndex != 4 {
		t.Errorf("case-1 ShardIndex = %v, want 4 (its own embedded value, not its file's processing order)", case1.ShardIndex)
	}

	// Video resolution: exactly one UploadVideo call, for the one video
	// attachment, and its resulting attachment carries StorageKey/FileSize
	// with LocalPath cleared.
	if len(sender.uploadCalls) != 1 {
		t.Fatalf("UploadVideo called %d times, want 1", len(sender.uploadCalls))
	}
	if sender.uploadCalls[0].localPath != videoPath {
		t.Errorf("UploadVideo localPath = %q, want %q", sender.uploadCalls[0].localPath, videoPath)
	}
	if sender.uploadCalls[0].mimeType != "video/mp4" {
		t.Errorf("UploadVideo mimeType = %q, want %q", sender.uploadCalls[0].mimeType, "video/mp4")
	}

	if len(case1.Attachments) != 1 {
		t.Fatalf("case-1 expected 1 attachment, got %d: %+v", len(case1.Attachments), case1.Attachments)
	}
	att := case1.Attachments[0]
	if att.StorageKey != "case-run-attachments/proj/clip.mp4" {
		t.Errorf("StorageKey = %q, want the resolved storage key", att.StorageKey)
	}
	if att.FileSize != 12345 {
		t.Errorf("FileSize = %d, want 12345", att.FileSize)
	}
	if att.LocalPath != "" {
		t.Errorf("LocalPath = %q, want cleared after resolution", att.LocalPath)
	}

	// case-0 never referenced a video, so it must carry no attachments and
	// UploadVideo must never have been asked about it.
	if len(case0.Attachments) != 0 {
		t.Errorf("case-0 expected no attachments, got %+v", case0.Attachments)
	}
}
