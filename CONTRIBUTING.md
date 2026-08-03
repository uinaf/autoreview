# Contributing

## Setup

Install the toolchain and build the CLI:

```bash
mise install
go build ./cmd/autoreview
```

## Validation

Run the deterministic core checks before opening or updating a pull request:

```bash
mise run verify
```

Before the `v0.1.0` source tag, run the complete release gate and the pinned
hosted skill review. The release gate includes the current Go vulnerability
database and therefore requires network access:

```bash
mise run verify:release
mise run skill:review
```

For command-surface changes, also exercise the built binary directly.
For skill changes, run the pinned hosted quality gate:

```bash
mise run skill:review
```

## Pull Requests

- Start from a GitHub issue.
- Keep changes focused and use conventional commits.
- Update tests and user-facing documentation with contract changes.
- Treat AI-review findings as hypotheses: validate them, fix actionable
  defects, and close the corresponding threads.

Release automation, signed binary distribution, and Homebrew publishing are
tracked separately after the source and skill milestone.
