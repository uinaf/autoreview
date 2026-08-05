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

Before release-related changes, run the complete release gate and release
configuration checks. The release gate requires the installed `trufflehog`
executable, exercises its benign and detection paths, and checks the current Go
vulnerability database, so it also requires network access:

```bash
mise run verify:release
mise run release:check
mise run release:snapshot
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

## Releases

Successful pushes to protected `main` evaluate Conventional Commits after the
macOS/Linux verification and snapshot-package jobs pass. `fix`, `perf`, and
`refactor` publish patches; `feat` publishes a minor; breaking changes publish
a major; and docs, test, chore, build, and CI changes do not publish.

The release job creates no version-bump commit. It mints a short-lived
`uinaf-releaser` token inside the `release` Environment, creates the tag and
GitHub Release, validates Apple signing credentials supplied by that protected
Environment, signs and notarizes the macOS binaries, signs and attests all
artifacts, and updates the Homebrew tap. See [Releases](docs/RELEASES.md) for
the complete contract and recovery path.
