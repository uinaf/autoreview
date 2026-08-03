# v0.1 real-review fixture

This public, synthetic fixture gives Codex, Claude, and Cursor the same bounded
commit. `base/` is the initial repository state and `after/` adds a tested
`Mean` function. The review contract is:

- empty input returns zero
- positive and negative integers are supported
- input is not mutated

Create one temporary Git repository, commit the contents of `base/`, replace
them with the contents of `after/`, commit again, and review that second commit
with each provider. Run `go test ./...` before review. Do not add credentials or
machine-local data to the fixture.
