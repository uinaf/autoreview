![autoreview — structured independent code review, as a CLI and an agent skill.](https://uinaf.dev/og/banner/autoreview.png)

# autoreview

`autoreview` is a Go CLI and agent skill for one structured, independent code
review through Codex CLI, Claude Code, Cursor Agent, or Grok Build. It freezes
an explicit Git target, scans the complete bundle for secrets, validates the
provider result locally, and emits a stable terminal or JSON report.

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

## Quick use

Inspect the effective configuration and its sources, then review dirty local
changes:

```bash
prompt="Review this completed change against its acceptance criteria."
autoreview config --engine codex
autoreview review --mode local --engine codex \
  --prompt "$prompt"
```

Use `branch` for the complete merge-base-to-HEAD diff or `commit` for one
non-merge commit:

```bash
autoreview review --mode branch --base origin/main --engine claude \
  --prompt "$prompt"

autoreview review --mode commit --commit HEAD --engine codex \
  --prompt "$prompt"
```

Autoreview never edits source, runs tests, commits, pushes, chooses a provider,
or falls back to another model. Builder verification happens before review.

## Machine output

Use `--output json` for the versioned result contract. The JSON document is the
only stdout value; progress and diagnostics use stderr.

```bash
prompt="Review this completed change against its acceptance criteria."
autoreview review --mode branch --base origin/main --engine codex \
  --output json --prompt "$prompt" > result.json
```

Exit 0 means a valid clean review, exit 1 means valid findings, and exit 2 means
no trustworthy review result.

## Agent skill

The bundled [agent skill](skills/autoreview/SKILL.md) delegates one independent
review to the installed CLI. It does not contain a second runtime.

## Documentation

- [Configuration and isolation](docs/CONFIG.md)
- [Review engines](docs/engines/README.md)
- [Local, branch, and commit targets](docs/TARGETS.md)
- [Versioned result and exit contract](docs/RESULT_SCHEMA.md)
- [Release artifacts and verification](docs/RELEASES.md)

## Contributing

See [Contributing](CONTRIBUTING.md) for setup, release gates, and pull-request
expectations.

## License

[MIT](LICENSE)
