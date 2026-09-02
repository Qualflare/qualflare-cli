package factory

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

// `go test -json` emits NDJSON — one object per line — so a whole-file
// json.Unmarshal always fails on it. Detection then fell through to the
// filename, which recognises "go-test", a ".out" extension, and a
// word-boundary "go" token, but NOT "golang". The practical effect: the exact
// command every Go project runs,
//
//	go test -json ./... > results.json
//
// produced a file that could not be detected at all and demanded --format
// golang. examples/unit/golang-example.json was the in-repo proof, failing
// `make validate-examples` — invisible for as long as that target was itself
// broken.
func TestDetectNDJSONFromContent(t *testing.T) {
	f := NewParserFactory()

	goTestJSON := strings.Join([]string{
		`{"Time":"2024-01-15T10:30:00.123456Z","Action":"run","Package":"github.com/example/myapp","Test":"TestUserService"}`,
		`{"Time":"2024-01-15T10:30:00.234567Z","Action":"output","Package":"github.com/example/myapp","Test":"TestUserService","Output":"=== RUN\n"}`,
		`{"Time":"2024-01-15T10:30:01.000000Z","Action":"pass","Package":"github.com/example/myapp","Test":"TestUserService","Elapsed":0.88}`,
	}, "\n")

	// The filename deliberately carries no usable hint: "results" matches no
	// framework token, so this can only pass via content detection.
	got, err := f.DetectFrameworkFromContent("results.json", []byte(goTestJSON))
	if err != nil {
		t.Fatalf("DetectFrameworkFromContent: %v", err)
	}
	if got != domain.FrameworkGolang {
		t.Errorf("detected %q, want %q", got, domain.FrameworkGolang)
	}
}

// A single trailing newline, or none at all, must not change the answer — real
// files come both ways.
func TestDetectNDJSONIgnoresTrailingNewline(t *testing.T) {
	f := NewParserFactory()
	one := `{"Action":"run","Package":"github.com/example/myapp","Test":"T"}`

	for name, content := range map[string]string{
		"no trailing newline":   one,
		"trailing newline":      one + "\n",
		"leading blank ignored": one + "\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := f.DetectFrameworkFromContent("results.json", []byte(content))
			if err != nil {
				t.Fatalf("DetectFrameworkFromContent: %v", err)
			}
			if got != domain.FrameworkGolang {
				t.Errorf("detected %q, want %q", got, domain.FrameworkGolang)
			}
		})
	}
}

// The NDJSON path must not be a backdoor that accepts what the single-document
// path would reject. It hands the parsed object to the same key registry, so
// garbage stays undetectable rather than becoming some arbitrary framework.
func TestDetectNDJSONRejectsNonJSON(t *testing.T) {
	f := NewParserFactory()

	for name, content := range map[string]string{
		"plain text":            "this is not json at all\nsecond line",
		"truncated object":      `{"Action":"run"`,
		"json array per line":   "[1,2,3]\n[4,5,6]",
		"empty":                 "",
		"unknown ndjson object": `{"totallyUnknownKey":1}` + "\n" + `{"totallyUnknownKey":2}`,
	} {
		t.Run(name, func(t *testing.T) {
			// A .json filename with no framework token, so nothing else can
			// rescue it — detection must fail rather than guess.
			got, err := f.DetectFrameworkFromContent("results.json", []byte(content))
			if err == nil {
				t.Errorf("expected detection to fail, got framework %q", got)
			}
		})
	}
}
