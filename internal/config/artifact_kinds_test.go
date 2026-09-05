package config

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestParseArtifactKinds_EmptyMeansNothing(t *testing.T) {
	for _, raw := range []string{"", "   ", ","} {
		kinds, err := ParseArtifactKinds(raw, []string{"video", "trace"})
		if err != nil {
			t.Errorf("%q: unexpected error %v", raw, err)
		}
		if len(kinds) != 0 {
			t.Errorf("%q: expected no kinds, got %v", raw, kinds)
		}
	}
}

func TestParseArtifactKinds_AcceptsAListAndNormalises(t *testing.T) {
	kinds, err := ParseArtifactKinds(" Video , TRACE ", []string{"video", "trace"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !kinds["video"] || !kinds["trace"] || len(kinds) != 2 {
		t.Errorf("got %v, want both kinds", kinds)
	}
}

// A typo must fail loudly. Silently uploading nothing because someone wrote
// "vidoe" is indistinguishable from the gate working as intended, which is the
// exact surprise this flag exists to prevent.
func TestParseArtifactKinds_RejectsAnUnknownKindAndNamesTheValidOnes(t *testing.T) {
	_, err := ParseArtifactKinds("vidoe", []string{"video", "trace"})
	if err == nil {
		t.Fatal("expected an error for an unknown kind")
	}
	for _, want := range []string{"vidoe", "video", "trace"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q; got %q", want, err.Error())
		}
	}
}

// The heavy kinds default to off; images do not. This test used to be named
// "DefaultsToFalseForEveryKind" and passed only because it never asked about
// images -- the name would have kept asserting something false.
func TestIsArtifactUploadEnabled_HeavyKindsOffByDefaultImagesOn(t *testing.T) {
	cfg := DefaultConfig()
	for _, kind := range []string{"video", "trace", "anything"} {
		if cfg.IsArtifactUploadEnabled(kind) {
			t.Errorf("%q should be disabled by default", kind)
		}
	}
	if !cfg.IsArtifactUploadEnabled(domain.ArtifactKindImage) {
		t.Error("images should upload by default; making them opt-in would silently " +
			"stop delivering screenshots for everyone who never passed the flag")
	}
}

func TestParseArtifactKinds_NoneDeclinesEveryKindIncludingImages(t *testing.T) {
	cfg := DefaultConfig()
	kinds, err := ParseArtifactKinds("none", domain.AllArtifactKinds())
	if err != nil {
		t.Fatalf("none should parse: %v", err)
	}
	// Non-nil but empty: that is what separates an explicit refusal from an
	// absent flag, which SetUploadArtifacts must not confuse.
	if kinds == nil {
		t.Fatal("none must produce a non-nil empty set, not nil")
	}
	cfg.SetUploadArtifacts(kinds)
	for _, kind := range domain.AllArtifactKinds() {
		if cfg.IsArtifactUploadEnabled(kind) {
			t.Errorf("%q should be declined by none", kind)
		}
	}
}

func TestParseArtifactKinds_NoneCannotBeCombinedWithAKind(t *testing.T) {
	if _, err := ParseArtifactKinds("none,video", domain.AllArtifactKinds()); err == nil {
		t.Error("none alongside a kind asks for two opposite things and must be rejected")
	}
}

// A named kind ADDS to the defaults. Replacing them would mean
// --upload-artifacts=video silently stopped uploading the screenshots the user
// was already getting.
func TestSetUploadArtifacts_AddsToTheDefaultsRatherThanReplacingThem(t *testing.T) {
	cfg := DefaultConfig()
	kinds, err := ParseArtifactKinds("video", domain.AllArtifactKinds())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cfg.SetUploadArtifacts(kinds)
	if !cfg.IsArtifactUploadEnabled(domain.ArtifactKindVideo) {
		t.Error("video was asked for")
	}
	if !cfg.IsArtifactUploadEnabled(domain.ArtifactKindImage) {
		t.Error("images must survive asking for video")
	}
	if cfg.IsArtifactUploadEnabled(domain.ArtifactKindTrace) {
		t.Error("trace was not asked for")
	}
}

// envArtifactKinds used to validate against a hardcoded {"video", "trace"},
// so a newly added kind was accepted by the flag and silently rejected by the
// environment. This is that drift.
func TestQFUploadArtifactsAcceptsEveryKindTheFlagDoes(t *testing.T) {
	for _, kind := range domain.AllArtifactKinds() {
		t.Setenv("QF_UPLOAD_ARTIFACTS", kind)
		cfg := DefaultConfig()
		cfg.LoadFromEnv()
		if !cfg.IsArtifactUploadEnabled(kind) {
			t.Errorf("QF_UPLOAD_ARTIFACTS=%s should enable %s", kind, kind)
		}
	}
}

func TestQFUploadArtifactsNoneDeclinesEverything(t *testing.T) {
	t.Setenv("QF_UPLOAD_ARTIFACTS", "none")
	cfg := DefaultConfig()
	cfg.LoadFromEnv()
	for _, kind := range domain.AllArtifactKinds() {
		if cfg.IsArtifactUploadEnabled(kind) {
			t.Errorf("QF_UPLOAD_ARTIFACTS=none should decline %s", kind)
		}
	}
}

func TestQFUploadArtifactsEnvVarOptsIn(t *testing.T) {
	t.Setenv("QF_UPLOAD_ARTIFACTS", "trace")
	cfg := DefaultConfig()
	cfg.LoadFromEnv()

	if !cfg.IsArtifactUploadEnabled("trace") {
		t.Error("QF_UPLOAD_ARTIFACTS=trace should enable traces")
	}
	if cfg.IsArtifactUploadEnabled("video") {
		t.Error("QF_UPLOAD_ARTIFACTS=trace must not enable videos")
	}
}
