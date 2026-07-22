package config

import (
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
