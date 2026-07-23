# Embedded release signing keys

`*.asc` in this directory are the OpenPGP **public** keys that sign
solo-provisioner release artifacts. They are embedded into the binary
(`//go:embed` in `../codesign.go`) and are the trust anchor for verifying
downloaded release binaries — see `docs/dev/daemon/cli-verification-design.md`.

Public key material only. Never place a private key here.

## `release-pubkey.asc`

- Fingerprint: `9BBEC9EF1C3F21653824610BDB125DC2EB561F1C`
- Primary key id: `DB125DC2EB561F1C`
- This is the key that signs the `.asc` assets on GitHub releases (verified
  against the issuer fingerprint embedded in a released `.sha256.asc`).

### Provenance / how to re-fetch and verify

The key is published (UID-stripped) on keys.openpgp.org and can be re-fetched by
fingerprint:

```bash
curl -fsS \
  https://keys.openpgp.org/vks/v1/by-fingerprint/9BBEC9EF1C3F21653824610BDB125DC2EB561F1C \
  -o release-pubkey.asc
```

To confirm a copy matches the key that actually signs releases, check that a
released signature's issuer fingerprint matches:

```bash
gh release download <tag> -R hashgraph/solo-weaver -p 'solo-provisioner-linux-amd64.sha256.asc'
gpg --list-packets solo-provisioner-linux-amd64.sha256.asc | grep 'issuer fpr'
# expect: 9BBEC9EF1C3F21653824610BDB125DC2EB561F1C
```

> Note: this key currently carries no user-id packet (keys.openpgp.org serves it
> UID-stripped because no email is verified there). `codesign` loads it at the
> packet level for that reason; standard `gpg --import` rejects a UID-less key.

## Rotation

Add the new public key block as another `*.asc` here; keep the previous key until
every deployed binary signed under it has been retired. `codesign` loads every
`*.asc` and matches a signature to a key by issuer key id.
