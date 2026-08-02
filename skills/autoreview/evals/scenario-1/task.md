# Diagnose a surprising effective configuration

An engineer wants a review of a dirty local patch. Their account config contains
`engine: claude`, `timeout: 20m`, and `isolation: native`. The repository file
contains `engine: codex`, `timeout: 8m`, and `isolation: strict`. Their shell has
`AUTOREVIEW_ENGINE=cursor`, `AUTOREVIEW_TIMEOUT=3m`,
`AUTOREVIEW_WEB_ACCESS=true`, and `AUTOREVIEW_ISOLATION=native`. They plan to add
`--engine codex --model gpt-5.6-sol --reasoning-effort high --timeout 90s` to
the command. The model and effort are explicit user choices. The XDG path was
selected through the `XDG_CONFIG_HOME` environment variable.

Write `diagnosis.md` with the effective settings, the diagnostic command to
confirm them, any configuration errors, and the safe local-review command.
