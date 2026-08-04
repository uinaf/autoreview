# Provider runtime

Provider adapters receive one already frozen prompt and return one canonical
review. They do not collect Git content, retry, select another model, render a
report, or mutate the reviewed repository.

## Shared process boundary

The runtime resolves each executable to a regular executable outside the
reviewed repository. Provider commands are argument arrays executed directly.
Repository material is sent on standard input or through a private temporary
prompt file and is never placed in process arguments.

Every process runs in its own process group with a fixed timeout, cancellation,
and bounded stdout and stderr. Before the leader is reaped, the runtime
terminates remaining group members after success, provider failure, timeout,
cancellation, or output overflow. Diagnostics redact credential-bearing
environment values, escape terminal control characters, and remain bounded.

## Codex CLI

The Codex adapter currently requires these observed CLI surfaces:

- top level: `--ask-for-approval`, plus `--strict-config` in strict mode and
  `--search` when web access is enabled;
- `exec`: `--ephemeral`, `--skip-git-repo-check`, `--output-schema`,
  `--output-last-message`, `--json`, and `--cd`;
- strict `exec`: `--ignore-user-config`, `--ignore-rules`, and `--sandbox`.

Before model invocation, the adapter checks `--version`, both help surfaces,
and `login status` in native mode when neither `CODEX_API_KEY` nor
`OPENAI_API_KEY` is present. Strict mode requires one of those API keys and
reports the exact requirement before probing the provider. Capability,
authentication, timeout, cancellation, provider-process, and protocol failures
remain distinct.

The adapter always passes an explicit model so result metadata is exact. An
empty model setting resolves to `gpt-5.6-sol`; there is no model fallback.
Web access is explicitly disabled unless enabled by trusted configuration.

| Mode | Provider state | Codex controls |
| --- | --- | --- |
| `native` (default) | Existing provider environment and authentication | User Codex configuration is preserved; review still runs in an empty temporary workspace |
| `strict` | Empty home, XDG, and Codex directories; `CODEX_API_KEY` or `OPENAI_API_KEY` | Read-only sandbox, ignored user config/rules, disabled hooks/plugins/skills/multi-agent, fixed shell environment |

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
| `native` (default) | Existing Claude environment, keychain login, and user configuration | Empty provider workspace and explicit safe tool inventory |
| `strict` | Empty home and Claude state; required `ANTHROPIC_API_KEY` remains process-local | Safe mode, disabled auto-memory, strict MCP, no unsafe tools |

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

Cursor Agent does not expose a documented per-run web-disable flag. Selecting
Cursor with the explicit CLI `--engine cursor` therefore changes otherwise-unset
web access to on implicitly. Engine selection from repository, environment, or
XDG configuration does not grant web access. An explicit `web_access: false`
remains authoritative, and the adapter then fails capability preflight rather
than claiming an isolation guarantee it cannot enforce.

| Mode | Authentication and configuration | Isolation controls |
| --- | --- | --- |
| `native` (default) | Existing Cursor environment, login, and user configuration | Ask mode in an empty provider workspace; user sandbox configuration is preserved |
| `strict` | Empty home and Cursor state; required `CURSOR_API_KEY` remains process-local | Ask mode, forced sandbox, generated deny rules for shell/file/MCP access; explicit CLI Cursor selection still implies web unless set false, which fails preflight |

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

## Grok Build

Install the official CLI and authenticate before selecting the provider:

```bash
npm install --global @xai-official/grok
grok login
```

The Grok adapter requires the official headless prompt-file, JSON Schema,
explicit model and effort, bounded turns, permission, feature-disable, working
directory, and authentication surfaces. It probes `--version`, `--help`, and
`grok models` before model invocation when `XAI_API_KEY` is absent.

The frozen prompt is written to a private `0600` file inside the empty provider
workspace and removed with that workspace after the run. The prompt never
appears in process arguments. Every run disables plan mode, subagents, memory,
shell, edits, file reads, grep, and MCP tools. WebSearch and WebFetch are denied
when web access is off and are the only explicitly allowed tools when it is on.
The built-in tool inventory is empty when web is off and contains only those two
web tools when enabled. The `dontAsk` permission mode silently denies anything
without an explicit allow rule and prevents interactive approval prompts.

| Mode | Authentication and configuration | Isolation controls |
| --- | --- | --- |
| `native` (default) | Existing Grok environment, `grok login` session, and user configuration | Empty provider workspace and explicit per-run tool and feature policy |
| `strict` | Empty home and `GROK_HOME`; required `XAI_API_KEY` remains process-local | Grok `workspace` sandbox plus the same explicit deny policy |

The model is explicit and defaults to `grok-4.5`; there is no fallback. The
adapter fixes `--max-turns 2`: the supported CLI can return exit zero with a
cancelled, missing structured result when bounded to one turn. Success therefore
requires `stopReason: end_turn`, non-empty session and request identifiers, no
structured-output error, and complete canonical review objects in both `text`
and `structuredOutput`. Those objects must agree exactly after decoding. No
prose extraction or protocol recovery is attempted.

The compatibility contract and live structured-output smoke were confirmed
against Grok Build CLI v0.2.118. Capability probes fail closed when a later CLI
drops required flags, enumerated values, or authentication-status shape.

Each review attempt invokes the selected xAI model and may consume plan or API
quota. A configured protocol retry invokes it once more only after malformed
output; authentication, capability, timeout, cancellation, and process failures
are never retried.

Default tests use a controlled fake executable. The optional authenticated
smoke is:

```bash
AUTOREVIEW_TEST_LIVE_GROK=1 go test ./internal/provider -run '^TestGrokLive$' -count=1 -v
```
