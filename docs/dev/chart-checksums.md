# Chart checksums

Helm charts under `cluster:` in `pkg/software/infrastructure-catalog.yaml`
are pinned by an integrity record. There is one record per version:

```yaml
versions:
  1.4.0:
    algorithm: sha256
    checksum: '<hex>'
```

The semantics of `checksum:` depend on the chart `type:`:

| `type:`   | `checksum:` value                                  |
|-----------|----------------------------------------------------|
| `classic` | SHA256 of the chart `.tgz` produced by `helm pull` |
| `oci`     | OCI manifest digest (the `Digest:` line emitted by `helm pull`) |

## Updating an existing chart version

1. Bump the `default:` field for the chart (and add the new entry under
   `versions:` if it is not yet present).
2. Recompute the checksum for every version listed under that chart.
3. Run `task lint` and the unit/integration tests so the schema validator
   catches typos and unknown versions.

## Computing the values

Run the Task target from the repository root:

```bash
task chart-checksums
```

It does the following:

- Loads `pkg/software/infrastructure-catalog.yaml` via the same
  `software.LoadInfrastructureCatalog()` used at runtime — so the chart
  list, repo, and version list come straight from the catalog (no
  hardcoded duplicates) and are validated by the schema before any
  network call is made.
- Adds the upstream Helm repositories used by classic charts.
- Runs `helm pull` for every chart/version pair currently shipped under
  `cluster:`.
- Prints the SHA256 of each `.tgz` and the OCI manifest digest for each
  OCI chart.

Source lives in `scripts/chart-checksums/main.go`. Requires `helm` on
PATH.

Copy the values into `pkg/software/infrastructure-catalog.yaml` under
the matching `versions:` entry. The target is also handy as a
verification step before a release: rerun it and confirm the printed
digests match the values committed to the catalog.

## Adding a new chart

1. Add a new entry under `cluster:` in
   `pkg/software/infrastructure-catalog.yaml` with `type`, `chart`
   (and `repo` for classic charts), `default`, and at least one
   `versions:` record.
2. Run `task chart-checksums` — it picks up the new chart automatically
   because it reads the catalog. Paste the printed checksum into the new
   `versions:` entry.
3. Run `task test:unit` — the catalog loader validates every chart at
   load time, so a missing/empty checksum is reported as a schema error
   rather than failing later at install time.

## Schema invariants

The loader in `pkg/software/config.go` rejects the catalog at startup if
any of the following hold:

- A `host:` or `cluster:` entry has no `default:`, or `default:` points to
  a version absent from `versions:`.
- A `cluster:` entry has no `type:` or a `type:` that is neither `classic`
  nor `oci`.
- A `classic` entry is missing `repo:`, or an `oci` entry declares `repo:`.
- A `cluster:` entry has no `chart:` reference.
- A `versions:` record has an empty `algorithm:` or `checksum:`.

## Future overrides

> **Not implemented yet.** This section captures the planned design from
> the alternatives analysis (see HIP draft
> `solo-weaver-catalog-alternatives.md` §8) so the catalog rename is
> grounded in concrete intent.

The embedded `infrastructure-catalog.yaml` is the in-binary manifest of
*what this build of `solo-provisioner` is able to install*. The default
version for every component is set by the catalog's `default:` field —
that is the single source of truth at install time. Two future
mechanisms relate to that default; neither lives in this PR.

### CN release package audit manifest (declarative, not an override)

The CN release package (`build-v<semver>.zip`) ships a file named
`manifests/infrastructure-versions.yaml` with the same top-level
`host:` and `cluster:` keys as our catalog, but a **minimal schema**:
each entry lists only `name` + `version`. Filename collision with the
embedded catalog is the primary reason the embedded file is named
`infrastructure-catalog.yaml` — the two files have distinct schemas,
distinct producers, and distinct roles, and the filenames need to make
that obvious.

The audit manifest is **declarative**, not imperative: at apply time
the provisioner validates that every listed version equals the
embedded catalog's `default:` for that component, and aborts on the
first mismatch. The provisioner still installs from its catalog — the
audit manifest does not direct version selection, it just records the
combination that the CN release pipeline has validated for the council
member to inspect.

### Hidden CLI flag for emergency overrides

`solo-provisioner` will expose a hidden flag for last-resort,
operator-controlled overrides of the catalog default for a single
component (`solo-weaver-catalog-alternatives.md` §8, "Exception —
hidden emergency flag"):

```
--override-component <name>=<version>
--confirm-untested-combination
```

Contract:

- The requested version **must** exist in the embedded catalog's
  `versions:` map. The flag selects among versions the running binary
  already supports; it cannot introduce new components or new versions.
- The companion `--confirm-untested-combination` flag is required —
  the override fails closed without it.
- The provisioner prints a prominent warning naming the exact
  (component, version) combination applied, so support traces can
  reconstruct the run from logs.
- The flag is hidden from `--help` (see `docs/dev/hidden-flags.md` for
  the existing pattern, e.g. `--skip-hardware-checks`) so it is not
  used casually.

Catalog access at flag-handling time is via the existing
`(*InfrastructureCatalog).GetHostArtifact(name)` /
`GetClusterComponent(name)` lookups and `cm.Versions[Version(req)]`
existence check. On miss, the abort message names the supported
versions, e.g.

```
--override-component alloy=1.5.0 requests a version not supported by
this build of solo-provisioner (supported versions for alloy: 1.4.0)
```
