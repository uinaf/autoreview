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

Before a source tag, run the complete release gate and the pinned hosted skill
review. The release gate requires the installed `trufflehog` executable,
exercises its benign and detection paths, and checks the current Go
vulnerability database, so it also requires network access:

```bash
mise run verify:release
mise run skill:review
```

Freezing the current checkout is intentionally separate because it depends on
local changes. Opt in when target-collection behavior needs that additional
smoke check:

```bash
mise run test:current-checkout
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
