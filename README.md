![autoreview — structured independent code review, as a CLI and an agent skill.](https://uinaf.dev/og/banner/autoreview.png)

# autoreview

`autoreview` is a Go CLI and agent skill for structured, independent code
review through Codex CLI, Claude Code, or Cursor Agent.

The project is under active development toward `v0.1.0`. The first proven
release will support local Git targets, frozen and secret-scanned review
bundles, strict result validation, terminal and JSON reports, and a standalone
Tessl skill. CI reporting, signed binary releases, and Homebrew distribution
will follow after the local package is proven.

## Development

Build and inspect the bootstrap CLI:

```bash
go build ./cmd/autoreview
./autoreview --version
```

Run the current verification suite:

```bash
mise run verify
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the pull-request workflow and
[NOTICE](NOTICE) for upstream provenance. The
[migration matrix](docs/MIGRATION.md) tracks Python-to-Go behavior, and the
[result protocol](docs/RESULT_SCHEMA.md) defines the versioned machine contract.
The [target boundary](docs/TARGETS.md) documents frozen Git input and secret
scanning. [Configuration and isolation](docs/CONFIG.md) defines the typed
precedence rules, effective-config diagnostic, and strict/native provider
workspace contract.

## License

MIT. See [LICENSE](LICENSE).
