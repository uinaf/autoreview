# Result protocol

Autoreview has two JSON boundaries:

- [review-v1.schema.json](../schema/review-v1.schema.json) is the exact object a
  provider must return.
- [result-v1.schema.json](../schema/result-v1.schema.json) is the final CLI
  report after local validation and metadata attachment.

Both reject unknown and missing fields. The Go decoder also enforces semantic
rules that JSON Schema cannot express conveniently, including normalized
repository-relative paths, non-overlapping reviewed line ranges, finding
locations inside those ranges, sequential attempts, and status consistency.

## Status and exit code

| Status | Meaning | Exit |
| --- | --- | ---: |
| `clean` | A valid review returned no findings | 0 |
| `findings` | A valid review returned one or more findings | 1 |
| `failure` | No trustworthy review result was obtained | 2 |

Operational failure never becomes a clean result. A clean result must have an
empty findings array; a findings result must have at least one finding.

## Provider review

The provider returns only findings, an overall explanation, and overall
confidence. Status is derived locally from the validated finding count. This
replaces the older independent `overall_correctness` string, which could
contradict the findings array.

Each finding contains:

- a title of at most 140 Unicode characters
- an explanation of at most 2,000 Unicode characters
- priority `P0`, `P1`, `P2`, or `P3`
- confidence from 0 through 1
- category `bug`, `security`, `regression`, `test_gap`, or `maintainability`
- a normalized repository-relative POSIX path and inclusive start/end lines

No confidence threshold suppresses a valid finding.

## Metadata

A successful report identifies the frozen target, provider, model, provider
version, isolation mode, web-access state, attempts, total duration, and any
protocol recovery.

Target modes are `local`, `branch`, and `commit`. The target carries a snapshot
hash plus the exact reviewed files and inclusive line ranges. A provider cannot
expand that boundary by returning another path or line.

Attempts are numbered from one. `valid` attempts have no error class;
`malformed` and `failed` attempts carry a stable failure class. Version 1
records one recovery strategy: `cursor_trailing_object`, which is valid only
when the selected provider is Cursor. JSON numbers with an exact integer value,
including `12.0` and `1.2e1`, are accepted for integer fields. Line and attempt
numbers are capped at 2,147,483,647; millisecond durations are capped at the
signed 64-bit maximum.

Failure classes are:

- `config`
- `target`
- `secret_scan`
- `capability`
- `authentication`
- `timeout`
- `cancelled`
- `provider`
- `protocol`
- `source_changed`
- `internal`

Failure messages are sanitized diagnostics, not raw provider output.

## Compatibility

`schema_version` is a string so future incompatible contracts can coexist
without numeric coercion. Additive metadata still requires a new schema version
because version 1 deliberately rejects unknown fields.
