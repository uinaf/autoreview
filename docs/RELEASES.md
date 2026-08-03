# Releases

`autoreview` evaluates a CLI release after every successful push to protected
`main`. Conventional Commits determine whether that evaluation publishes a
version; merges that contain no consumer-facing release type stop without a tag.

## Published artifacts

Each CLI release contains:

- macOS and Linux archives for amd64 and arm64;
- a SHA-256 `checksums.txt` manifest;
- a keyless Cosign bundle for that manifest; and
- GitHub build-provenance attestations for the archives, manifest, and bundle.

The checksum signature protects every archive named by the manifest. GitHub
attestations independently bind each uploaded artifact to the release workflow.
The macOS archives are covered by the Sigstore-signed checksum manifest but are
not Apple Developer ID signed or notarized. The Homebrew cask removes the
quarantine attribute during install; users that do not accept that boundary
should build from source with Go or wait for a future notarized distribution.

## Verify a release

Download one archive plus the manifest and signature bundle from the matching
GitHub Release, then verify the workflow identity and checksum:

```bash
archive=autoreview_v0.1.2_darwin_arm64.tar.gz

cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity \
    "https://github.com/uinaf/autoreview/.github/workflows/ci.yml@refs/heads/main" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt

grep "  ${archive}$" checksums.txt | shasum -a 256 -c -
gh attestation verify --owner uinaf "$archive"
```

Replace the example archive version and target with the release being checked.

## Automation boundary

Pull-request jobs receive read-only repository access and no release secrets.
The main-only release job enters the protected `release` Environment only after
macOS/Linux verification and snapshot packaging pass. It mints a short-lived
`uinaf-release-bot` installation token explicitly scoped to `autoreview` and
`homebrew-tap` with Contents write permission.

Semantic Release owns version selection, release notes, the Git tag, and the
initial GitHub Release. GoReleaser appends binaries, the checksum manifest, and
the Sigstore bundle, then updates the Homebrew cask. No release commit is pushed
to `main`.

If publication fails after the tag is created, rerunning the failed workflow is
safe: a release tag at `HEAD` resumes GoReleaser publication without choosing a
new version. Never delete a published tag merely to retry a partial release.

## Version tracks

CLI SemVer describes the Go executable contract. Tessl skill SemVer describes
the independently published agent package and may advance without changing the
CLI version. The skill documents its compatible CLI requirement; the two
version numbers are not required to match.
