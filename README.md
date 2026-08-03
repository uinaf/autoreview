![autoreview — structured independent code review, as a CLI and an agent skill.](https://uinaf.dev/og/banner/autoreview.png)

# autoreview

`autoreview` is a Go CLI and agent skill for one structured, independent code
review through Codex CLI, Claude Code, or Cursor Agent. It freezes an explicit
Git target, scans the complete bundle for secrets, validates the provider
result locally, and emits a stable terminal or JSON report.

## Install

On macOS, install the signed CLI from the `uinaf/tap` Homebrew tap:

```bash
brew install --cask uinaf/tap/autoreview
autoreview --version
```

Linux users and consumers that prefer Go tooling can install the latest tagged
CLI with Go 1.26 or newer:

```bash
go install github.com/uinaf/autoreview/cmd/autoreview@latest
autoreview --version
```

Runtime dependencies are Git 2.41 or newer, the `trufflehog` executable, and
the selected review harness available on `PATH`. Multiple supported harnesses
may be installed; `--engine` selects exactly one for each run.

The standalone [agent skill](skills/autoreview) invokes the installed CLI. It
does not bundle a second runtime.
See [Releases](docs/RELEASES.md) for archive, checksum, Sigstore signature, and
GitHub provenance verification.

## Quick use

Inspect the effective configuration and its sources, then review dirty local
changes:

```bash
autoreview config --engine codex
autoreview review --mode local --engine codex \
  --prompt "Review this completed change against its acceptance criteria."
```

Select one target explicitly:

```bash
# Complete branch or checked-out pull-request diff
autoreview review --mode branch --base origin/main --engine claude \
  --prompt "$task_contract"

# One non-merge commit
autoreview review --mode commit --commit HEAD --engine codex \
  --prompt "$task_contract"
```

Autoreview never edits source, runs tests, commits, pushes, chooses a provider,
or falls back to another model. Builder verification happens before review.

## Machine output

Use `--output json` for the versioned result contract. The JSON document is the
only stdout value; progress and diagnostics use stderr.

```bash
autoreview review --mode branch --base origin/main --engine codex \
  --output json --prompt "$task_contract" > result.json
```

Exit 0 means a valid clean review, exit 1 means valid findings, and exit 2 means
no trustworthy review result.

## Configuration and providers

Configuration resolves flags, environment variables, repository
`.autoreview.yaml`, account XDG config, then operational defaults. Strict
isolation and web-off are the defaults. Only an explicit flag or trusted
account-home XDG file may enable native provider state or web access.

- [Configuration and isolation](docs/CONFIG.md)
- [Codex, Claude, and Cursor execution](docs/PROVIDERS.md)
- [Local, branch, and commit targets](docs/TARGETS.md)
- [Versioned result and exit contract](docs/RESULT_SCHEMA.md)

## Security

Every frozen bundle, including deleted bytes and context files, is scanned by
external TruffleHog in offline mode before provider invocation. Unsafe,
incomplete, sensitive, oversized, or changed input fails closed. Report
suspected vulnerabilities privately through the repository Security tab; see
[Security](SECURITY.md).

## Project status

The CLI publishes signed macOS and Linux archives for amd64 and arm64 from
verified `main` commits. Conventional Commits select the next CLI version, and
the `uinaf/tap` Homebrew tap provides the prebuilt command.

GitHub Checks integration and CI-hosted review reporting remain in
[issue #11](https://github.com/uinaf/autoreview/issues/11); release automation
does not change the local review contract.

The [migration matrix](docs/MIGRATION.md) records the Python-to-Go decisions and
their v0.1 evidence. This project succeeds the
[original autoreview workflow](https://github.com/openclaw/agent-skills/tree/main/skills/autoreview),
and its [MIT license](LICENSE) preserves the copyright notices for both
`openclaw` and `uinaf`.

## Contributing

See [Contributing](CONTRIBUTING.md) for setup, release gates, and pull-request
expectations.
