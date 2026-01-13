# Technology Stack

**Analysis Date:** 2026-01-13

## Languages

**Primary:**
- Go 1.23 - All application code

**Secondary:**
- Shell (Makefile) - Build automation

## Runtime

**Environment:**
- Go 1.23 runtime
- Cross-platform: Linux, macOS (Darwin), Windows

**Package Manager:**
- Go Modules - go.mod, go.sum present
- Binary distribution - Single executable distribution

## Frameworks

**Core:**
- Cobra v1.8.0 - CLI framework (github.com/spf13/cobra)
- Clean Architecture - Port and Adapter pattern

**Testing:**
- Not detected - No formal test framework currently

**Build/Dev:**
- Go build toolchain - Native compilation
- GoReleaser v2 - Multi-platform release automation

## Key Dependencies

**Critical:**
- github.com/spf13/cobra v1.8.0 - CLI command framework
- github.com/spf13/pflag v1.0.5 - Flag parsing (indirect)
- github.com/inconshreveable/mousetrap v1.1.0 - Windows exec protection (indirect)

**Infrastructure:**
- Go standard library - net/http, encoding/json, io, context, os

## Configuration

**Environment:**
- Environment variables with QF_ prefix
- Key configs: QF_API_KEY, QF_API_ENDPOINT, QF_ENVIRONMENT required
- Git integration: Supports GitHub, GitLab, Bitbucket CI variables
- Default config: internal/config/config.go

**Build:**
- .goreleaser.yml - Release configuration
- Makefile - Build automation
- go.mod - Go module definition

## Platform Requirements

**Development:**
- Go 1.23+ required
- make (for build automation)
- Any platform with Go toolchain

**Production:**
- Standalone binary (qf)
- Docker container (ghcr.io/qualflare/qf)
- Homebrew package (qualflare/tap/qf)
- No runtime dependencies

---

*Stack analysis: 2026-01-13*
*Update after major dependency changes*
