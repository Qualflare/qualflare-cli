package flutter

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

const mib = 1 << 20

// inlineRun builds a synthetic run of one test per entry in sizes, each
// attaching sizes[i] bytes as name att<i>.<ext> of the given MIME type.
func inlineRun(t *testing.T, mime, ext string, sizes ...int) *domain.Suite {
	t.Helper()
	evs := make([]map[string]any, 0, 2+3*len(sizes))
	evs = append(evs,
		map[string]any{"type": "start", "time": 0},
		map[string]any{"type": "suite", "suite": map[string]any{"id": 0, "platform": "vm", "path": "test/a_test.dart"}},
	)
	for i, size := range sizes {
		id := i + 1
		data := base64.StdEncoding.EncodeToString(make([]byte, size))
		evs = append(evs,
			map[string]any{"type": "testStart", "time": 1, "test": map[string]any{"id": id, "name": fmt.Sprintf("t%d", id), "suiteID": 0, "metadata": map[string]any{}}},
			map[string]any{"type": "print", "testID": id, "messageType": "print", "message": marker(t, map[string]any{
				"k": "att", "id": "a", "name": fmt.Sprintf("att%d.%s", id, ext), "type": mime, "n": 1, "i": 0, "data": data,
			})},
			map[string]any{"type": "testDone", "time": 2, "testID": id, "result": "success"},
		)
	}
	return parseString(t, jsonl(t, evs...))
}

func droppedPrefix(name string, size int, mime string) string {
	return fmt.Sprintf("qualflare: attachment %q (%d bytes, %s) dropped: ", name, size, mime)
}

// Synthetic: two tests each attach 4 MiB of JSON, over the 1 MiB inline limit.
func TestInlineBudget_LargeNonImageDropped(t *testing.T) {
	suite := inlineRun(t, "application/json", "json", 4*mib, 4*mib)
	for i, c := range suite.Cases {
		if len(c.Attachments) != 0 {
			t.Errorf("%s: attachments = %q, want none", c.Name, attNames(c))
		}
		want := droppedPrefix(fmt.Sprintf("att%d.json", i+1), 4*mib, "application/json")
		if !strings.Contains(c.Properties[propSystemOut], want) {
			t.Errorf("%s: system-out %q lacks %q", c.Name, c.Properties[propSystemOut], want)
		}
	}
}

// Synthetic: nine tests each attach 0.9 MiB of JSON; the ninth would take the
// run's inline total past 8 MiB.
func TestInlineBudget_RunTotalSpansCases(t *testing.T) {
	size := 9 * mib / 10
	sizes := make([]int, 9)
	for i := range sizes {
		sizes[i] = size
	}
	suite := inlineRun(t, "application/json", "json", sizes...)
	for i, c := range suite.Cases[:8] {
		if len(c.Attachments) != 1 {
			t.Errorf("case %d: attachments = %q, want one", i+1, attNames(c))
		}
		if strings.Contains(c.Properties[propSystemOut], "dropped") {
			t.Errorf("case %d: unexpected warning %q", i+1, c.Properties[propSystemOut])
		}
	}
	ninth := suite.Cases[8]
	if len(ninth.Attachments) != 0 {
		t.Errorf("ninth: attachments = %q, want none", attNames(ninth))
	}
	if want := droppedPrefix("att9.json", size, "application/json"); !strings.Contains(ninth.Properties[propSystemOut], want) {
		t.Errorf("ninth: system-out %q lacks %q", ninth.Properties[propSystemOut], want)
	}
}

// Synthetic: a 4 MiB PNG is offloaded by the upload, so the inline budget
// does not apply to it.
func TestInlineBudget_ImagesUnaffected(t *testing.T) {
	c := inlineRun(t, "image/png", "png", 4*mib).Cases[0]
	if got := attNames(c); len(got) != 1 || got[0] != "att1.png" {
		t.Errorf("attachments = %q, want att1.png", got)
	}
	if out := c.Properties[propSystemOut]; out != "" {
		t.Errorf("system-out = %q, want empty", out)
	}
}

// Synthetic: 250 plain print lines, more than the output clamp keeps, and an
// attachment missing a chunk; its warning must survive the clamp.
func TestWarnings_SurviveOutputClamp(t *testing.T) {
	evs := make([]map[string]any, 0, 252)
	for i := range 250 {
		evs = append(evs, printEv(fmt.Sprintf("plain line %d", i)))
	}
	evs = append(evs,
		printEv(marker(t, map[string]any{"k": "att", "id": "a", "name": "log.txt", "type": "text/plain", "n": 2, "i": 0, "data": "aGk="})),
		map[string]any{"type": "testDone", "time": 5, "testID": 1, "result": "success"},
	)
	c := syntheticRun(t, []string{"test/a_test.dart"}, "chatty", nil, evs...).Cases[0]
	want := `qualflare: attachment "log.txt" is missing chunks (1 of 2 arrived); dropped`
	if out := c.Properties[propSystemOut]; !strings.HasSuffix(out, want) {
		t.Errorf("system-out ends with %q, want %q", out[max(0, len(out)-120):], want)
	}
}
