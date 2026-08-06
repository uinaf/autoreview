# Review engines

Autoreview runs exactly one review engine against an already frozen prompt and
returns one canonical review. Select the engine explicitly with `--engine` or a
trusted configuration source.

| Engine | Harness | Default model | Runtime details |
| --- | --- | --- | --- |
| `codex` | Codex CLI | `gpt-5.6-sol` | [Codex CLI](codex.md) |
| `claude` | Claude Code | `claude-opus-5` | [Claude Code](claude-code.md) |
| `cursor` | Cursor Agent | `cursor-grok-4.5-high-fast` | [Cursor Agent](cursor.md) |
| `grok` | Grok Build | `grok-4.5` | [Grok Build](grok-build.md) |

## Shared runtime boundary

Engine adapters do not collect Git content, select another model, render a
report, retry, or mutate the reviewed repository. Orchestration owns target
collection, the one configured protocol retry, and final report construction.

The runtime resolves the selected executable to a regular executable outside
the reviewed repository and invokes it directly with an argument array.
Repository material is sent on standard input or through a private temporary
prompt file; it is never placed in process arguments.

Every engine runs in an empty temporary workspace and its own process group with
a fixed timeout and bounded output. The runtime terminates remaining process
group members after success, failure, timeout, cancellation, or output overflow.
Diagnostics redact credential-bearing environment values, escape terminal
control characters, and remain bounded.

See [Configuration and isolation](../CONFIG.md) for configuration precedence,
strict authentication, and web-access policy.
