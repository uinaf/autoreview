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
isolation: strict
web_access: false
```

Corresponding environment variables are `AUTOREVIEW_ENGINE`,
`AUTOREVIEW_MODEL`, `AUTOREVIEW_REASONING_EFFORT`, `AUTOREVIEW_TIMEOUT`,
`AUTOREVIEW_RETRIES`, `AUTOREVIEW_MAX_BYTES`, `AUTOREVIEW_ISOLATION`, and
`AUTOREVIEW_WEB_ACCESS`.

Unknown keys, loose YAML booleans, multiple documents, retry counts outside
zero or one, and invalid types fail closed. There are no profiles or local
override files. `max_bytes` defaults to 1 MiB and cannot exceed 128 MiB.

Only an explicit CLI flag or an ownership-checked XDG file under the operating
system account home may enable `native` isolation or web access. Repository
configuration, environment variables, and an XDG path selected through
`XDG_CONFIG_HOME` cannot enable those capabilities.
