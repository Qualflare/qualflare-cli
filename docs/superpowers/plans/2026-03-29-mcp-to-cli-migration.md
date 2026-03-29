# MCP-to-CLI Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add 16 read commands across 7 domains to the CLI, migrating all MCP server functionality into `qf` subcommands.

**Architecture:** Extend the existing HTTP client with a generic GET method, update the config default to a base URL, then add one file per domain with noun-verb Cobra commands. All output is JSON to stdout.

**Tech Stack:** Go 1.23, Cobra CLI framework, existing HTTP client with retry logic

---

### Task 1: Change default API endpoint to base URL

**Files:**
- Modify: `internal/config/config.go:50`

- [ ] **Step 1: Update DefaultConfig**

Change the default endpoint from the full collect path to the base URL:

```go
// In DefaultConfig(), change line 50:
APIEndpoint:    "https://api.qualflare.com",
```

- [ ] **Step 2: Build to verify**

Run: `go build ./...`
Expected: Compiles with no errors

- [ ] **Step 3: Run tests**

Run: `go test ./...`
Expected: All tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "refactor: change default API endpoint to base URL"
```

---

### Task 2: Add Get method to HTTP client and update SendReport to append path

**Files:**
- Modify: `internal/adapters/http/client.go`

- [ ] **Step 1: Add `net/url` import**

Add `"net/url"` to the import block in `client.go`.

- [ ] **Step 2: Update `doRequest` to accept method and URL**

Refactor `doRequest` into `doRequestWithMethod` that accepts method and full URL, then make `doRequest` call it. This avoids duplicating retry/error logic:

```go
// doRequestWithMethod performs a single HTTP request with the given method and URL
func (c *Client) doRequestWithMethod(ctx context.Context, method, url string, body []byte) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, &APIError{
			Op:        "create_request",
			Message:   "failed to create request",
			Err:       err,
			Retryable: false,
		}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())
	req.Header.Set("Accept", "application/json")

	if apiKey := c.config.GetAPIKey(); apiKey != "" {
		req.Header.Set("QF_TOKEN", apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, &APIError{
			Op:        "send",
			Message:   "failed to send request",
			Err:       err,
			Retryable: true,
		}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return respBody, nil
	}

	apiErr := &APIError{
		Op:         "send",
		StatusCode: resp.StatusCode,
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err == nil {
		apiErr.Code = errResp.Code
		if friendlyMsg := getUserFriendlyMessage(errResp.Code); friendlyMsg != "" {
			apiErr.Message = friendlyMsg
		} else if errResp.Error != "" {
			apiErr.Message = errResp.Error
		} else if errResp.Message != "" {
			apiErr.Message = errResp.Message
		} else {
			apiErr.Message = fmt.Sprintf("API request failed with status %d", resp.StatusCode)
		}
	} else {
		apiErr.Message = fmt.Sprintf("API request failed with status %d", resp.StatusCode)
	}

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		apiErr.Retryable = true
		if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
			if seconds, err := strconv.Atoi(retryAfter); err == nil {
				apiErr.RetryAfter = time.Duration(seconds) * time.Second
			} else if t, err := http.ParseTime(retryAfter); err == nil {
				apiErr.RetryAfter = time.Until(t)
			}
		}
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		apiErr.Retryable = true
	case http.StatusInternalServerError:
		apiErr.Retryable = true
	default:
		apiErr.Retryable = false
	}

	return nil, apiErr
}
```

- [ ] **Step 3: Update doRequest to call doRequestWithMethod**

Replace the existing `doRequest` method body:

```go
func (c *Client) doRequest(ctx context.Context, body []byte) error {
	_, err := c.doRequestWithMethod(ctx, http.MethodPost, c.endpoint+"/api/v1/collect", body)
	return err
}
```

- [ ] **Step 4: Add the Get method with retry logic**

```go
// Get performs a GET request to the API with retry logic
func (c *Client) Get(ctx context.Context, path string, params url.Values) (json.RawMessage, error) {
	reqURL := c.endpoint + path
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	if c.config.IsVerbose() {
		fmt.Printf("GET %s\n", reqURL)
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := c.calculateBackoff(attempt)
			select {
			case <-ctx.Done():
				return nil, &APIError{Op: "get", Message: "request cancelled", Err: ctx.Err()}
			case <-time.After(delay):
			}
		}

		respBody, err := c.doRequestWithMethod(ctx, http.MethodGet, reqURL, nil)
		if err == nil {
			return json.RawMessage(respBody), nil
		}

		lastErr = err

		var apiErr *APIError
		if errors.As(err, &apiErr) {
			if !apiErr.Retryable {
				return nil, err
			}
			if apiErr.RetryAfter > 0 {
				select {
				case <-ctx.Done():
					return nil, &APIError{Op: "get", Message: "request cancelled", Err: ctx.Err()}
				case <-time.After(apiErr.RetryAfter):
				}
			}
		}
	}

	return nil, &APIError{
		Op:      "get",
		Message: fmt.Sprintf("failed after %d attempts", c.maxRetries+1),
		Err:     lastErr,
	}
}
```

- [ ] **Step 5: Build and test**

Run: `go build ./... && go test ./...`
Expected: Compiles, all tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/adapters/http/client.go
git commit -m "feat: add GET method to HTTP client with retry logic"
```

---

### Task 3: Add APIClient interface and update wiring

**Files:**
- Modify: `internal/core/ports/interfaces.go`
- Modify: `internal/adapters/cli/command.go`
- Modify: `cmd/main.go`

- [ ] **Step 1: Add APIClient interface to ports**

Add after the `ReportSender` interface in `internal/core/ports/interfaces.go`:

```go
// APIClient defines the interface for API communication (read + write)
type APIClient interface {
	ReportSender
	Get(ctx context.Context, path string, params url.Values) (json.RawMessage, error)
}
```

Add `"encoding/json"` and `"net/url"` to the imports.

- [ ] **Step 2: Update CLI struct to accept APIClient**

In `internal/adapters/cli/command.go`, update the struct and constructor:

```go
type CLI struct {
	reportService ports.ReportService
	config        *config.Config
	parserFactory ports.ParserFactory
	apiClient     ports.APIClient
}

func NewCLI(reportService ports.ReportService, cfg *config.Config, parserFactory ports.ParserFactory, apiClient ports.APIClient) *CLI {
	return &CLI{
		reportService: reportService,
		config:        cfg,
		parserFactory: parserFactory,
		apiClient:     apiClient,
	}
}
```

- [ ] **Step 3: Update main.go to pass httpClient as APIClient**

In `cmd/main.go`, update the `NewCLI` call:

```go
cliApp := cli.NewCLI(reportService, cfg, parserFactory, httpClient)
```

- [ ] **Step 4: Add --api-key flag to root command**

In `command.go` inside `CreateRootCommand()`, add a persistent flag for `--api-key` so all subcommands can authenticate:

```go
cmd.PersistentFlags().StringVar(&c.config.APIKey, "api-key", "", "API key for authentication (or set QF_API_KEY)")
```

- [ ] **Step 5: Build and test**

Run: `go build ./... && go test ./...`
Expected: Compiles, all tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/core/ports/interfaces.go internal/adapters/cli/command.go cmd/main.go
git commit -m "feat: add APIClient interface and wire into CLI"
```

---

### Task 4: Add output helper and create suites commands

**Files:**
- Create: `internal/adapters/cli/output.go`
- Create: `internal/adapters/cli/suites.go`
- Modify: `internal/adapters/cli/command.go`

- [ ] **Step 1: Create output helper**

Create `internal/adapters/cli/output.go` with a shared JSON output helper used by all read commands:

```go
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

// fetchAndPrint fetches data from the API and prints it as indented JSON to stdout.
func (c *CLI) fetchAndPrint(path string, params url.Values) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.GetTimeout())
	defer cancel()

	if c.config.GetAPIKey() == "" {
		return fmt.Errorf("API key is required. Set it via --api-key flag or QF_API_KEY environment variable")
	}

	data, err := c.apiClient.Get(ctx, path, params)
	if err != nil {
		return err
	}

	// Pretty-print JSON
	var pretty json.RawMessage
	if err := json.Unmarshal(data, &pretty); err != nil {
		// If not valid JSON, print raw
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	}

	out, err := json.MarshalIndent(pretty, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	}

	fmt.Fprintln(os.Stdout, string(out))
	return nil
}

// addListFlags adds common list flags (page, sort-by, sort-desc) to a command's params builder.
func addPagination(params url.Values, page int) {
	if page > 0 {
		params.Set("page", strconv.Itoa(page))
	}
}

func addSorting(params url.Values, sortBy string, sortDesc bool) {
	if sortBy != "" {
		params.Set("sortBy", sortBy)
	}
	if sortDesc {
		params.Set("sortDir", "desc")
	}
}

func addTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return 30 * time.Second
	}
	return timeout
}
```

- [ ] **Step 2: Create suites.go**

Create `internal/adapters/cli/suites.go`:

```go
package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func (c *CLI) createSuitesCommand() *cobra.Command {
	var (
		page     int
		sortBy   string
		sortDesc bool
		query    string
	)

	cmd := &cobra.Command{
		Use:   "suites",
		Short: "List test suites",
		Long:  "List test suites in the project.",
		Example: `  # List all suites
  qf suites list

  # List suites with search
  qf suites list --query "login"

  # List suites sorted by name
  qf suites list --sort-by name`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List test suites",
		RunE: func(cmd *cobra.Command, args []string) error {
			params := url.Values{}
			addPagination(params, page)
			addSorting(params, sortBy, sortDesc)
			if query != "" {
				params.Set("q", query)
			}
			return c.fetchAndPrint("/api/v1/suites", params)
		},
	}

	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by field")
	listCmd.Flags().BoolVar(&sortDesc, "sort-desc", false, "Sort in descending order")
	listCmd.Flags().StringVar(&query, "query", "", "Search query")

	cmd.AddCommand(listCmd)
	return cmd
}

func (c *CLI) createSuiteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suite",
		Short: "Get test suite details",
		Long:  "Get details for a specific test suite.",
		Example: `  # Get suite by sequence number
  qf suite get 42`,
	}

	getCmd := &cobra.Command{
		Use:   "get <seq>",
		Short: "Get a test suite by sequence number",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/suite/%s", args[0]), nil)
		},
	}

	cmd.AddCommand(getCmd)
	return cmd
}
```

- [ ] **Step 3: Register suites commands in command.go**

In `CreateRootCommand()`, add after the existing `cmd.AddCommand` calls:

```go
cmd.AddCommand(c.createSuitesCommand())
cmd.AddCommand(c.createSuiteCommand())
```

- [ ] **Step 4: Build and test**

Run: `go build ./... && go test ./...`
Expected: Compiles, all tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/cli/output.go internal/adapters/cli/suites.go internal/adapters/cli/command.go
git commit -m "feat: add suites list/get CLI commands"
```

---

### Task 5: Add cases commands

**Files:**
- Create: `internal/adapters/cli/cases.go`
- Modify: `internal/adapters/cli/command.go`

- [ ] **Step 1: Create cases.go**

Create `internal/adapters/cli/cases.go`:

```go
package cli

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func (c *CLI) createCasesCommand() *cobra.Command {
	var (
		suiteSeq int
		query    string
		states   []string
		priority []string
		sortBy   string
		sortDesc bool
	)

	cmd := &cobra.Command{
		Use:   "cases",
		Short: "List test cases",
		Long:  "List test cases in a suite.",
		Example: `  # List cases in suite 5
  qf cases list --suite 5

  # Filter by state
  qf cases list --suite 5 --state passed,failed`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List test cases in a suite",
		RunE: func(cmd *cobra.Command, args []string) error {
			if suiteSeq <= 0 {
				return fmt.Errorf("--suite flag is required")
			}
			params := url.Values{}
			addSorting(params, sortBy, sortDesc)
			if query != "" {
				params.Set("q", query)
			}
			for _, s := range states {
				for _, v := range strings.Split(s, ",") {
					params.Add("state[]", strings.TrimSpace(v))
				}
			}
			for _, p := range priority {
				for _, v := range strings.Split(p, ",") {
					params.Add("priority[]", strings.TrimSpace(v))
				}
			}
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/suite/%d/cases", suiteSeq), params)
		},
	}

	listCmd.Flags().IntVar(&suiteSeq, "suite", 0, "Suite sequence number (required)")
	listCmd.Flags().StringVar(&query, "query", "", "Search query")
	listCmd.Flags().StringSliceVar(&states, "state", nil, "Filter by state (passed,failed,skipped,...)")
	listCmd.Flags().StringSliceVar(&priority, "priority", nil, "Filter by priority (low,medium,high,critical)")
	listCmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by field")
	listCmd.Flags().BoolVar(&sortDesc, "sort-desc", false, "Sort in descending order")

	cmd.AddCommand(listCmd)
	return cmd
}

func (c *CLI) createCaseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "case",
		Short: "Get test case details or steps",
		Long:  "Get details or steps for a specific test case.",
		Example: `  # Get case details
  qf case get 123

  # Get case steps
  qf case steps 123`,
	}

	getCmd := &cobra.Command{
		Use:   "get <seq>",
		Short: "Get a test case by sequence number",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/case/%s", args[0]), nil)
		},
	}

	stepsCmd := &cobra.Command{
		Use:   "steps <seq>",
		Short: "Get steps for a test case",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/case/%s/steps", args[0]), nil)
		},
	}

	cmd.AddCommand(getCmd)
	cmd.AddCommand(stepsCmd)
	return cmd
}
```

- [ ] **Step 2: Register in command.go**

Add to `CreateRootCommand()`:

```go
cmd.AddCommand(c.createCasesCommand())
cmd.AddCommand(c.createCaseCommand())
```

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./...`
Expected: Compiles, all tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/adapters/cli/cases.go internal/adapters/cli/command.go
git commit -m "feat: add cases list/get/steps CLI commands"
```

---

### Task 6: Add plans commands

**Files:**
- Create: `internal/adapters/cli/plans.go`
- Modify: `internal/adapters/cli/command.go`

- [ ] **Step 1: Create plans.go**

Create `internal/adapters/cli/plans.go`:

```go
package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func (c *CLI) createPlansCommand() *cobra.Command {
	var (
		page     int
		query    string
		sortBy   string
		sortDesc bool
	)

	cmd := &cobra.Command{
		Use:   "plans",
		Short: "List test plans",
		Long:  "List test plans in the project.",
		Example: `  # List all test plans
  qf plans list

  # Search test plans
  qf plans list --query "regression"`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List test plans",
		RunE: func(cmd *cobra.Command, args []string) error {
			params := url.Values{}
			addPagination(params, page)
			addSorting(params, sortBy, sortDesc)
			if query != "" {
				params.Set("q", query)
			}
			return c.fetchAndPrint("/api/v1/test-plans", params)
		},
	}

	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().StringVar(&query, "query", "", "Search query")
	listCmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by field")
	listCmd.Flags().BoolVar(&sortDesc, "sort-desc", false, "Sort in descending order")

	cmd.AddCommand(listCmd)
	return cmd
}

func (c *CLI) createPlanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Get test plan details or cases",
		Long:  "Get details or cases for a specific test plan.",
		Example: `  # Get plan details
  qf plan get 5

  # Get plan cases
  qf plan cases 5`,
	}

	getCmd := &cobra.Command{
		Use:   "get <seq>",
		Short: "Get a test plan by sequence number",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/test-plan/%s", args[0]), nil)
		},
	}

	casesCmd := &cobra.Command{
		Use:   "cases <seq>",
		Short: "Get cases in a test plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/test-plan/%s/cases", args[0]), nil)
		},
	}

	cmd.AddCommand(getCmd)
	cmd.AddCommand(casesCmd)
	return cmd
}
```

- [ ] **Step 2: Register in command.go**

Add to `CreateRootCommand()`:

```go
cmd.AddCommand(c.createPlansCommand())
cmd.AddCommand(c.createPlanCommand())
```

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./...`
Expected: Compiles, all tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/adapters/cli/plans.go internal/adapters/cli/command.go
git commit -m "feat: add plans list/get/cases CLI commands"
```

---

### Task 7: Add launches commands

**Files:**
- Create: `internal/adapters/cli/launches.go`
- Modify: `internal/adapters/cli/command.go`

- [ ] **Step 1: Create launches.go**

Create `internal/adapters/cli/launches.go`:

```go
package cli

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

func (c *CLI) createLaunchesCommand() *cobra.Command {
	var (
		page        int
		milestoneSeq int
		environment string
		sortBy      string
		sortDesc    bool
	)

	cmd := &cobra.Command{
		Use:   "launches",
		Short: "List test launches",
		Long:  "List test execution launches in the project.",
		Example: `  # List all launches
  qf launches list

  # Filter by milestone
  qf launches list --milestone 3

  # Filter by environment
  qf launches list --environment prod`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List test launches",
		RunE: func(cmd *cobra.Command, args []string) error {
			params := url.Values{}
			addPagination(params, page)
			addSorting(params, sortBy, sortDesc)
			if milestoneSeq > 0 {
				params.Set("milestone", strconv.Itoa(milestoneSeq))
			}
			if environment != "" {
				params.Add("environments[]", environment)
			}
			return c.fetchAndPrint("/api/v1/launches", params)
		},
	}

	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().IntVar(&milestoneSeq, "milestone", 0, "Filter by milestone sequence number")
	listCmd.Flags().StringVar(&environment, "environment", "", "Filter by environment UID")
	listCmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by field")
	listCmd.Flags().BoolVar(&sortDesc, "sort-desc", false, "Sort in descending order")

	cmd.AddCommand(listCmd)
	return cmd
}

func (c *CLI) createLaunchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "launch",
		Short: "Get launch details",
		Long:  "Get details for a specific test launch.",
		Example: `  # Get launch by sequence number
  qf launch get 10`,
	}

	getCmd := &cobra.Command{
		Use:   "get <seq>",
		Short: "Get a launch by sequence number",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/launch/%s", args[0]), nil)
		},
	}

	cmd.AddCommand(getCmd)
	return cmd
}
```

- [ ] **Step 2: Register in command.go**

Add to `CreateRootCommand()`:

```go
cmd.AddCommand(c.createLaunchesCommand())
cmd.AddCommand(c.createLaunchCommand())
```

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./...`
Expected: Compiles, all tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/adapters/cli/launches.go internal/adapters/cli/command.go
git commit -m "feat: add launches list/get CLI commands"
```

---

### Task 8: Add defects commands

**Files:**
- Create: `internal/adapters/cli/defects.go`
- Modify: `internal/adapters/cli/command.go`

- [ ] **Step 1: Create defects.go**

Create `internal/adapters/cli/defects.go`:

```go
package cli

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func (c *CLI) createDefectsCommand() *cobra.Command {
	var (
		page     int
		severity []string
		status   []string
		sortBy   string
		sortDesc bool
	)

	cmd := &cobra.Command{
		Use:   "defects",
		Short: "List defects",
		Long:  "List defects detected in the project.",
		Example: `  # List all defects
  qf defects list

  # Filter by severity
  qf defects list --severity critical,high

  # Filter by status
  qf defects list --status active`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List defects",
		RunE: func(cmd *cobra.Command, args []string) error {
			params := url.Values{}
			addPagination(params, page)
			addSorting(params, sortBy, sortDesc)
			for _, s := range severity {
				for _, v := range strings.Split(s, ",") {
					params.Add("severity[]", strings.TrimSpace(v))
				}
			}
			for _, s := range status {
				for _, v := range strings.Split(s, ",") {
					params.Add("status[]", strings.TrimSpace(v))
				}
			}
			return c.fetchAndPrint("/api/v1/defects", params)
		},
	}

	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().StringSliceVar(&severity, "severity", nil, "Filter by severity (critical,high,medium,low)")
	listCmd.Flags().StringSliceVar(&status, "status", nil, "Filter by status (active,closed,...)")
	listCmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by field")
	listCmd.Flags().BoolVar(&sortDesc, "sort-desc", false, "Sort in descending order")

	cmd.AddCommand(listCmd)
	return cmd
}

func (c *CLI) createDefectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "defect",
		Short: "Get defect details",
		Long:  "Get details for a specific defect.",
		Example: `  # Get defect by sequence number
  qf defect get 7`,
	}

	getCmd := &cobra.Command{
		Use:   "get <seq>",
		Short: "Get a defect by sequence number",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/defect/%s", args[0]), nil)
		},
	}

	cmd.AddCommand(getCmd)
	return cmd
}
```

- [ ] **Step 2: Register in command.go**

Add to `CreateRootCommand()`:

```go
cmd.AddCommand(c.createDefectsCommand())
cmd.AddCommand(c.createDefectCommand())
```

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./...`
Expected: Compiles, all tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/adapters/cli/defects.go internal/adapters/cli/command.go
git commit -m "feat: add defects list/get CLI commands"
```

---

### Task 9: Add clusters commands

**Files:**
- Create: `internal/adapters/cli/clusters.go`
- Modify: `internal/adapters/cli/command.go`

- [ ] **Step 1: Create clusters.go**

Create `internal/adapters/cli/clusters.go`:

```go
package cli

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func (c *CLI) createClustersCommand() *cobra.Command {
	var (
		page     int
		severity []string
		sortBy   string
		sortDesc bool
	)

	cmd := &cobra.Command{
		Use:   "clusters",
		Short: "List failure clusters",
		Long:  "List failure clusters detected in the project.",
		Example: `  # List all failure clusters
  qf clusters list

  # Filter by severity
  qf clusters list --severity critical`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List failure clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
			params := url.Values{}
			addPagination(params, page)
			addSorting(params, sortBy, sortDesc)
			for _, s := range severity {
				for _, v := range strings.Split(s, ",") {
					params.Add("severity[]", strings.TrimSpace(v))
				}
			}
			return c.fetchAndPrint("/api/v1/clusters", params)
		},
	}

	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().StringSliceVar(&severity, "severity", nil, "Filter by severity (critical,high,medium,low)")
	listCmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by field")
	listCmd.Flags().BoolVar(&sortDesc, "sort-desc", false, "Sort in descending order")

	cmd.AddCommand(listCmd)
	return cmd
}

func (c *CLI) createClusterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Get cluster details",
		Long:  "Get details for a specific failure cluster.",
		Example: `  # Get cluster by ID
  qf cluster get 15`,
	}

	getCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a failure cluster by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/cluster/%s", args[0]), nil)
		},
	}

	cmd.AddCommand(getCmd)
	return cmd
}
```

- [ ] **Step 2: Register in command.go**

Add to `CreateRootCommand()`:

```go
cmd.AddCommand(c.createClustersCommand())
cmd.AddCommand(c.createClusterCommand())
```

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./...`
Expected: Compiles, all tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/adapters/cli/clusters.go internal/adapters/cli/command.go
git commit -m "feat: add clusters list/get CLI commands"
```

---

### Task 10: Add milestones commands

**Files:**
- Create: `internal/adapters/cli/milestones.go`
- Modify: `internal/adapters/cli/command.go`

- [ ] **Step 1: Create milestones.go**

Create `internal/adapters/cli/milestones.go`:

```go
package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func (c *CLI) createMilestonesCommand() *cobra.Command {
	var (
		page     int
		query    string
		sortBy   string
		sortDesc bool
	)

	cmd := &cobra.Command{
		Use:   "milestones",
		Short: "List milestones",
		Long:  "List milestones/releases in the project.",
		Example: `  # List all milestones
  qf milestones list

  # Search milestones
  qf milestones list --query "v2.0"`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List milestones",
		RunE: func(cmd *cobra.Command, args []string) error {
			params := url.Values{}
			addPagination(params, page)
			addSorting(params, sortBy, sortDesc)
			if query != "" {
				params.Set("q", query)
			}
			return c.fetchAndPrint("/api/v1/milestones", params)
		},
	}

	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().StringVar(&query, "query", "", "Search query")
	listCmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by field")
	listCmd.Flags().BoolVar(&sortDesc, "sort-desc", false, "Sort in descending order")

	cmd.AddCommand(listCmd)
	return cmd
}

func (c *CLI) createMilestoneCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "milestone",
		Short: "Get milestone details",
		Long:  "Get details for a specific milestone.",
		Example: `  # Get milestone by sequence number
  qf milestone get 3`,
	}

	getCmd := &cobra.Command{
		Use:   "get <seq>",
		Short: "Get a milestone by sequence number",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/milestone/%s", args[0]), nil)
		},
	}

	cmd.AddCommand(getCmd)
	return cmd
}
```

- [ ] **Step 2: Register in command.go**

Add to `CreateRootCommand()`:

```go
cmd.AddCommand(c.createMilestonesCommand())
cmd.AddCommand(c.createMilestoneCommand())
```

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./...`
Expected: Compiles, all tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/adapters/cli/milestones.go internal/adapters/cli/command.go
git commit -m "feat: add milestones list/get CLI commands"
```

---

### Task 11: Update root command help text and final verification

**Files:**
- Modify: `internal/adapters/cli/command.go`

- [ ] **Step 1: Update root command Long description**

In `CreateRootCommand()`, update the `Long` text to include the new commands:

```go
Long: `qf is a CLI tool for Qualflare — parse test results and manage test data.

Collect & Parse:
  collect        Collect test results and send to Qualflare
  validate       Validate test result files
  list-formats   List supported test frameworks

Test Management:
  suites/suite         List and view test suites
  cases/case           List and view test cases and steps
  plans/plan           List and view test plans
  launches/launch      List and view test launches
  defects/defect       List and view defects
  clusters/cluster     List and view failure clusters
  milestones/milestone List and view milestones

Supported frameworks:
  Unit Testing:    junit, python, golang, jest, mocha, rspec, phpunit
  BDD:             cucumber, karate
  UI/E2E Testing:  playwright, cypress, selenium, testcafe
  API Testing:     newman, k6
  Security:        zap, trivy, snyk, sonarqube`,
```

- [ ] **Step 2: Full build and test**

Run: `go build ./... && go test ./...`
Expected: Compiles, all tests pass

- [ ] **Step 3: Verify help output**

Run: `go run cmd/main.go --help`
Expected: Shows all commands including new suites, cases, plans, launches, defects, clusters, milestones commands

- [ ] **Step 4: Verify subcommand help**

Run: `go run cmd/main.go suites list --help`
Expected: Shows page, sort-by, sort-desc, query flags

Run: `go run cmd/main.go case --help`
Expected: Shows get and steps subcommands

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/cli/command.go
git commit -m "docs: update root command help text with new commands"
```
