# Classify two Cursor review outputs

An operator supplies two sanitized Cursor results:

- Run A contains a short prose sentence followed by one canonical review object
  that consumes the entire remaining output. Its report records
  `protocol_recovery.applied=true` and strategy `cursor_trailing_object`.
- Run B contains a fenced review object followed by another JSON object and
  suffix prose. It exits 2 with failure class `protocol` after the configured
  retry.

Write `classification.md` with the disposition of each run and the next action.
