# @qualflare/cli

npm distribution of the **Qualflare CLI** (`qf`) — upload test results to [Qualflare](https://qualflare.com) from any CI pipeline or local machine. Supports 23 testing frameworks with automatic format detection.

## Install

```bash
npm install -g @qualflare/cli
qf version
```

Requires Node ≥ 16.

### No install scripts

This package runs **no `postinstall` and no install script of any kind**, and has no runtime dependencies.

The platform-native `qf` binary ships as a normal npm package — `@qualflare/cli-darwin-arm64`, `@qualflare/cli-linux-x64`, and so on — listed in `optionalDependencies`. Your package manager reads each one's `os`/`cpu` fields and downloads only the one matching your machine. `bin/qf` is a small launcher that resolves that package and executes the binary.

This means:

- Nothing is downloaded or executed while installing.
- The binary is covered by the `integrity` hash in your lockfile, verified by your package manager on every install.
- `npm install --ignore-scripts` works with no special handling.
- A private registry mirror proxies the binary like any other tarball.

Prebuilt binaries are provided for:

| OS | Architectures | Package |
|----|---------------|---------|
| macOS | arm64, x64 | `@qualflare/cli-darwin-{arm64,x64}` |
| Linux | arm64, x64 | `@qualflare/cli-linux-{arm64,x64}` |
| Windows | arm64, x64 | `@qualflare/cli-win32-{arm64,x64}` |

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

Because there is no install script, nothing in CI needs to reach GitHub Releases at install time — your registry is the only host involved.

## Restricted networks and air-gapped installs

The binary is an ordinary npm tarball, so a registry mirror (Verdaccio, Artifactory, Nexus, …) covers it with no extra configuration — point npm at your mirror and both `@qualflare/cli` and the platform package come from it.

If you already have a `qf` binary and want to use it directly:

```bash
QF_BINARY=/opt/qualflare/qf qf version
```

`QF_BINARY` takes an absolute path to a `qf` executable and bypasses package resolution entirely.

> **Migrating from ≤ 0.1.11.** `QUALFLARE_CLI_SKIP_INSTALL` and `QUALFLARE_CLI_BASE_URL` no longer exist and are safe to delete. There is no download to skip, and mirroring is handled by your npm registry rather than a separate base URL. Use `QF_BINARY` if you were pointing at a self-hosted binary.

## Troubleshooting

**`The package "@qualflare/cli-<os>-<arch>" could not be found`**

Three usual causes:

1. **Installed with `--omit=optional` or `--no-optional`.** Drop the flag — the platform binary *is* an optional dependency.
2. **A stale lockfile.** npm has a long-standing bug where platform-specific optional dependencies are dropped from `package-lock.json` ([npm/cli#4828](https://github.com/npm/cli/issues/4828)). Fix:
   ```bash
   rm -rf node_modules package-lock.json && npm install
   ```
3. **`node_modules` built on another platform** — a Docker bind-mount, a WSL/Windows shared directory, or Rosetta. Reinstall on the machine that will run it. The launcher detects this case and says so.

**Unsupported platform**

Binaries cover macOS, Linux, and Windows on arm64 and x64. Elsewhere, build from source or use another channel below.

## Other install methods

Homebrew, Docker (GHCR + Docker Hub), and direct binary downloads are documented in the [main README](https://github.com/Qualflare/qualflare-cli#installation). Those channels ship a `checksums.txt` and a keyless [cosign](https://github.com/sigstore/cosign) signature over it, plus a CycloneDX SBOM per archive.

## Links

- **Documentation:** https://docs.qualflare.com
- **Source & issues:** https://github.com/Qualflare/qualflare-cli
- **Releases / changelog:** https://github.com/Qualflare/qualflare-cli/releases

Licensed under the [Apache License 2.0](https://github.com/Qualflare/qualflare-cli/blob/main/LICENSE).
