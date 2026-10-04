package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
)

// wireBytes encodes a launch the way the HTTP adapter's resty client puts it on
// the wire for /collect: encoding/json with HTML escaping on, plus the encoder's
// trailing newline, uncompressed. client_test.go's
// TestSendReport_BodyIsUncompressedJSONEncoding pins that side of the contract.
func wireBytes(t *testing.T, l *domain.Launch) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(l); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func blobAttachment(name, mime string, decoded int, fill byte) domain.Attachment {
	return domain.Attachment{
		Name:     name,
		MimeType: mime,
		Content:  base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, decoded)),
	}
}

// bodyLimitService runs ProcessTestResults over one parsed suite, with every
// image upload failing — the only way an image is still inline by the time the
// body limit is enforced.
func bodyLimitService(suite *domain.Suite) (*ReportService, *videoStubSender, *strings.Builder) {
	parser := &stubParser{framework: domain.FrameworkJUnit, suite: suite}
	fac := &stubFactory{parsers: map[domain.Framework]ports.Parser{domain.FrameworkJUnit: parser}}
	sender := &videoStubSender{attachmentErr: errors.New("presign: 500")}
	warn := &strings.Builder{}
	svc := &ReportService{parserFactory: fac, sender: sender, config: config.DefaultConfig(), warn: warn}
	return svc, sender, warn
}

func TestBodyLimit_DropsLargestNonImagesFirst(t *testing.T) {
	const mb = 1_000_000
	// Encoded (base64) sizes: JSON 1.0 / 0.6 / 0.2 MB, PNG 6.0 / 3.6 MB — about
	// 11.4 MB inline. Dropping every JSON still leaves 9.6 MB, so exactly one
	// image (the larger) has to go too.
	suite := &domain.Suite{
		Name: "suite",
		Cases: []domain.Case{
			{Name: "case-json-mid", Status: domain.StatusPassed, Attachments: []domain.Attachment{
				blobAttachment("mid.json", "application/json", 450_000, 'b'),
			}},
			{Name: "case-png-big", Status: domain.StatusFailed, Attachments: []domain.Attachment{
				blobAttachment("big.png", "image/png", 4_500_000, 'p'),
				{Name: "already.png", MimeType: "image/png", StorageKey: "k/already.png", FileSize: 10},
			}},
			{Name: "case-json-big", Status: domain.StatusPassed, Attachments: []domain.Attachment{
				blobAttachment("big.json", "application/json", 750_000, 'a'),
			}},
			{Name: "case-png-small", Status: domain.StatusFailed, Attachments: []domain.Attachment{
				blobAttachment("small.png", "image/png", 2_700_000, 'q'),
			}},
			{Name: "case-json-small", Status: domain.StatusPassed, Attachments: []domain.Attachment{
				blobAttachment("small.json", "application/json", 150_000, 'c'),
			}},
		},
	}
	if n := len(wireBytes(t, &domain.Launch{Suites: []domain.Suite{*suite}})); n < 11*mb {
		t.Fatalf("fixture should be ~11.4 MB to be meaningful, got %d", n)
	}

	svc, sender, warn := bodyLimitService(suite)
	f := writeFile(t, t.TempDir(), "r.xml", "<x/>")
	if err := svc.ProcessTestResults(context.Background(), []string{f}, domain.FrameworkJUnit); err != nil {
		t.Fatalf("ProcessTestResults() = %v", err)
	}
	if sender.sent != 1 || sender.last == nil {
		t.Fatalf("expected one send, got %d", sender.sent)
	}

	if n := len(wireBytes(t, sender.last)); n > maxCollectBodyBytes {
		t.Errorf("sent body is %d bytes, want <= %d", n, maxCollectBodyBytes)
	}

	kept := map[string]bool{}
	for _, c := range sender.last.Suites[0].Cases {
		for _, a := range c.Attachments {
			kept[a.Name] = true
		}
	}
	for _, name := range []string{"big.json", "mid.json", "small.json", "big.png"} {
		if kept[name] {
			t.Errorf("%s should have been dropped", name)
		}
	}
	for _, name := range []string{"small.png", "already.png"} {
		if !kept[name] {
			t.Errorf("%s should have been kept", name)
		}
	}

	out := warn.String()
	wantOrder := []string{
		`warning: dropped attachment "big.json" (1.0 MB) from "case-json-big" to keep the upload under the server's 10 MB limit`,
		`warning: dropped attachment "mid.json" (600.0 KB) from "case-json-mid"`,
		`warning: dropped attachment "small.json" (200.0 KB) from "case-json-small"`,
		`warning: dropped attachment "big.png" (6.0 MB) from "case-png-big"`,
		"dropped 4 inline attachment(s) totalling 7.8 MB",
	}
	last := -1
	for _, w := range wantOrder {
		i := strings.Index(out, w)
		if i < 0 {
			t.Errorf("warnings missing %q\ngot:\n%s", w, out)
			continue
		}
		if i < last {
			t.Errorf("%q is out of order (JSON largest-first, then images)\ngot:\n%s", w, out)
		}
		last = i
	}
}

func TestBodyLimit_SmallLaunchUntouched(t *testing.T) {
	l := &domain.Launch{Suites: []domain.Suite{{Name: "s", Cases: []domain.Case{{
		Name:   "c <&>", // HTML-escaped by the encoder; must survive unchanged
		Status: domain.StatusPassed,
		Attachments: []domain.Attachment{
			blobAttachment("log.json", "application/json", 10_000, 'x'),
			blobAttachment("shot.png", "image/png", 10_000, 'y'),
		},
	}}}}}
	before := wireBytes(t, l)
	warn := &strings.Builder{}

	if err := (&ReportService{warn: warn}).enforceBodyLimit(l); err != nil {
		t.Fatalf("enforceBodyLimit() = %v", err)
	}

	if after := wireBytes(t, l); !bytes.Equal(before, after) {
		t.Error("a launch under the limit must be sent byte-identical")
	}
	if warn.String() != "" {
		t.Errorf("nothing to say about a small launch, got %q", warn.String())
	}
}

func TestBodyLimit_ResultsAloneTooLarge(t *testing.T) {
	// ~12 MB of results with no attachments: nothing can be dropped.
	cases := make([]domain.Case, 0, 12_000)
	msg := strings.Repeat("m", 1_000)
	for i := 0; i < 12_000; i++ {
		cases = append(cases, domain.Case{Name: "case", Status: domain.StatusFailed, Error: msg})
	}
	suite := &domain.Suite{Name: "huge", Cases: cases}

	svc, sender, _ := bodyLimitService(suite)
	f := writeFile(t, t.TempDir(), "r.xml", "<x/>")
	err := svc.ProcessTestResults(context.Background(), []string{f}, domain.FrameworkJUnit)
	if err == nil {
		t.Fatal("ProcessTestResults() = nil, want an error before sending")
	}
	for _, want := range []string{"MB, over /collect's 10 MB limit even without attachments", "split the upload"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err.Error(), want)
		}
	}
	if sender.sent != 0 {
		t.Errorf("nothing should be sent, got %d sends", sender.sent)
	}
}

// --dry-run never offloads (that needs the network), so its launch still holds
// every image inline and is not the body that would be sent. Applying the drop
// there would report images as dropped that a real run uploads, so dry-run
// leaves the launch alone.
func TestBodyLimit_NotAppliedUnderDryRun(t *testing.T) {
	suite := &domain.Suite{Name: "s", Cases: []domain.Case{{Name: "c", Status: domain.StatusFailed,
		Attachments: []domain.Attachment{blobAttachment("big.json", "application/json", 8_000_000, 'a')}}}}
	svc, sender, warn := bodyLimitService(suite)
	cfg := config.DefaultConfig()
	cfg.DryRun = true
	svc.config = cfg
	f := writeFile(t, t.TempDir(), "r.xml", "<x/>")

	if err := svc.ProcessTestResults(context.Background(), []string{f}, domain.FrameworkJUnit); err != nil {
		t.Fatalf("ProcessTestResults() = %v", err)
	}
	if sender.sent != 0 {
		t.Errorf("dry run sent %d times", sender.sent)
	}
	if strings.Contains(warn.String(), "dropped") {
		t.Errorf("dry run should not drop anything, got %q", warn.String())
	}
}
