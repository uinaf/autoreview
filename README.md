# autoreview

`autoreview` is a Go CLI and agent skill for one structured, independent code
review through Codex CLI, Claude Code, or Cursor Agent. It freezes an explicit
Git target, scans the complete bundle for secrets, validates the provider
result locally, and emits a stable terminal or JSON report.

## Install

Install the signed source release with Go 1.26 or newer:

```bash
go install github.com/uinaf/autoreview/cmd/autoreview@v0.1.0
autoreview --version
```

Runtime dependencies are Git 2.41 or newer, the `trufflehog` executable, and
exactly one supported review harness available on `PATH`.

Install the standalone skill from Tessl into a Codex project:

```bash
tessl install --agent codex uinaf/autoreview@0.1.0
```

The skill invokes the installed CLI. It does not bundle a second runtime.

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

`v0.1.0` is the source and Tessl package milestone for macOS and Linux on amd64
and arm64. GitHub Checks integration, signed binary assets, automated
publishing, and Homebrew distribution are deferred to [issue #11](https://github.com/uinaf/autoreview/issues/11).

The [migration matrix](docs/MIGRATION.md) records the Python-to-Go decisions and
their v0.1 evidence. [NOTICE](NOTICE) records upstream provenance. The project
is MIT licensed.

## Contributing

See [Contributing](CONTRIBUTING.md) for setup, release gates, and pull-request
expectations.
