package config

import (
	"strings"
	"testing"
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

func TestIsArtifactUploadEnabled_DefaultsToFalseForEveryKind(t *testing.T) {
	cfg := DefaultConfig()
	for _, kind := range []string{"video", "trace", "anything"} {
		if cfg.IsArtifactUploadEnabled(kind) {
			t.Errorf("%q should be disabled by default", kind)
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
