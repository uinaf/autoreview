# Claude Code engine

Select this engine with `--engine claude`. The adapter always passes an explicit
model; an empty model setting resolves to `claude-opus-5`, with no fallback.
Effort must be `low`, `medium`, `high`, `xhigh`, or `max`.

## Runtime contract

The adapter capability-probes the installed CLI before model invocation. Every
mode requires `--print`, `--no-session-persistence`, JSON structured output, an
explicit model and effort, a fixed tool inventory, `dontAsk` permissions, and
disabled Chrome integration. Strict mode also requires `--safe-mode`, user-only
setting sources, strict MCP configuration, and an MCP-tool deny rule.

The frozen prompt is delivered on standard input.

## Isolation and web access

Web access is off by default. When enabled, `WebSearch` is the only exposed
tool; filesystem, shell, MCP, browser, and unrestricted fetch tools remain
unavailable.

| Mode | Authentication and configuration | Isolation controls |
| --- | --- | --- |
| `native` (default) | Existing environment, keychain login, and user configuration | Empty workspace and explicit safe tool inventory |
| `strict` | Empty home and Claude state; requires `ANTHROPIC_API_KEY` | Safe mode, disabled auto-memory, strict MCP, and no unsafe tools |

## Output contract

The adapter accepts one Claude `result` envelope with `subtype: success`,
`is_error: false`, and an object in `structured_output`. The inner object must
pass the complete canonical Go decoder. Prose extraction and engine-local
protocol recovery are not accepted.

## Verify

Default tests use a controlled fake executable. Run the optional authenticated
smoke explicitly:

```bash
AUTOREVIEW_TEST_LIVE_CLAUDE=1 go test ./internal/provider -run '^TestClaudeLive$' -count=1 -v
```
