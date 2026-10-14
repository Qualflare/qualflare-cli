package allure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
)

const fixtureDir = "testdata/allure-results"

var _ ports.PathAwareParser = (*Parser)(nil)

func parseFixture(t *testing.T) *domain.Suite {
	t.Helper()
	suite, err := New().ParsePath(fixtureDir)
	if err != nil {
		t.Fatalf("ParsePath: %v", err)
	}
	return suite
}

func caseNamed(t *testing.T, s *domain.Suite, name string) domain.Case {
	t.Helper()
	for _, c := range s.Cases {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no case named %q", name)
	return domain.Case{}
}

func TestParser_Contract(t *testing.T) {
	p := New()
	if p.GetFramework() != domain.FrameworkAllure {
		t.Errorf("GetFramework() = %q, want allure", p.GetFramework())
	}
	if got := p.SupportedFileExtensions(); len(got) != 1 || got[0] != ".json" {
		t.Errorf("SupportedFileExtensions() = %v, want [.json]", got)
	}
}

// Six result files, two of them attempts of one test: five cases. Containers,
// executor.json, categories.json and environment.properties are not tests.
func TestParsePath_OneCasePerTestAndCountsMatch(t *testing.T) {
	s := parseFixture(t)
	if len(s.Cases) != 5 {
		t.Fatalf("got %d cases, want 5", len(s.Cases))
	}
	if s.TotalTests != 5 || s.Passed != 2 || s.Failed != 1 || s.Errors != 1 || s.Skipped != 1 {
		t.Errorf("counts total=%d passed=%d failed=%d errors=%d skipped=%d, want 5/2/1/1/1",
			s.TotalTests, s.Passed, s.Failed, s.Errors, s.Skipped)
	}
	if s.Flaky != 1 || s.Retries != 1 {
		t.Errorf("flaky=%d retries=%d, want 1/1", s.Flaky, s.Retries)
	}
}

// Allure's statuses: broken = the test could not run properly (an error, not an
// assertion failure); unknown has no better home than error either.
func TestParsePath_StatusMapping(t *testing.T) {
	s := parseFixture(t)
	for name, want := range map[string]domain.Status{
		"test_login_ok": domain.StatusPassed,
		"test_logout":   domain.StatusFailed,
		"test_db_setup": domain.StatusError,
		"test_later":    domain.StatusSkipped,
		"test_search":   domain.StatusPassed,
	} {
		if got := caseNamed(t, s, name).Status; got != want {
			t.Errorf("%s: status %q, want %q", name, got, want)
		}
	}
}

// Results sharing a historyId are attempts of one test. The latest is the
// result; earlier ones are retries; failed-then-passed is flaky.
func TestParsePath_RetriesBecomeAttempts(t *testing.T) {
	c := caseNamed(t, parseFixture(t), "test_search")
	if c.RetryCount == nil || *c.RetryCount != 1 {
		t.Errorf("RetryCount = %v, want 1", c.RetryCount)
	}
	if c.IsFlaky == nil || !*c.IsFlaky {
		t.Errorf("IsFlaky = %v, want true", c.IsFlaky)
	}
	if len(c.Attempts) != 2 || c.Attempts[0].Status != domain.StatusFailed || c.Attempts[1].Status != domain.StatusPassed {
		t.Fatalf("attempts = %+v, want [failed, passed]", c.Attempts)
	}
	if c.Attempts[0].Message != "TimeoutError" {
		t.Errorf("first attempt message = %q", c.Attempts[0].Message)
	}
}

func TestParsePath_FailureDetails(t *testing.T) {
	c := caseNamed(t, parseFixture(t), "test_logout")
	if !strings.Contains(c.Error, "expected 200, got 500") || !strings.Contains(c.Error, "test_logout.py") {
		t.Errorf("Error should carry message and trace, got %q", c.Error)
	}
}

func TestParsePath_CaseMetadata(t *testing.T) {
	c := caseNamed(t, parseFixture(t), "test_login_ok")

	if c.ID != "tc-login" {
		t.Errorf("ID = %q, want the testCaseId", c.ID)
	}
	if c.ClassName != "tests.auth > test_login > TestAuth" {
		t.Errorf("ClassName = %q, want the Allure suite path", c.ClassName)
	}
	if c.Duration != 1500*time.Millisecond {
		t.Errorf("Duration = %v", c.Duration)
	}
	if c.StartedAt == nil || !c.StartedAt.Equal(time.UnixMilli(1760000000000)) {
		t.Errorf("StartedAt = %v", c.StartedAt)
	}
	if c.Priority != domain.SeverityCritical {
		t.Errorf("Priority = %q, want critical", c.Priority)
	}
	if len(c.Tags) != 1 || c.Tags[0] != "smoke" {
		t.Errorf("Tags = %v, want [smoke]", c.Tags)
	}
	if len(c.Links) != 1 || c.Links[0].URL != "https://tracker.example/QF-1" || c.Links[0].Type != "issue" {
		t.Errorf("Links = %+v", c.Links)
	}
	found := false
	for _, l := range c.Labels {
		if l.Name == "feature" && l.Value == "Login" {
			found = true
		}
	}
	if !found {
		t.Errorf("labels must be kept verbatim, got %+v", c.Labels)
	}
}

// Allure parameter modes: default and excluded are shown; masked is shown
// without its value; hidden is not shown at all.
func TestParsePath_ParameterModes(t *testing.T) {
	p := caseNamed(t, parseFixture(t), "test_login_ok").Properties
	if p[propParamPrefix+"browser"] != "chrome" || p[propParamPrefix+"run"] != "3" {
		t.Errorf("default/excluded params must be kept: %v", p)
	}
	if v, ok := p[propParamPrefix+"password"]; !ok || v != maskedValue {
		t.Errorf("masked param must be present without its value, got %q (present=%v)", v, ok)
	}
	if strings.Contains(strings.Join(mapValues(p), " "), "hunter2") {
		t.Error("a masked value leaked")
	}
	if _, ok := p[propParamPrefix+"internal"]; ok {
		t.Error("hidden param must not be kept")
	}
}

// Nested steps are flattened depth-first, children pointing at their parent.
func TestParsePath_NestedSteps(t *testing.T) {
	steps := caseNamed(t, parseFixture(t), "test_login_ok").Steps
	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(steps))
	}
	if steps[0].Name != "open login page" || steps[0].ParentIndex != nil {
		t.Errorf("step 0 = %+v", steps[0])
	}
	if steps[1].Name != "wait for form" || steps[1].ParentIndex == nil || *steps[1].ParentIndex != 0 {
		t.Errorf("step 1 must be a child of step 0: %+v", steps[1])
	}
	if steps[2].Name != "submit credentials" || steps[2].ParentIndex != nil {
		t.Errorf("step 2 = %+v", steps[2])
	}
	if len(steps[0].Parameters) != 1 || steps[0].Parameters[0].Value != "/login" {
		t.Errorf("step parameters = %+v", steps[0].Parameters)
	}
}

// Attachments resolve against the results directory; a missing file is dropped
// rather than sent as a dangling reference.
func TestParsePath_Attachments(t *testing.T) {
	a := caseNamed(t, parseFixture(t), "test_login_ok").Attachments
	if len(a) != 1 {
		t.Fatalf("got %d attachments, want 1 (the missing one dropped)", len(a))
	}
	want, _ := filepath.Abs(filepath.Join(fixtureDir, "aaaaaaaa-attachment.png"))
	if a[0].Name != "after login" || a[0].MimeType != "image/png" || a[0].LocalPath != want {
		t.Errorf("attachment = %+v", a[0])
	}
}

// Allure is a FORMAT: the suite records the framework that produced it, and its
// category is that framework's, so the launch is labelled pytest, not allure.
func TestParsePath_SourceFramework(t *testing.T) {
	s := parseFixture(t)
	if s.Properties[domain.PropSourceFramework] != string(domain.FrameworkPython) {
		t.Errorf("source framework = %q, want python", s.Properties[domain.PropSourceFramework])
	}
	if s.Category != domain.FrameworkPython.GetCategory() {
		t.Errorf("category = %q", s.Category)
	}
}

// An unmappable producer falls back to generic rather than guessing — an
// unknown category would 400 the whole launch server-side.
func TestParse_UnknownProducerIsGeneric(t *testing.T) {
	s, err := New().Parse(strings.NewReader(`{"uuid":"u","historyId":"h","name":"t","status":"passed","start":1,"stop":2,
		"labels":[{"name":"framework","value":"jasmine"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Properties[domain.PropSourceFramework]; ok {
		t.Errorf("no source framework for an unmapped producer, got %v", s.Properties)
	}
	if s.Category != domain.CategoryGeneric {
		t.Errorf("category = %q, want generic", s.Category)
	}
}

func TestParse_SingleResultFile(t *testing.T) {
	f, err := os.Open(filepath.Join(fixtureDir, "22222222-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := New().Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Cases) != 1 || s.Cases[0].Status != domain.StatusFailed {
		t.Errorf("got %+v", s.Cases)
	}
}

func TestParsePath_DirectoryWithoutResultsIsAnError(t *testing.T) {
	if _, err := New().ParsePath(t.TempDir()); err == nil {
		t.Error("an empty directory must be an error, not an empty upload")
	}
}

func TestIsResultsDir(t *testing.T) {
	if !IsResultsDir(fixtureDir) {
		t.Error("fixture is an allure-results dir")
	}
	if IsResultsDir(t.TempDir()) {
		t.Error("an empty dir is not")
	}
	if IsResultsDir(filepath.Join(fixtureDir, "22222222-result.json")) {
		t.Error("a file is not a directory")
	}
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// An attachment source is a file name inside the results directory, never a
// path: a crafted "../" must not read files from elsewhere on the machine.
func TestParsePath_AttachmentSourceCannotEscapeTheDirectory(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)
	doc := `{"uuid":"u","historyId":"h","name":"t","status":"passed","start":1,"stop":2,
		"attachments":[{"name":"x","source":"../secret.txt","type":"text/plain"}]}`
	if err := os.WriteFile(filepath.Join(dir, "u-result.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New().ParsePath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(s.Cases[0].Attachments); n != 0 {
		t.Fatalf("a ../ source must be dropped, got %d attachments", n)
	}
}
