# Configuration and isolation

Autoreview loads one flat, typed configuration in descending precedence:

1. explicit CLI flags
2. `AUTOREVIEW_*` environment variables
3. `.autoreview.yaml` at the resolved Git root
4. `$XDG_CONFIG_HOME/autoreview/config.yaml`, or
   `$HOME/.config/autoreview/config.yaml` when `XDG_CONFIG_HOME` is unset
5. built-in operational defaults

The engine has no default. Select exactly one of `codex`, `claude`, or `cursor`
through a flag or configuration source. Autoreview never chooses an engine by
examining `PATH`.

## File schema

Both YAML files use the same strict schema:

```yaml
engine: codex
model: gpt-5.6
reasoning_effort: high
timeout: 15m
retries: 1
max_bytes: 1048576
isolation: strict
web_access: false
```

Unknown keys, multiple YAML documents, invalid types, and retry counts above one
are errors. Configuration cannot contain commands, provider argument strings,
or named profiles. There is no `.autoreview.local.yaml`.

Each configuration file must be a regular file no larger than 64 KiB, and
symbolic links are rejected. Boolean fields accept only `true` or `false`;
`yes`, `no`, `on`, and `off` are invalid. A trusted account-home XDG file must
also be owned by the current user and must not be group- or world-writable.

The corresponding environment variables are `AUTOREVIEW_ENGINE`,
`AUTOREVIEW_MODEL`, `AUTOREVIEW_REASONING_EFFORT`, `AUTOREVIEW_TIMEOUT`,
`AUTOREVIEW_RETRIES`, `AUTOREVIEW_MAX_BYTES`, `AUTOREVIEW_ISOLATION`, and
`AUTOREVIEW_WEB_ACCESS`.

## Security controls

Web access defaults to off and isolation defaults to `strict`. Only an explicit
CLI flag or the ownership-checked XDG file under the operating-system account
home may enable web access or `native` isolation. A path selected through the
`XDG_CONFIG_HOME` environment variable is still loaded at XDG precedence, but
cannot enable either capability. Repository configuration and environment
variables may keep those controls strict or make them stricter, but attempts to
enable either capability fail.

Both isolation modes run the provider from a new empty temporary workspace and
pass only the already frozen review bundle. `strict` replaces home and provider
state directories with empty temporary directories and preserves only required
system variables, proxy and certificate settings, and supported provider API
keys. `native` preserves the normal provider environment and user configuration.

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
