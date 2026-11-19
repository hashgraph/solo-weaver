# Catalog updates

How to add and drop versions in the infrastructure catalog, `pkg/software/catalog/`.
The file format and the rules the loader checks are in
[`pkg/software/catalog/README.md`](../../pkg/software/catalog/README.md).

## Adding and dropping versions

- **A new Kubernetes minor:** add `host-k8s-<minor>.yaml` with its `kubernetes:`
  entry (see [Finding a minor's chart minimums](#finding-a-minors-chart-minimums)),
  and check each Cilium version's range. To make it the default for new installs,
  also update `defaults.yaml`.
- **A new version of any other component:** add it under the component's
  `versions:`. A new version does not change the default.
- **Host checksums:** write the new block with empty checksums and run
  `task host-checksums -- -write`. See [host-checksums.md](host-checksums.md).
- **Chart checksums:** see [chart-checksums.md](chart-checksums.md).
- **Dropping a version:** delete its entry, or the whole `host-k8s-<minor>.yaml`
  file for a minor. Hosts installed with that version can no longer verify their
  binaries against the catalog, so drop a version only once no supported host
  runs it. `pkg/software/catalog_history_test.go` lists the versions that must
  stay.

## Finding a minor's chart minimums

Check every chart in `cluster.yaml` against the new minor. Cilium, in
`cilium.yaml`, is not checked this way: each version's `kubernetes` range already
records its support.

1. **Declared support.** Read `kubeVersion` in each catalog version's
   `Chart.yaml`, and the chart's compatibility notes or release notes. Helm
   refuses to install or upgrade a chart whose `kubeVersion` excludes the cluster.
2. **Removed APIs.** Render each catalog version for the new minor with
   `helm template <chart> --version <version> --kube-version <minor>.0`. Check the
   rendered `apiVersion`s against the APIs that minor removes.
3. **A real cluster.** Install and upgrade each catalog version on a cluster of
   the new minor. Some breaks show only there: MetalLB 0.15 renders cleanly, but
   its CRDs fail validation on the 1.36 API server.
4. **Upgrade notes.** For any newer version you add, read its upgrade notes for
   new defaults our values must override. MetalLB 0.16, for example, deploys
   frr-k8s unless `frrk8s.enabled=false`.

When every catalog version of a chart passes, leave the chart out: `charts: {}`
is the usual entry. Otherwise name the lowest version that passes. That version
must be listed under the chart's `versions:` (see
[chart-checksums.md](chart-checksums.md)). If the new minor becomes the default,
raise the chart's default to at least that version. Clusters being upgraded move
the chart to its minimum just before they move to that minor.

Upgrades also need a Cilium version whose range covers both the old and the new
minor. If none does, add a newer Cilium to `cilium.yaml` first.
