# Frozen target boundary

Autoreview resolves exactly one explicit target before invoking a provider:

| Mode | Required identity | Material |
| --- | --- | --- |
| `local` | current `HEAD`, or the repository-format empty tree when unborn | separate staged and unstaged diffs, deletions, and non-ignored untracked files |
| `branch` | merge base of the explicit base revision and `HEAD`, plus resolved `HEAD` | committed merge-base-to-head diff |
| `commit` | one resolved non-merge commit | parent-to-commit diff, or empty-tree-to-commit for a root commit |

Revision input is never auto-detected, fetched from GitHub, or interpreted as a
command option. Git runs through fixed argument arrays with hooks, filters,
credentials, global configuration, external diffs, text conversion, and
filesystem monitors disabled where applicable. Content-producing commands use
an isolated temporary Git directory, copied index, empty attribute source, and
read-only access to the repository object database; repository config and
`info/attributes` therefore cannot select executable filters. Merge bases and
commit parents are resolved in that same raw object view, without replacement,
graft, or shallow-boundary metadata.

## Frozen bundle

The boundary freezes the resolved target identity, exact diff, raw deleted blobs,
untracked file contents, task prompt, and repeatable repository-relative
context files into a length-delimited UTF-8 payload. Repository diff and context
sections are explicitly labeled as untrusted data. Providers receive a copied
payload and typed metadata, not a path to the live checkout.

The aggregate bundle limit defaults to 1 MiB and is configurable up to 1 GiB.
An oversized target fails before provider execution, reports the largest byte
contributors from a bounded streaming count, and is never chunked. Deleted,
untracked, and context reads share the same aggregate budget. Binary data,
invalid UTF-8, sensitive paths,
Git links, changed symlinks, symlink escapes, merge commits, unsafe revisions,
FIFOs and other special files, context-path symlinks, and incomplete file reads
fail closed. Git split indexes are rejected rather than copied into the isolated
metadata directory.

Target collection requires Git 2.41 or newer.

The snapshot includes resolved Git identity plus the raw copied index, tracked working-tree,
status, untracked target, prompt, and context state. Collection is repeated
before scanning to catch concurrent reads. The caller must run the supplied
unchanged check after provider completion; a mismatch invalidates the result as
`source_changed`.

## Secret scan

The installed `trufflehog` executable scans the complete frozen payload,
including deleted bytes and appended context. It runs offline with verification
disabled, no update check, one worker, no inherited environment, and
`--fail-on-scan-errors`. Any detection, scan error, missing executable, output
overflow, or cancellation is an operational failure. The scanner runs in its
own process group and terminates remaining descendants before returning on any
exit path. TruffleHog is an external runtime dependency; its AGPL code is not
linked or embedded in this MIT project.
