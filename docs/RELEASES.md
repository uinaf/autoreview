# Releases

`autoreview` evaluates a CLI release after every successful push to protected
`main`. Conventional Commits determine whether that evaluation publishes a
version; merges that contain no consumer-facing release type stop without a tag.

## Published artifacts

Each CLI release contains:

- macOS archives containing Developer ID signed and notarized binaries for
  amd64 and arm64;
- Linux archives for amd64 and arm64;
- a SHA-256 `checksums.txt` manifest;
- a keyless Cosign bundle for that manifest; and
- GitHub build-provenance attestations for the archives, manifest, and bundle.

The checksum signature protects every archive named by the manifest. GitHub
attestations independently bind each uploaded artifact to the release workflow.
The macOS binaries are signed with hardened runtime and a secure timestamp
before Apple accepts their notarization submissions. Their Apple signing ID is
`autoreview`; managed execution controls can combine that ID with the expected
Apple Team ID. Apple creates tickets for standalone binaries but does not
support stapling tickets to them, so Gatekeeper retrieves the ticket online.

## Linux installer trust boundary

The installer script and the release tag, matching archive, and `checksums.txt`
it obtains all come over HTTPS from GitHub. The checksum detects a corrupt or
mismatched archive, but because the installer, archive, and checksum use the
same transport and hosting boundary, it does not provide independent provenance
verification. The installer never modifies shell startup files or uses
privilege escalation.

For independent verification, download the artifacts instead and use the
Cosign signature and GitHub build attestation workflow below before installing
the binary.

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
`uinaf-releaser` installation token explicitly scoped to `autoreview` and
`homebrew-tap` with Contents write permission.

The protected `release` Environment stores the Developer ID certificate,
certificate password, and notary private key as secrets. It stores the Apple
issuer, key, and Team IDs as environment variables. GitHub injects the three
secrets only into the main-only publication step; pull requests cannot access
them. The identifier variables also enter that step, and the Team ID is reused
by post-publication signature verification. Before publication, the credential
verifier checks the certificate trust, matching signing private key, and
identifiers and rejects a certificate outside the Team ID pinned by
`APPLE_TEAM_ID`.

Semantic Release owns version selection, release notes, the Git tag, and the
initial GitHub Release. GoReleaser Developer ID signs both macOS binaries,
waits for Apple to accept both notarization submissions, appends all archives,
the checksum manifest, and the Sigstore bundle, then updates the Homebrew cask.
No release commit is pushed to `main`.

If publication fails after the tag is created, rerunning the failed workflow is
safe: a release tag at `HEAD` resumes GoReleaser publication without choosing a
new version. This includes Apple service failures before artifact publication.
Never delete a published tag merely to retry a partial release.

## Version tracks

CLI SemVer describes the Go executable contract. Tessl skill SemVer describes
the independently published agent package and may advance without changing the
CLI version. The skill documents its compatible CLI requirement; the two
version numbers are not required to match.
