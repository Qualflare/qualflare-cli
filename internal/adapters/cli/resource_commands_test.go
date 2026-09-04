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

func (s *stubAPIClient) UploadAttachment(context.Context, []byte, string, string) (string, int64, error) {
	return "", 0, nil
}

func (s *stubAPIClient) UploadVideo(context.Context, string, string) (string, int64, error) {
	return "", 0, nil
}

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

// The remaining list commands share newListCommand but each builds its own path and
// filter set. This sweeps all of them so a wrong endpoint or a dropped filter is caught
// — including the bracketed param names (severity[], status[]) the API expects, which
// are easy to "tidy" into the unbracketed form by accident.
func TestResourceListCommands_PathsAndFilters(t *testing.T) {
	tests := []struct {
		name       string
		cmd        func(c *CLI) *cobra.Command
		args       []string
		wantPath   string
		wantParams map[string]string // param -> expected value ("" = just must be present)
	}{
		{
			"clusters", (*CLI).createClustersCommand,
			[]string{"list", "--severity", "critical,high"},
			apiV1 + "/clusters",
			map[string]string{"severity[]": ""},
		},
		{
			"defects", (*CLI).createDefectsCommand,
			[]string{"list", "--severity", "high", "--status", "active"},
			apiV1 + "/defects",
			map[string]string{"severity[]": "", "status[]": ""},
		},
		{
			"milestones", (*CLI).createMilestonesCommand,
			[]string{"list", "--query", "q1"},
			apiV1 + "/milestones",
			map[string]string{"q": "q1"},
		},
		{
			"suites", (*CLI).createSuitesCommand,
			[]string{"list", "--query", "smoke"},
			apiV1 + "/suites",
			map[string]string{"q": "smoke"},
		},
		{
			"launches", (*CLI).createLaunchesCommand,
			[]string{"list", "--milestone", "3", "--environment", "prod"},
			apiV1 + "/launches",
			map[string]string{"milestone": "3", "environments": "prod"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, api := newAPICLI(t)
			if err := run(t, tt.cmd(c), tt.args...); err != nil {
				t.Fatalf("%s = %v", tt.name, err)
			}
			if api.path != tt.wantPath {
				t.Errorf("path = %q, want %q", api.path, tt.wantPath)
			}
			for k, want := range tt.wantParams {
				got := api.params.Get(k)
				if got == "" {
					t.Errorf("param %q was dropped", k)
					continue
				}
				if want != "" && got != want {
					t.Errorf("param %q = %q, want %q", k, got, want)
				}
			}
			// All five are paginated, unlike cases — but page is sent only when the
			// user asked for one, so the server's own default can apply (API-02).
			if api.params.Get("page") != "" {
				t.Errorf("%s sent page=%q without --page; it must be omitted so the server defaults",
					tt.name, api.params.Get("page"))
			}
			c2, api2 := newAPICLI(t)
			if err := run(t, tt.cmd(c2), append(tt.args, "--page", "2")...); err != nil {
				t.Fatalf("%s with --page = %v", tt.name, err)
			}
			if api2.params.Get("page") != "2" {
				t.Errorf("%s with --page 2 sent page=%q, want 2", tt.name, api2.params.Get("page"))
			}
		})
	}
}

// The singular detail commands are a separate family: one required positional arg,
// path-escaped into the URL.
func TestResourceDetailCommands_Paths(t *testing.T) {
	tests := []struct {
		name     string
		cmd      func(c *CLI) *cobra.Command
		arg      string
		wantPath string
	}{
		{"cluster", (*CLI).createClusterCommand, "abc", apiV1 + "/cluster/abc"},
		{"defect", (*CLI).createDefectCommand, "12", apiV1 + "/defect/12"},
		{"milestone", (*CLI).createMilestoneCommand, "3", apiV1 + "/milestone/3"},
		{"suite", (*CLI).createSuiteCommand, "7", apiV1 + "/suite/7"},
		{"launch", (*CLI).createLaunchCommand, "10", apiV1 + "/launch/10"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, api := newAPICLI(t)
			if err := run(t, tt.cmd(c), "get", tt.arg); err != nil {
				t.Fatalf("%s get = %v", tt.name, err)
			}
			if api.path != tt.wantPath {
				t.Errorf("path = %q, want %q", api.path, tt.wantPath)
			}
		})
	}
}
