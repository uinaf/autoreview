# Handle Cursor web capability refusal

Cursor was selected for a completed commit review. Web access is false and the
adapter returns exit 2 with failure class `capability`, explaining that Cursor
cannot guarantee a per-run web disable. Codex and Claude are installed, but the
user explicitly configured web access off and did not authorize a provider
change. The user also requested Cursor with a separate `reasoning_effort: high`
value but did not choose a compatible effort-bearing model ID.

Write `decision.md` with the current verdict, whether to retry or switch, how to
handle the unsupported effort choice, and the precise choices that require user
selection.
