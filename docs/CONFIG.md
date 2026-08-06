# Configuration and isolation

Autoreview loads one flat, typed configuration in descending precedence:

1. explicit CLI flags
2. `AUTOREVIEW_*` environment variables
3. `.autoreview.yaml` at the resolved Git root
4. `$XDG_CONFIG_HOME/autoreview/config.yaml`, or
   `$HOME/.config/autoreview/config.yaml` when `XDG_CONFIG_HOME` is unset
5. built-in operational defaults

The engine has no default. Select exactly one of `codex`, `claude`, `cursor`, or
`grok` through a flag or configuration source. Autoreview never chooses an
engine by examining `PATH`.

## File schema

Both YAML files use the same strict schema:

```yaml
engine: codex
model: gpt-5.6
reasoning_effort: high
timeout: 15m
retries: 1
max_bytes: 1048576
isolation: native
web_access: false
```

Unknown keys, multiple YAML documents, invalid types, and retry counts above one
are errors. Configuration cannot contain commands, provider argument strings,
or named profiles. There is no `.autoreview.local.yaml`.

`max_bytes` defaults to 1 MiB and must be between 1 byte and 128 MiB
(`134217728`).

Each configuration file must be a regular file no larger than 64 KiB, and
symbolic links are rejected. Boolean fields accept only `true` or `false`;
`yes`, `no`, `on`, and `off` are invalid. A trusted account-home XDG file must
also be owned by the current user and must not be group- or world-writable.

The corresponding environment variables are `AUTOREVIEW_ENGINE`,
`AUTOREVIEW_MODEL`, `AUTOREVIEW_REASONING_EFFORT`, `AUTOREVIEW_TIMEOUT`,
`AUTOREVIEW_RETRIES`, `AUTOREVIEW_MAX_BYTES`, `AUTOREVIEW_ISOLATION`, and
`AUTOREVIEW_WEB_ACCESS`.

## Security controls

Isolation defaults to `native`, which preserves normal provider login and user
configuration. Any source may select stricter `strict` isolation. A
higher-precedence repository or environment source cannot weaken an already
selected `strict` value back to `native`.

Web access defaults to off for Codex, Claude, and Grok. An explicit CLI
`--engine cursor` changes an otherwise unset default to on because Cursor Agent
has no documented per-run web-disable control. Cursor selected by repository,
environment, or XDG engine configuration does not receive that implicit grant;
pass `--web-access` with the explicit command, or set `web_access: true` in an
ownership-checked account XDG config. An explicit `web_access: false` from any
source remains authoritative and makes Cursor fail capability preflight. A path selected through
`XDG_CONFIG_HOME`, repository configuration, and environment variables cannot
enable web access. Their `web_access: true` values are rejected even when an
explicit CLI Cursor selection would otherwise imply web; omit the untrusted
restatement and let the flag-derived value apply.

Both isolation modes run the provider from a new empty temporary workspace and
pass only the already frozen review bundle. `strict` replaces home and provider
state directories with empty temporary directories and preserves only required
system variables, proxy and certificate settings, and supported provider API
keys. `native` preserves the normal provider environment and user configuration.
Strict authentication requires the provider's supported API-key environment
variable; session-backed login belongs to native mode. Grok strict mode uses
`XAI_API_KEY`; native mode uses the normal `grok login` session. Explicit CLI
Cursor selection still grants otherwise-unset web access in strict mode; pass
`--web-access=false` to reject that capability and fail preflight instead.

## Effective configuration

Inspect resolved values and their source without printing credentials or the
full environment:

```bash
autoreview config --engine codex
autoreview config --engine codex --json
```

The diagnostic supports the same typed overrides: `--model`,
`--reasoning-effort`, `--timeout`, `--retries`, `--max-bytes`, `--isolation`, and
`--web-access`. Use `--repository` to inspect another checkout.
