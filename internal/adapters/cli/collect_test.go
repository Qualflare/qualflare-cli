package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
)

// --- the units extracted from runCollect ----------------------------------------------

func TestValidatePlatform(t *testing.T) {
	for _, p := range []string{"android", "ios", "desktop", "web", "api"} {
		if err := validatePlatform(p); err != nil {
			t.Errorf("validatePlatform(%q) = %v, want nil", p, err)
		}
	}
	// Empty means "not set" — the server applies its own default.
	if err := validatePlatform(""); err != nil {
		t.Errorf("validatePlatform(\"\") = %v, want nil", err)
	}
	// The check exists so a typo fails here rather than as a 400 on the whole upload,
	// so the message must name the accepted values.
	for _, p := range []string{"Android", "linux", "macos", " web"} {
		err := validatePlatform(p)
		if err == nil {
			t.Errorf("validatePlatform(%q) = nil, want an error", p)
			continue
		}
		if !strings.Contains(err.Error(), "android, ios, desktop, web, api") {
			t.Errorf("validatePlatform(%q) message does not list the valid values: %v", p, err)
		}
	}
}

// applyCollectOptions must not clobber values that env/CI detection already supplied
// when the corresponding flag is unset — the setters are the guard, this pins it.
func TestApplyCollectOptions(t *testing.T) {
	t.Run("applies set values", func(t *testing.T) {
		cfg := config.DefaultConfig()
		_ = applyCollectOptions(cfg, collectOptions{
			environment: "staging",
			language:    "en-GB",
			platform:    "web",
			branch:      "feature/x",
			commit:      "cafe123",
			timeout:     42 * time.Second,
			dryRun:      true,
			shard:       true,
		})
		if cfg.GetEnvironment() != "staging" || cfg.GetLanguage() != "en-GB" {
			t.Errorf("environment/language = %q/%q", cfg.GetEnvironment(), cfg.GetLanguage())
		}
		if cfg.GetBranch() != "feature/x" || cfg.GetCommit() != "cafe123" {
			t.Errorf("branch/commit = %q/%q", cfg.GetBranch(), cfg.GetCommit())
		}
		if cfg.GetTimeout() != 42*time.Second || !cfg.IsDryRun() {
			t.Errorf("timeout/dryRun = %v/%v", cfg.GetTimeout(), cfg.IsDryRun())
		}
		if !cfg.IsShard() {
			t.Error("shard = false, want true")
		}
	})

	t.Run("unset flags leave detected values intact", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Environment = "from-ci"
		cfg.Branch = "main"
		cfg.Commit = "abc"
		before := cfg.GetTimeout()

		_ = applyCollectOptions(cfg, collectOptions{}) // every field zero

		if cfg.GetEnvironment() != "from-ci" || cfg.GetBranch() != "main" || cfg.GetCommit() != "abc" {
			t.Errorf("detected values clobbered: env=%q branch=%q commit=%q",
				cfg.GetEnvironment(), cfg.GetBranch(), cfg.GetCommit())
		}
		if cfg.GetTimeout() != before {
			t.Errorf("timeout = %v, want the default %v preserved", cfg.GetTimeout(), before)
		}
		if cfg.IsShard() {
			t.Error("shard = true, want false (unset flag)")
		}
	})
}

func TestVerifyFilesExist(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.xml")
	if err := os.WriteFile(a, []byte("<x/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "gone.xml")

	if err := verifyFilesExist([]string{a}); err != nil {
		t.Errorf("verifyFilesExist(existing) = %v, want nil", err)
	}
	if err := verifyFilesExist(nil); err != nil {
		t.Errorf("verifyFilesExist(nil) = %v, want nil", err)
	}

	// Fails on the first missing path, and names it — uploading a partial set silently
	// would be worse than refusing.
	err := verifyFilesExist([]string{a, missing})
	if err == nil {
		t.Fatal("verifyFilesExist(missing) = nil, want an error")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not name the missing file", err)
	}
}

func TestResolveFramework(t *testing.T) {
	// Empty means auto-detect per file.
	if fw, err := resolveFramework(""); err != nil || fw != "" {
		t.Errorf("resolveFramework(\"\") = %q, %v; want \"\", nil", fw, err)
	}
	// Case is normalised, so --format JUnit works.
	for _, in := range []string{"junit", "JUnit", "JUNIT"} {
		fw, err := resolveFramework(in)
		if err != nil || fw != domain.FrameworkJUnit {
			t.Errorf("resolveFramework(%q) = %q, %v; want junit", in, fw, err)
		}
	}
	err := resolveFramework0Err(t, "nosuchframework")
	if !strings.Contains(err.Error(), "qf list-formats") {
		t.Errorf("error should point at `qf list-formats`, got %q", err)
	}
}

func resolveFramework0Err(t *testing.T, format string) error {
	t.Helper()
	_, err := resolveFramework(format)
	if err == nil {
		t.Fatalf("resolveFramework(%q) = nil error, want a failure", format)
	}
	return err
}

// API-01: --output must be rejected rather than silently ignored.
func TestValidateOutputOption(t *testing.T) {
	tests := []struct {
		name    string
		opts    collectOptions
		wantErr string
	}{
		{"unset is fine", collectOptions{}, ""},
		{"json with dry run", collectOptions{output: "json", dryRun: true}, ""},
		{"unsupported format", collectOptions{output: "yaml", dryRun: true}, "only 'json' is supported"},
		{"json without dry run", collectOptions{output: "json"}, "only applies to --dry-run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOutputOption(tt.opts)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("validateOutputOption() = %v, want nil", err)
			case tt.wantErr != "" && err == nil:
				t.Errorf("validateOutputOption() = nil, want %q", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Errorf("validateOutputOption() = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

// --- runCollect orchestration -----------------------------------------------------------

type stubReportService struct {
	processed  int
	parsed     int
	lastFiles  []string
	lastFW     domain.Framework
	processErr error
	parseErr   error
	launch     *domain.Launch
}

func (s *stubReportService) ProcessTestResults(_ context.Context, files []string, fw domain.Framework) error {
	s.processed++
	s.lastFiles, s.lastFW = files, fw
	return s.processErr
}

func (s *stubReportService) ParseTestResults(_ context.Context, files []string, fw domain.Framework) (*domain.Launch, error) {
	s.parsed++
	s.lastFiles, s.lastFW = files, fw
	if s.parseErr != nil {
		return nil, s.parseErr
	}
	if s.launch != nil {
		return s.launch, nil
	}
	return &domain.Launch{Framework: string(fw)}, nil
}

func (s *stubReportService) ValidateFiles(context.Context, []string, domain.Framework) ([]ports.ValidationResult, error) {
	return nil, nil
}

// newCollectCLI wires a CLI with a token already present, so runCollect gets past
// config.Validate and the test can exercise everything after it.
func newCollectCLI(t *testing.T) (*CLI, *stubReportService, string) {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "results.xml")
	if err := os.WriteFile(f, []byte("<testsuite/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.APIKey = "qf_token"
	svc := &stubReportService{}
	return NewCLI(svc, cfg, nil, nil, nil), svc, f
}

func baseOpts() collectOptions {
	return collectOptions{timeout: 10 * time.Second}
}

func TestRunCollect_Success(t *testing.T) {
	c, svc, f := newCollectCLI(t)
	c.config.Quiet = true

	if err := c.runCollect(context.Background(), []string{f}, baseOpts()); err != nil {
		t.Fatalf("runCollect() = %v", err)
	}
	if svc.processed != 1 {
		t.Errorf("ProcessTestResults called %d times, want 1", svc.processed)
	}
	if len(svc.lastFiles) != 1 || svc.lastFiles[0] != f {
		t.Errorf("files = %v, want [%s]", svc.lastFiles, f)
	}
}

// The order of the guards is behaviour, not incidental: --platform is rejected before
// the config is touched, and a missing file is reported before --format/--output are
// looked at, so a bad path still reads as a bad path.
func TestRunCollect_ValidationOrder(t *testing.T) {
	tests := []struct {
		name    string
		files   func(existing string) []string
		mutate  func(*collectOptions)
		wantErr string
	}{
		{
			"bad platform beats everything",
			func(f string) []string { return []string{"nope.xml"} },
			func(o *collectOptions) { o.platform = "linux"; o.format = "bogus"; o.output = "yaml" },
			"invalid --platform",
		},
		{
			"missing file beats bad format",
			func(f string) []string { return []string{"definitely-missing.xml"} },
			func(o *collectOptions) { o.format = "bogus" },
			"file does not exist",
		},
		{
			"bad format beats bad output",
			func(f string) []string { return []string{f} },
			func(o *collectOptions) { o.format = "bogus"; o.output = "yaml" },
			"unsupported format",
		},
		{
			"bad output",
			func(f string) []string { return []string{f} },
			func(o *collectOptions) { o.output = "yaml"; o.dryRun = true },
			"only 'json' is supported",
		},
		{
			"glob matching nothing",
			func(f string) []string { return []string{filepath.Join(t.TempDir(), "*.nope")} },
			func(o *collectOptions) {},
			"no files match pattern",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, svc, f := newCollectCLI(t)
			c.config.Quiet = true
			opts := baseOpts()
			tt.mutate(&opts)

			err := c.runCollect(context.Background(), tt.files(f), opts)
			if err == nil {
				t.Fatalf("runCollect() = nil, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("runCollect() = %q, want it to mention %q", err, tt.wantErr)
			}
			if svc.processed != 0 {
				t.Error("the report was processed despite a validation failure")
			}
		})
	}
}

// A missing token must stop collect before any file work happens.
func TestRunCollect_RequiresToken(t *testing.T) {
	c, svc, f := newCollectCLI(t)
	c.config.APIKey = ""
	c.config.Quiet = true

	if err := c.runCollect(context.Background(), []string{f}, baseOpts()); err == nil {
		t.Fatal("runCollect() = nil, want a missing-token error")
	}
	if svc.processed != 0 {
		t.Error("the report was processed without a token")
	}
}

// --dry-run --output json parses and prints instead of uploading.
func TestRunCollect_DryRunJSONDoesNotUpload(t *testing.T) {
	c, svc, f := newCollectCLI(t)
	c.config.Quiet = true
	opts := baseOpts()
	opts.dryRun = true
	opts.output = "json"

	if err := c.runCollect(context.Background(), []string{f}, opts); err != nil {
		t.Fatalf("runCollect() = %v", err)
	}
	if svc.parsed != 1 {
		t.Errorf("ParseTestResults called %d times, want 1", svc.parsed)
	}
	if svc.processed != 0 {
		t.Error("ProcessTestResults was called during a --dry-run --output json run")
	}
}

func TestRunCollect_PropagatesServiceErrors(t *testing.T) {
	t.Run("process failure", func(t *testing.T) {
		c, svc, f := newCollectCLI(t)
		c.config.Quiet = true
		svc.processErr = errors.New("upload exploded")
		err := c.runCollect(context.Background(), []string{f}, baseOpts())
		if err == nil || !strings.Contains(err.Error(), "failed to process test results") {
			t.Fatalf("runCollect() = %v, want the process failure wrapped", err)
		}
	})

	t.Run("parse failure under dry run", func(t *testing.T) {
		c, svc, f := newCollectCLI(t)
		c.config.Quiet = true
		svc.parseErr = errors.New("bad xml")
		opts := baseOpts()
		opts.dryRun = true
		opts.output = "json"
		err := c.runCollect(context.Background(), []string{f}, opts)
		if err == nil || !strings.Contains(err.Error(), "failed to parse test results") {
			t.Fatalf("runCollect() = %v, want the parse failure wrapped", err)
		}
	})
}

// --format is normalised and handed to the service; without it the service receives the
// zero value and auto-detects per file.
func TestRunCollect_PassesFrameworkThrough(t *testing.T) {
	c, svc, f := newCollectCLI(t)
	c.config.Quiet = true
	opts := baseOpts()
	opts.format = "JUnit"

	if err := c.runCollect(context.Background(), []string{f}, opts); err != nil {
		t.Fatalf("runCollect() = %v", err)
	}
	if svc.lastFW != domain.FrameworkJUnit {
		t.Errorf("framework = %q, want %q", svc.lastFW, domain.FrameworkJUnit)
	}

	c2, svc2, f2 := newCollectCLI(t)
	c2.config.Quiet = true
	if err := c2.runCollect(context.Background(), []string{f2}, baseOpts()); err != nil {
		t.Fatalf("runCollect() = %v", err)
	}
	if svc2.lastFW != "" {
		t.Errorf("framework = %q, want empty so the service auto-detects", svc2.lastFW)
	}
}
