package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
)

// --- stubs ---------------------------------------------------------------------------

type stubParser struct {
	framework domain.Framework
	suite     *domain.Suite
	err       error
	parsed    int
}

func (p *stubParser) Parse(r io.Reader) (*domain.Suite, error) {
	p.parsed++
	if p.err != nil {
		return nil, p.err
	}
	if _, err := io.ReadAll(r); err != nil {
		return nil, err
	}
	// Deep-copy Cases so multiple Parse() calls on one stubParser (multiple files
	// mapped to the same framework) hand back independent backing arrays, matching a
	// real parser's per-file allocation — otherwise callers that mutate one returned
	// suite's Cases in place (e.g. tagShardsByFile) would corrupt every other
	// "file's" suite that aliased the same underlying array.
	s := *p.suite
	s.Cases = append([]domain.Case(nil), p.suite.Cases...)
	return &s, nil
}
func (p *stubParser) GetFramework() domain.Framework    { return p.framework }
func (p *stubParser) SupportedFileExtensions() []string { return []string{".xml"} }

type stubFactory struct {
	parsers      map[domain.Framework]ports.Parser
	detect       map[string]domain.Framework // keyed by file base name
	detectErr    error
	getParserErr error
}

func (f *stubFactory) GetParser(fw domain.Framework) (ports.Parser, error) {
	if f.getParserErr != nil {
		return nil, f.getParserErr
	}
	p, ok := f.parsers[fw]
	if !ok {
		return nil, errors.New("unsupported framework: " + string(fw))
	}
	return p, nil
}
func (f *stubFactory) DetectFramework(filename string) (domain.Framework, error) {
	return f.detectFor(filename)
}
func (f *stubFactory) DetectFrameworkFromContent(filename string, _ []byte) (domain.Framework, error) {
	return f.detectFor(filename)
}
func (f *stubFactory) detectFor(filename string) (domain.Framework, error) {
	if f.detectErr != nil {
		return "", f.detectErr
	}
	if fw, ok := f.detect[filepath.Base(filename)]; ok {
		return fw, nil
	}
	return "", errors.New("cannot detect framework")
}
func (f *stubFactory) GetSupportedFrameworks() []domain.Framework { return nil }
func (f *stubFactory) RegisterParser(ports.Parser)                {}

// cappedConfig is the real config with a smaller per-file size cap, so the BUG-18
// guard can be exercised without writing a 100 MB fixture.
type cappedConfig struct {
	*config.Config
	limit int64
}

func (c *cappedConfig) GetMaxFileSize() int64 { return c.limit }

type stubSender struct {
	sent int
	err  error
	last *domain.Launch
}

func (s *stubSender) SendReport(_ context.Context, report *domain.Launch) error {
	s.sent++
	s.last = report
	return s.err
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func onePassing(fw domain.Framework) *domain.Suite {
	return &domain.Suite{
		Name:       string(fw) + " suite",
		TotalTests: 1,
		Passed:     1,
		Cases:      []domain.Case{{Name: "t", Status: domain.StatusPassed}},
	}
}

// --- tagShardsByFile (mechanism B: --shard) ---------------------------------------------

// TestTagShardsByFile pins the direct unit contract: shard_index is the 0-based file
// index the suite occupies, and it unconditionally overwrites any value another
// mechanism (native worker index, or the shard-property fallback) already set — that
// overwrite is the entire point of an explicit --shard: it must win.
func TestTagShardsByFile(t *testing.T) {
	suites := []domain.Suite{
		{Cases: []domain.Case{
			{Name: "a1"},
			{Name: "a2", ShardIndex: domain.IntPtr(99)}, // pre-set by mechanism A/C
		}},
		{Cases: []domain.Case{
			{Name: "b1", ShardIndex: domain.IntPtr(7)},
		}},
		{Cases: []domain.Case{
			{Name: "c1"},
		}},
	}

	tagShardsByFile(suites)

	want := [][]int{{0, 0}, {1}, {2}}
	for i := range suites {
		for j := range suites[i].Cases {
			got := suites[i].Cases[j].ShardIndex
			if got == nil {
				t.Fatalf("suite %d case %d: ShardIndex = nil, want %d", i, j, want[i][j])
			}
			if *got != want[i][j] {
				t.Errorf("suite %d case %d: ShardIndex = %d, want %d", i, j, *got, want[i][j])
			}
		}
	}
}

// --- ParseTestResults ------------------------------------------------------------------

func TestParseTestResults_NoFiles(t *testing.T) {
	s := NewReportService(&stubFactory{}, &stubSender{}, config.DefaultConfig())
	if _, err := s.ParseTestResults(context.Background(), nil, ""); err == nil {
		t.Fatal("ParseTestResults() with no files = nil, want an error")
	}
}

func TestParseTestResults_ExplicitFrameworkWins(t *testing.T) {
	dir := t.TempDir()
	f := writeFile(t, dir, "r.xml", "<x/>")
	parser := &stubParser{framework: domain.FrameworkJUnit, suite: onePassing(domain.FrameworkJUnit)}
	fac := &stubFactory{parsers: map[domain.Framework]ports.Parser{domain.FrameworkJUnit: parser}}
	s := NewReportService(fac, &stubSender{}, config.DefaultConfig())

	report, err := s.ParseTestResults(context.Background(), []string{f}, domain.FrameworkJUnit)
	if err != nil {
		t.Fatalf("ParseTestResults() = %v", err)
	}
	if report.Framework != string(domain.FrameworkJUnit) {
		t.Errorf("Framework = %q, want %q", report.Framework, domain.FrameworkJUnit)
	}
	if len(report.Suites) != 1 {
		t.Errorf("Suites = %d, want 1", len(report.Suites))
	}
}

// BUG-40: the same file passed twice (directly or via overlapping globs) must be parsed
// once, or every result in it is silently double-counted.
func TestParseTestResults_DedupesFiles(t *testing.T) {
	dir := t.TempDir()
	f := writeFile(t, dir, "r.xml", "<x/>")
	parser := &stubParser{framework: domain.FrameworkJUnit, suite: onePassing(domain.FrameworkJUnit)}
	fac := &stubFactory{parsers: map[domain.Framework]ports.Parser{domain.FrameworkJUnit: parser}}
	s := NewReportService(fac, &stubSender{}, config.DefaultConfig())

	// Same file, three spellings that resolve to one absolute path. Compare relative
	// forms from inside the directory rather than mixing in the absolute path: on
	// macOS t.TempDir() hands back /var/... while the resolved cwd is /private/var/...,
	// and dedupeFiles keys on filepath.Abs, which does not follow symlinks.
	base := filepath.Base(f)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	report, err := s.ParseTestResults(context.Background(), []string{base, "./" + base, base}, domain.FrameworkJUnit)
	if err != nil {
		t.Fatalf("ParseTestResults() = %v", err)
	}
	if parser.parsed != 1 {
		t.Errorf("parser ran %d times, want 1 — duplicates were not deduped", parser.parsed)
	}
	if len(report.Suites) != 1 {
		t.Errorf("Suites = %d, want 1", len(report.Suites))
	}
}

// End-to-end: --shard numbers each file's cases by argument position, 0-based, in the
// same order ParseTestResults processed the (globbed, deduped) files.
func TestParseTestResults_ShardFlag(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeFile(t, dir, "shard0.xml", "<x/>"),
		writeFile(t, dir, "shard1.xml", "<x/>"),
		writeFile(t, dir, "shard2.xml", "<x/>"),
	}
	parser := &stubParser{framework: domain.FrameworkJUnit, suite: onePassing(domain.FrameworkJUnit)}
	fac := &stubFactory{parsers: map[domain.Framework]ports.Parser{domain.FrameworkJUnit: parser}}
	cfg := config.DefaultConfig()
	cfg.SetShard(true)
	s := NewReportService(fac, &stubSender{}, cfg)

	report, err := s.ParseTestResults(context.Background(), files, domain.FrameworkJUnit)
	if err != nil {
		t.Fatalf("ParseTestResults() = %v", err)
	}
	if len(report.Suites) != 3 {
		t.Fatalf("Suites = %d, want 3", len(report.Suites))
	}
	for i, suite := range report.Suites {
		for j, c := range suite.Cases {
			if c.ShardIndex == nil || *c.ShardIndex != i {
				got := "nil"
				if c.ShardIndex != nil {
					got = fmt.Sprint(*c.ShardIndex)
				}
				t.Errorf("suite %d (file %s) case %d: ShardIndex = %s, want %d", i, filepath.Base(files[i]), j, got, i)
			}
		}
	}
}

// --shard must not create a phantom extra shard slot when dedupeFiles collapses the
// same path (given twice, directly or via overlapping globs) into one file: the merged
// file gets exactly one shard index, not two.
func TestParseTestResults_ShardFlag_DedupeInteraction(t *testing.T) {
	dir := t.TempDir()
	f := writeFile(t, dir, "r.xml", "<x/>")
	parser := &stubParser{framework: domain.FrameworkJUnit, suite: onePassing(domain.FrameworkJUnit)}
	fac := &stubFactory{parsers: map[domain.Framework]ports.Parser{domain.FrameworkJUnit: parser}}
	cfg := config.DefaultConfig()
	cfg.SetShard(true)
	s := NewReportService(fac, &stubSender{}, cfg)

	// The same file listed twice must dedupe to one file, and with only one file
	// left after dedupe, --shard becomes a no-op (fewer than 2 suites): the case's
	// ShardIndex is left exactly as the parser produced it (nil here — onePassing
	// sets none), not stamped to 0.
	report, err := s.ParseTestResults(context.Background(), []string{f, f}, domain.FrameworkJUnit)
	if err != nil {
		t.Fatalf("ParseTestResults() = %v", err)
	}
	if len(report.Suites) != 1 {
		t.Fatalf("Suites = %d, want 1 (deduped)", len(report.Suites))
	}
	for _, c := range report.Suites[0].Cases {
		if c.ShardIndex != nil {
			t.Errorf("ShardIndex = %v, want nil — single file (post-dedupe) + --shard must be a no-op", *c.ShardIndex)
		}
	}
}

// --shard with exactly one input file must be a true no-op: it must not overwrite
// a real per-worker ShardIndex a file's own parser already set (native WorkerIndex,
// or the shard-property fallback), matching the flag's documented "requires 2+
// files. Does not apply to a single file." behavior.
func TestParseTestResults_ShardFlag_SingleFileIsNoOp(t *testing.T) {
	dir := t.TempDir()
	f := writeFile(t, dir, "r.xml", "<x/>")
	suite := onePassing(domain.FrameworkJUnit)
	suite.Cases[0].ShardIndex = domain.IntPtr(3) // pre-set by mechanism A/C
	parser := &stubParser{framework: domain.FrameworkJUnit, suite: suite}
	fac := &stubFactory{parsers: map[domain.Framework]ports.Parser{domain.FrameworkJUnit: parser}}
	cfg := config.DefaultConfig()
	cfg.SetShard(true)
	s := NewReportService(fac, &stubSender{}, cfg)

	report, err := s.ParseTestResults(context.Background(), []string{f}, domain.FrameworkJUnit)
	if err != nil {
		t.Fatalf("ParseTestResults() = %v", err)
	}
	if len(report.Suites) != 1 {
		t.Fatalf("Suites = %d, want 1", len(report.Suites))
	}
	for _, c := range report.Suites[0].Cases {
		if c.ShardIndex == nil || *c.ShardIndex != 3 {
			got := "nil"
			if c.ShardIndex != nil {
				got = fmt.Sprint(*c.ShardIndex)
			}
			t.Errorf("ShardIndex = %s, want 3 — --shard with a single file must not overwrite an existing ShardIndex", got)
		}
	}
}

// BUG-41: when auto-detection disagrees across files, the launch must be labelled
// "mixed" rather than tagged with whichever file happened to be parsed first.
func TestParseTestResults_LaunchFrameworkLabel(t *testing.T) {
	junit := &stubParser{framework: domain.FrameworkJUnit, suite: onePassing(domain.FrameworkJUnit)}
	golang := &stubParser{framework: domain.FrameworkGolang, suite: onePassing(domain.FrameworkGolang)}

	tests := []struct {
		name  string
		files map[string]string // filename -> detected framework
		want  domain.Framework
	}{
		{"all agree", map[string]string{"a.xml": "junit", "b.xml": "junit"}, domain.FrameworkJUnit},
		{"disagree becomes mixed", map[string]string{"a.xml": "junit", "b.json": "golang"}, "mixed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			detect := map[string]domain.Framework{}
			var files []string
			for name, fw := range tt.files {
				files = append(files, writeFile(t, dir, name, "{}"))
				detect[name] = domain.Framework(fw)
			}
			fac := &stubFactory{
				parsers: map[domain.Framework]ports.Parser{
					domain.FrameworkJUnit:  junit,
					domain.FrameworkGolang: golang,
				},
				detect: detect,
			}
			s := NewReportService(fac, &stubSender{}, config.DefaultConfig())

			// Empty framework => auto-detect per file.
			report, err := s.ParseTestResults(context.Background(), files, "")
			if err != nil {
				t.Fatalf("ParseTestResults() = %v", err)
			}
			if report.Framework != string(tt.want) {
				t.Errorf("Framework = %q, want %q", report.Framework, tt.want)
			}
		})
	}
}

func TestParseTestResults_Errors(t *testing.T) {
	dir := t.TempDir()
	good := writeFile(t, dir, "r.xml", "<x/>")

	t.Run("unknown explicit framework", func(t *testing.T) {
		s := NewReportService(&stubFactory{}, &stubSender{}, config.DefaultConfig())
		_, err := s.ParseTestResults(context.Background(), []string{good}, "nope")
		if err == nil || !strings.Contains(err.Error(), "failed to get parser for framework") {
			t.Fatalf("err = %v, want a get-parser failure", err)
		}
	})

	t.Run("detection failure", func(t *testing.T) {
		fac := &stubFactory{detectErr: errors.New("no idea")}
		s := NewReportService(fac, &stubSender{}, config.DefaultConfig())
		_, err := s.ParseTestResults(context.Background(), []string{good}, "")
		if err == nil || !strings.Contains(err.Error(), "failed to detect framework") {
			t.Fatalf("err = %v, want a detection failure", err)
		}
	})

	t.Run("parse failure names the file", func(t *testing.T) {
		parser := &stubParser{framework: domain.FrameworkJUnit, err: errors.New("bad xml")}
		fac := &stubFactory{parsers: map[domain.Framework]ports.Parser{domain.FrameworkJUnit: parser}}
		s := NewReportService(fac, &stubSender{}, config.DefaultConfig())
		_, err := s.ParseTestResults(context.Background(), []string{good}, domain.FrameworkJUnit)
		if err == nil || !strings.Contains(err.Error(), filepath.Base(good)) {
			t.Fatalf("err = %v, want it to name the offending file", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		parser := &stubParser{framework: domain.FrameworkJUnit, suite: onePassing(domain.FrameworkJUnit)}
		fac := &stubFactory{parsers: map[domain.Framework]ports.Parser{domain.FrameworkJUnit: parser}}
		s := NewReportService(fac, &stubSender{}, config.DefaultConfig())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := s.ParseTestResults(ctx, []string{good}, domain.FrameworkJUnit); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})
}

// BUG-18: --format skips detection entirely, so parseFile must enforce the size cap
// itself or an unbounded file is read straight into memory.
func TestParseTestResults_RejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	big := writeFile(t, dir, "big.xml", strings.Repeat("x", 4096))

	// config.MaxFileSize is a package constant (100 MB), so shrink the cap by
	// overriding just that method on top of the real config.
	cfg := &cappedConfig{Config: config.DefaultConfig(), limit: 10}

	parser := &stubParser{framework: domain.FrameworkJUnit, suite: onePassing(domain.FrameworkJUnit)}
	fac := &stubFactory{parsers: map[domain.Framework]ports.Parser{domain.FrameworkJUnit: parser}}
	s := NewReportService(fac, &stubSender{}, cfg)

	_, err := s.ParseTestResults(context.Background(), []string{big}, domain.FrameworkJUnit)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v, want a size-cap rejection", err)
	}
	if parser.parsed != 0 {
		t.Error("the parser ran despite the file exceeding the size cap")
	}
}

// --- ProcessTestResults ----------------------------------------------------------------

func TestProcessTestResults(t *testing.T) {
	dir := t.TempDir()
	f := writeFile(t, dir, "r.xml", "<x/>")
	newSvc := func(cfg *config.Config) (*ReportService, *stubSender) {
		parser := &stubParser{framework: domain.FrameworkJUnit, suite: onePassing(domain.FrameworkJUnit)}
		fac := &stubFactory{parsers: map[domain.Framework]ports.Parser{domain.FrameworkJUnit: parser}}
		sender := &stubSender{}
		return NewReportService(fac, sender, cfg), sender
	}

	t.Run("sends the parsed report", func(t *testing.T) {
		s, sender := newSvc(config.DefaultConfig())
		if err := s.ProcessTestResults(context.Background(), []string{f}, domain.FrameworkJUnit); err != nil {
			t.Fatalf("ProcessTestResults() = %v", err)
		}
		if sender.sent != 1 || sender.last == nil {
			t.Fatalf("sender called %d times, want 1 with a report", sender.sent)
		}
	})

	// --dry-run must parse and validate but never reach the network.
	t.Run("dry run does not send", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.DryRun = true
		s, sender := newSvc(cfg)
		if err := s.ProcessTestResults(context.Background(), []string{f}, domain.FrameworkJUnit); err != nil {
			t.Fatalf("ProcessTestResults() = %v", err)
		}
		if sender.sent != 0 {
			t.Errorf("sender called %d times under --dry-run, want 0", sender.sent)
		}
	})

	t.Run("parse failure short-circuits the send", func(t *testing.T) {
		s, sender := newSvc(config.DefaultConfig())
		if err := s.ProcessTestResults(context.Background(), nil, domain.FrameworkJUnit); err == nil {
			t.Fatal("ProcessTestResults() = nil, want an error")
		}
		if sender.sent != 0 {
			t.Error("the report was sent despite a parse failure")
		}
	})

	t.Run("propagates a send failure", func(t *testing.T) {
		s, sender := newSvc(config.DefaultConfig())
		sender.err = errors.New("upload failed")
		if err := s.ProcessTestResults(context.Background(), []string{f}, domain.FrameworkJUnit); err == nil {
			t.Fatal("ProcessTestResults() = nil, want the send error")
		}
	})
}

// --- ValidateFiles ---------------------------------------------------------------------

func TestValidateFiles(t *testing.T) {
	dir := t.TempDir()
	good := writeFile(t, dir, "good.xml", "<x/>")
	bad := writeFile(t, dir, "bad.xml", "<x/>")
	missing := filepath.Join(dir, "nope.xml")

	okParser := &stubParser{framework: domain.FrameworkJUnit, suite: onePassing(domain.FrameworkJUnit)}
	fac := &stubFactory{
		parsers: map[domain.Framework]ports.Parser{domain.FrameworkJUnit: okParser},
		detect:  map[string]domain.Framework{"good.xml": domain.FrameworkJUnit},
	}
	s := NewReportService(fac, &stubSender{}, config.DefaultConfig())

	results, err := s.ValidateFiles(context.Background(), []string{good, missing, bad}, "")
	if err != nil {
		t.Fatalf("ValidateFiles() = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}

	// One result per input, in order — a missing file is reported, not fatal.
	if !results[0].Valid || results[0].TestCount != 1 || results[0].Framework != domain.FrameworkJUnit {
		t.Errorf("good file: %+v", results[0])
	}
	if results[1].Valid || !strings.Contains(results[1].Error, "does not exist") {
		t.Errorf("missing file: %+v", results[1])
	}
	// bad.xml exists but has no detection entry.
	if results[2].Valid || results[2].Error == "" {
		t.Errorf("undetectable file: %+v", results[2])
	}
}

func TestValidateFiles_ExplicitFrameworkSkipsDetection(t *testing.T) {
	dir := t.TempDir()
	f := writeFile(t, dir, "r.xml", "<x/>")
	parser := &stubParser{framework: domain.FrameworkJUnit, suite: onePassing(domain.FrameworkJUnit)}
	// No detect map at all: detection would fail if it were consulted.
	fac := &stubFactory{parsers: map[domain.Framework]ports.Parser{domain.FrameworkJUnit: parser}}
	s := NewReportService(fac, &stubSender{}, config.DefaultConfig())

	results, err := s.ValidateFiles(context.Background(), []string{f}, domain.FrameworkJUnit)
	if err != nil {
		t.Fatalf("ValidateFiles() = %v", err)
	}
	if !results[0].Valid {
		t.Errorf("explicit framework should bypass detection, got %+v", results[0])
	}
}

func TestValidateFiles_CancelledContext(t *testing.T) {
	s := NewReportService(&stubFactory{}, &stubSender{}, config.DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ValidateFiles(ctx, []string{"anything"}, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
