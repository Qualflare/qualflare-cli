package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestDefaultTimeoutHasServerHeadroom (BUG-27): the client collect timeout must exceed
// the server's ~30s /collect DB budget, or a legitimately slow upload trips the client
// deadline at the exact moment the server is finishing. 120s leaves room for retries.
func TestDefaultTimeoutHasServerHeadroom(t *testing.T) {
	if got := DefaultConfig().Timeout; got < 120*time.Second {
		t.Fatalf("default Timeout = %v, want >= 120s (headroom over the server's 30s budget)", got)
	}
}

// TestSetEnvironment_SkipsEmpty (CLI-H1) locks in the mechanism the fix relies
// on: the collect command passes an empty --environment when the flag is unset,
// and SetEnvironment("") must NOT clobber the value LoadFromEnv already read from
// QF_ENVIRONMENT. Before the fix the flag defaulted to a non-empty "staging",
// so every unset run overwrote the env var and landed in staging.
func TestSetEnvironment_SkipsEmpty(t *testing.T) {
	c := DefaultConfig()
	c.SetEnvironment("production")
	c.SetEnvironment("") // simulates an unset --environment flag
	if c.Environment != "production" {
		t.Fatalf("Environment = %q, want %q (empty must not override)", c.Environment, "production")
	}
	c.SetLanguage("de-DE")
	c.SetLanguage("")
	if c.Language != "de-DE" {
		t.Fatalf("Language = %q, want %q", c.Language, "de-DE")
	}
}

// TestPlatformAndMilestone (SYNC-05/SYNC-12) covers the new contract inputs:
// QF_PLATFORM/QF_MILESTONE are read, an unset flag doesn't clobber them, and the
// built-in platform default is the backward-compatible "api".
func TestPlatformAndMilestone(t *testing.T) {
	if DefaultConfig().Platform != "api" {
		t.Fatalf("default platform = %q, want %q", DefaultConfig().Platform, "api")
	}

	t.Setenv("QF_PLATFORM", "web")
	t.Setenv("QF_MILESTONE", "42")
	c := DefaultConfig()
	c.LoadFromEnv()
	if c.GetPlatform() != "web" {
		t.Fatalf("platform = %q, want web", c.GetPlatform())
	}
	if c.GetMilestone() != 42 {
		t.Fatalf("milestone = %d, want 42", c.GetMilestone())
	}

	// An unset flag (empty / 0) must not clobber the env-derived values.
	c.SetPlatform("")
	c.SetMilestone(0)
	if c.GetPlatform() != "web" || c.GetMilestone() != 42 {
		t.Fatalf("empty flag clobbered env: platform=%q milestone=%d", c.GetPlatform(), c.GetMilestone())
	}

	// An explicit flag overrides.
	c.SetPlatform("ios")
	c.SetMilestone(7)
	if c.GetPlatform() != "ios" || c.GetMilestone() != 7 {
		t.Fatalf("explicit flag not applied: platform=%q milestone=%d", c.GetPlatform(), c.GetMilestone())
	}
}

// TestLoadFromEnv_QFEnvironmentWins confirms QF_ENVIRONMENT/QF_LANGUAGE are read
// and (with the empty flag default) survive to the emitted payload.
func TestLoadFromEnv_QFEnvironmentWins(t *testing.T) {
	t.Setenv("QF_ENVIRONMENT", "prod-ci")
	t.Setenv("QF_LANGUAGE", "fr-FR")

	c := DefaultConfig()
	c.LoadFromEnv()

	if c.Environment != "prod-ci" {
		t.Fatalf("Environment = %q, want %q", c.Environment, "prod-ci")
	}
	if c.Language != "fr-FR" {
		t.Fatalf("Language = %q, want %q", c.Language, "fr-FR")
	}

	// An unset flag (empty) then leaves the env value intact.
	c.SetEnvironment("")
	c.SetLanguage("")
	if c.Environment != "prod-ci" || c.Language != "fr-FR" {
		t.Fatalf("env values clobbered by empty flag: env=%q lang=%q", c.Environment, c.Language)
	}
}

// TestValidate_RequiresTokenUnlessDryRun pins the one gate that decides whether the CLI
// can talk to the API at all: a missing token is fatal, except under --dry-run, which
// must stay usable with no credentials so `qf collect --dry-run` works in a fresh clone.
func TestValidate_RequiresTokenUnlessDryRun(t *testing.T) {
	t.Run("missing token is fatal", func(t *testing.T) {
		c := DefaultConfig()
		c.APIKey = ""
		err := c.Validate()
		if err == nil {
			t.Fatal("Validate() = nil, want an error for a missing token")
		}
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("Validate() returned %T, want *ValidationError", err)
		}
		if ve.Field != "api_key" {
			t.Errorf("ValidationError.Field = %q, want %q", ve.Field, "api_key")
		}
		if !strings.Contains(ve.Error(), "qf login") {
			t.Errorf("error message should point at `qf login`, got %q", ve.Error())
		}
	})

	t.Run("dry run needs no token", func(t *testing.T) {
		c := DefaultConfig()
		c.APIKey = ""
		c.DryRun = true
		if err := c.Validate(); err != nil {
			t.Fatalf("Validate() under --dry-run = %v, want nil", err)
		}
	})

	t.Run("token present is fine", func(t *testing.T) {
		c := DefaultConfig()
		c.APIKey = "qf_token"
		if err := c.Validate(); err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
	})
}

// TestValidate_RepairsOutOfRangeValues: Validate doubles as a normaliser, so a config
// assembled from flags or env can't leave the retry/timeout knobs in a state that would
// hang the CLI (zero timeout) or hammer the API (unbounded retries).
func TestValidate_RepairsOutOfRangeValues(t *testing.T) {
	c := DefaultConfig()
	c.APIKey = "qf_token"
	c.RetryMax = maxRetryCount + 5
	c.Timeout = 0
	c.RetryBaseDelay = 0
	c.RetryMaxDelay = -1

	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	if c.RetryMax != maxRetryCount {
		t.Errorf("RetryMax = %d, want it clamped to %d", c.RetryMax, maxRetryCount)
	}
	if c.Timeout != 120*time.Second {
		t.Errorf("Timeout = %v, want 120s", c.Timeout)
	}
	if c.RetryBaseDelay != 1*time.Second {
		t.Errorf("RetryBaseDelay = %v, want 1s", c.RetryBaseDelay)
	}
	if c.RetryMaxDelay != 30*time.Second {
		t.Errorf("RetryMaxDelay = %v, want 30s", c.RetryMaxDelay)
	}
}

// TestSetters_IgnoreEmptyValues: every setter is fed from a cobra flag that defaults to
// empty/zero, so an unset flag must never clobber a value that env detection already
// supplied. SetVerbose/SetQuiet/SetDryRun are booleans and deliberately assign always.
func TestSetters_IgnoreEmptyValues(t *testing.T) {
	c := DefaultConfig()
	c.SetAPIKey("token-1")
	c.SetBranch("main")
	c.SetCommit("abc123")
	c.SetTimeout(45 * time.Second)

	c.SetAPIKey("")
	c.SetBranch("")
	c.SetCommit("")
	c.SetTimeout(0)
	c.SetTimeout(-1)

	if c.GetAPIKey() != "token-1" {
		t.Errorf("GetAPIKey() = %q, want %q", c.GetAPIKey(), "token-1")
	}
	if c.GetBranch() != "main" {
		t.Errorf("GetBranch() = %q, want %q", c.GetBranch(), "main")
	}
	if c.GetCommit() != "abc123" {
		t.Errorf("GetCommit() = %q, want %q", c.GetCommit(), "abc123")
	}
	if c.GetTimeout() != 45*time.Second {
		t.Errorf("GetTimeout() = %v, want 45s", c.GetTimeout())
	}
}

func TestBooleanSettersAndAccessors(t *testing.T) {
	c := DefaultConfig()

	c.SetVerbose(true)
	c.SetQuiet(true)
	c.SetDryRun(true)
	if !c.IsVerbose() || !c.IsQuiet() || !c.IsDryRun() {
		t.Errorf("verbose/quiet/dryRun = %v/%v/%v, want all true", c.IsVerbose(), c.IsQuiet(), c.IsDryRun())
	}

	// Unlike the string setters, booleans must assign unconditionally — otherwise a
	// flag could be turned on but never off.
	c.SetVerbose(false)
	c.SetQuiet(false)
	c.SetDryRun(false)
	if c.IsVerbose() || c.IsQuiet() || c.IsDryRun() {
		t.Errorf("verbose/quiet/dryRun = %v/%v/%v, want all false", c.IsVerbose(), c.IsQuiet(), c.IsDryRun())
	}
}

func TestAccessorsReflectConfig(t *testing.T) {
	c := DefaultConfig()
	c.APIKey = "k"
	c.Environment = "staging"
	c.Language = "en-GB"
	c.Debug = true
	c.NoCaptureOutput = true

	if c.GetAPIEndpoint() != apiEndpoint {
		t.Errorf("GetAPIEndpoint() = %q, want %q", c.GetAPIEndpoint(), apiEndpoint)
	}
	if c.GetEnvironment() != "staging" || c.GetLanguage() != "en-GB" {
		t.Errorf("environment/language = %q/%q", c.GetEnvironment(), c.GetLanguage())
	}
	if !c.IsDebug() || !c.IsNoCaptureOutput() {
		t.Errorf("debug/noCapture = %v/%v, want both true", c.IsDebug(), c.IsNoCaptureOutput())
	}
	if c.GetMaxFileSize() <= 0 {
		t.Errorf("GetMaxFileSize() = %d, want a positive limit", c.GetMaxFileSize())
	}
	if c.GetCLIVersion() == "" {
		t.Error("GetCLIVersion() is empty")
	}
	retryMax, base, maxDelay := c.GetRetryConfig()
	if retryMax != c.RetryMax || base != c.RetryBaseDelay || maxDelay != c.RetryMaxDelay {
		t.Errorf("GetRetryConfig() = %v/%v/%v, want %v/%v/%v",
			retryMax, base, maxDelay, c.RetryMax, c.RetryBaseDelay, c.RetryMaxDelay)
	}
}

// TestDetectGit_SkipsWhenAlreadySet: git detection shells out, so the collect path must
// not pay for it when CI env vars already supplied both values (BUG-39).
func TestDetectGit_SkipsWhenAlreadySet(t *testing.T) {
	c := DefaultConfig()
	c.Branch = "from-ci"
	c.Commit = "deadbeef"
	c.DetectGit()
	if c.Branch != "from-ci" || c.Commit != "deadbeef" {
		t.Errorf("DetectGit() overwrote CI-supplied values: branch=%q commit=%q", c.Branch, c.Commit)
	}
}

func TestNewConfig_AppliesEnv(t *testing.T) {
	t.Setenv("QF_ENVIRONMENT", "from-env")
	c := NewConfig()
	if c == nil {
		t.Fatal("NewConfig() = nil")
	}
	if c.Environment != "from-env" {
		t.Errorf("Environment = %q, want %q — NewConfig must apply LoadFromEnv", c.Environment, "from-env")
	}
	if c.Timeout <= 0 {
		t.Errorf("Timeout = %v, want the default to be applied", c.Timeout)
	}
}
