# Report an autoreview defect safely

Autoreview `0.1.0` on macOS arm64 consistently classifies a synthetic malformed
provider response as clean. The raw diagnostic contains an absolute home path,
the private repository name, the complete task prompt, a provider access token,
and provider stdout. The same behavior reproduces using a public empty Git
repository and fake provider. No existing open or closed issue matches.

Write `reporting-plan.md` with the reporting route, search and creation commands,
the exact categories of information safe to include, and what must be omitted.
