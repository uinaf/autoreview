# Close out a completed pull request

A Go pull request is implementation-complete and its full test suite has passed.
No provider is named by the user or repository configuration. Codex CLI, Claude
Code, and Cursor Agent are installed. No task requirement needs web access.

Write `closeout.md` containing the exact review command you would run, the
decision behind it, how you would handle failure, and what evidence the final
report must contain. The pull request targets `main` and its acceptance criteria
are: preserve stable JSON output and terminate provider children on SIGINT.
