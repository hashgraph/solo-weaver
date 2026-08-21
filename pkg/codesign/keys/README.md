# Embedded release signing keys

`*.asc` in this directory are the OpenPGP **public** keys that sign
solo-provisioner release artifacts. They are embedded into the binary
(`//go:embed` in `../codesign.go`) and are the trust anchor for verifying
downloaded release binaries — see `docs/dev/daemon/cli-verification-design.md`.

Public key material only. Never place a private key here.

Every key here needs a matching real-signature fixture in `../testdata/`, wired
into `Test_EmbeddedKey_VerifiesRealReleaseSignature`, so a missing or wrong key
fails the unit suite offline.

## Which key signs what

| Key id | Releases | File |
|---|---|---|
| `F9423513CFB6304E` | `v0.28.0` onwards (current) | `release-pubkey-cfb6304e.asc` |
| `DB125DC2EB561F1C` | up to and including `v0.27.0` | `release-pubkey.asc` |

## `release-pubkey-cfb6304e.asc` (current)

- Fingerprint: `BD0B32F54BD7AAC0180C6E65F9423513CFB6304E`
- Primary key id: `F9423513CFB6304E` (signing subkey `CFFDD73A7338F437`)
- Signs the `.asc` assets on `v0.28.0` and later releases.

```bash
curl -fsS \
  https://keys.openpgp.org/vks/v1/by-fingerprint/BD0B32F54BD7AAC0180C6E65F9423513CFB6304E \
  -o release-pubkey-cfb6304e.asc
```

## `release-pubkey.asc` (previous)

- Fingerprint: `9BBEC9EF1C3F21653824610BDB125DC2EB561F1C`
- Primary key id: `DB125DC2EB561F1C`
- Signed releases up to `v0.27.0`. Kept embedded so a host installing a pinned
  older version can still verify it.

```bash
curl -fsS \
  https://keys.openpgp.org/vks/v1/by-fingerprint/9BBEC9EF1C3F21653824610BDB125DC2EB561F1C \
  -o release-pubkey.asc
```

## Provenance / how to verify a copy

Both keys are published (UID-stripped) on keys.openpgp.org and re-fetchable by
fingerprint with the commands above. To confirm a copy matches the key that
actually signs releases, check a released signature's issuer fingerprint:

```bash
gh release download <tag> -R hashgraph/solo-weaver -p 'solo-provisioner-linux-amd64.sha256.asc'
gpg --list-packets solo-provisioner-linux-amd64.sha256.asc | grep 'issuer fpr'
# expect one of the two fingerprints listed above
```

> Note: these keys carry no user-id packet (keys.openpgp.org serves them
> UID-stripped because no email is verified there). `codesign` loads them at the
> packet level for that reason; standard `gpg --import` rejects a UID-less key.

## Rotation

When the release signing key rotates:

1. Add the new public key as another `*.asc` here (one file per key). Keep the
   previous key until every version signed under it is retired — `codesign` loads
   every `*.asc` and matches a signature to a key by issuer key id.
2. Add a real-signature fixture from the first release signed by the new key to
   `../testdata/` and add a case to `Test_EmbeddedKey_VerifiesRealReleaseSignature`.
3. Update the table above.

`task sign` ends with `task verify:signatures`, which verifies the freshly signed
artifacts against this directory. A rotation that skips step 1 fails the release
build rather than shipping assets no deployed CLI can verify (this is what #1036
was: the key rotated at `v0.28.0` and the daemon auto-download path stayed broken
for two releases).
