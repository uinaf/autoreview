# Codex CLI engine

Select this engine with `--engine codex`. The adapter always passes an explicit
model; an empty model setting resolves to `gpt-5.6-sol`, with no fallback.

## Runtime contract

The adapter capability-probes the following installed CLI surfaces before model
invocation:

- top level: `--ask-for-approval`, plus `--strict-config` in strict mode and
  `--search` when web access is enabled;
- `exec`: `--ephemeral`, `--skip-git-repo-check`, `--output-schema`,
  `--output-last-message`, `--json`, and `--cd`;
- strict `exec`: `--ignore-user-config`, `--ignore-rules`, and `--sandbox`.

It checks `--version`, both help surfaces, and `login status` in native mode
when neither `CODEX_API_KEY` nor `OPENAI_API_KEY` is present. Capability,
authentication, timeout, cancellation, provider-process, and protocol failures
remain distinct.

## Isolation and web access

Web access is off unless trusted configuration enables it.

| Mode | Authentication and configuration | Isolation controls |
| --- | --- | --- |
| `native` (default) | Existing environment, authentication, and user configuration | Review runs in an empty temporary workspace |
| `strict` | Empty home, XDG, and Codex directories; requires `CODEX_API_KEY` or `OPENAI_API_KEY` | Read-only sandbox, ignored user config and rules, disabled hooks, plugins, skills, and multi-agent behavior |

## Output contract

Codex structured output uses a projection of the canonical JSON Schema that
omits the unsupported `not` path rule. The returned last-message file and JSONL
agent message must agree after surrounding whitespace is removed. The result is
then decoded against the complete canonical Go contract; schema projection does
not make an invalid result acceptable.

## Verify

Default tests use a controlled fake executable. Run the optional authenticated
smoke explicitly:

```bash
AUTOREVIEW_TEST_LIVE_CODEX=1 go test ./internal/provider -run '^TestCodexLive$' -count=1 -v
```
