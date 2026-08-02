# Agent Guide

`autoreview` is a Go-only CLI and standalone agent skill for structured,
independent code review.

## Commands

- `mise run verify`
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./cmd/autoreview`
- `go run ./cmd/autoreview --version`

## Boundaries

- Keep the runtime entirely in Go. Do not add Python files, Python workflow
  steps, shell-based runtime helpers, or Go-to-Python wrappers.
- Runtime dependencies are Git, TruffleHog, and the selected review harness.
- The CLI reviews and reports; it never edits reviewed source, runs tests,
  commits, pushes, or invokes nested review workflows.
- Keep provider execution, protocol validation, review policy, configuration,
  and report rendering in separate packages.
- Parse provider output at the boundary and fail closed on ambiguous or invalid
  results.
- Keep the bundled skill thin and aligned with the released CLI contract.
- Treat JSON output as a stable machine contract and terminal output as a
  separate renderer.

## Pull Requests

- Work from a GitHub issue and keep each PR focused.
- Exercise the built CLI while implementing command behavior.
- Run `mise run verify` before review.
- Address actionable AI-review feedback and resolve review threads before
  merging to `main`.
- Use conventional commits and signed commits.
