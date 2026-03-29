# MCP Server to CLI Migration — Design Spec

## Context

The Qualflare MCP server (`mcp-server/`) exposes 16 tools across 8 domains for reading and writing test management data. We are migrating these capabilities into the CLI tool (`qualflare-cli/`) so users and AI agents interact with Qualflare exclusively through `qf` commands. The MCP server will be retired after migration.

## Decisions

- **Command structure**: Noun-verb subcommands (`qf suites list`, `qf suite get 42`)
- **Auth**: Raw access token via `QF_TOKEN` header (same as collect). No JWT exchange.
- **API**: All endpoints are on the public-api-service which already supports access token auth
- **Output**: JSON only to stdout. No table formatting.
- **Base URL**: Config changes from full collect URL to base URL. Collect and read commands each append their path.

## Command Reference

### Suites
```
qf suites list [--page N] [--sort-by field] [--sort-desc] [--query text]
qf suite get <seq>
```

### Cases
```
qf cases list --suite <seq> [--query text] [--state passed,failed,...] [--priority low,medium,high,critical] [--sort-by field] [--sort-desc]
qf case get <seq>
qf case steps <seq>
```

### Test Plans
```
qf plans list [--page N] [--query text] [--sort-by field] [--sort-desc]
qf plan get <seq>
qf plan cases <seq>
```

### Launches
```
qf launches list [--page N] [--milestone N] [--environment uid] [--sort-by field] [--sort-desc]
qf launch get <seq>
```

### Defects
```
qf defects list [--page N] [--severity critical,high,...] [--status active,closed,...] [--sort-by field] [--sort-desc]
qf defect get <seq>
```

### Clusters
```
qf clusters list [--page N] [--severity critical,high,...] [--sort-by field] [--sort-desc]
qf cluster get <id>
```

### Milestones
```
qf milestones list [--page N] [--query text] [--sort-by field] [--sort-desc]
qf milestone get <seq>
```

## Architecture

### HTTP Client Extension

The existing `Client` in `internal/adapters/http/client.go` currently only supports POST (for collect). We add a `Get` method:

```go
func (c *Client) Get(ctx context.Context, path string, params url.Values) (json.RawMessage, error)
```

- Constructs URL: `baseEndpoint + path + "?" + params.Encode()`
- Sets `QF_TOKEN` header, `User-Agent`, `Accept: application/json`
- Uses same retry logic as `SendReport`
- Returns raw JSON response body

### Config Changes

- Default `APIEndpoint` changes from `https://api.qualflare.com/api/v1/collect` to `https://api.qualflare.com`
- `SendReport` appends `/api/v1/collect` to the base endpoint
- `Get` method uses the endpoint + provided path (e.g., `/api/v1/suites`)
- `QF_API_ENDPOINT` env var now expects the base URL

### API Client Interface

Add to `internal/core/ports/interfaces.go`:

```go
type APIClient interface {
    ReportSender
    Get(ctx context.Context, path string, params url.Values) (json.RawMessage, error)
}
```

### CLI File Organization

One file per domain to keep files focused and maintainable:

```
internal/adapters/cli/
├── command.go       # Root command + collect + validate + version + list-formats (existing)
├── suites.go        # qf suites list, qf suite get
├── cases.go         # qf cases list, qf case get, qf case steps
├── plans.go         # qf plans list, qf plan get, qf plan cases
├── launches.go      # qf launches list, qf launch get
├── defects.go       # qf defects list, qf defect get
├── clusters.go      # qf clusters list, qf cluster get
└── milestones.go    # qf milestones list, qf milestone get
```

### CLI Struct Extension

The `CLI` struct needs access to the API client for GET requests:

```go
type CLI struct {
    reportService ports.ReportService
    config        *config.Config
    parserFactory ports.ParserFactory
    apiClient     ports.APIClient   // NEW: for read commands
}
```

### Command Pattern

Each domain follows the same pattern:

```go
// suites.go
func (c *CLI) createSuitesCommand() *cobra.Command {
    // "qf suites" parent with "list" subcommand
}

func (c *CLI) createSuiteCommand() *cobra.Command {
    // "qf suite" parent with "get" subcommand
}
```

Common flags across all list commands: `--page`, `--sort-by`, `--sort-desc`
Domain-specific flags: `--query`, `--severity`, `--status`, `--suite`, `--milestone`, `--environment`

### Output

All commands print JSON to stdout via `json.MarshalIndent`. Errors go to stderr. This makes output pipeable and parseable:

```bash
qf suites list | jq '.suites[].name'
qf case get 42 | jq '.status'
```

### API Path Mapping

| CLI Command | API Path |
|---|---|
| `qf suites list` | `GET /api/v1/suites` |
| `qf suite get <seq>` | `GET /api/v1/suite/<seq>` |
| `qf cases list --suite <seq>` | `GET /api/v1/suite/<seq>/cases` |
| `qf case get <seq>` | `GET /api/v1/case/<seq>` |
| `qf case steps <seq>` | `GET /api/v1/case/<seq>/steps` |
| `qf plans list` | `GET /api/v1/test-plans` |
| `qf plan get <seq>` | `GET /api/v1/test-plan/<seq>` |
| `qf plan cases <seq>` | `GET /api/v1/test-plan/<seq>/cases` |
| `qf launches list` | `GET /api/v1/launches` |
| `qf launch get <seq>` | `GET /api/v1/launch/<seq>` |
| `qf defects list` | `GET /api/v1/defects` |
| `qf defect get <seq>` | `GET /api/v1/defect/<seq>` |
| `qf clusters list` | `GET /api/v1/clusters` |
| `qf cluster get <id>` | `GET /api/v1/cluster/<id>` |
| `qf milestones list` | `GET /api/v1/milestones` |
| `qf milestone get <seq>` | `GET /api/v1/milestone/<seq>` |

## Files to Modify

### Existing files
- `internal/adapters/http/client.go` — Add `Get` method, change `SendReport` to append `/api/v1/collect`
- `internal/adapters/cli/command.go` — Register new command groups, update CLI struct
- `internal/config/config.go` — Change default endpoint to base URL
- `internal/core/ports/interfaces.go` — Add `APIClient` interface
- `cmd/main.go` — Pass API client to CLI constructor

### New files (7)
- `internal/adapters/cli/suites.go`
- `internal/adapters/cli/cases.go`
- `internal/adapters/cli/plans.go`
- `internal/adapters/cli/launches.go`
- `internal/adapters/cli/defects.go`
- `internal/adapters/cli/clusters.go`
- `internal/adapters/cli/milestones.go`

## Verification

1. `go build ./...` — compiles
2. `go test ./...` — all existing tests pass
3. Manual test: `qf suites list --api-key <key>` against a real API
4. Manual test: `qf suite get 1 --api-key <key>` returns JSON
5. Pipe test: `qf launches list | jq '.launches | length'` works
6. Error test: invalid API key returns friendly error message
