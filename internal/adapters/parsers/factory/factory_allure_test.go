package factory

import (
	"os"
	"path/filepath"
	"testing"

	"qualflare-cli/internal/core/domain"
)

const allureFixture = "../generic/allure/testdata/allure-results"

func TestParserFactory_AllureIsRegistered(t *testing.T) {
	if _, err := NewParserFactory().GetParser(domain.FrameworkAllure); err != nil {
		t.Fatalf("--format allure must resolve: %v", err)
	}
}

// `qf collect ./allure-results` arrives here as the directory itself; it is
// recognised by its contents, whatever the directory is called.
func TestParserFactory_DetectsAllureResultsDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "build-output")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(allureFixture, "22222222-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "abc-result.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := NewParserFactory().DetectFramework(dir)
	if err != nil || got != domain.FrameworkAllure {
		t.Fatalf("DetectFramework(dir) = %q, %v; want allure", got, err)
	}
}

func TestParserFactory_DetectsSingleAllureResultByContent(t *testing.T) {
	path := filepath.Join(allureFixture, "22222222-result.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewParserFactory().DetectFrameworkFromContent(path, data)
	if err != nil || got != domain.FrameworkAllure {
		t.Fatalf("content detection = %q, %v; want allure", got, err)
	}
}
