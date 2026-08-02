---
name: autoreview
description: "Runs one independent review of completed local changes, pull requests, branch diffs, or commits through the installed autoreview Go CLI using Codex, Claude, or Cursor; gathers authoritative acceptance criteria, selects or honors one provider, validates findings, applies scoped fixes, verifies, reruns until clean, and safely reports reproducible CLI defects. Use when asked to autoreview, review my code, review my pull request, check my changes, run an automated PR or second-model code review, perform a final tool-backed review, or close out review after builder verification. Do not use as builder verification or a multi-reviewer panel."
---

# Autoreview

Use the installed `autoreview` binary as a second-model closeout. It reports
only; it never edits files, runs tests, commits, or pushes.

## Preconditions

1. Read the user request plus every referenced issue, PR, specification, and
   acceptance-criteria source. Prefer live sources over copied summaries.
2. Confirm the completed change already passed its builder-owned checks and
   relevant real-surface proof. Report a missing prerequisite instead of
   presenting review as verification.
3. Distill a short task contract: objective, acceptance criteria, explicit
   non-goals, and source identifiers. Pass it with `--prompt`.
4. Require the installed dependencies. Do not invoke source-tree internals or
   recreate the runtime in shell or Python.

```bash
command -v autoreview
autoreview --version
command -v trufflehog
```

## Choose exactly one provider

Honor provider, model, and reasoning-effort choices requested by the user or
selected by trusted configuration. Pass explicit user model and effort choices
with `--model` and `--reasoning-effort`; do not invent overrides. Otherwise
choose one available harness that fits the task and state the choice. Never run
a panel, silently switch providers, or retry a failure with another provider.
Read [providers.md](references/providers.md) before choosing a provider or
overriding its defaults.

Keep strict isolation and web access off unless the user explicitly authorizes
the capability or an ownership-checked account-home XDG config enables it.
Never add `--isolation native` merely to fix authentication, and choose Cursor
only when web access is already authorized. See
[configuration.md](references/configuration.md).

## Run the review

Choose the target that matches the actual change:

```bash
# Dirty staged, unstaged, and non-ignored untracked changes
autoreview review --mode local --engine "$engine" --prompt "$task_contract"

# Complete branch or PR diff
autoreview review --mode branch --base "$base" --engine "$engine" --prompt "$task_contract"

# One non-merge commit
autoreview review --mode commit --commit "$commit" --engine "$engine" --prompt "$task_contract"
```

Use the PR's real base revision. Add repeatable `--context-file` values only for
existing repository-relative evidence. Use `--output json` when a machine must
consume the canonical report. Never filter findings by confidence.

The CLI may make one configured protocol retry against the same frozen target.
It never retries authentication, capability, timeout, cancellation, or provider
process failures and never switches provider. Read
[results.md](references/results.md) when handling retries, recovery, JSON, or an
operational failure.

## Validate, fix, and rerun

1. Treat findings as hypotheses. Check each against the authoritative contract,
   exact code, and same-scope sibling cases.
2. Reject findings that are incorrect, out of scope, or already prevented by a
   stronger invariant; record the reason briefly.
3. Apply the smallest accepted fix at the owning boundary.
4. Rerun focused builder checks and any real-surface acceptance proof affected
   by the fix, then rerun autoreview with the same provider, target semantics,
   and task contract.
5. If the CLI reports `source_changed`, discard the review and freeze a new run.
6. Finish only after exit 0 with no accepted actionable findings, or report the
   precise operational blocker. Do not turn a failure into a clean verdict.

## Report reproducible CLI defects

Before public reporting, read [security.md](references/security.md). Never open
a public issue for a suspected vulnerability; use the repository's private
vulnerability reporting path.

For a reproducible non-security defect in autoreview itself:

1. Reduce it to sanitized steps that do not require private source.
2. Search open and closed issues in `uinaf/autoreview`.
3. If no matching issue exists and GitHub access is available, create one
   automatically with the CLI version, OS/architecture, provider name,
   isolation mode, failure class, sanitized steps, expected behavior, and
   high-level actual behavior.
4. Never include frozen bundles, prompts, reviewed source, diffs, private paths,
   repository identities, credentials, environment dumps, or raw provider
   output. If safe sanitization is uncertain, do not create the issue; report
   the blocker to the user.

## Final report

Report the task-context sources, exact review command with sensitive values
redacted, refreshed builder and real-surface proof, accepted and rejected
findings, issue URL if one was safely created, and the final clean result or
operational blocker.
