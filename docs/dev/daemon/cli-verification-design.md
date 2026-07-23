# Design: Daemon-side verification of the solo-provisioner CLI (embedded GPG key)

> Status: proposal / ticket draft. Companion to the bin-dir hardening that made
> `/opt/solo/weaver/bin` `root:root 0755`.

## Problem

The unprivileged daemon (`User=weaver`) performs privileged work by exec'ing the
root `solo-provisioner` CLI via a NOPASSWD sudoers grant. Whatever binary sits at
the granted path runs as root. The bin-dir permission fix stops a *weaver*-level
actor from swapping that binary, but we still want the daemon to **prove the CLI
it is about to run as root is an authentic Hashgraph release** — defending against
a tampered/MITM'd self-upgrade download, a compromised mirror, or a binary swapped
by some other root-capable process — with **no outbound network call at verify
time**.

## Trust anchor

The release pipeline already publishes, per binary (`taskfiles/cli.yaml`,
`taskfiles/daemon.yaml`, `.releaserc`):

- `<binary>`            — the binary
- `<binary>.sha256`     — checksum
- `<binary>.sha256.asc` — GPG detached signature of the checksum
- `<binary>.asc`        — GPG detached signature of the binary

The trust anchor is therefore the **GPG public key**, not the bare SHA. The key is
stable across releases, so it is embeddable at build time; a checksum is not (a
binary cannot know the SHA of a version released after it, and a self-written
checksum is self-attested). We embed the key; we do not embed or trust host-stored
checksums as an authenticity control.

### Key model: embedded primary public key + rotatable signing subkeys (recommended)

A key clarification drove this design: embedding a verification key does **not**
force a daemon upgrade for new CLI *versions* — a daemon verifies any future CLI
signed by a key it trusts, regardless of version. The only event that would force
a daemon update under naive single-key embedding is a **signing-key rotation**
(rare: years apart, or on compromise). The design optimizes for that rare event.

We must never bootstrap trust by *fetching the very key we use to verify* — an
attacker who controls the key endpoint (MITM, compromised hosting account, DNS)
could serve their own key and sign a malicious CLI that verifies cleanly, yielding
root. There must always be an anchor that is not itself fetched untrusted.

GPG's primary/subkey model gives both rotation-without-daemon-upgrade **and** a
durable offline anchor:

- The release identity is a **long-lived primary key** whose private half stays
  offline / air-gapped and effectively never changes.
- Day-to-day releases are signed by **signing subkeys** certified by the primary.
  Subkeys live in CI and can be rotated freely.
- The daemon embeds only the **primary public key**. To trust a newly minted
  subkey, the daemon may **download the updated public key block and verify the
  subkey's binding signature chains to the embedded primary public key** — the download is
  safe precisely because its authenticity is checked against the embedded anchor,
  not taken on faith. Rotating a signing subkey therefore needs **no daemon
  upgrade**.

Ranked options (we adopt the first):

| Approach | Rotation w/o daemon upgrade | Anchor | Verdict |
|---|---|---|---|
| **Embed primary public key, rotate subkeys (download + verify chain to primary)** | unlimited | offline primary private key | **recommended** |
| Embed a short ordered key list | within the list | offline keys | simpler interim fallback |
| Embed a single key | none (rotation needs update) | offline key | simplest, weakest |
| Download key with no embedded anchor | unlimited | key URL + PKI + DNS | rejected: trust-on-download, root-equivalent target |

The short-list approach is an acceptable simpler starting point if subkey
structure is not ready; it covers planned rotations within the pre-embedded set
and falls back to a daemon update only for rotations beyond the horizon (a
compromise probably warrants a daemon update anyway).

## Design overview

```
   release: offline PRIMARY key (air-gapped) certifies CI SIGNING SUBKEYS
        | publishes binary + .asc (signed by a subkey) + current public key block
        v
  [CLI install / self-upgrade]  -- HTTP ok, downloading anyway --
        1. download <binary> + <binary>.asc + public key block
        2. verify the public key block: subkey binding chains to EMBEDDED primary public key
        3. verify .asc against the now-trusted subkey            <-- authenticity gate
        4. atomic promote into root:root bin dir, alongside .asc + verified key block
        5. record verified {name,version,sha256} in root:root manifest
        v
  [daemon pre-exec hook]  -- no network --
        6. before sudo-exec'ing the CLI, re-verify the on-disk binary's .asc
           against the on-disk key block, whose subkey chains to the daemon's
           EMBEDDED primary public key
        7. exec only on success; otherwise refuse + emit StatusError
```

The embedded anchor is the **primary public key only**. Signing subkeys are
verified by chaining to it, so they rotate without a daemon upgrade. The on-disk
key block (copied in at install) keeps the pre-exec path **network-free**.

Decision (confirmed): pre-exec uses **full signature re-verification (option b)**,
not a cheap hash-vs-manifest compare. The manifest SHA is retained only as a fast
integrity/version pre-check and for `/status` reporting, never as the security
boundary.

## Components / changes

### 1. New package `pkg/codesign` (pure Go)

- Vendored dep: `github.com/ProtonMail/go-crypto/openpgp` (no shelling to `gpg`,
  consistent with the daemon's no-raw-tool posture). Requires `go mod vendor`.
- Embedded **primary public key(s)** — a short ordered list of the public halves of
  long-lived *primary* keys (the rotation horizon they cover is the *primary*, not
  the day-to-day signing key). Only public key material is embedded; the primary
  *private* key never leaves the offline signer. Pinned by fingerprint; a parse-able
  key with an unexpected fingerprint is rejected.
- `VerifyKeyring(keyringPath string) (*Keyring, error)` — loads a downloaded/
  on-disk public key block and accepts only the keys/subkeys whose binding
  signatures chain to an embedded primary public key. Returns the validated keyring.
- `VerifyDetached(artifact, sigPath string, kr *Keyring) error` — verifies a
  detached armored signature against the validated keyring; `errorx` error on
  failure.
- `VerifyBinary(binPath, keyringPath string) error` — derives `<binPath>.asc`,
  loads + validates the keyring, then `VerifyDetached`.
- Errors carry `ErrPropertyResolution` so the doctor layer surfaces an actionable
  message (e.g. "reinstall via `sudo solo-provisioner daemon service install`").

### 2. Embed the primary public key at build time

- Embed armored **primary public key(s)** via `//go:embed` inside `pkg/codesign`.
  Embedding the *public* key (not a host file) is required so the anchor ships inside
  the signed verifier and is not host-mutable. The primary private key is never
  embedded — it stays offline. Signing subkeys are not embedded either; they are
  validated at runtime by chaining to the embedded primary public key.
- Both the CLI and the daemon import `pkg/codesign`, so both carry the same anchor.

### 3. Install / self-upgrade gate (CLI side)

- In the daemon-binary install path (`pkg/software/daemon_installer.go` `Install`)
  and the CLI self-install/self-upgrade path: after download and before promote,
  download the `.asc` **and the current public key block**, call
  `codesign.VerifyBinary(binary, keyring)` (which first chains the key block to the
  embedded primary public key, then verifies the signature), and only then `installFile` into
  the bin dir. Copy the verified `.asc` **and the verified key block** next to the
  binary so the daemon's later check stays network-free.
- Reuse `VerifyChecksum` (`pkg/software/integrity_checker.go`) as a cheap pre-step;
  signature verification is the authoritative gate.
- After promote, write/refresh a `root:root 0644` manifest in the bin dir, e.g.
  `bin/.binaries.json`: `[{name, version, sha256}]`.

### 4. Daemon pre-exec hook (daemon side)

- Define the **single** delegation chokepoint the daemon uses to shell out to the
  CLI (today none exists; future `self-upgrade` / decommission delegation will
  flow through it). Proposed: a small `internal/daemon/cli` runner `Run(ctx, args...)`
  that:
    1. resolves the CLI path (config-pinned, see below),
    2. calls `codesign.VerifyBinary(cliPath, <bin-dir key block>)`,
    3. on success execs `sudo <cliPath> <args...>`; on failure refuses and records
       a `StatusError` (reason `CLIVerificationFailed`) visible in `/status`.
- No network. The only inputs are the on-disk binary, its `.asc`, the on-disk key
  block, and the embedded primary public key.

### 5. daemon.yaml

- Add `cli_binary_path` (default `/opt/solo/weaver/bin/solo-provisioner`) so the
  chokepoint has an explicit, validated target. Validate with a `sanity.*` path
  helper; reject paths outside the allowed bin dirs.
- Do **NOT** store a CLI checksum in daemon.yaml: `ConfigDir` is `root:weaver 2775`
  (group-writable), so a checksum there is not a trust anchor. Authenticity comes
  from the signature + embedded primary public key only.

## Key rotation

- **Signing-subkey rotation (common, no daemon upgrade):** mint a new signing
  subkey under the existing primary, publish the updated public key block in
  releases. Daemons trust the new subkey because it chains to the embedded primary
  public key. Nothing to ship to running daemons.
- **Primary-key rotation (rare — primary compromise or scheduled multi-year roll):**
  this is the only case needing a daemon update. Add the new primary public key to the
  embedded short list, ship CLI + daemon releases whose key block is cross-signed by the
  **old** primary (so existing daemons still chain-validate it), cut over to the new
  primary, then drop the old primary public key in a later release once all daemons
  have moved.
- Pin by fingerprint; log the primary fingerprint + signing-subkey fingerprint that
  satisfied verification, for audit.

### Primary rotation via self-upgrade

A new daemon binary can embed a new primary public key, so primary rotation rides the
normal self-upgrade path rather than being a special operation. The daemon binary
is just another signed artifact installed by the root CLI, which verifies it the
same way the daemon verifies the CLI.

The catch is bootstrapping: the artifact that *teaches* a node the new primary must
still be trusted by the node's *current* verifier, or self-upgrade fails closed.
There is a who-verifies-whom dependency:

- The **daemon verifies the CLI** it execs → a CLI signed under `P_new` only runs
  once the daemon already embeds `P_new`.
- The **CLI verifies the new daemon binary** during self-upgrade → that binary only
  installs on an old node if it still chains to `P_old`.

Safe sequence (**roll the verifiers before cutting over the signing**):

1. Release the new **daemon** binary that *embeds* `P_new` but is *signed chaining to
   `P_old`* (cross-signed). Old CLIs accept and install it; the node now embeds
   `P_new`.
2. Once daemons across the fleet embed `P_new`, release the **CLI** signed under
   `P_new`.
3. Drop `P_old` from the embedded list in a later release.

Get the order backwards and self-upgrade bricks, forcing manual installs fleet-wide.

**Compromise recovery:** if `P_old` is compromised you cannot keep cross-signing
with it, so there is no clean automated cutover. The escape hatch is manual: an
operator with root runs `sudo solo-provisioner daemon service install` with a fresh,
out-of-band-verified daemon binary — there the operator *is* the trust anchor and
the daemon's own verification is bypassed by design. This is why primary-key
compromise is classified as "needs operator action."

## Release pipeline changes

The current pipeline signs with a single GPG key (`taskfiles/cli.yaml`,
`taskfiles/daemon.yaml` `sign:cli` / `sign:daemon` produce `.asc` / `.sha256.asc`).
To support this design it must:

1. **Restructure the signing key into primary + signing subkey(s).** The primary
   private key is generated once and kept **offline / air-gapped**; only a signing
   subkey is exported to CI for release signing. (If the current key is a bare
   primary used directly for signing, it becomes the primary and a signing subkey
   is added under it.)
2. **Publish the public key block as a release asset** (e.g.
   `solo-provisioner-pubkey.asc` containing the primary + current subkeys with
   binding signatures), alongside the existing `<binary>.asc`. Add it to
   the `.releaserc` assets.
3. **Keep signing the binary directly** (`<binary>.asc`) — that is what the daemon
   verifies. The `.sha256` / `.sha256.asc` remain for integrity/manual checks.
4. **Export the primary public key** into the repo (`pkg/codesign` `//go:embed`
   source) — public key material only — and document the procedure so an audit can
   confirm the embedded anchor matches the offline primary's fingerprint.
5. **Document the subkey-rotation runbook** (mint subkey, re-export key block,
   verify it still chains to the published primary public key) and the rarer
   primary-rotation runbook (embed new primary public key, cross-sign, cut over,
   drop old primary public key).

## What each layer defends (honest labeling)

| Layer | Defends against | Security? |
|---|---|---|
| bin dir `root:root 0755` (done) | weaver-level binary swap | yes |
| sig-verify at download (subkey chains to embedded primary public key) | tampered/MITM download, bad mirror, substituted key | yes (primary) |
| sha in root:root manifest | corruption, wrong version | integrity only |
| sig re-verify pre-exec (chains to embedded primary public key) | swap by other root, perms regression | yes (defense-in-depth) |

## Testing

- Unit (`pkg/codesign`): valid sig passes; tampered binary fails; signature by a
  subkey **not** chaining to the embedded primary public key fails; signature by a valid
  rotated subkey (new subkey under same primary) passes; second-primary (rotation
  list entry) passes; malformed armor / unbound subkey fails. Generate a test
  primary + subkey in-test (pure Go; runs on macOS).
- Unit (installer): promote is skipped when verification fails; manifest written on
  success.
- Unit (daemon runner): exec refused + `StatusError` set on verification failure;
  exec attempted on success (inject a fake exec/verifier).
- Integration (VM): full self-upgrade happy path + a corrupted-download negative.

## Acceptance criteria

- Daemon refuses to exec a CLI whose `.asc` does not verify against an embedded key.
- Self-upgrade rejects a download whose signature does not verify, leaving the prior
  binary in place.
- No outbound network call on the daemon pre-exec path.
- Key rotation documented and covered by a test.
- `pkg/codesign` is pure Go and runs in CI on macOS; new dep vendored.

## Open questions

- Where does the daemon obtain the `.asc` and key block for the *installed* binary —
  copied next to it at install time (proposed) vs re-downloaded on demand (rejected:
  needs network). Proposal: copy at install.
- Should the CLI verify *itself* on every privileged invocation, or only the daemon
  verify the CLI before delegating? (Proposal: daemon verifies before delegating;
  CLI self-verify is optional later hardening.)
- Confirm the current release key is (or can be restructured into) a primary with
  signing subkeys, and that its type/curve is supported by `ProtonMail/go-crypto`.
- **Revocation:** how does a years-running daemon learn a leaked *signing subkey*
  has been revoked? Options: (a) honor revocation certificates shipped in the
  downloaded key block (a revoked subkey fails chain validation); (b) a minimum
  acceptable subkey-creation-date floor bumped via releases; (c) periodic key-block
  refresh. Pure embedding has the same gap, but the downloaded-key-block model makes
  (a) natural — lean toward honoring embedded revocations in the key block, with the
  caveat that an attacker controlling the download can withhold a revocation (so
  pair with (b) for defense-in-depth).
- Primary-key *compromise* still requires a daemon update (by design) — confirm that
  is an acceptable operational constraint for the air-gapped primary.
