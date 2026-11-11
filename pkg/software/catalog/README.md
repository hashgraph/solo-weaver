# Infrastructure catalog

The `*.yaml` files in this directory are embedded into the `solo-provisioner`
binary. Together they list every component version this build can install, with
the integrity metadata needed to verify each artifact. `LoadInfrastructureCatalog()`
in `pkg/software` merges them into one catalog at load time.

## Files

| File | Contents |
|---|---|
| `defaults.yaml` | The default version of every component: what gets installed when no other version is requested. The only place a default is set. |
| `host-k8s-<minor>.yaml` | Host binaries tied to one Kubernetes minor: `cri-o`, `kubelet`, `kubeadm`, `kubectl`. |
| `host-tools.yaml` | Host tools not tied to a Kubernetes minor: `helm`, `k9s`, the `cilium` CLI, `teleport`, `solo-provisioner-daemon`. |
| `cluster.yaml` | Helm charts installed into the cluster. |

`host:` lists artifacts installed on the host (archives, binaries, configs).
`cluster:` lists Helm charts. A chart entry declares:

- `type`: `classic` (a classic Helm repository) or `oci` (an OCI registry).
- `repo`: required for classic charts, omitted for OCI charts.
- `chart`: the chart reference, e.g. `grafana/alloy` or `oci://registry/path`.
- `namespace` and `release`: where the chart is installed and its Helm release name.
- `versions`: one integrity record per version. The checksum is the SHA256 of the
  chart `.tgz` for classic charts and the OCI manifest digest for OCI charts.
  See `docs/dev/chart-checksums.md`.

## How the files merge

- Every `*.yaml` file other than `defaults.yaml` is a component file. File names
  carry no meaning to the loader.
- A component may appear in several files. Its `versions:` maps are merged.
- Listing the same version of a component in two files is an error.
- A chart's `type`, `repo`, `chart`, `namespace` and `release` must be the same in
  every file that lists it. So must a host artifact's `selfRelease`.
- A component file must not declare `default:`. Every component needs a default in
  `defaults.yaml`, and that default must be one of its listed versions.
- Unknown keys are errors.
- Files are read in name order, and each component keeps the position where it
  first appears.

`solo-operator` is the one exception to the defaults rule. Its default is
`deps.SOLO_OPERATOR_VERSION`, injected at load, so it has no entry in
`defaults.yaml`. Its version key in `cluster.yaml` must equal that constant.
`task bump:solo-operator` updates both.

## Adding and dropping versions

- **A new Kubernetes minor:** add `host-k8s-<minor>.yaml`. No existing file changes.
  To make it the default for new installs, also update `defaults.yaml`.
- **A new version of any other component:** add it under the component's
  `versions:`. A new version does not change the default.
- **Host checksums:** write the new block with empty checksums and run
  `task host-checksums -- -write`. See `docs/dev/host-checksums.md`.
- **Dropping a version:** delete its entry, or the whole `host-k8s-<minor>.yaml`
  file for a minor. Hosts installed with that version can no longer verify their
  binaries against the catalog, so drop a version only once no supported host
  runs it. `pkg/software/catalog_history_test.go` lists the versions that must
  stay.
