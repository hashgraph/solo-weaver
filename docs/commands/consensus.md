# `consensus` commands

> **Experimental.** The `consensus` command group is hidden and refuses to run
> unless you opt in  the hidden `--experimental` flag or
> `SOLO_PROVISIONER_ENABLE_CONSENSUS=1`. It is not production-ready.

Commands for provisioning and operating Hedera/Hiero consensus nodes via the
solo-operator. This page currently documents the `consensus keys` family;
the full onboarding walkthrough lands  the key-management docs story.

## `consensus keys`

Generate, import, verify, and export the cryptographic material a consensus node
needs, and turn it into the Kubernetes secrets the solo-operator consumes.

A consensus node uses three kinds of keys:

| Key | Purpose | Custody |
|---|---|---|
| **gossip signing** | the node's network identity (roster `gossip_ca_certificate`) | private key in a node-pod secret |
| **gRPC TLS** | the node's TLS cert behind HAProxy | private key in a node-pod secret |
| **admin** | signs `nodeCreate`/`nodeUpdate`/`nodeDelete` (DAB) transactions | **private key off-cluster**; only the public key goes to Kubernetes |

Key types are selected  `--gossip` / `--tls` / `--admin`. When none is given,
all apply.

### `consensus keys generate`

Generate key material into a local folder as enhanced PEM (separate private key
and public certificate/key). Generation is **node-id-free by default**: the
gossip certificate CN is the machine UUID and the gRPC certificate CN is
`localhost`, so the material is valid *before* the network assigns a node-id
during a DAB join.

For a DAB join you need three public values: the gossip certificate
(`s-public.pem`), the admin public key (`admin.pub`), and the SHA-384 hash of the
gRPC certificate — the command prints that hash to stdout (it is the one value
not otherwise on disk; recompute it any time  `openssl dgst -sha384` over
`hedera.crt`). solo-provisioner does **not** submit the transaction — your wallet
or SDK does. The admin **private** key is written to the output folder only and
is never pushed to Kubernetes.

```bash
# All key types, node-id-free (typical for a DAB join):
sudo solo-provisioner consensus keys generate --experimental \
  --keys-dir ./node-keys --identity my-operator

# Only the gossip signing key,  an ECDSA admin key algorithm:
sudo solo-provisioner consensus keys generate --experimental \
  --keys-dir ./node-keys --gossip --admin-algo ecdsa
```

`--keys-dir` is one node's folder. For a genesis network with multiple nodes,
run `generate` once per node into its own `--keys-dir`. When several nodes share
one machine (testing), also give each a distinct `--machine-id` so their gossip
certificate CNs differ:

```bash
for i in 0 1 2; do
  sudo solo-provisioner consensus keys generate --experimental \
    --keys-dir ./keys/node$i --machine-id test-node$i
done
```

On real hardware each node is its own host with a unique `/etc/machine-id`, so no
override is needed.

To also create the Kubernetes secrets in one step (known node-id), use
`consensus keys init` (see the key-management docs story).

#### Flags

| Flag | Description | Default |
|---|---|---|
| `--keys-dir` | Directory for this node's key material (required); make it node-specific | — |
| `--identity` | Optional human-readable label recorded in the gossip certificate OU/SAN (e.g. operator or host name) | — |
| `--machine-id` | Override the gossip certificate CN; defaults to the host machine UUID (`/etc/machine-id`) | host machine UUID |
| `--gossip` | Generate the gossip signing key | all types when no selector is given |
| `--tls` | Generate the gRPC TLS key | all types when no selector is given |
| `--admin` | Generate the node admin key | all types when no selector is given |
| `--admin-algo` | Admin key algorithm: `ed25519` or `ecdsa` (secp256k1) | `ed25519` |
| `--grpc-cn` | Common name (subject and issuer) of the gRPC TLS certificate | `localhost` |
| `--grpc-org` | Organization (subject and issuer O) of the gRPC TLS certificate | omitted |
| `--grpc-dns` | Extra fully qualified DNS SAN for the gRPC TLS certificate, added to `localhost` (repeatable) | — |
| `--grpc-ip` | Extra IP SAN for the gRPC TLS certificate, added to `127.0.0.1` and `::1` (repeatable) | — |
| `--grpc-validity-days` | gRPC TLS certificate lifetime in days (1-36500) | 100 years |
| `--force` | Regenerate even if key material already exists in `--keys-dir` (overwrites it) | `false` |

The gRPC TLS certificate always keeps the `localhost`, `127.0.0.1`, and `::1`
SANs; `--grpc-dns` and `--grpc-ip` only add to them. `--grpc-dns` takes a fully
qualified name (at least one dot, e.g. `node2.example.org`); a bare name like
`node2` is rejected. `--grpc-cn` and `--grpc-org` set the subject CN and O, and so the issuer, of
this self-signed certificate (free text, up to 64 characters each). Shell-quote values with
spaces, e.g. `--grpc-org "Example Org"`. The
gossip certificate lifetime is fixed at 100 years because it is the node's network
identity. If the gRPC key already exists in `--keys-dir`, the `--grpc-*` flags have
no effect unless you pass `--force`.

`generate` is node-id-free — it takes no `--node-id`. The node-id enters later,
at `import`/`init`.

Generation is **idempotent**: re-running with the same `--keys-dir` keeps any key
type that already exists (it logs a warning and only fills in missing types), so
it never silently overwrites a node's gossip identity. Pass `--force` to
regenerate.

#### Output layout

Files use neutral, index-free names that match the Kubernetes secret inner-keys,
so `keys import` maps a generated folder to secrets one-to-one:

| File | Contents |
|---|---|
| `s-private.pem` | gossip signing private key (PKCS#8) |
| `s-public.pem` | gossip signing certificate |
| `hedera.key` | gRPC TLS private key |
| `hedera.crt` | gRPC TLS certificate |
| `admin.key` | admin private key (off-cluster; PKCS#8 for Ed25519, SEC1 for secp256k1) |
| `admin.pub` | admin public key (hex) |

Private key files are written  `0600` permissions. No combined artifacts
file is written — the join inputs are these files plus the gRPC certificate hash
printed at generation time.

### `consensus keys import`

Read the selected key material from a folder and create the three per-node
Kubernetes secrets the solo-operator consumes. `--node-id` is required — it
selects which per-node secrets to write.

Each secret is named by the key's own date and fingerprint:

| Secret | Name |
|---|---|
| gossip signing key and certificate | `node<N>-gossip-keys-<date>-<fp8>` |
| gRPC TLS key and certificate | `node<N>-grpc-tls-keys-<date>-<fp8>` |
| admin public key | `node<N>-admin-pubkey-<fp8>` |

`<date>` is the certificate's `Not before` date (UTC, `YYYYMMDD`), so it never
depends on when the import ran: importing the same folder again, for example after
a cluster reset, gives the same names. `<fp8>` is the first 8 hex characters of the
SHA-384 of the certificate (or of the raw admin public key, which has no date).
Different key material therefore gets a different name, and importing new keys (a
rotation) creates new secrets next to the live ones instead of replacing them. Each
secret also carries labels (`solo.hiero.org/node`, `solo.hiero.org/key-type`,
`solo.hiero.org/cert-fingerprint`) and annotations with the full SHA-384 and the
certificate expiry, so the secrets can be listed and matched to a roster entry. Use
`--label` to add your own labels, for example a reference to an external system.
When a secret's content is already up to date, `import --label` changes only its
labels; the key data is never sent again, so it cannot be overwritten.

Only the admin **public** key (`admin.pub`) is imported; the admin private key
(`admin.key`) is never read or pushed to the cluster. The admin secret holds the
public key as **raw bytes** (32 for Ed25519), which is what the solo-operator reads;
`admin.pub` itself is hex text, so `import` decodes it and `export` writes it back as hex. The gossip signing secret
Re-importing identical material is idempotent. A secret of the same name whose
content was edited by hand is refused unless `--force` is passed.

```bash
# Create all three secrets for node 0 from a generated folder:
sudo solo-provisioner consensus keys import --experimental \
  --namespace my-orbit --node-id 0 --keys-dir ./node-keys

# Only refresh the gRPC TLS secret:
sudo solo-provisioner consensus keys import --experimental \
  --namespace my-orbit --node-id 0 --keys-dir ./node-keys --tls
```

| Flag | Description | Default |
|---|---|---|
| `--keys-dir` | Node key folder to import from (required for `--source folder`) | — |
| `--source` | Key material source: `folder` (vault-backed import is tracked separately) | `folder` |
| `--secret-suffix` | Replace the certificate date in the secret names (for example `v0.75.6`); lowercase letters, digits, `-` and `.` | certificate date |
| `--label` | Extra label for every secret, as `key=value` (repeatable), for example `example.com/external-ref=ticket-42` or `app.kubernetes.io/version=v0.75.6`. Kubernetes label syntax applies, and weaver's own labels cannot be used | — |
| `--force` | Overwrite a same-named secret whose content differs (for example one edited by hand); re-importing identical material is idempotent without it | `false` |
| `--gossip` / `--tls` / `--admin` | Select which secrets to create | all when none given |

`--namespace` (the orbit name) and `--node-id` are inherited from `consensus
node`; `--node-id` is **required** here.

### `consensus keys export`

Read the selected per-node secrets from the cluster and write their contents to a
folder — a backup, or to move a node's material between namespaces. The admin
secret holds only the public key, so the admin **private** key cannot be
recovered from the cluster; keep the off-cluster `admin.key` from `keys generate`
safe.

```bash
sudo solo-provisioner consensus keys export --experimental \
  --namespace my-orbit --node-id 0 --keys-dir ./node0-backup
```

| Flag | Description | Default |
|---|---|---|
| `--keys-dir` | Node key folder to export into (required) | — |
| `--fingerprint` | Pick the secret whose SHA-384 fingerprint starts with this (4+ hex characters); needs exactly one of `--gossip`/`--tls`/`--admin` | — |
| `--secret-suffix` | Pick the secrets imported with this suffix or date | — |
| `--force` | Overwrite existing files whose content differs (identical files are left as-is) | `false` |
| `--gossip` / `--tls` / `--admin` | Select which secrets to export | all when none given |

Export reads the secret's data and checks it against the secret's name and
fingerprint label; a secret that was renamed or edited is refused. When a node has
more than one generation of a key type, pass `--fingerprint` or `--secret-suffix` to
choose one; otherwise the command lists the candidates and stops.

Export is idempotent: identical files are left untouched, and a file that holds
different content is refused unless `--force` is passed.

`--namespace` and `--node-id` are inherited from `consensus node`; `--node-id` is
**required** here.

### `consensus keys verify`

Validate that the selected key material is well-formed. Verify a folder with `--keys-dir`, or the in-cluster secrets with `--from-cluster --node-id N`. Checks:

- private keys and certificates parse as enhanced PEM;
- each certificate's public key matches its private key;
- the gossip signing key is RSA-3072;
- the admin public key is a valid Ed25519 (32-byte) or secp256k1 (33-byte
  compressed) key;
- when a folder also holds the admin private key, it derives the public key.

The command prints a per-key report and exits non-zero if any check fails.

```bash
# Validate a folder before importing:
sudo solo-provisioner consensus keys verify --experimental --keys-dir ./node-keys

# Validate what is actually in the cluster:
sudo solo-provisioner consensus keys verify --experimental \
  --from-cluster --namespace my-orbit --node-id 0
```

| Flag | Description | Default |
|---|---|---|
| `--keys-dir` | Node key folder to validate | — |
| `--from-cluster` | Validate the in-cluster per-node secrets instead of a folder (requires `--node-id`) | `false` |
| `--fingerprint` | With `--from-cluster`, pick the secret whose SHA-384 fingerprint starts with this (4+ hex characters); needs exactly one of `--gossip`/`--tls`/`--admin` | — |
| `--secret-suffix` | With `--from-cluster`, pick the secrets imported with this suffix or date | — |
| `--gossip` / `--tls` / `--admin` | Select which key types to check | all when none given |

`--keys-dir` and `--from-cluster` are mutually exclusive; `--from-cluster` requires
the inherited `--namespace` and `--node-id`. In the cluster, each secret is checked
against its name and fingerprint label (a renamed or edited secret fails), and when a
node has several generations of a key type the failing line lists them so you can
choose with `--fingerprint` or `--secret-suffix`.
