# Framework Output Schema

This document describes the JSON output schema for each supported testing framework in qualflare-cli.

## Suite Structure

Every parsed test result produces a `Suite` with the following structure:

```json
{
  "name": "string",
  "category": "unit|bdd|e2e|api|security",
  "total": 0,
  "passed": 0,
  "failed": 0,
  "skipped": 0,
  "errors": 0,
  "flaky": 0,
  "assertions": 0,
  "retries": 0,
  "duration": 0,
  "timestamp": "2024-01-15T10:30:00Z",
  "properties": {},
  "cases": []
}
```

### Field Descriptions

| Field | Type | Description | Used By |
|-------|------|-------------|---------|
| `name` | string | Suite name | All |
| `category` | string | Test category: unit, bdd, e2e, api, security | All |
| `total` | int | Total test count | All |
| `passed` | int | Passed test count | All |
| `failed` | int | Failed test count | All |
| `skipped` | int | Skipped test count | All |
| `errors` | int | Error count | JUnit, Pytest |
| `flaky` | int | Flaky test count (passed after retry) | Playwright |
| `assertions` | int | Total assertions executed | Newman, k6 |
| `retries` | int | Total retry attempts | Playwright |
| `properties` | object | Framework-specific key-value pairs | All |

> **Note:** Browser and platform information for E2E tests are stored in `properties` and should be extracted to `Launch.Browser` when creating a Launch.

---

## Unit Test Frameworks

### JUnit (`junit`)
**Category:** `unit`

**Suite Properties:** None

**Case Properties:**
| Property | Description |
|----------|-------------|
| `file` | Source file path |

---

### Pytest (`python`)
**Category:** `unit`

**Suite Properties:** Custom properties from pytest XML

**Case Properties:**
| Property | Description |
|----------|-------------|
| `file` | Source file path |
| `line` | Line number |

---

### Go Test (`golang`)
**Category:** `unit`

**Suite Properties:** None

**Case Properties:**
| Property | Description |
|----------|-------------|
| `package` | Go package name |

---

### Jest (`jest`)
**Category:** `unit`

**Suite Properties:** None

**Case Properties:**
| Property | Description |
|----------|-------------|
| `file` | Source file path |

---

### Mocha (`mocha`)
**Category:** `unit`

**Suite Properties:** None

**Case Properties:**
| Property | Description |
|----------|-------------|
| `file` | Source file path |
| `speed` | Test speed (fast/medium/slow) |

---

### RSpec (`rspec`)
**Category:** `unit`

**Suite Properties:** None

**Case Properties:**
| Property | Description |
|----------|-------------|
| `file` | Source file path |
| `line_number` | Line number |

---

### PHPUnit (`phpunit`)
**Category:** `unit`

**Suite Properties:** None

**Case Properties:**
| Property | Description |
|----------|-------------|
| `file` | Source file path |
| `line` | Line number |

---

## BDD Frameworks

### Cucumber (`cucumber`)
**Category:** `bdd`

**Suite Properties:** None

**Case Properties:**
| Property | Description |
|----------|-------------|
| `feature` | Feature name |
| `uri` | Feature file URI |

**Case Steps:** Each case contains `steps` array with:
- `name`: Step text
- `keyword`: Given/When/Then
- `status`: passed/failed/skipped
- `duration`: Step duration
- `error`: Error message if failed
- `location`: Step definition location

---

### Karate (`karate`)
**Category:** `bdd`

**Suite Properties:** None

**Case Properties:**
| Property | Description |
|----------|-------------|
| `feature` | Feature name |

**Case Steps:** Same as Cucumber

---

## E2E Testing Frameworks

### Playwright (`playwright`)
**Category:** `e2e`

**Suite Fields:**
| Field | Source |
|-------|--------|
| `flaky` | report.Stats.Flaky |
| `retries` | Count of tests with multiple results |

**Suite Properties:**
| Property | Description |
|----------|-------------|
| `browser` | Collected from test.ProjectName (chromium, firefox, webkit) |

**Case Properties:**
| Property | Description |
|----------|-------------|
| `file` | Spec file path |
| `project` | Project/browser name |

**Case Attachments:** Screenshots, videos, traces

---

### Cypress (`cypress`)
**Category:** `e2e`

**Suite Fields:** None (browser info not in mochawesome format)

**Case Properties:**
| Property | Description |
|----------|-------------|
| `file` | Spec file path |
| `fullTitle` | Full test title including suite |
| `speed` | Test speed |

---

### Selenium (`selenium`)
**Category:** `e2e`

**Suite Properties:**
| Property | Description |
|----------|-------------|
| `browser` | Browser name from report.Browser |
| `platform` | Platform/OS from report.Platform |

**Case Properties:**
| Property | Description |
|----------|-------------|
| `methodName` | Test method name |
| `browser` | Browser for this test |

---

### TestCafe (`testcafe`)
**Category:** `e2e`

**Suite Properties:**
| Property | Description |
|----------|-------------|
| `browser` | First UserAgent from report.UserAgents |

**Case Properties:**
| Property | Description |
|----------|-------------|
| `fixture` | Fixture name |
| `path` | Fixture file path |

---

## API Testing Frameworks

### Newman (`newman`)
**Category:** `api`

**Suite Fields:**
| Field | Source |
|-------|--------|
| `assertions` | report.Run.Stats.Assertions.Total |

**Suite Properties:** None

**Case Properties:**
| Property | Description |
|----------|-------------|
| `method` | HTTP method (GET, POST, etc.) |
| `responseCode` | HTTP response code |
| `responseTime` | Response time in ms |

---

### k6 (`k6`)
**Category:** `api`

**Suite Fields:**
| Field | Source |
|-------|--------|
| `assertions` | Sum of all check passes + fails |

**Suite Properties:**
| Property | Description |
|----------|-------------|
| `http_req_duration_avg` | Average request duration |
| `http_req_duration_p95` | 95th percentile request duration |
| `http_reqs_count` | Total HTTP requests |
| `iterations_count` | Total iterations |
| `vus_max` | Maximum virtual users |

**Case Properties (for checks):**
| Property | Description |
|----------|-------------|
| `path` | Check path |
| `passes` | Pass count |
| `fails` | Fail count |
| `passRate` | Pass percentage |
| `group` | Group path |

**Case Properties (for thresholds):**
| Property | Description |
|----------|-------------|
| `avg`, `p(95)`, etc. | Metric values |

---

## Security Testing Frameworks

### Trivy (`trivy`)
**Category:** `security`

**Suite Properties:**
| Property | Description |
|----------|-------------|
| `artifactName` | Scanned artifact name |
| `artifactType` | Artifact type (image, filesystem, etc.) |

**Case Fields:**
| Field | Description |
|-------|-------------|
| `severity` | critical, high, medium, low, unknown |

**Case Properties (vulnerabilities):**
| Property | Description |
|----------|-------------|
| `package` | Package name |
| `installedVersion` | Current version |
| `fixedVersion` | Version with fix |
| `severity` | Severity level |
| `url` | Reference URL |
| `cvss_*` | CVSS scores by source |

**Case Properties (misconfigurations):**
| Property | Description |
|----------|-------------|
| `type` | Misconfiguration type |
| `severity` | Severity level |
| `resolution` | How to fix |
| `url` | Reference URL |

---

### Snyk (`snyk`)
**Category:** `security`

**Suite Properties:**
| Property | Description |
|----------|-------------|
| `projectName` | Project name |
| `packageManager` | npm, pip, maven, etc. |
| `dependencyCount` | Total dependencies |
| `uniqueCount` | Unique vulnerabilities |
| `summary` | Summary text |

**Case Fields:**
| Field | Description |
|-------|-------------|
| `severity` | critical, high, medium, low |

**Case Properties:**
| Property | Description |
|----------|-------------|
| `package` | Package name |
| `version` | Vulnerable version |
| `severity` | Severity level |
| `cvssScore` | CVSS score |
| `isPatchable` | Can be patched |
| `isUpgradable` | Can be upgraded |
| `language` | Programming language |
| `packageManager` | Package manager |
| `fixedIn` | Fixed version |
| `dependencyPath` | Dependency chain |

---

### OWASP ZAP (`zap`)
**Category:** `security`

**Suite Properties:**
| Property | Description |
|----------|-------------|
| `zapVersion` | ZAP version |

**Case Fields:**
| Field | Description |
|-------|-------------|
| `severity` | high, medium, low, info |

**Case Properties:**
| Property | Description |
|----------|-------------|
| `host` | Target host |
| `port` | Target port |
| `riskCode` | Risk code (0-3) |
| `riskDesc` | Risk description |
| `confidence` | Confidence level |
| `cweId` | CWE identifier |
| `wascId` | WASC identifier |
| `solution` | Recommended fix |
| `reference` | Reference links |
| `instanceCount` | Number of instances |
| `affectedURL` | First affected URL |
| `method` | HTTP method |

---

### SonarQube (`sonarqube`)
**Category:** `security`

**Suite Properties:**
| Property | Description |
|----------|-------------|
| `total` | Total issues |
| `effortTotal` | Total effort to fix (minutes) |

**Case Fields:**
| Field | Description |
|-------|-------------|
| `severity` | critical, medium, low, info |

**Case Properties:**
| Property | Description |
|----------|-------------|
| `rule` | Rule key |
| `ruleName` | Rule name |
| `severity` | BLOCKER, CRITICAL, MAJOR, MINOR, INFO |
| `type` | BUG, VULNERABILITY, CODE_SMELL, SECURITY_HOTSPOT |
| `status` | Issue status |
| `effort` | Effort to fix |
| `component` | Component key |
| `project` | Project key |
| `line` | Line number |
| `assignee` | Assigned user |

---

## Severity Mapping

| Framework | Source Values | Maps To |
|-----------|--------------|---------|
| Trivy | CRITICAL, HIGH, MEDIUM, LOW, UNKNOWN | critical, high, medium, low, unknown |
| Snyk | critical, high, medium, low | critical, high, medium, low |
| ZAP | riskCode: 3, 2, 1, 0 | high, medium, low, info |
| SonarQube | BLOCKER, CRITICAL | critical |
| SonarQube | MAJOR | medium |
| SonarQube | MINOR | low |
| SonarQube | INFO | info |
