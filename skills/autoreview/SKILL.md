---
name: autoreview
description: "Runs one independent review of completed local changes, pull requests, branch diffs, or commits through the installed autoreview Go CLI using Codex, Claude, Cursor, or Grok; gathers authoritative acceptance criteria, selects or honors one provider, validates findings, applies scoped fixes, verifies and reruns accepted fixes, and safely reports reproducible CLI defects. Use when the user asks for an autoreview, a review of their code, pull request, or changes, an automated PR or second-model code review, a final tool-backed review, or review closeout after builder verification. Do not use as builder verification or a multi-reviewer panel."
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
4. Require the installed dependencies. If `autoreview` is missing, stop and
   offer the canonical install command for the host. Run it only with user
   approval:

   ```bash
   # macOS: signed and notarized release
   brew install --cask uinaf/tap/autoreview

   # Linux or another Go-managed environment: latest tagged release
   go install github.com/uinaf/autoreview/cmd/autoreview@latest
   ```

   Do not invoke source-tree internals, build an ad hoc replacement, or
   recreate the runtime in shell or Python. Report any other missing dependency
   instead of guessing how the user manages the host.

```bash
command -v autoreview
autoreview --version
command -v trufflehog
```

## Choose exactly one provider

Honor provider, model, and effort choices from the user or trusted config; do
not invent overrides. Pass explicit model and effort flags except for Cursor,
whose effort belongs in its model ID. Report unsupported combinations. If no
source chooses a provider, select one installed harness and state why. Never
run a panel or fall back. Read [providers.md](references/providers.md).

Native isolation is the default and uses normal provider login from an empty
bundle-only workspace. Select strict explicitly only when the task requires the
hardened provider-state boundary and a supported API key is available. Keep web
access off for Codex, Claude, and Grok unless configured; the skill's explicit
`--engine cursor` selection implicitly enables an otherwise-unset value because
Cursor cannot guarantee web-off. Repository, environment, or XDG engine selection
does not grant web access. Honor any explicit `web_access: false`, which makes
Cursor unavailable. See
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
4. Confirm every fix belongs to the next frozen target: local includes the
   worktree, branch requires a commit on that branch, and commit mode requires
   an amended commit. If committing is not authorized, report the blocker.
5. Rerun focused builder checks and any real-surface acceptance proof affected
   by the fix, then rerun autoreview with the same provider, target semantics,
   and task contract.
6. If the CLI reports `source_changed`, discard the review and freeze a new run.
7. Finish after exit 0 with no findings, or after exit 1 only when every finding
   is explicitly rejected. Report exit 1 as findings, never clean. Exit 2 is an
   operational blocker; do not turn it into a clean verdict.

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
all-rejected findings result, or operational blocker.
