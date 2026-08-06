# Cursor Agent engine

Select this engine with `--engine cursor`. The adapter always passes an explicit
model; an empty model setting resolves to `cursor-grok-4.5-high-fast`, with no
fallback. Cursor model IDs encode effort, so a separate non-default
`reasoning_effort` is rejected.

## Runtime contract

The adapter requires the non-interactive JSON, Ask mode, workspace, trust,
model, and authentication-status CLI surfaces. Strict mode also requires and
forces Cursor sandboxing. The reviewed source is never mounted in the provider
workspace; the frozen prompt is delivered on standard input.

## Isolation and web access

Cursor Agent has no documented per-run web-disable flag. Explicit CLI selection
with `--engine cursor` therefore enables otherwise-unset web access. Engine
selection from repository, environment, or XDG configuration does not grant web
access. An explicit `web_access: false` remains authoritative and fails
capability preflight rather than claiming an unenforceable isolation guarantee.

| Mode | Authentication and configuration | Isolation controls |
| --- | --- | --- |
| `native` (default) | Existing environment, login, and user configuration | Ask mode in an empty workspace; user sandbox configuration is preserved |
| `strict` | Empty home and Cursor state; requires `CURSOR_API_KEY` | Ask mode, forced sandbox, and generated deny rules for shell, file, and MCP access |

## Output contract

The outer JSON must be one successful Cursor `result` envelope with a non-empty
string result. The adapter appends a trusted protocol instruction and the exact
embedded review schema after the frozen bundle.

The inner result is first decoded as exactly one canonical review object. If
that fails, the only recovery accepts non-JSON prose followed by one complete
canonical object that consumes the remaining suffix. Fences, ambiguous braces,
JSON-value prefixes, malformed or multiple objects, suffix prose, and
non-canonical reviews fail closed. Successful recovery is recorded as
`cursor_trailing_object`.

## Verify

Default tests use a controlled fake executable and a complete recovery matrix.
Run the optional authenticated smoke explicitly:

```bash
AUTOREVIEW_TEST_LIVE_CURSOR=1 go test ./internal/provider -run '^TestCursorLive$' -count=1 -v
```
