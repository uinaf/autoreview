# Security and issue reporting

The CLI freezes one explicit local, branch, or commit target into a bounded
UTF-8 bundle. It labels repository material as untrusted, scans the complete
bundle with installed TruffleHog in offline mode, invokes provider executables
outside the reviewed repository, and refuses stale source after provider
execution.

Do not bypass a secret-scan, sensitive-path, size, binary-data, symlink,
revision, capability, isolation, or source-change refusal. Do not split an
oversized bundle and claim whole-change cleanliness.

## Public defects

Create a public issue only for a reproducible non-security autoreview defect.
Search open and closed issues first:

```bash
gh issue list --repo uinaf/autoreview --state all --search "$sanitized_summary in:title"
```

The issue may contain only:

- autoreview version
- operating system and architecture
- provider name, isolation mode, and web-access state
- stable failure class
- sanitized reproduction steps using public or synthetic input
- expected behavior and high-level actual behavior

Never include the frozen bundle, task prompt, reviewed source, diff, repository
identity, private or absolute paths, credentials, environment output, provider
command line, or raw provider stdout/stderr. Write a sanitized body to a
temporary file and use `gh issue create --repo uinaf/autoreview --body-file`.

If GitHub access is missing or any field cannot be safely sanitized, do not
create the issue. Tell the user what prevented safe reporting.

## Vulnerabilities

Never open a public issue for suspected secret exposure, command injection,
path traversal, unsafe provider execution, sandbox escape, bundle-boundary
failure, or malformed output accepted as clean. Use private vulnerability
reporting from the `uinaf/autoreview` repository Security tab. Use synthetic,
high-level reproduction details. Even in a private report, never include a
frozen bundle, task prompt, reviewed source or diff, repository identity,
private or absolute path, credential, environment output, provider command
line, or raw provider output.
