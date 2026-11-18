package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"qualflare-cli/internal/auth"
)

// These tests drive run(), the function main hands to os.Exit, against a local
// fake API, so what they pin is what a CI job sees: the exit code and the two
// output streams. QF_API_URL always points at the fake server (or, for the
// network failure, at a closed local port), and the config directory is a
// temporary one, so no test can reach a real Qualflare server or a real sign-in.

const (
	collectPath = "/api/v1/collect"

	monthlyLimitBody = `{"code":"quota.exceeded","message":"Launches quota exceeded (100/100)","request_id":"req-monthly"}`
	caseLimitBody    = `{"code":"quota.exceeded","message":"Test Cases limit reached: this would add 5 test cases to the 998 the workspace has, and the plan allows 1000. Nothing was saved. Delete test cases or upgrade the plan.","request_id":"req-cases"}`

	// notUploadedMarker is the part of the warning that says the results are gone.
	notUploadedMarker = "NOT uploaded"
	successMarker     = "collected successfully"

	junitReport = `<testsuite name="suite" tests="1"><testcase classname="pkg" name="passes"/></testsuite>`
)

type qfResult struct {
	exit       int
	stdout     string
	stderr     string
	collects   int32 // POSTs to /collect
	uploadURLs int32 // POSTs to /attachments/upload-url
}

// refuseCollect answers every /collect with the given status and body.
func refuseCollect(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != collectPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// writeReports writes one JUnit report per name into a new directory.
func writeReports(t *testing.T, names ...string) []string {
	t.Helper()
	dir := t.TempDir()
	paths := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(junitReport), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	return paths
}

// runQF runs `qf demo <args...>` in-process against handler, with env applied on
// top of a clean environment. A nil handler leaves the API unreachable.
func runQF(t *testing.T, handler http.Handler, env map[string]string, args ...string) qfResult {
	t.Helper()

	var res qfResult
	apiURL := ""
	if handler == nil {
		closed := httptest.NewServer(http.NotFoundHandler())
		apiURL = closed.URL
		closed.Close()
	} else {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case collectPath:
				atomic.AddInt32(&res.collects, 1)
			case "/api/v1/attachments/upload-url":
				atomic.AddInt32(&res.uploadURLs, 1)
			}
			handler.ServeHTTP(w, r)
		}))
		t.Cleanup(srv.Close)
		apiURL = srv.URL
	}

	home := t.TempDir()
	for key, value := range map[string]string{
		"HOME":                  home,
		"XDG_CONFIG_HOME":       filepath.Join(home, ".config"),
		"AppData":               filepath.Join(home, "AppData"),
		"QF_API_URL":            apiURL,
		"QF_RETRY_MAX":          "0", // a retryable status answers at once
		"QF_WARN_ON_PLAN_LIMIT": "",
		"QF_QUIET":              "",
		"QF_VERBOSE":            "",
		"QF_DEBUG":              "",
	} {
		t.Setenv(key, value)
	}
	for key, value := range env {
		t.Setenv(key, value)
	}

	storePath, err := auth.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	store, err := auth.Load(storePath)
	if err != nil {
		t.Fatal(err)
	}
	store.Set("demo", "qf_test_token")
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	stdoutFile, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdoutFile.Close() }()
	stderrFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stderrFile.Close() }()

	oldArgs, oldStdout, oldStderr := os.Args, os.Stdout, os.Stderr
	os.Args = append([]string{"qf", "demo"}, args...)
	os.Stdout, os.Stderr = stdoutFile, stderrFile
	defer func() { os.Args, os.Stdout, os.Stderr = oldArgs, oldStdout, oldStderr }()

	res.exit = run()

	out, err := os.ReadFile(stdoutFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	errOut, err := os.ReadFile(stderrFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	res.stdout, res.stderr = string(out), string(errOut)
	return res
}

// collectArgs is `collect <one JUnit report> --format junit` plus extra.
func collectArgs(t *testing.T, extra ...string) []string {
	t.Helper()
	return append([]string{"collect", writeReports(t, "results.xml")[0], "--format", "junit"}, extra...)
}

// The default is the contract existing pipelines rely on: a plan limit fails the
// build with exit 4 and the usual error, and no warning takes its place.
func TestCollectPlanLimit_DefaultStillExits4(t *testing.T) {
	for name, body := range map[string]string{"monthly reports": monthlyLimitBody, "test case limit": caseLimitBody} {
		t.Run(name, func(t *testing.T) {
			res := runQF(t, refuseCollect(http.StatusPaymentRequired, body), nil, collectArgs(t)...)

			if res.exit != exitForbidden {
				t.Errorf("exit = %d, want %d; stderr:\n%s", res.exit, exitForbidden, res.stderr)
			}
			if !strings.Contains(res.stderr, "Error: ") || !strings.Contains(res.stderr, "a plan limit was reached") {
				t.Errorf("stderr lacks the usual error:\n%s", res.stderr)
			}
			if strings.Contains(res.stderr, "warning:") || strings.Contains(res.stderr, successMarker) {
				t.Errorf("default run must not warn or report success:\n%s", res.stderr)
			}
		})
	}
}

func TestCollectPlanLimit_OptInWarnsAndExits0(t *testing.T) {
	cases := []struct {
		name  string
		env   map[string]string
		extra []string
		body  string
		want  []string
	}{
		{
			name:  "flag, monthly reports",
			extra: []string{"--warn-on-plan-limit"},
			body:  monthlyLimitBody,
			want:  []string{"Launches quota exceeded (100/100)", "req-monthly"},
		},
		{
			name:  "flag, test case limit",
			extra: []string{"--warn-on-plan-limit"},
			body:  caseLimitBody,
			want:  []string{"Test Cases limit reached", "the plan allows 1000", "req-cases"},
		},
		{
			name: "env var true",
			env:  map[string]string{"QF_WARN_ON_PLAN_LIMIT": "true"},
			body: monthlyLimitBody,
			want: []string{"Launches quota exceeded (100/100)"},
		},
		{
			name: "env var 1",
			env:  map[string]string{"QF_WARN_ON_PLAN_LIMIT": "1"},
			body: monthlyLimitBody,
			want: []string{"Launches quota exceeded (100/100)"},
		},
		{
			name:  "--quiet does not hide it",
			extra: []string{"--warn-on-plan-limit", "--quiet"},
			body:  monthlyLimitBody,
			want:  []string{"Launches quota exceeded (100/100)"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runQF(t, refuseCollect(http.StatusPaymentRequired, tc.body), tc.env, collectArgs(t, tc.extra...)...)

			if res.exit != exitOK {
				t.Errorf("exit = %d, want %d; stderr:\n%s", res.exit, exitOK, res.stderr)
			}
			if !strings.Contains(res.stderr, "warning: ") || !strings.Contains(res.stderr, notUploadedMarker) {
				t.Errorf("stderr lacks the warning:\n%s", res.stderr)
			}
			for _, want := range tc.want {
				if !strings.Contains(res.stderr, want) {
					t.Errorf("stderr lacks %q:\n%s", want, res.stderr)
				}
			}
			if strings.Contains(res.stderr, successMarker) {
				t.Errorf("refused results reported as collected:\n%s", res.stderr)
			}
			if strings.Contains(res.stderr, "Error: ") {
				t.Errorf("a softened refusal must not print an error:\n%s", res.stderr)
			}
			if res.stdout != "" {
				t.Errorf("stdout must stay empty, got %q", res.stdout)
			}
		})
	}
}

// The option is only on when it is asked for in a form the CLI recognises, and an
// explicit flag beats the variable, so one job can opt out of a shared CI setting.
func TestCollectPlanLimit_OptionOff(t *testing.T) {
	cases := []struct {
		name  string
		env   map[string]string
		extra []string
	}{
		{"flag false beats env var", map[string]string{"QF_WARN_ON_PLAN_LIMIT": "true"}, []string{"--warn-on-plan-limit=false"}},
		{"env var false", map[string]string{"QF_WARN_ON_PLAN_LIMIT": "false"}, nil},
		{"env var with an unrecognised value", map[string]string{"QF_WARN_ON_PLAN_LIMIT": "yes"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runQF(t, refuseCollect(http.StatusPaymentRequired, monthlyLimitBody), tc.env, collectArgs(t, tc.extra...)...)
			if res.exit != exitForbidden {
				t.Errorf("exit = %d, want %d; stderr:\n%s", res.exit, exitForbidden, res.stderr)
			}
		})
	}
}

// Everything that is not a plan-limit refusal fails exactly as it does without
// the option, including responses that resemble one.
func TestCollectPlanLimit_OptionLeavesOtherFailuresAlone(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   int
	}{
		{"401 invalid token", 401, `{"code":"auth.invalid_token","message":"invalid or expired access token"}`, exitAuth},
		{"403 access denied", 403, `{"code":"auth.forbidden","message":"no access to this project"}`, exitForbidden},
		{"403 carrying the quota code", 403, `{"code":"quota.exceeded","message":"not a plan limit"}`, exitForbidden},
		{"402 frozen workspace", 402, `{"code":"billing.workspace_frozen","message":"This workspace is larger than its plan allows."}`, exitForbidden},
		{"402 storage full", 402, `{"code":"quota.storage_exceeded","message":"Storage is full."}`, exitForbidden},
		{"402 subscription inactive", 402, `{"code":"billing.subscription_inactive","message":"subscription is inactive or expired"}`, exitForbidden},
		{"402 payment required", 402, `{"code":"billing.payment_required","message":"payment required"}`, exitForbidden},
		{"402 without a code", 402, `{"message":"payment required"}`, exitForbidden},
		{"402 that is not JSON", 402, `payment required`, exitForbidden},
		{"400 validation error", 400, `{"code":"common.validation_failed","message":"bad report"}`, exitGeneric},
		{"400 carrying the quota code", 400, `{"code":"quota.exceeded","message":"too many of something else"}`, exitGeneric},
		{"404 not found", 404, `{"code":"common.resource_not_found","message":"not found"}`, exitNotFound},
		{"429 rate limited", 429, `{"code":"common.rate_limited","message":"slow down"}`, exitTransient},
		{"500 server error", 500, `{"code":"common.unknown","message":"boom"}`, exitTransient},
		{"503 unavailable", 503, `unavailable`, exitTransient},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runQF(t, refuseCollect(tc.status, tc.body), nil, collectArgs(t, "--warn-on-plan-limit")...)

			if res.exit != tc.want {
				t.Errorf("exit = %d, want %d; stderr:\n%s", res.exit, tc.want, res.stderr)
			}
			if strings.Contains(res.stderr, notUploadedMarker) {
				t.Errorf("must not be treated as a plan limit:\n%s", res.stderr)
			}
			if !strings.Contains(res.stderr, "Error: ") {
				t.Errorf("stderr lacks the error:\n%s", res.stderr)
			}
		})
	}

	t.Run("network error", func(t *testing.T) {
		res := runQF(t, nil, nil, collectArgs(t, "--warn-on-plan-limit")...)
		if res.exit != exitGeneric {
			t.Errorf("exit = %d, want %d; stderr:\n%s", res.exit, exitGeneric, res.stderr)
		}
		if strings.Contains(res.stderr, notUploadedMarker) {
			t.Errorf("must not be treated as a plan limit:\n%s", res.stderr)
		}
	})

	t.Run("report that cannot be parsed", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "results.xml")
		if err := os.WriteFile(bad, []byte("not xml"), 0o600); err != nil {
			t.Fatal(err)
		}
		res := runQF(t, refuseCollect(402, monthlyLimitBody), nil,
			"collect", bad, "--format", "junit", "--warn-on-plan-limit")
		if res.exit != exitGeneric {
			t.Errorf("exit = %d, want %d; stderr:\n%s", res.exit, exitGeneric, res.stderr)
		}
		if res.collects != 0 {
			t.Errorf("sent %d reports, want 0", res.collects)
		}
	})
}

// Every input file is merged into one launch and sent in one request, which the
// server accepts or refuses whole. So a refusal covers all of the files: one
// warning, and no claim that any of them arrived.
func TestCollectPlanLimit_MultipleFilesAreOneRefusal(t *testing.T) {
	cases := []struct {
		name  string
		extra []string
	}{
		{"several files", nil},
		{"shards", []string{"--shard"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"collect"}, writeReports(t, "a.xml", "b.xml", "c.xml")...)
			args = append(args, "--format", "junit", "--warn-on-plan-limit")
			args = append(args, tc.extra...)

			res := runQF(t, refuseCollect(402, monthlyLimitBody), nil, args...)

			if res.exit != exitOK {
				t.Errorf("exit = %d, want %d; stderr:\n%s", res.exit, exitOK, res.stderr)
			}
			if res.collects != 1 {
				t.Errorf("sent %d reports, want 1 merged launch", res.collects)
			}
			if got := strings.Count(res.stderr, notUploadedMarker); got != 1 {
				t.Errorf("printed the warning %d times, want 1:\n%s", got, res.stderr)
			}
			if strings.Contains(res.stderr, successMarker) {
				t.Errorf("refused results reported as collected:\n%s", res.stderr)
			}
		})
	}
}

// A screenshot leaves before the report does, so it can be stored even though the
// report is then refused. The run still has to say the results did not arrive.
func TestCollectPlanLimit_ScreenshotUploadedBeforeRefusal(t *testing.T) {
	dir := t.TempDir()
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nnot really an image"))
	report := fmt.Sprintf(`{
		"framework": "playwright",
		"metadata": {},
		"suites": [{
			"name": "spec", "duration": 1000000,
			"cases": [{
				"id": "c1", "name": "shows the page", "status": "passed", "duration": 1000000,
				"attachments": [{"name": "shot.png", "mimeType": "image/png", "content": %q}]
			}]
		}]
	}`, png)
	path := filepath.Join(dir, "report.json")
	if err := os.WriteFile(path, []byte(report), 0o600); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/attachments/upload-url", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"uploadUrl":  "http://" + r.Host + "/storage/shot.png",
			"storageKey": "runs/shot.png",
		})
	})
	mux.HandleFunc("/storage/shot.png", func(http.ResponseWriter, *http.Request) {})
	mux.Handle(collectPath, refuseCollect(402, monthlyLimitBody))

	res := runQF(t, mux, nil, "collect", path, "--format", "qualflare-json", "--warn-on-plan-limit")

	if res.uploadURLs != 1 {
		t.Fatalf("asked for %d upload URLs, want 1 (the scenario needs the screenshot to go first); stderr:\n%s",
			res.uploadURLs, res.stderr)
	}
	if res.exit != exitOK {
		t.Errorf("exit = %d, want %d; stderr:\n%s", res.exit, exitOK, res.stderr)
	}
	if !strings.Contains(res.stderr, notUploadedMarker) || strings.Contains(res.stderr, successMarker) {
		t.Errorf("stderr must say the results were not uploaded, and only that:\n%s", res.stderr)
	}
}

// With the option on, an accepted upload is an ordinary success.
func TestCollectPlanLimit_AcceptedUploadIsUnchanged(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"seq":1}`))
	})
	res := runQF(t, handler, nil, collectArgs(t, "--warn-on-plan-limit")...)

	if res.exit != exitOK {
		t.Errorf("exit = %d, want %d; stderr:\n%s", res.exit, exitOK, res.stderr)
	}
	if !strings.Contains(res.stderr, successMarker) {
		t.Errorf("stderr lacks the success line:\n%s", res.stderr)
	}
	if strings.Contains(res.stderr, "warning:") {
		t.Errorf("an accepted upload must not warn:\n%s", res.stderr)
	}
}

// --output json is the CLI's one machine-readable collect output. It belongs to
// --dry-run, which sends nothing, so the option cannot touch it.
func TestCollectPlanLimit_JSONOutputStaysValid(t *testing.T) {
	refuse := refuseCollect(402, monthlyLimitBody)

	t.Run("dry run prints only the JSON report", func(t *testing.T) {
		res := runQF(t, refuse, nil, collectArgs(t, "--warn-on-plan-limit", "--dry-run", "--output", "json")...)

		if res.exit != exitOK {
			t.Errorf("exit = %d, want %d; stderr:\n%s", res.exit, exitOK, res.stderr)
		}
		if res.collects != 0 {
			t.Errorf("sent %d reports, want 0", res.collects)
		}
		var report map[string]any
		if err := json.Unmarshal([]byte(res.stdout), &report); err != nil {
			t.Errorf("stdout is not valid JSON: %v\n%s", err, res.stdout)
		}
		if strings.Contains(res.stderr, "warning:") {
			t.Errorf("a dry run must not warn:\n%s", res.stderr)
		}
	})

	t.Run("--output without --dry-run is still rejected", func(t *testing.T) {
		res := runQF(t, refuse, nil, collectArgs(t, "--warn-on-plan-limit", "--output", "json")...)

		if res.exit != exitGeneric {
			t.Errorf("exit = %d, want %d; stderr:\n%s", res.exit, exitGeneric, res.stderr)
		}
		if res.collects != 0 {
			t.Errorf("sent %d reports, want 0", res.collects)
		}
	})
}
