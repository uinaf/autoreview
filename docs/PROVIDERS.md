# Provider runtime

Provider adapters receive one already frozen prompt and return one canonical
review. They do not collect Git content, retry, select another model, render a
report, or mutate the reviewed repository.

## Shared process boundary

The runtime resolves each executable to a regular executable outside the
reviewed repository. Provider commands are argument arrays executed directly;
repository material is sent only on standard input.

Every process runs in its own process group with a fixed timeout, cancellation,
and bounded stdout and stderr. Timeout or cancellation kills the complete group.
Diagnostics redact credential-bearing environment values, escape terminal
control characters, and remain bounded.

## Codex CLI

The Codex adapter currently requires these observed CLI surfaces:

- top level: `--ask-for-approval`, plus `--strict-config` in strict mode and
  `--search` when web access is enabled;
- `exec`: `--ephemeral`, `--skip-git-repo-check`, `--output-schema`,
  `--output-last-message`, `--json`, and `--cd`;
- strict `exec`: `--ignore-user-config`, `--ignore-rules`, and `--sandbox`.

Before model invocation, the adapter checks `--version`, both help surfaces,
and `login status` when neither `CODEX_API_KEY` nor `OPENAI_API_KEY` is present. Capability,
authentication, timeout, cancellation, provider-process, and protocol failures
remain distinct.

The adapter always passes an explicit model so result metadata is exact. An
empty model setting resolves to `gpt-5.6-sol`; there is no model fallback.
Web access is explicitly disabled unless enabled by trusted configuration.

| Mode | Provider state | Codex controls |
| --- | --- | --- |
| `strict` | Empty home, XDG, and Codex directories; selected provider credential only | Read-only sandbox, ignored user config/rules, disabled hooks/plugins/skills/multi-agent, fixed shell environment |
| `native` | Existing provider environment and authentication | User Codex configuration is preserved; review still runs in an empty temporary workspace |

Codex structured outputs support a smaller JSON Schema vocabulary than the
canonical local validator. Generation therefore uses a projection that omits
the unsupported `not` path rule. The returned last-message file and JSONL agent
message must agree byte-for-byte after surrounding whitespace, and the result
is then decoded against the complete canonical Go contract. A projection never
makes an invalid result acceptable.

## Verification

Default tests use controlled fake executables and never invoke a model. The
authenticated opt-in smoke is:

```bash
AUTOREVIEW_TEST_LIVE_CODEX=1 go test ./internal/provider -run '^TestCodexLive$' -count=1 -v
```

CLI target collection, retries, final report construction, output rendering,
and exit codes belong to the orchestration milestone in issue #8.

## Claude Code

The Claude adapter capability-probes the installed CLI before model invocation.
Every mode requires `--print`, `--no-session-persistence`, JSON structured
output, explicit model and effort, a fixed tool inventory, `dontAsk`
permissions, and disabled Chrome integration. Strict mode additionally requires
`--safe-mode`, user-only setting sources, strict MCP configuration, and an MCP
tool deny rule.

The prompt is delivered on standard input. The adapter accepts only one Claude
`result` envelope with `subtype: success`, `is_error: false`, and an object in
`structured_output`; the inner object must then pass the complete canonical Go
decoder. No prose extraction or protocol recovery is attempted.

The model is always explicit and defaults to `claude-opus-5`. Effort must be
one of `low`, `medium`, `high`, `xhigh`, or `max`; there is no model fallback.
Web access is off by default. When enabled, the only exposed tool is
`WebSearch`; filesystem, shell, MCP, browser, and unrestricted fetch tools are
not exposed.

| Mode | Authentication and configuration | Isolation controls |
| --- | --- | --- |
| `strict` | Empty home and Claude state; optional `ANTHROPIC_API_KEY` remains process-local | Safe mode, disabled auto-memory, strict MCP, no unsafe tools |
| `native` | Existing Claude environment, keychain login, and user configuration | Empty provider workspace and explicit safe tool inventory |

Default tests use a controlled fake executable. The optional authenticated
smoke is:

```bash
AUTOREVIEW_TEST_LIVE_CLAUDE=1 go test ./internal/provider -run '^TestClaudeLive$' -count=1 -v
```

## Cursor Agent

The Cursor adapter requires the current non-interactive JSON, Ask mode,
workspace, trust, model, and authentication-status surfaces. Strict mode also
requires and forces Cursor sandboxing. The reviewed source is never mounted in
the provider workspace; the frozen prompt is delivered on standard input.

Cursor Agent does not expose a documented per-run web-disable flag. The adapter
therefore fails capability preflight when `web_access` is false rather than
claiming an isolation guarantee it cannot enforce. Since repository config and
environment variables cannot enable web access, a Cursor run requires explicit
CLI authorization or trusted XDG configuration.

| Mode | Authentication and configuration | Isolation controls |
| --- | --- | --- |
| `strict` | Empty home and Cursor state; optional `CURSOR_API_KEY` remains process-local | Ask mode, forced sandbox, generated deny rules for shell/file/MCP access |
| `native` | Existing Cursor environment, login, and user configuration | Ask mode in an empty provider workspace; user sandbox configuration is preserved |

The model is explicit and defaults to `cursor-grok-4.5-high-fast`. Cursor model
IDs encode effort, so a separate non-default `reasoning_effort` is rejected.
There is no fallback.

The outer JSON must be one successful Cursor `result` envelope with a non-empty
string result. Because Cursor's JSON output format does not constrain that inner
assistant text, the adapter appends a trusted protocol instruction and the exact
embedded review schema after the frozen bundle. The inner result is first decoded
as exactly one canonical review object. If that fails, the only recovery accepts
non-JSON prose followed by one complete canonical object that consumes the
remaining suffix. Fences, ambiguous braces, JSON-value prefixes, malformed or
multiple objects, suffix prose, and non-canonical reviews fail closed. Successful
recovery is recorded as `cursor_trailing_object`.

Default tests use a controlled fake executable and a complete recovery matrix.
The optional authenticated smoke is:

```bash
AUTOREVIEW_TEST_LIVE_CURSOR=1 go test ./internal/provider -run '^TestCursorLive$' -count=1 -v
```
