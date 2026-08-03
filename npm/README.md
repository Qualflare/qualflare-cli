# @qualflare/cli

npm distribution of the **Qualflare CLI** (`qf`) — upload test results to [Qualflare](https://qualflare.com) from any CI pipeline or local machine. Supports 23 testing frameworks with automatic format detection.

This package is a thin installer with **no runtime dependencies**: on `npm install` it downloads the platform-native `qf` binary for your OS/architecture from the matching [GitHub release](https://github.com/Qualflare/qualflare-cli/releases), verifies its SHA-256 checksum against the release `checksums.txt`, and exposes it as the `qf` command.

## Install

```bash
npm install -g @qualflare/cli
qf version
```

Requires Node ≥ 16. Prebuilt binaries are provided for:

| OS | Architectures |
|----|---------------|
| macOS | arm64, x64 |
| Linux | arm64, x64 |
| Windows | arm64, x64 |

## Quick start

```bash
# Save your project API token (Project → Settings → Access Tokens in the dashboard)
qf login myapp qf_your_token_here

# Upload a report — the framework is auto-detected from the file
qf myapp collect path/to/results.xml
```

## In CI

```bash
qf login ci "$QF_TOKEN" --force
qf ci collect test-results/*.xml
```

Set `QF_TOKEN` from a secret, plus `QF_BRANCH` / `QF_COMMIT` for git context (auto-detected from common CI variables when unset).

## Environment variables (install-time)

| Variable | Purpose |
|----------|---------|
| `QUALFLARE_CLI_SKIP_INSTALL=1` | Skip the postinstall binary download (e.g. restricted/offline installs) |
| `QUALFLARE_CLI_BASE_URL` | Override the release download base URL (mirrors / air-gapped setups) |

## Other install methods

Homebrew, Docker (GHCR + Docker Hub), and direct binary downloads are documented in the [main README](https://github.com/Qualflare/qualflare-cli#installation).

## Links

- **Documentation:** https://docs.qualflare.com
- **Source & issues:** https://github.com/Qualflare/qualflare-cli
- **Releases / changelog:** https://github.com/Qualflare/qualflare-cli/releases

Licensed under the [Apache License 2.0](https://github.com/Qualflare/qualflare-cli/blob/main/LICENSE).
