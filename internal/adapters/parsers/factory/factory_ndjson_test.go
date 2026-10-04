package factory

import (
	"os"
	"path/filepath"
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

// `flutter test --reporter json` (and `--file-reporter json:<path>`) emit NDJSON
// whose first record is {"protocolVersion":...,"type":"start"}.
func TestDetect_FlutterMachineAndFileReporter(t *testing.T) {
	f := NewParserFactory()
	for _, name := range []string{"widget-machine.jsonl", "widget-file-reporter.jsonl", "device-android.jsonl"} {
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("..", "unit", "flutter", "testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			for _, fn := range []string{"results.json", name} {
				got, err := f.DetectFrameworkFromContent(fn, content)
				if err != nil {
					t.Fatalf("DetectFrameworkFromContent(%s): %v", fn, err)
				}
				if got != domain.FrameworkFlutter {
					t.Errorf("%s: detected %q, want %q", fn, got, domain.FrameworkFlutter)
				}
			}
		})
	}
}

// `flutter test -v` on a device prints tool log lines before the JSON stream.
func TestDetect_FlutterPastLeadingNoise(t *testing.T) {
	f := NewParserFactory()
	content, err := os.ReadFile(filepath.Join("..", "unit", "flutter", "testdata", "device-ios.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(content), "[") {
		t.Fatalf("fixture no longer starts with tool log noise: %.40q", content)
	}
	got, err := f.DetectFrameworkFromContent("results.json", content)
	if err != nil {
		t.Fatalf("DetectFrameworkFromContent: %v", err)
	}
	if got != domain.FrameworkFlutter {
		t.Errorf("detected %q, want %q", got, domain.FrameworkFlutter)
	}
}

// Only the first object line within the first 1 MiB is classified.
func TestDetectNDJSON_ScanLimits(t *testing.T) {
	f := NewParserFactory()
	start := `{"protocolVersion":"0.1.1","type":"start","time":0}`

	beyond := strings.Repeat("noise line\n", 100000) + start // > 1 MiB of noise
	if _, err := f.DetectFrameworkFromContent("results.json", []byte(beyond)); err == nil {
		t.Error("an object line past the 1 MiB window must not be classified")
	}

	// Only the FIRST object line is classified: an unknown one stops the scan.
	second := `{"unknown":1}` + "\n" + start
	if got, err := f.DetectFrameworkFromContent("results.json", []byte(second)); err == nil {
		t.Errorf("only the first object line counts, got %q", got)
	}
}

func TestDetect_GoTestJSONUnchanged(t *testing.T) {
	f := NewParserFactory()
	content := `{"Time":"2024-01-15T10:30:00Z","Action":"run","Package":"example.com/p","Test":"TestX"}` + "\n" +
		`{"Time":"2024-01-15T10:30:01Z","Action":"pass","Package":"example.com/p","Test":"TestX","Elapsed":0.1}` + "\n"
	got, err := f.DetectFrameworkFromContent("results.json", []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if got != domain.FrameworkGolang {
		t.Errorf("detected %q, want %q", got, domain.FrameworkGolang)
	}
}

// A directory named after flutter but holding an .xcresult is an XCTest bundle
// (DetectFramework is called directly for directories); a flutter-named JSON
// file is still flutter.
func TestDetectFramework_FlutterNameDoesNotShadowBundles(t *testing.T) {
	f := NewParserFactory()
	for name, want := range map[string]domain.Framework{
		"flutter_ios.xcresult": domain.FrameworkXCTest,
		"flutter-results.json": domain.FrameworkFlutter,
	} {
		got, err := f.DetectFramework(name)
		if err != nil || got != want {
			t.Errorf("DetectFramework(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
}
