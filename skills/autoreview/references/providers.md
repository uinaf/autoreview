# Provider selection

Select one provider for the entire review. Honor the user's provider, model, and
reasoning-effort choices first, then trusted repository or account
configuration. Pass explicit user model and effort choices as CLI flags. If no
source chooses a provider, select one installed harness that satisfies the task
and explain the choice. Do not run multiple providers, fall back after failure,
or claim consensus.

| Engine | Default model | Strict authentication | Important constraint |
| --- | --- | --- | --- |
| `codex` | `gpt-5.6-sol` | `CODEX_API_KEY` or `OPENAI_API_KEY` | Session-backed login generally requires explicit native isolation |
| `claude` | `claude-opus-5` | `ANTHROPIC_API_KEY` | Effort supports `low`, `medium`, `high`, `xhigh`, or `max` |
| `cursor` | `cursor-grok-4.5-high-fast` | `CURSOR_API_KEY` | Requires authorized web access; effort is encoded in the model ID |

For Cursor, pass a requested compatible model with `--model` and never add
`--reasoning-effort`. If the user requests Cursor plus a separate effort value
without a compatible model ID, report the unsupported combination instead of
guessing a model.

All adapters capability-probe the installed executable and use one explicit
model with no model fallback. A capability or authentication failure is an
operational result, not permission to switch engines.

Strict mode runs with empty provider state and a constrained environment.
Native mode preserves the provider's normal environment and login but still
runs the review in an empty temporary workspace with only the frozen bundle.
Use native mode only after explicit authorization or through trusted account
configuration.

Web access defaults off. Codex enables its search surface only when authorized;
Claude exposes only WebSearch. Cursor has no documented per-run web disable, so
it fails capability preflight unless web access is authorized.
