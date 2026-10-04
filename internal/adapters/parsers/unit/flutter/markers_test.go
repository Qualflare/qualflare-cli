package flutter

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/core/domain"
)

var pngMagic = []byte("\x89PNG\r\n\x1a\n")

// marker renders one `##qualflare[v1]` line the way the package does.
func marker(t *testing.T, fields map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return markerPrefix + string(raw)
}

func printEv(msg string) map[string]any {
	return map[string]any{"type": "print", "testID": 1, "messageType": "print", "message": msg}
}

func attNames(c domain.Case) []string {
	names := make([]string, 0, len(c.Attachments))
	for _, a := range c.Attachments {
		names = append(names, a.Name)
	}
	return names
}

func findAtt(t *testing.T, c domain.Case, name string) domain.Attachment {
	t.Helper()
	for _, a := range c.Attachments {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("case %q has no attachment %q; have %q", c.Name, name, attNames(c))
	return domain.Attachment{}
}

func decodeAtt(t *testing.T, a domain.Attachment) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(a.Content)
	if err != nil {
		t.Fatalf("attachment %q is not base64: %v", a.Name, err)
	}
	return b
}

func assertNoMarkersInOutput(t *testing.T, suite *domain.Suite) {
	t.Helper()
	for _, c := range suite.Cases {
		if strings.Contains(c.Properties[propSystemOut], "##qualflare") {
			t.Errorf("%s: marker line in system-out", c.ID)
		}
		for _, a := range c.Attempts {
			for _, l := range a.Stdout {
				if strings.Contains(l, "##qualflare") {
					t.Errorf("%s attempt %d: marker line in stdout", c.ID, a.Number)
				}
			}
		}
	}
}

// assertPackageCapture checks what the package's example suite records, the
// same on the host and on devices.
func assertPackageCapture(t *testing.T, fixture, dir string, wantNative bool) {
	suite := parseFixture(t, fixture)
	cases := byID(suite)
	assertNoMarkersInOutput(t, suite)

	pays := mustCase(t, cases, dir+"#pays for the cart")
	if want := []domain.Label{{Name: "feature", Value: "checkout"}}; !reflect.DeepEqual(pays.Labels, want) {
		t.Errorf("labels = %+v, want %+v", pays.Labels, want)
	}
	if want := []domain.Link{{Type: "issue", Name: "QF-42", URL: "https://example.com/issues/42"}}; !reflect.DeepEqual(pays.Links, want) {
		t.Errorf("links = %+v, want %+v", pays.Links, want)
	}
	if want := []string{"smoke", "checkout"}; !reflect.DeepEqual(pays.Tags, want) {
		t.Errorf("tags = %q, want %q", pays.Tags, want)
	}
	if pays.Priority != domain.SeverityHigh {
		t.Errorf("priority = %q, want high", pays.Priority)
	}
	wantSteps := []struct {
		name   string
		parent *int
	}{
		{"open the checkout", nil},
		{"see the total", domain.IntPtr(0)},
		{"apply a promo code", nil},
		{"pay", nil},
	}
	if len(pays.Steps) != len(wantSteps) {
		t.Fatalf("steps = %+v, want %d", pays.Steps, len(wantSteps))
	}
	for i, w := range wantSteps {
		s := pays.Steps[i]
		if s.Name != w.name || s.Status != domain.StatusPassed || s.Error != "" || !reflect.DeepEqual(s.ParentIndex, w.parent) {
			t.Errorf("step %d = %+v, want %q parent %v passed", i, s, w.name, w.parent)
		}
	}
	if pays.Steps[0].Duration <= 0 || pays.Steps[0].Duration < pays.Steps[1].Duration {
		t.Errorf("step durations = %v / %v", pays.Steps[0].Duration, pays.Steps[1].Duration)
	}
	if got, want := attNames(pays), []string{"order.json", "confirmed.png"}; !reflect.DeepEqual(got, want) {
		t.Errorf("attachments = %q, want %q", got, want)
	}
	order := findAtt(t, pays, "order.json")
	if order.MimeType != "application/json" || string(decodeAtt(t, order)) != `{"total":15,"promo":"SAVE5"}` {
		t.Errorf("order.json = %s %q", order.MimeType, decodeAtt(t, order))
	}
	if shot := findAtt(t, pays, "confirmed.png"); shot.MimeType != "image/png" || !bytes.HasPrefix(decodeAtt(t, shot), pngMagic) {
		t.Errorf("confirmed.png is not a PNG (%s)", shot.MimeType)
	}

	fails := mustCase(t, cases, dir+"#shows a receipt (fails on purpose)")
	if fails.Status != domain.StatusFailed || !strings.Contains(fails.Error, `"Receipt"`) {
		t.Errorf("failing test: status=%s error=%q", fails.Status, fails.Error)
	}
	if got, want := attNames(fails), []string{"failure.png"}; !reflect.DeepEqual(got, want) {
		t.Errorf("failing test attachments = %q, want %q", got, want)
	}
	if shot := findAtt(t, fails, "failure.png"); shot.MimeType != "image/png" || !bytes.HasPrefix(decodeAtt(t, shot), pngMagic) {
		t.Errorf("failure.png is not a PNG (%s)", shot.MimeType)
	}
	if len(fails.Labels)+len(fails.Links)+len(fails.Tags)+len(fails.Steps) != 0 || fails.Priority != "" {
		t.Errorf("failing test picked up metadata: %+v", fails)
	}

	if wantNative {
		native := mustCase(t, cases, dir+"#captures the native screen")
		if got, want := attNames(native), []string{"native.png"}; !reflect.DeepEqual(got, want) {
			t.Errorf("native attachments = %q, want %q", got, want)
		}
		if shot := findAtt(t, native, "native.png"); !bytes.HasPrefix(decodeAtt(t, shot), pngMagic) {
			t.Error("native.png is not a PNG")
		}
	}
}

func TestPackageCapture_Widget(t *testing.T) {
	assertPackageCapture(t, "package-widget.jsonl", "test/checkout_widget_test.dart", false)
}

func TestPackageCapture_Android(t *testing.T) {
	assertPackageCapture(t, "package-android.jsonl", "integration_test/checkout_test.dart", true)
}

func TestPackageCapture_IOS(t *testing.T) {
	assertPackageCapture(t, "package-ios.jsonl", "integration_test/checkout_test.dart", true)
}

// The capture's retried test fails once (taking failure.png) and then passes.
func TestRetriedTest_AttachmentsFromBothAttempts(t *testing.T) {
	for fixture, dir := range map[string]string{
		"package-widget.jsonl":  "test/checkout_widget_test.dart",
		"package-android.jsonl": "integration_test/checkout_test.dart",
		"package-ios.jsonl":     "integration_test/checkout_test.dart",
	} {
		t.Run(fixture, func(t *testing.T) {
			c := mustCase(t, byID(parseFixture(t, fixture)), dir+"#recovers on the second attempt")
			if c.Status != domain.StatusPassed || len(c.Attempts) != 2 {
				t.Fatalf("status=%s attempts=%d", c.Status, len(c.Attempts))
			}
			if got, want := attNames(c), []string{"attempt 1: failure.png"}; !reflect.DeepEqual(got, want) {
				t.Errorf("attachments = %q, want %q", got, want)
			}
			if !bytes.HasPrefix(decodeAtt(t, findAtt(t, c, "attempt 1: failure.png")), pngMagic) {
				t.Error("attempt 1: failure.png is not a PNG")
			}
		})
	}

	// Synthetic: both attempts attach, and only the final attempt's steps stay.
	att := func(id, name, data string) string {
		return marker(t, map[string]any{"k": "att", "id": id, "name": name, "type": "text/plain", "n": 1, "i": 0, "data": base64.StdEncoding.EncodeToString([]byte(data))})
	}
	suite := syntheticRun(t, []string{"test/a_test.dart"}, "flaky", nil,
		printEv(marker(t, map[string]any{"k": "step+", "id": 1, "name": "first try", "t": 10})),
		printEv(att("a1", "log.txt", "one")),
		printEv(marker(t, map[string]any{"k": "label", "name": "owner", "value": "ana"})),
		map[string]any{"type": "error", "testID": 1, "error": "boom", "stackTrace": "", "isFailure": true},
		printEv("Retry: flaky"),
		printEv(marker(t, map[string]any{"k": "step+", "id": 2, "name": "second try", "t": 20})),
		printEv(marker(t, map[string]any{"k": "step-", "id": 2, "status": "passed", "t": 25})),
		printEv(att("a2", "log.txt", "two")),
		printEv(marker(t, map[string]any{"k": "label", "name": "owner", "value": "ana"})),
		map[string]any{"type": "testDone", "time": 30, "testID": 1, "result": "success"},
	)
	c := suite.Cases[0]
	if got, want := attNames(c), []string{"attempt 1: log.txt", "log.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("attachments = %q, want %q", got, want)
	}
	if string(decodeAtt(t, c.Attachments[0])) != "one" || string(decodeAtt(t, c.Attachments[1])) != "two" {
		t.Errorf("attachment contents = %+v", c.Attachments)
	}
	if len(c.Steps) != 1 || c.Steps[0].Name != "second try" || c.Steps[0].Duration != 5*time.Millisecond {
		t.Errorf("steps = %+v", c.Steps)
	}
	if want := []domain.Label{{Name: "owner", Value: "ana"}}; !reflect.DeepEqual(c.Labels, want) {
		t.Errorf("labels = %+v", c.Labels)
	}
	if strings.Contains(c.Properties[propSystemOut], "did not finish") {
		t.Errorf("an earlier attempt's open step was reported: %q", c.Properties[propSystemOut])
	}
}

func TestMarkers_ChunksInterleavedAndOutOfOrder(t *testing.T) {
	a := []byte(strings.Repeat("first attachment ", 20))
	b := bytes.Repeat([]byte{0, 1, 2, 250}, 30)
	ea, eb := base64.StdEncoding.EncodeToString(a), base64.StdEncoding.EncodeToString(b)
	chunk := func(id, name, data string, n, i, size int) string {
		end := min((i+1)*size, len(data))
		return marker(t, map[string]any{"k": "att", "id": id, "name": name, "type": "application/octet-stream", "n": n, "i": i, "data": data[i*size : end]})
	}
	// Chunk sizes are multiples of 4 characters, as the package's are.
	prints := []string{
		chunk("b", "b.bin", eb, 3, 2, 64),
		"plain output",
		chunk("a", "a.txt", ea, 2, 1, 240),
		chunk("b", "b.bin", eb, 3, 0, 64),
		chunk("a", "a.txt", ea, 2, 0, 240),
		chunk("b", "b.bin", eb, 3, 1, 64),
	}
	set, rest := extractMarkers(prints)
	if !reflect.DeepEqual(rest, []string{"plain output"}) {
		t.Errorf("rest = %q", rest)
	}
	if len(set.warnings) != 0 {
		t.Errorf("warnings = %q", set.warnings)
	}
	if len(set.attachments) != 2 {
		t.Fatalf("attachments = %+v", set.attachments)
	}
	// In order of first appearance.
	got := map[string][]byte{}
	for _, at := range set.attachments {
		raw, err := base64.StdEncoding.DecodeString(at.Content)
		if err != nil {
			t.Fatal(err)
		}
		got[at.Name] = raw
	}
	if set.attachments[0].Name != "b.bin" || !bytes.Equal(got["b.bin"], b) || !bytes.Equal(got["a.txt"], a) {
		t.Errorf("reassembled = %q", got)
	}
}

func TestMarkers_IncompleteAttachmentDropped(t *testing.T) {
	suite := syntheticRun(t, []string{"test/a_test.dart"}, "partial", nil,
		printEv(marker(t, map[string]any{"k": "att", "id": "a1", "name": "big.png", "type": "image/png", "n": 3, "i": 0, "data": "AAAA"})),
		printEv(marker(t, map[string]any{"k": "att", "id": "a1", "name": "big.png", "type": "image/png", "n": 3, "i": 2, "data": "AAAA"})),
		map[string]any{"type": "testDone", "time": 5, "testID": 1, "result": "success"},
	)
	c := suite.Cases[0]
	if len(c.Attachments) != 0 {
		t.Errorf("attachments = %+v", c.Attachments)
	}
	out := c.Properties[propSystemOut]
	if !strings.Contains(out, "qualflare: ") || !strings.Contains(out, "big.png") || strings.Contains(out, "##qualflare") {
		t.Errorf("system-out = %q", out)
	}
}

func TestMarkers_CapsReapplied(t *testing.T) {
	att := func(id string, size int) string {
		return marker(t, map[string]any{"k": "att", "id": id, "name": id + ".bin", "type": "application/octet-stream", "n": 1, "i": 0,
			"data": base64.StdEncoding.EncodeToString(make([]byte, size))})
	}
	// 5 MiB is allowed (inclusive); one byte more is not. Four at the cap fill
	// the 20 MiB test budget exactly; a fifth is dropped.
	set, _ := extractMarkers([]string{
		att("over", maxAttachmentBytes+1),
		att("a", maxAttachmentBytes), att("b", maxAttachmentBytes), att("c", maxAttachmentBytes), att("d", maxAttachmentBytes),
		att("e", 1),
	})
	names := make([]string, 0, len(set.attachments))
	for _, a := range set.attachments {
		names = append(names, a.Name)
	}
	if want := []string{"a.bin", "b.bin", "c.bin", "d.bin"}; !reflect.DeepEqual(names, want) {
		t.Errorf("kept = %q, want %q", names, want)
	}
	if len(set.warnings) != 2 {
		t.Errorf("warnings = %q", set.warnings)
	}
}

func TestMarkers_UnknownVersionWarned(t *testing.T) {
	set, rest := extractMarkers([]string{
		`##qualflare[v2] {"k":"label","name":"a","value":"b"}`,
		`##qualflare[v2] {"k":"label","name":"c","value":"d"}`,
		marker(t, map[string]any{"k": "label", "name": "kept", "value": "yes"}),
	})
	if len(rest) != 0 {
		t.Errorf("rest = %q", rest)
	}
	if want := []domain.Label{{Name: "kept", Value: "yes"}}; !reflect.DeepEqual(set.labels, want) {
		t.Errorf("labels = %+v", set.labels)
	}
	if len(set.warnings) != 1 || !strings.Contains(set.warnings[0], "version") {
		t.Errorf("warnings = %q, want one about the version", set.warnings)
	}
}

func TestMarkers_MalformedSkipped(t *testing.T) {
	set, rest := extractMarkers([]string{
		markerPrefix + `{"k":"label",`,
		markerPrefix + `not json`,
		markerPrefix + `{"k":"dance"}`,
		markerPrefix + `{"k":"dance","x":1}`,
		markerPrefix + `{"k":"link","type":"jira","url":"https://x"}`,
		markerPrefix + `{"k":"priority","value":"urgent"}`,
		marker(t, map[string]any{"k": "tag", "tags": []string{"ok"}}),
		marker(t, map[string]any{"k": "warn", "msg": "screenshot \"x\" failed: no binding"}),
	})
	if len(rest) != 0 {
		t.Errorf("rest = %q", rest)
	}
	if !reflect.DeepEqual(set.tags, []string{"ok"}) || len(set.links) != 0 || set.priority != "" {
		t.Errorf("set = %+v", set)
	}
	want := map[string]bool{"malformed": false, `"dance"`: false, "jira": false, "urgent": false, `screenshot "x" failed`: false}
	for _, w := range set.warnings {
		for k := range want {
			if strings.Contains(w, k) {
				want[k] = true
			}
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("no warning mentioning %s in %q", k, set.warnings)
		}
	}
	if len(set.warnings) != len(want) {
		t.Errorf("got %d warnings, want one each kind (%d): %q", len(set.warnings), len(want), set.warnings)
	}
}

func TestMarkers_StepUnfinishedIsError(t *testing.T) {
	set, _ := extractMarkers([]string{
		marker(t, map[string]any{"k": "step+", "id": 1, "name": "outer", "t": 100}),
		marker(t, map[string]any{"k": "step+", "id": 2, "parent": 1, "name": "inner", "t": 110}),
		marker(t, map[string]any{"k": "step-", "id": 2, "status": "failed", "t": 150, "error": "Expected: 1"}),
	})
	if len(set.steps) != 2 {
		t.Fatalf("steps = %+v", set.steps)
	}
	outer, inner := set.steps[0], set.steps[1]
	if outer.Status != domain.StatusError || outer.Error != "step did not finish" || outer.ParentIndex != nil {
		t.Errorf("outer = %+v", outer)
	}
	if inner.Status != domain.StatusFailed || inner.Error != "Expected: 1" || inner.Duration != 40*time.Millisecond ||
		inner.ParentIndex == nil || *inner.ParentIndex != 0 {
		t.Errorf("inner = %+v", inner)
	}
}

func TestMarkers_AfterTestDoneIgnored(t *testing.T) {
	suite := syntheticRun(t, []string{"test/a_test.dart"}, "done", nil,
		printEv(marker(t, map[string]any{"k": "tag", "tags": []string{"before"}})),
		map[string]any{"type": "testDone", "time": 5, "testID": 1, "result": "success"},
		printEv(marker(t, map[string]any{"k": "tag", "tags": []string{"after"}})),
		printEv("late plain output"),
	)
	c := suite.Cases[0]
	if !reflect.DeepEqual(c.Tags, []string{"before"}) {
		t.Errorf("tags = %q", c.Tags)
	}
	if out := c.Properties[propSystemOut]; out != "late plain output" {
		t.Errorf("system-out = %q", out)
	}
}

func TestMarkers_HiddenTestIgnored(t *testing.T) {
	suite := syntheticRun(t, []string{"test/a_test.dart"}, "(setUpAll)", nil,
		printEv(marker(t, map[string]any{"k": "tag", "tags": []string{"x"}})),
		map[string]any{"type": "testDone", "time": 5, "testID": 1, "result": "success", "hidden": true},
	)
	if len(suite.Cases) != 0 {
		t.Errorf("cases = %+v", suite.Cases)
	}
}

// goldenSuite renders a suite with its wall-clock timestamp zeroed.
func goldenSuite(t *testing.T, s *domain.Suite) []byte {
	t.Helper()
	s.Timestamp = time.Time{}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

// The four Phase 1 captures, which carry no markers, parse exactly as v0.2.0
// did. The goldens were written by v0.2.0's parser (QF_UPDATE_GOLDEN=1).
func TestMarkers_NoneParsesAsBefore(t *testing.T) {
	for _, name := range []string{"widget-machine.jsonl", "widget-file-reporter.jsonl", "device-android.jsonl", "device-ios.jsonl"} {
		t.Run(name, func(t *testing.T) {
			got := goldenSuite(t, parseFixture(t, name))
			path := filepath.Join("testdata", "v0.2.0", strings.TrimSuffix(name, ".jsonl")+".golden.json")
			if os.Getenv("QF_UPDATE_GOLDEN") == "1" {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s parses differently from v0.2.0", name)
			}
		})
	}
}

// The per-test cap covers every attempt together, as the package counts it.
func TestMarkers_TestCapAcrossAttempts(t *testing.T) {
	full := base64.StdEncoding.EncodeToString(make([]byte, maxAttachmentBytes))
	att := func(name string) domain.Attachment {
		return domain.Attachment{Name: name, MimeType: "image/png", Content: full}
	}
	var c domain.Case
	warnings := applyMarkers(&c, []markerSet{
		{attachments: []domain.Attachment{att("a.png"), att("b.png"), att("c.png")}},
		{attachments: []domain.Attachment{att("d.png"), att("e.png")}},
	})
	if got, want := attNames(c), []string{"attempt 1: a.png", "attempt 1: b.png", "attempt 1: c.png", "d.png"}; !reflect.DeepEqual(got, want) {
		t.Errorf("attachments = %q, want %q", got, want)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "e.png") {
		t.Errorf("warnings = %q", warnings)
	}
}
