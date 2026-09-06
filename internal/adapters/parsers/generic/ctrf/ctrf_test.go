package ctrf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/core/domain"
)

func parse(t *testing.T, doc string) *domain.Suite {
	t.Helper()
	suite, err := New().Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return suite
}

const minimalDoc = `{"reportFormat":"CTRF","results":{"tool":{"name":"jest"},
	"tests":[{"name":"a","status":"passed","duration":1000}]}}`

func TestParser_Contract(t *testing.T) {
	p := New()
	if p.GetFramework() != domain.FrameworkCTRF {
		t.Errorf("GetFramework() = %q, want ctrf", p.GetFramework())
	}
	if got := p.SupportedFileExtensions(); len(got) != 1 || got[0] != ".json" {
		t.Errorf("SupportedFileExtensions() = %v, want [.json]", got)
	}
}

func TestParse_AcceptsBothDocumentShapes(t *testing.T) {
	t.Run("the current shape", func(t *testing.T) {
		parse(t, minimalDoc)
	})

	// This is what every published README and every reporter pinned to an older
	// ctrf release still emits, so it is the majority of files in the wild.
	t.Run("the legacy shape with no reportFormat or specVersion", func(t *testing.T) {
		suite := parse(t, `{"results":{"tool":{"name":"jest"},"summary":{"start":0,"stop":1},
			"tests":[{"name":"a","status":"passed","duration":1}]}}`)
		if len(suite.Cases) != 1 {
			t.Fatalf("expected 1 case, got %d", len(suite.Cases))
		}
	})

	t.Run("unknown properties are ignored, not rejected", func(t *testing.T) {
		parse(t, `{"reportFormat":"CTRF","somethingNew":1,"results":{
			"tests":[{"name":"a","status":"passed","duration":1,"futureField":true}]}}`)
	})

	t.Run("a document naming another format is refused", func(t *testing.T) {
		_, err := New().Parse(strings.NewReader(`{"reportFormat":"JUnit","results":{"tests":[]}}`))
		if err == nil {
			t.Fatal("expected an error for a non-CTRF reportFormat")
		}
	})

	t.Run("a document with no results object is refused", func(t *testing.T) {
		if _, err := New().Parse(strings.NewReader(`{"reportFormat":"CTRF"}`)); err == nil {
			t.Fatal("expected an error when results is absent")
		}
	})

	t.Run("malformed JSON is refused", func(t *testing.T) {
		if _, err := New().Parse(strings.NewReader(`{not json`)); err == nil {
			t.Fatal("expected an error for malformed JSON")
		}
	})
}

// The mapping must match native/qualflare's mapStatus for the same status words,
// or the same outcome would be recorded differently depending on which file
// extension it arrived in.
func TestMapStatus(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		rawStatus string
		want      domain.Status
	}{
		{"passed", "passed", "", domain.StatusPassed},
		{"failed", "failed", "", domain.StatusFailed},
		{"skipped", "skipped", "", domain.StatusSkipped},

		// NOT folded to skipped, unlike the mocha-family parsers. CTRF's enum
		// carries skipped and pending as separate values, so a document saying
		// pending means pending — folding it would discard a distinction the
		// format draws on purpose.
		{"pending stays pending", "pending", "", domain.StatusPending},

		// other is fail-visible: it carries broken/errored/interrupted in
		// practice, and anything green would let a bad run report clean.
		{"other maps to error", "other", "", domain.StatusError},
		{"an unrecognized status maps to error", "banana", "", domain.StatusError},
		{"an empty status maps to error", "", "", domain.StatusError},

		// rawStatus recovers what CTRF's five-value enum flattened away.
		{"rawStatus recovers error", "failed", "error", domain.StatusError},
		{"rawStatus broken means error", "other", "broken", domain.StatusError},
		{"rawStatus recovers aborted", "failed", "aborted", domain.StatusAborted},
		{"rawStatus recovers timeout", "other", "timeout", domain.StatusTimeout},
		{"rawStatus is matched case-insensitively", "failed", "  TIMEDOUT ", domain.StatusTimeout},
		{"interrupted is an abort", "other", "interrupted", domain.StatusAborted},
		// "excluded from the run" and "has not run yet" are different things.
		{"todo is skipped", "other", "todo", domain.StatusSkipped},
		{"notrun is pending", "other", "notrun", domain.StatusPending},
		{"an unknown rawStatus falls through to the status", "passed", "weird", domain.StatusPassed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapStatus(tt.status, tt.rawStatus); got != tt.want {
				t.Errorf("mapStatus(%q, %q) = %q, want %q", tt.status, tt.rawStatus, got, tt.want)
			}
		})
	}
}

// A category outside the server's oneof would fail validation and reject the
// whole launch, so this must always resolve to something valid.
func TestCategoryForTool(t *testing.T) {
	tests := []struct {
		tool string
		want domain.FrameworkCategory
	}{
		{"playwright", domain.FrameworkPlaywright.GetCategory()},
		{"Playwright", domain.FrameworkPlaywright.GetCategory()},
		{"  jest  ", domain.FrameworkJest.GetCategory()},
		{"go", domain.FrameworkGolang.GetCategory()},
		{"Go Test", domain.FrameworkGolang.GetCategory()},
		{"pytest", domain.FrameworkPython.GetCategory()},
		{"postman", domain.FrameworkNewman.GetCategory()},

		// No Qualflare framework exists for these; inventing one would put a
		// falsehood in the data model.
		{"jasmine", domain.CategoryGeneric},
		{"wdio", domain.CategoryGeneric},
		{"nightwatch", domain.CategoryGeneric},
		{"MSTest", domain.CategoryGeneric},
		{"NUnit", domain.CategoryGeneric},
		{"", domain.CategoryGeneric},
		{"some-runner-nobody-has-heard-of", domain.CategoryGeneric},

		// ctrf and qualflare-json are passthrough formats, not producing tools;
		// neither may be echoed back as a category.
		{"ctrf", domain.CategoryGeneric},
		{"qualflare-json", domain.CategoryGeneric},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			if got := categoryForTool(tt.tool); got != tt.want {
				t.Errorf("categoryForTool(%q) = %q, want %q", tt.tool, got, tt.want)
			}
		})
	}
}

// Every category this parser can produce must be one the server accepts.
func TestCategoryForToolAlwaysProducesAValidCategory(t *testing.T) {
	valid := map[domain.FrameworkCategory]bool{}
	for _, f := range domain.AllFrameworks() {
		valid[f.GetCategory()] = true
	}
	valid[domain.CategoryGeneric] = true

	seeds := []string{"playwright", "jest", "go", "jasmine", "", "ctrf", "нечто", "MSTest"}
	names := make([]string, 0, len(seeds)+len(domain.AllFrameworks()))
	names = append(names, seeds...)
	for _, f := range domain.AllFrameworks() {
		names = append(names, string(f))
	}
	for _, n := range names {
		if got := categoryForTool(n); !valid[got] {
			t.Errorf("categoryForTool(%q) = %q, which no framework advertises", n, got)
		}
	}
}

func TestParse_SuiteLevel(t *testing.T) {
	suite := parse(t, `{"reportFormat":"CTRF","results":{
		"tool":{"name":"playwright","version":"1.47.0"},
		"environment":{"reportName":"Nightly","osPlatform":"linux","shardId":"2"},
		"summary":{"start":1756720800000,"stop":1756720812000},
		"tests":[
			{"name":"a","status":"passed","duration":1000,"browser":"chromium"},
			{"name":"b","status":"failed","duration":2000,"browser":"chromium"}
		]}}`)

	if suite.Name != "Nightly" {
		t.Errorf("name = %q, want Nightly", suite.Name)
	}
	// The PRODUCING tool's identity, not a synthetic ctrf category.
	if suite.Category != domain.FrameworkPlaywright.GetCategory() {
		t.Errorf("category = %q, want playwright's", suite.Category)
	}
	if want := 12 * time.Second; suite.Duration != want {
		t.Errorf("duration = %v, want %v", suite.Duration, want)
	}
	if suite.Timestamp.UnixMilli() != 1756720800000 {
		t.Errorf("timestamp = %v, want the summary start", suite.Timestamp)
	}
	if suite.Properties["ctrfTool"] != "playwright" {
		t.Errorf("the raw tool name must be preserved, got %q", suite.Properties["ctrfTool"])
	}
	if suite.Properties["browser"] != "chromium" {
		t.Errorf("browser = %q, want chromium", suite.Properties["browser"])
	}
	// A shard id applies to the whole report, so every case carries it.
	for _, c := range suite.Cases {
		if c.ShardIndex == nil || *c.ShardIndex != 2 {
			t.Errorf("shardIndex = %v, want 2", c.ShardIndex)
		}
	}
}

// A summary that disagrees with tests[] must not be able to roll a red run
// green: counts come from the cases.
func TestParse_CountsComeFromCasesNotTheSummary(t *testing.T) {
	suite := parse(t, `{"results":{"tool":{"name":"jest"},
		"summary":{"tests":99,"passed":99,"failed":0,"start":0,"stop":1},
		"tests":[{"name":"a","status":"failed","duration":1}]}}`)

	if suite.TotalTests != 1 || suite.Failed != 1 || suite.Passed != 0 {
		t.Errorf("counts = total %d passed %d failed %d; a lying summary must be ignored",
			suite.TotalTests, suite.Passed, suite.Failed)
	}
}

func TestParse_CaseLevel(t *testing.T) {
	suite := parse(t, `{"results":{"tests":[{
		"name":"pays with a card","status":"failed","duration":2100,
		"testId":"stable-1","suite":["Checkout","Payment"],
		"filePath":"a.spec.ts","line":84,
		"message":"boom","trace":"at a.spec.ts:84","snippet":"expect(x)",
		"tags":["smoke"],"labels":{"owner":["ana","bo"],"tier":1},
		"stdout":["one","two"],"stderr":["err"],
		"steps":[{"name":"s1","status":"passed"}]}]}}`)

	c := suite.Cases[0]
	if c.ID != "stable-1" {
		t.Errorf("id = %q, want the testId", c.ID)
	}
	if c.ClassName != "Checkout > Payment" {
		t.Errorf("className = %q, want the joined suite path", c.ClassName)
	}
	if want := 2100 * time.Millisecond; c.Duration != want {
		t.Errorf("duration = %v, want %v (ms -> ns)", c.Duration, want)
	}
	if !strings.Contains(c.Error, "boom") || !strings.Contains(c.Error, "at a.spec.ts:84") {
		t.Errorf("error must merge message and trace, got %q", c.Error)
	}
	if c.Properties["ctrfSnippet"] != "expect(x)" {
		t.Error("the snippet has no first-class home and must survive as a property")
	}
	if c.Properties["system-out"] != "one\ntwo" {
		t.Errorf("stdout = %q, want newline-joined", c.Properties["system-out"])
	}
	if c.Properties["ctrfSuitePath"] != `["Checkout","Payment"]` {
		t.Errorf("the exact suite path must survive, got %q", c.Properties["ctrfSuitePath"])
	}
	// An array label becomes one pair per element, which is why Labels is a
	// slice and not a map.
	owners := 0
	for _, l := range c.Labels {
		if l.Name == "owner" {
			owners++
		}
	}
	if owners != 2 || len(c.Labels) != 3 {
		t.Errorf("labels = %v; an array value must expand to one pair per element", c.Labels)
	}
	if len(c.Steps) != 1 || c.Steps[0].Status != domain.StatusPassed {
		t.Errorf("steps = %v", c.Steps)
	}
}

func TestParse_CaseIdentity(t *testing.T) {
	t.Run("a synthesized identity is prefixed and stable", func(t *testing.T) {
		doc := `{"results":{"tests":[{"name":"a","status":"passed","duration":1,"suite":["S"]}]}}`
		first, second := parse(t, doc), parse(t, doc)

		id := first.Cases[0].ID
		if !strings.HasPrefix(id, "ctrf:") {
			t.Errorf("a synthesized identity must be distinguishable, got %q", id)
		}
		if id != second.Cases[0].ID {
			t.Error("a synthesized identity must be stable across runs, or flaky history breaks silently")
		}
	})

	t.Run("the legacy id is used when testId is absent", func(t *testing.T) {
		suite := parse(t, `{"results":{"tests":[{"name":"a","status":"passed","duration":1,"id":"legacy-1"}]}}`)
		if suite.Cases[0].ID != "legacy-1" {
			t.Errorf("id = %q, want legacy-1", suite.Cases[0].ID)
		}
	})
}

func TestParse_Retries(t *testing.T) {
	t.Run("retryAttempts length wins over a disagreeing retries", func(t *testing.T) {
		suite := parse(t, `{"results":{"tests":[{"name":"a","status":"passed","duration":1,
			"retries":9,"retryAttempts":[{"attempt":1,"status":"failed"},{"attempt":2,"status":"failed"}]}]}}`)
		c := suite.Cases[0]
		if c.RetryCount == nil || *c.RetryCount != 2 {
			t.Errorf("retryCount = %v, want 2 — the observed attempts are the truth", c.RetryCount)
		}
		// The wire model carries per-attempt history since the CLI gained
		// Case.Attempts, so the attempts are mapped rather than stringified into
		// a property.
		if len(c.Attempts) != 3 {
			t.Fatalf("attempts = %d, want 3 — two reported plus the test itself as the final one", len(c.Attempts))
		}
		if c.Properties["ctrfRetryAttempts"] != "" {
			t.Error("the attempts are first-class now; the JSON-blob property must be gone")
		}
	})

	t.Run("the test object is appended as the final attempt", func(t *testing.T) {
		// CTRF's retryAttempts deliberately EXCLUDES the last execution: the test
		// object is it. api-service's launch.Case.Attempts names this mapper as
		// the place that off-by-one is resolved, so a report claiming one retry
		// must produce two attempts, ending in the one that decided the outcome.
		suite := parse(t, `{"results":{"tests":[{"name":"a","status":"passed","duration":1200,"message":"final ok",
			"retries":1,"retryAttempts":[{"attempt":1,"status":"failed","duration":2900,"message":"gateway timed out"}]}]}}`)
		got := suite.Cases[0].Attempts
		if len(got) != 2 {
			t.Fatalf("attempts = %d, want 2", len(got))
		}
		if got[0].Number != 1 || got[0].Status != domain.StatusFailed || got[0].Message != "gateway timed out" {
			t.Errorf("first attempt = %+v; want the reported failure", got[0])
		}
		if got[0].Duration != 2900*time.Millisecond {
			t.Errorf("duration = %v; CTRF reports milliseconds", got[0].Duration)
		}
		if got[1].Number != 2 || got[1].Status != domain.StatusPassed {
			t.Errorf("final attempt = %+v; want the test object, numbered last", got[1])
		}
		if got[1].Duration != 1200*time.Millisecond {
			t.Errorf("final duration = %v; want the test's own duration", got[1].Duration)
		}
	})

	t.Run("a producer that already included the final attempt is not double-counted", func(t *testing.T) {
		// `retries` counts RETRIES, so len(retryAttempts) == retries+1 means the
		// producer put the final execution in the list itself. Appending again
		// would invent an attempt that never ran.
		suite := parse(t, `{"results":{"tests":[{"name":"a","status":"passed","duration":1,
			"retries":1,"retryAttempts":[{"attempt":1,"status":"failed"},{"attempt":2,"status":"passed"}]}]}}`)
		if got := suite.Cases[0].Attempts; len(got) != 2 {
			t.Fatalf("attempts = %d, want 2 — the list was already complete", len(got))
		}
	})

	t.Run("a single attempt sends nothing", func(t *testing.T) {
		// Fewer than two attempts persists nothing server-side: a lone attempt
		// carries no status or duration the case run does not already hold.
		//
		// Both routes in, because they are different code paths: no retryAttempts
		// at all, and a one-element list the producer already closed out (retries:0
		// with one entry means that entry IS the final attempt, so nothing is
		// appended and the list stays at one).
		noList := parse(t, `{"results":{"tests":[{"name":"a","status":"passed","duration":1,"retries":0}]}}`)
		if got := noList.Cases[0].Attempts; got != nil {
			t.Errorf("attempts = %v, want nil when no retryAttempts are reported", got)
		}

		alreadyClosed := parse(t, `{"results":{"tests":[{"name":"a","status":"passed","duration":1,
			"retries":0,"retryAttempts":[{"attempt":1,"status":"passed"}]}]}}`)
		if got := alreadyClosed.Cases[0].Attempts; got != nil {
			t.Errorf("attempts = %v, want nil for a lone attempt that is already the final one", got)
		}
	})

	t.Run("an oversized history keeps the attempt that decided the outcome", func(t *testing.T) {
		var b strings.Builder
		b.WriteString(`{"results":{"tests":[{"name":"a","status":"passed","duration":1,"retryAttempts":[`)
		for i := 1; i <= 60; i++ {
			if i > 1 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"attempt":%d,"status":"failed"}`, i)
		}
		b.WriteString(`]}]}}`)
		got := parse(t, b.String()).Cases[0].Attempts
		if len(got) != 50 {
			t.Fatalf("attempts = %d, want 50", len(got))
		}
		// A plain slice(0,50) would drop the appended final attempt, which is the
		// only element explaining why the case passed.
		if got[49].Status != domain.StatusPassed {
			t.Errorf("last attempt = %+v; the final attempt must survive the cap", got[49])
		}
	})

	t.Run("captured output is carried onto the attempt and bounded", func(t *testing.T) {
		// Bounded by LINES first, then by total runes -- the same order the server
		// truncates in, so what is dropped here is what would be dropped anyway.
		var lines []string
		for i := 0; i < 250; i++ {
			lines = append(lines, `"line"`)
		}
		doc := fmt.Sprintf(`{"results":{"tests":[{"name":"a","status":"passed","duration":1,
			"retryAttempts":[{"attempt":1,"status":"failed","stdout":[%s],"stderr":["err"]}]}]}}`,
			strings.Join(lines, ","))
		got := parse(t, doc).Cases[0].Attempts
		if n := len(got[0].Stdout); n != 200 {
			t.Errorf("stdout lines = %d, want the 200-line cap", n)
		}
		if len(got[0].Stderr) != 1 || got[0].Stderr[0] != "err" {
			t.Errorf("stderr = %v; a short stream must pass through untouched", got[0].Stderr)
		}
	})

	t.Run("a single oversized output line is cut to the rune budget", func(t *testing.T) {
		doc := fmt.Sprintf(`{"results":{"tests":[{"name":"a","status":"passed","duration":1,
			"retryAttempts":[{"attempt":1,"status":"failed","stdout":[%q]}]}]}}`, strings.Repeat("x", 20000))
		got := parse(t, doc).Cases[0].Attempts
		if n := len([]rune(got[0].Stdout[0])); n != 16384 {
			t.Errorf("stdout runes = %d, want the 16384 cap", n)
		}
	})

	t.Run("an attempt start time is carried, and a missing duration is not invented", func(t *testing.T) {
		suite := parse(t, `{"results":{"tests":[{"name":"a","status":"passed","duration":1,
			"retryAttempts":[{"attempt":1,"status":"failed","start":1756720800000}]}]}}`)
		got := suite.Cases[0].Attempts
		if got[0].StartedAt == nil {
			t.Fatal("startedAt must be carried when CTRF reports start")
		}
		if got[0].StartedAt.UTC().Format(time.RFC3339) != "2025-09-01T10:00:00Z" {
			t.Errorf("startedAt = %v; CTRF reports epoch milliseconds", got[0].StartedAt)
		}
		// No duration reported: zero, not a value borrowed from the case.
		if got[0].Duration != 0 {
			t.Errorf("duration = %v, want 0 when the attempt reports none", got[0].Duration)
		}
	})

	t.Run("a negative attempt duration becomes zero, not a negative duration", func(t *testing.T) {
		suite := parse(t, `{"results":{"tests":[{"name":"a","status":"passed","duration":1,
			"retryAttempts":[{"attempt":1,"status":"failed","duration":-5}]}]}}`)
		if d := suite.Cases[0].Attempts[0].Duration; d != 0 {
			t.Errorf("duration = %v, want 0", d)
		}
	})

	t.Run("attempt text is clamped to what the server stores", func(t *testing.T) {
		long := strings.Repeat("x", 20000)
		doc := fmt.Sprintf(`{"results":{"tests":[{"name":"a","status":"passed","duration":1,
			"retryAttempts":[{"attempt":1,"status":"failed","message":%q}]}]}}`, long)
		got := parse(t, doc).Cases[0].Attempts
		if n := len([]rune(got[0].Message)); n != 8192 {
			t.Errorf("message runes = %d, want 8192", n)
		}
	})

	t.Run("flaky follows CTRF's narrow definition when not stated", func(t *testing.T) {
		passed := parse(t, `{"results":{"tests":[{"name":"a","status":"passed","duration":1,"retries":1}]}}`)
		if passed.Cases[0].IsFlaky == nil || !*passed.Cases[0].IsFlaky {
			t.Error("passed after a retry is flaky")
		}
		failed := parse(t, `{"results":{"tests":[{"name":"a","status":"failed","duration":1,"retries":1}]}}`)
		if failed.Cases[0].IsFlaky == nil || *failed.Cases[0].IsFlaky {
			t.Error("failed after retries is NOT flaky under CTRF's definition")
		}
	})

	t.Run("an explicit flaky flag is honoured", func(t *testing.T) {
		suite := parse(t, `{"results":{"tests":[{"name":"a","status":"failed","duration":1,"flaky":true}]}}`)
		if suite.Cases[0].IsFlaky == nil || !*suite.Cases[0].IsFlaky {
			t.Error("an explicitly reported flaky flag must be preserved")
		}
	})
}

func TestParse_Attachments(t *testing.T) {
	t.Run("a base64 screenshot becomes inline content", func(t *testing.T) {
		suite := parse(t, `{"results":{"tests":[{"name":"a","status":"failed","duration":1,
			"screenshot":"iVBORw0KGgoAAAA"}]}}`)
		att := suite.Cases[0].Attachments
		if len(att) != 1 || att[0].Content == "" || att[0].MimeType != "image/png" {
			t.Fatalf("expected inline png content, got %+v", att)
		}
	})

	t.Run("a screenshot that is really a path is demoted to a reference", func(t *testing.T) {
		suite := parse(t, `{"results":{"tests":[{"name":"a","status":"failed","duration":1,
			"screenshot":"/tmp/a.png"}]}}`)
		att := suite.Cases[0].Attachments
		if len(att) != 1 || att[0].Content != "" || att[0].Path != "/tmp/a.png" {
			t.Fatalf("a producer violating the spec must degrade, not store garbage bytes: %+v", att)
		}
	})

	t.Run("a CTRF attachment carries no bytes and is never read", func(t *testing.T) {
		suite := parse(t, `{"results":{"tests":[{"name":"a","status":"failed","duration":1,
			"attachments":[{"name":"trace.zip","contentType":"application/zip","path":"https://ci/trace.zip"}]}]}}`)
		att := suite.Cases[0].Attachments
		if len(att) != 1 || att[0].Content != "" {
			t.Fatalf("path is opaque and must never be dereferenced: %+v", att)
		}
		if att[0].Path != "https://ci/trace.zip" || att[0].MimeType != "application/zip" {
			t.Errorf("reference metadata must pass through, got %+v", att[0])
		}
	})

	t.Run("an unnamed attachment is dropped rather than failing the upload", func(t *testing.T) {
		suite := parse(t, `{"results":{"tests":[{"name":"a","status":"failed","duration":1,
			"attachments":[{"name":"","contentType":"application/zip","path":"x"}]}]}}`)
		if len(suite.Cases[0].Attachments) != 0 {
			t.Error("name is required server-side, so a nameless attachment must be dropped")
		}
	})
}

// The interop hazards: buildNumber typed both ways, float durations, and bare
// strings where the schema says array.
func TestParse_ToleratesRealWorldTypeDrift(t *testing.T) {
	t.Run("buildNumber as a string", func(t *testing.T) {
		suite := parse(t, `{"results":{"environment":{"buildNumber":"1042"},
			"tests":[{"name":"a","status":"passed","duration":1}]}}`)
		if suite.Properties["ctrfBuildNumber"] != "1042" {
			t.Errorf("buildNumber = %q, want 1042", suite.Properties["ctrfBuildNumber"])
		}
	})

	t.Run("buildNumber as an integer", func(t *testing.T) {
		suite := parse(t, `{"results":{"environment":{"buildNumber":1042},
			"tests":[{"name":"a","status":"passed","duration":1}]}}`)
		if suite.Properties["ctrfBuildNumber"] != "1042" {
			t.Errorf("buildNumber = %q, want 1042", suite.Properties["ctrfBuildNumber"])
		}
	})

	t.Run("a float duration truncates rather than failing", func(t *testing.T) {
		suite := parse(t, `{"results":{"tests":[{"name":"a","status":"passed","duration":12.75}]}}`)
		if want := 12 * time.Millisecond; suite.Cases[0].Duration != want {
			t.Errorf("duration = %v, want %v", suite.Cases[0].Duration, want)
		}
	})

	t.Run("suite and stdout as bare strings", func(t *testing.T) {
		suite := parse(t, `{"results":{"tests":[{"name":"a","status":"passed","duration":1,
			"suite":"solo","stdout":"one line"}]}}`)
		if suite.Cases[0].ClassName != "solo" {
			t.Errorf("className = %q, want solo", suite.Cases[0].ClassName)
		}
		if suite.Cases[0].Properties["system-out"] != "one line" {
			t.Errorf("stdout = %q", suite.Cases[0].Properties["system-out"])
		}
	})

	t.Run("a duration derived from start and stop when absent", func(t *testing.T) {
		suite := parse(t, `{"results":{"tests":[{"name":"a","status":"passed","start":1000,"stop":3500}]}}`)
		if want := 2500 * time.Millisecond; suite.Cases[0].Duration != want {
			t.Errorf("duration = %v, want %v", suite.Cases[0].Duration, want)
		}
	})
}

// The shipped example must actually parse, or `make validate-ctrf` documents a
// format nobody verified.
func TestParse_ShippedExample(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "..", "examples", "generic", "ctrf-example.json")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("the example must exist and be readable: %v", err)
	}
	defer f.Close()

	suite, err := New().Parse(f)
	if err != nil {
		t.Fatalf("the shipped example must parse: %v", err)
	}
	if suite.TotalTests != 5 {
		t.Errorf("total = %d, want 5", suite.TotalTests)
	}
	// passed + failed(1) + error(from "other"/broken) + skipped(skipped+pending)
	if suite.Passed != 1 || suite.Failed != 1 || suite.Errors != 1 || suite.Skipped != 2 {
		t.Errorf("counts = passed %d failed %d errors %d skipped %d",
			suite.Passed, suite.Failed, suite.Errors, suite.Skipped)
	}
	if suite.Flaky != 1 {
		t.Errorf("flaky = %d, want 1", suite.Flaky)
	}
}
