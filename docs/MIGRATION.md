# Python-to-Go migration matrix

The behavioral baseline is the existing `uinaf/agents` autoreview skill,
including its 11,768-line Python helper, 7,327-line hardening suite, fixtures,
references, evals, and agent metadata as inspected on 2026-08-02. Its upstream
provenance points to OpenClaw agent-skills commit `66cf3df`, under MIT.

The paused exploratory worktree named in the original brief was not present at
the supplied path when this matrix was created. It is therefore not treated as
an implementation source or unexplained behavior gap.

Disposition means:

- **Port**: preserve the observable contract in Go.
- **Improve**: replace it with the stated tested behavior.
- **Obsolete**: intentionally exclude it from v0.1 with a concrete reason.
- **Defer**: preserve the architectural boundary but deliver it after the
  proven local package.

Every non-obsolete row links to its owning implementation issue. All Port and
Improve rows owned by issues #2 through #9 are implemented for v0.1; Defer rows
remain explicit post-proof boundaries, and Obsolete rows are intentional
exclusions rather than gaps.

## CLI and orchestration

| Baseline behavior | Disposition | Go v0.1 contract | Owner |
| --- | --- | --- | --- |
| Auto-select local or branch target | Improve | Require explicit local, branch, or commit mode; never guess the reviewed diff | #3 |
| `local` and `uncommitted` aliases | Improve | Keep `local`; drop the redundant alias | #3 |
| Branch diff against explicit or detected base | Improve | Require an explicit base; no GitHub lookup in core | #3 |
| Commit review with merge-commit rejection | Port | Validate the exact revision and reject ambiguous merge commits | #3 |
| Pull-request base discovery through `gh` | Obsolete | A checked-out PR is a branch target; GitHub integration belongs to #11 | #11 |
| Inline prompt, prompt files, and datasets | Improve | Keep `--prompt`; replace both file concepts with repeatable repo-relative context files | #8 |
| Provider defaults to Codex | Improve | Require one engine from flags or config | #4 |
| Model and thinking defaults | Improve | Use typed explicit/configured values without hidden provider choice | #4 |
| Preferred-model fallback chain | Obsolete | Never change model or provider automatically; prevents surprise cost | #8 |
| Panels and partial-panel merging | Obsolete | One reviewer per run unless a future explicit design reintroduces panels | #8 |
| Parallel test command | Obsolete | Builders and verification own tests; autoreview never executes repo commands | #8 |
| Dry-run and hidden Python self-tests | Improve | Go tests plus explicit config/target diagnostics replace internal test flags | #4, #8 |
| Human output file and separate JSON file | Improve | Stable stdout terminal or JSON rendering; callers own redirection | #8 |
| Engine-output streaming | Obsolete | Bound and buffer provider output for trustworthy validation | #5 |
| Finding substring expectations for harness tests | Improve | Go fixtures assert typed results directly | #2, #8 |

## Review input and security

| Baseline behavior | Disposition | Go v0.1 contract | Owner |
| --- | --- | --- | --- |
| Freeze staged, unstaged, untracked, branch, or commit content | Port | Materialize one immutable reviewed target before invocation | #3 |
| Source snapshot before and after review | Port | Invalidate stale results as `source_changed` | #3 |
| TruffleHog verified/unknown scan | Port | Require the external Go binary and fail closed | #3 |
| Synthetic Git history for additions and deletions | Port | Scan both added and deleted reviewed bytes without unchanged history | #3 |
| Thousands of language-specific secret heuristics | Improve | Use TruffleHog plus narrow sensitive-path and structural guards; do not embed brittle parser emulation | #3 |
| Sensitive file omission/redaction | Improve | Reject or explicitly omit before the complete bundle scan; never append unscanned context | #3 |
| Binary, gitlink, invalid UTF-8, and incomplete-input guards | Port | Reject inputs the reviewer cannot interpret completely | #3 |
| Symlink and path-traversal guards | Port | Resolve within the Git root and reject escapes | #3 |
| Safe Git config and trusted excludes | Port | Invoke native Git with fixed arguments and hardened environment | #3 |
| Bundle and prompt byte bounds | Improve | One configurable aggregate byte limit with contributor diagnostics | #3 |
| Multi-pass chunk packing | Obsolete | Fail oversized before invocation; chunks cannot prove cross-file contracts | #3 |
| Repository material marked untrusted | Port | Delimit bundle/context and prohibit command construction from content | #3, #8 |
| Output paths forbidden inside reviewed repo | Obsolete | v0.1 does not write report files | #8 |
| Sanitized test environment | Obsolete | v0.1 does not run tests | #8 |
| Credentialed proxy and injected process-variable guards | Port | Provider runner forwards only validated environment data | #5 |

## Providers and isolation

| Baseline behavior | Disposition | Go v0.1 contract | Owner |
| --- | --- | --- | --- |
| Codex executable discovery and capability checks | Port | Real adapter with fake-executable tests and an opt-in authenticated smoke | #5 |
| Claude Code executable discovery and safe-mode checks | Port | Real adapter with fake-executable tests and an opt-in authenticated smoke | #6 |
| Cursor Agent executable discovery and sandbox checks | Port | Real adapter with fake-executable tests and an opt-in authenticated smoke | #7 |
| Existing CLI authentication reuse | Port | Reference provider auth in place; never store credentials | #5, #6, #7 |
| Empty bundle-only provider workspace | Port | Default `strict` isolation | #4, #5, #6, #7 |
| Provider user rules, skills, plugins, and memory disabled | Improve | Strict mode disables them; explicit trusted `native` mode preserves them | #4 |
| Web search enabled by default | Improve | Default off; only CLI or trusted XDG config may enable | #4 |
| Claude read-only web tools | Improve | Capability-aware strict/native policy, with web separately authorized | #6 |
| Process heartbeat and platform `ps` sampling | Improve | Bounded progress plus duration metadata without scraping process tables | #5 |
| Timeout, cancellation, and child cleanup | Port | Shared Go process runner terminates the provider tree before leader reap on every exit | #5, #29 |
| Bounded and terminal-escaped provider output | Port | Shared runner bounds raw streams and exposes sanitized diagnostics | #5 |
| Codex structured response decoding | Improve | Codex-compatible generation schema plus strict canonical local decoding | #5 |
| Claude structured envelope decoding | Port | Strict provider review schema | #6 |
| Cursor outer envelope decoding | Port | Strict outer envelope plus strict inner review | #7 |
| Cursor prose before valid trailing JSON fails | Improve | Accept one unambiguous complete trailing object and record recovery | #7 |
| Cursor arbitrary mixed JSON extraction | Obsolete | Ambiguous, fenced, multiple, or suffix output remains a protocol failure | #7 |
| Multiple model attempts on availability errors | Obsolete | No automatic model fallback | #8 |
| Repeated protocol attempts | Improve | Configurable zero or one retry after local recovery fails | #8 |

## Result validation and reporting

| Baseline behavior | Disposition | Go v0.1 contract | Owner |
| --- | --- | --- | --- |
| Provider object with findings and overall fields | Improve | Separate strict provider review from final report metadata | #2 |
| `overall_correctness` may disagree with findings | Improve | Derive `clean` or `findings` from validated finding count | #2 |
| P0-P3, confidence, category, title, body | Port | Typed enums, bounds, and no confidence suppression | #2 |
| Single finding line | Improve | Inclusive start/end range | #2 |
| Out-of-scope findings are ignored and may flip verdict clean | Improve | Reject unreviewed paths or lines as protocol failure | #2, #8 |
| No first-class operational result | Improve | `failure` status with stable error class and sanitized message | #2 |
| Provider/model usage printed separately | Improve | Attach provider, model, version, attempts, and duration as metadata | #2, #8 |
| Source identity described in prose | Improve | Typed target mode, revisions, snapshot hash, files, and line ranges | #2, #3 |
| Protocol recovery invisible | Improve | Record applied recovery and strategy | #2, #7 |
| Findings and incorrect verdict exit 1; most errors also exit nonzero | Improve | Stable exits 0 clean, 1 findings, 2 failure | #2, #8 |
| Terminal control escaping | Port | Terminal renderer and diagnostics escape controls | #8 |
| JSON report written atomically to arbitrary path | Improve | Emit versioned JSON on stdout; no core filesystem reporter | #8 |
| JSONL streaming | Defer | Add only with a demonstrated automation need | #11 |
| GitHub Checks and annotations | Defer | Idempotent exact-SHA reporter after local proof | #11 |

## Skill, packaging, and verification

| Baseline behavior | Disposition | Go v0.1 contract | Owner |
| --- | --- | --- | --- |
| Skill bundles Python helper and shell harness | Improve | Thin standalone skill invokes installed Go binary | #9 |
| Skill references provider/scope/troubleshooting docs | Port | Concise one-hop public references | #9 |
| Existing five workflow evals | Improve | Port useful cases and add config, isolation, failure, recovery, and issue-reporting evals | #9 |
| Tessl packaging in `uinaf/agents` | Improve | Use current official folder-level package format in this repo | #9 |
| Tessl score previously 94 | Improve | Legitimate pinned 100/100 publication gate | #9 |
| Python self-test and unittest suite | Improve | Table-driven Go unit/integration tests and language-neutral fixtures | #2-#8 |
| Runtime Python dependency | Obsolete | Repository and runtime remain Go-only | #1 |
| Global skill replacement during development | Defer | Keep legacy reviewer until real-provider and publication proof passes | #10 |
| CI, signed binaries, Homebrew, automated Tessl publish | Defer | Post-proof infrastructure epic | #11 |

## v0.1 completion evidence

The matrix is closed by contract area rather than duplicating a proof command
on every row:

| Contract area | Evidence |
| --- | --- |
| Result protocol and reporting | Schema conformance, semantic decoder, renderer, retry, exit-code, and CLI end-to-end tests in `mise run verify` |
| Frozen targets and secret scanning | Git-boundary tests plus real TruffleHog integration in `mise run verify:release` |
| Configuration and isolation | Typed precedence, trusted-XDG, strict/native runtime, and capability tests in `mise run verify` |
| Codex, Claude, and Cursor | Fake-executable contract suites plus the bounded same-commit fixture in `testdata/v0.1-fixture/` |
| Go-only runtime and supported source targets | `mise run runtime:go-only` and `mise run build:cross` |
| Go dependency vulnerability analysis | Pinned `govulncheck` binary against the current official database in `mise run vuln` |
| Standalone skill and evals | Package/privacy/link/CLI compatibility tests plus `mise run skill:review` |
| Public skill distribution | Published `uinaf/autoreview@0.1.1` registry install proof |
| Source distribution | Post-merge signed tag and isolated Go install gates recorded on issue #10 |

`mise run verify:release` composes the deterministic v0.1 checks with installed
TruffleHog integration and the live Go vulnerability database gate. The scanner
gate fails when the executable is missing or incompatible; freezing the current
checkout remains a separate smoke. Run `mise run test:current-checkout`
explicitly to opt in. The hosted Tessl review remains a separate authenticated
gate. Real-provider outcomes, tag verification, and clean-room installation are
release evidence reported on issue #10 rather than machine-local facts stored
in this public document.
