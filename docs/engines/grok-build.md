# Grok Build engine

Select this engine with `--engine grok`. Install the official CLI and
authenticate before the first native review:

```bash
npm install --global @xai-official/grok
grok login
```

The adapter always passes an explicit model; an empty model setting resolves to
`grok-4.5`, with no fallback.

## Runtime contract

The adapter requires the official headless prompt-file, JSON Schema, explicit
model and effort, bounded-turn, permission, feature-disable, working-directory,
and authentication surfaces. It checks `--version`, `--help`, and `grok models`
before model invocation when `XAI_API_KEY` is absent.

The frozen prompt is written to a private `0600` file inside the empty provider
workspace and removed with that workspace after the run. The prompt never
appears in process arguments. Every run disables plan mode, subagents, memory,
shell, edits, file reads, grep, and MCP tools. The `dontAsk` permission mode
silently denies tools without an explicit allow rule and prevents interactive
approval prompts. Tool filtering uses Grok's documented internal IDs. With web
off, the adapter starts from `web_search` and removes it with
`--disallowed-tools`. It also removes the always-on `search_tool` and `use_tool`
MCP meta-tools, leaving no tools for the model to call.

## Isolation and web access

Web access is off by default. `web_search` and `web_fetch` are removed when web
access is off and are the only tools when it is on. MCP meta-tools are removed
in both modes.

| Mode | Authentication and configuration | Isolation controls |
| --- | --- | --- |
| `native` (default) | Existing environment, `grok login` session, and user configuration | Empty workspace and explicit per-run tool and feature policy |
| `strict` | Empty home and `GROK_HOME`; requires `XAI_API_KEY` | Grok `workspace` sandbox plus the same explicit deny policy |

## Output contract

The adapter fixes `--max-turns 2`; the supported CLI can exit successfully with
a cancelled, missing structured result when bounded to one turn. Success
requires `stopReason: end_turn`, non-empty session and request identifiers, no
structured-output error, and complete canonical review objects in both `text`
and `structuredOutput`. Those decoded objects must agree exactly. Prose
extraction and engine-local protocol recovery are not accepted.

Grok treats the schema's unanchored non-whitespace string pattern as a
full-string constraint and otherwise truncates explanations, titles, bodies,
and paths to one character. The provider-facing projection omits only that
pattern for Grok. Canonical decoding still enforces non-blank text, length
bounds, and safe relative paths before a result can succeed.

The compatibility contract and live structured-output smoke were confirmed
against Grok Build CLI v0.2.118. Capability probes fail closed when a later CLI
drops required flags, enumerated values, or authentication-status shape.

Each attempt invokes the selected xAI model and may consume plan or API quota.
The one configured protocol retry invokes it again only after malformed output;
authentication, capability, timeout, cancellation, and process failures are not
retried.

## Verify

Default tests use a controlled fake executable. Run the optional authenticated
smoke explicitly:

```bash
AUTOREVIEW_TEST_LIVE_GROK=1 go test ./internal/provider -run '^TestGrokLive$' -count=1 -v
```
