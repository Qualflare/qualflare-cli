package config

import "testing"

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
