package config

import "testing"

// The precedence these tests pin, highest first:
//
//	--environment flag  >  QF_ENVIRONMENT  >  the report file  >  "development"
//
// The file sits BELOW the env var deliberately. Someone exporting
// QF_ENVIRONMENT in CI is configuring the upload they are running now;
// the report only records what the test process was told at run time. Putting
// the file above it would also silently change behaviour for everyone already
// relying on QF_ENVIRONMENT.
func TestEnvironmentFallback_FileFillsInWhenUserChoseNothing(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SetEnvironmentFallback("staging")
	if got := cfg.GetEnvironment(); got != "staging" {
		t.Errorf("with no flag and no env var the report's environment must be used; got %q", got)
	}
}

func TestEnvironmentFallback_FlagWinsOverTheFile(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SetEnvironment("production") // as --environment production would
	cfg.SetEnvironmentFallback("staging")
	if got := cfg.GetEnvironment(); got != "production" {
		t.Errorf("an explicit flag must outrank the report; got %q", got)
	}
}

func TestEnvironmentFallback_EnvVarWinsOverTheFile(t *testing.T) {
	t.Setenv("QF_ENVIRONMENT", "production")
	cfg := DefaultConfig()
	cfg.LoadFromEnv()
	cfg.SetEnvironmentFallback("staging")
	if got := cfg.GetEnvironment(); got != "production" {
		t.Errorf("QF_ENVIRONMENT must outrank the report; got %q", got)
	}
}

// The load-bearing backwards-compatibility case: a report from a reporter that
// never wrote an environment must leave the existing default alone, not blank
// it out.
func TestEnvironmentFallback_EmptyFileValueChangesNothing(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SetEnvironmentFallback("")
	if got := cfg.GetEnvironment(); got != "development" {
		t.Errorf("an absent environment must leave the default intact; got %q", got)
	}
}

// A user who explicitly asks for the same value the default happens to be is
// still an explicit user: the file must not override them.
func TestEnvironmentFallback_ExplicitDevelopmentIsStillExplicit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SetEnvironment("development")
	cfg.SetEnvironmentFallback("staging")
	if got := cfg.GetEnvironment(); got != "development" {
		t.Errorf("explicitly choosing the default value must still outrank the report; got %q", got)
	}
}
