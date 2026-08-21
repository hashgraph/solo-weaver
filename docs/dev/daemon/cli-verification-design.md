# Design: daemon-side verification of the solo-provisioner CLI

> Status: proposal, not built. Companion to the bin-dir hardening that made
> `/opt/solo/weaver/bin` `root:root 0755`.

## Scope

The CLI binary the daemon exec's. The *daemon* binary the CLI installs is a
separate matter, already settled: it is verified by SHA-256 against the digest of
the co-released daemon, stamped into the CLI at link time, or against the
release's published `.sha256` asset for any other version
(`pkg/software/daemon_digest.go`).

## Problem

The unprivileged daemon (`User=weaver`) performs privileged work by exec'ing the
root `solo-provisioner` CLI through a NOPASSWD sudoers grant. Whatever binary sits
at the granted path runs as root. The bin-dir permissions stop a *weaver*-level
actor from swapping it, but not a tampered self-upgrade download, a compromised
mirror, or a swap by another root-capable process. The daemon should prove the
binary it is about to run as root is an authentic release, and it must do so with
**no outbound network call at exec time**.

## Layers

| Layer | Defends against | Security boundary? |
|---|---|---|
| bin dir `root:root 0755` (built) | weaver-level binary swap | yes |
| authenticity check at download | tampered or MITM'd download, bad mirror | yes |
| sha recorded in a `root:root` manifest | corruption, wrong version | integrity only |
| re-check before exec | swap by another root process, permissions regression | yes, defence in depth |

The manifest sha is a fast pre-check and a `/status` field, never the security
boundary; the pre-exec decision has to be a full re-verification.

## Trust anchor: open

The CLI the daemon exec's can be any released version, including one published
after the daemon was built. So the anchor has to validate an artifact the verifier
has never seen, which is what rules out the digest-pinning the daemon download
uses.

A verification key committed to this repository is also not available: the release
signing key is an organisation-wide secret rotated outside this repository, so a
rotation invalidates every binary already deployed.

That leaves an anchor that is an *identity* rather than a key. The leading option
is GitHub artifact attestations / Sigstore keyless, verified against the release
workflow's OIDC identity (`hashgraph/solo-weaver`), which never rotates. It costs
a vendored `sigstore-go` and a dependency on the Sigstore TUF root, which rotates
itself.

## Open questions

- Where the daemon gets its verification material for the installed binary, given
  the no-network constraint at exec time. Copying it in beside the binary at
  install time is the obvious answer; fetching on demand is not available.
- Revocation: how a daemon running for months learns that an identity or key it
  trusts has been revoked. An attacker who controls the download can withhold a
  revocation, so this needs a second mechanism such as a minimum-acceptable-date
  floor bumped by releases.
- Whether the CLI also verifies itself on every privileged invocation, or only the
  daemon verifies before delegating.

## Acceptance criteria

- The daemon refuses to exec a CLI that fails verification, and surfaces a
  `StatusError` rather than failing silently.
- No outbound network call on the pre-exec path.
- Self-upgrade rejects a download that fails verification, leaving the previously
  installed binary in place.
- Verification is pure Go and its tests run on macOS.
