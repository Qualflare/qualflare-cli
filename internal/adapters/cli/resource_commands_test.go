package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
)

// stubAPIClient records the path and params the command layer asked for, so the tests
// can assert on the request that would go out rather than on the printed output.
type stubAPIClient struct {
	calls  int
	path   string
	params url.Values
	body   string
	err    error
}

func (s *stubAPIClient) Get(_ context.Context, path string, params url.Values) (json.RawMessage, error) {
	s.calls++
	s.path, s.params = path, params
	if s.err != nil {
		return nil, s.err
	}
	if s.body == "" {
		return json.RawMessage(`{"ok":true}`), nil
	}
	return json.RawMessage(s.body), nil
}

func (s *stubAPIClient) SendReport(context.Context, *domain.Launch) error { return nil }

// newAPICLI wires a CLI with a token present so fetchAndPrint gets past its auth guard.
func newAPICLI(t *testing.T) (*CLI, *stubAPIClient) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.APIKey = "qf_token"
	api := &stubAPIClient{}
	return NewCLI(nil, cfg, nil, api, nil), api
}

// run executes a subcommand path (e.g. "list") on cmd with the given args.
func run(t *testing.T, cmd *cobra.Command, args ...string) error {
	t.Helper()
	cmd.SetArgs(args)
	cmd.SetOut(new(strings.Builder))
	cmd.SetErr(new(strings.Builder))
	return cmd.Execute()
}

// SYNC-08: the suite-cases endpoint is suite-scoped and unpaginated. --suite is required,
// and the filters must reach the query string rather than being silently dropped.
func TestCasesList_BuildsSuiteScopedRequest(t *testing.T) {
	c, api := newAPICLI(t)

	err := run(t, c.createCasesCommand(),
		"list", "--suite", "5", "--query", "login", "--state", "active,review", "--priority", "high")
	if err != nil {
		t.Fatalf("cases list = %v", err)
	}
	if api.calls != 1 {
		t.Fatalf("api called %d times, want 1", api.calls)
	}
	if api.path != apiV1+"/suite/5/cases" {
		t.Errorf("path = %q, want the suite-scoped cases path", api.path)
	}
	if got := api.params.Get("q"); got != "login" {
		t.Errorf("q = %q, want %q", got, "login")
	}
	for _, k := range []string{"state", "priority"} {
		if api.params.Get(k) == "" {
			t.Errorf("param %q was dropped", k)
		}
	}
	// Unpaginated endpoint: no page parameter should be sent.
	if api.params.Get("page") != "" {
		t.Errorf("page = %q, want it absent for the unpaginated cases endpoint", api.params.Get("page"))
	}
}

func TestCasesList_RequiresSuite(t *testing.T) {
	c, api := newAPICLI(t)

	err := run(t, c.createCasesCommand(), "list")
	if err == nil || !strings.Contains(err.Error(), "--suite") {
		t.Fatalf("cases list without --suite = %v, want a --suite required error", err)
	}
	if api.calls != 0 {
		t.Error("a request was made despite the missing --suite")
	}
}

func TestCaseCommand_GetAndSteps(t *testing.T) {
	tests := []struct {
		args     []string
		wantPath string
	}{
		{[]string{"get", "123"}, apiV1 + "/case/123"},
		{[]string{"steps", "123"}, apiV1 + "/case/123/steps"},
	}
	for _, tt := range tests {
		t.Run(tt.args[0], func(t *testing.T) {
			c, api := newAPICLI(t)
			if err := run(t, c.createCaseCommand(), tt.args...); err != nil {
				t.Fatalf("case %v = %v", tt.args, err)
			}
			if api.path != tt.wantPath {
				t.Errorf("path = %q, want %q", api.path, tt.wantPath)
			}
		})
	}
}

func TestCaseCommand_RequiresExactlyOneArg(t *testing.T) {
	c, api := newAPICLI(t)
	if err := run(t, c.createCaseCommand(), "get"); err == nil {
		t.Error("case get with no seq = nil, want an argument error")
	}
	if api.calls != 0 {
		t.Error("a request was made despite the missing argument")
	}
}

func TestPlansList_BuildsRequest(t *testing.T) {
	c, api := newAPICLI(t)

	if err := run(t, c.createPlansCommand(), "list", "--query", "regression", "--page", "2"); err != nil {
		t.Fatalf("plans list = %v", err)
	}
	if api.path != apiV1+"/test-plans" {
		t.Errorf("path = %q, want %q", api.path, apiV1+"/test-plans")
	}
	if api.params.Get("q") != "regression" {
		t.Errorf("q = %q, want %q", api.params.Get("q"), "regression")
	}
	// plans is the paginated variant, unlike cases.
	if api.params.Get("page") == "" {
		t.Error("page was not sent for the paginated plans endpoint")
	}
}

func TestPlanCommand_GetAndCases(t *testing.T) {
	tests := []struct {
		args     []string
		wantPath string
	}{
		{[]string{"get", "5"}, apiV1 + "/test-plan/5"},
		{[]string{"cases", "5"}, apiV1 + "/test-plan/5/cases"},
	}
	for _, tt := range tests {
		t.Run(tt.args[0], func(t *testing.T) {
			c, api := newAPICLI(t)
			if err := run(t, c.createPlanCommand(), tt.args...); err != nil {
				t.Fatalf("plan %v = %v", tt.args, err)
			}
			if api.path != tt.wantPath {
				t.Errorf("path = %q, want %q", api.path, tt.wantPath)
			}
		})
	}
}

// fetchAndPrint must refuse before touching the network when no token is loaded, and the
// message has to point at the command that fixes it.
func TestFetchAndPrint_RequiresToken(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.APIKey = ""
	api := &stubAPIClient{}
	c := NewCLI(nil, cfg, nil, api, nil)

	err := c.fetchAndPrint(apiV1+"/suites", nil)
	if err == nil || !strings.Contains(err.Error(), "qf login") {
		t.Fatalf("fetchAndPrint without a token = %v, want a login hint", err)
	}
	if api.calls != 0 {
		t.Error("a request was made without a token")
	}
}

func TestFetchAndPrint_PropagatesAPIError(t *testing.T) {
	c, api := newAPICLI(t)
	api.err = errors.New("boom")

	if err := c.fetchAndPrint(apiV1+"/suites", nil); err == nil {
		t.Fatal("fetchAndPrint = nil, want the API error")
	}
}

// A non-JSON body must still be printed rather than turning into an error — the CLI
// should not swallow a response just because it failed to indent.
func TestFetchAndPrint_ToloeratesNonJSONBody(t *testing.T) {
	c, api := newAPICLI(t)
	api.body = `not json at all`

	if err := c.fetchAndPrint(apiV1+"/suites", nil); err != nil {
		t.Fatalf("fetchAndPrint with a non-JSON body = %v, want nil", err)
	}
}
