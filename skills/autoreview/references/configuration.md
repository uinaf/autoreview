# Configuration

Autoreview resolves one flat typed configuration in this precedence order:

1. CLI flags
2. `AUTOREVIEW_*` environment variables
3. `.autoreview.yaml` at the Git root
4. account XDG config
5. built-in operational defaults

The engine has no built-in default. Valid engines are `codex`, `claude`, and
`cursor`. Inspect resolved values and their source with:

```bash
autoreview config --repository . --engine "$engine"
autoreview config --repository . --engine "$engine" --json
```

The YAML schema is:

```yaml
engine: codex
model: gpt-5.6-sol
reasoning_effort: high
timeout: 15m
retries: 1
max_bytes: 1048576
isolation: native
web_access: false
```

Corresponding environment variables are `AUTOREVIEW_ENGINE`,
`AUTOREVIEW_MODEL`, `AUTOREVIEW_REASONING_EFFORT`, `AUTOREVIEW_TIMEOUT`,
`AUTOREVIEW_RETRIES`, `AUTOREVIEW_MAX_BYTES`, `AUTOREVIEW_ISOLATION`, and
`AUTOREVIEW_WEB_ACCESS`.

Unknown keys, loose YAML booleans, multiple documents, retry counts outside
zero or one, and invalid types fail closed. There are no profiles or local
override files. `max_bytes` defaults to 1 MiB and cannot exceed 128 MiB.

Native isolation is the default and preserves session-backed provider login in
an empty bundle-only workspace. Any source may select `strict`; an untrusted
higher-precedence source cannot weaken an already selected strict value. Strict
mode requires the provider's supported API-key environment variable.

Web access defaults off for Codex and Claude. Explicit CLI `--engine cursor`
enables otherwise-unset web access implicitly because Cursor cannot guarantee a
per-run web disable. Repository, environment, or XDG engine selection does not
grant web access. Explicit `web_access: false` remains authoritative and
prevents a Cursor run. Only an explicit flag or ownership-checked account-home
XDG file may otherwise enable web access.
