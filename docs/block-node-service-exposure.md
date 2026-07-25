# Block-node service exposure: `--load-balancer-enabled`, `service.type`, and the split topology

How the block node's Kubernetes Services are exposed is decided by **three independent
inputs** that combine at install/upgrade time. This page explains how they interact and shows
the three configurations operators actually use, with the resulting `kubectl get svc` output.

## The three inputs

| Input | Where it's set | What it controls |
|---|---|---|
| `--load-balancer-enabled` | CLI flag (default `true`) | Whether weaver injects the MetalLB `metallb.io/address-pool` annotation onto the **main** service. Set `false` in clusters without MetalLB. |
| `service.type` | operator `-f` values file | The main `<release>-block-node-server` service type. If you don't set it, weaver picks the default (below). |
| `loadBalancer.enabled` | operator `-f` values file (a **chart** value) | Turns on the chart's **split topology**: the chart renders a dedicated `<release>-block-node-server-external` LoadBalancer service and keeps the main service on the chart's `ClusterIP` default. |

Weaver renders a base values template and merges your `-f` on top, then at deploy time
resolves the main service type in `injectServiceAnnotations`. The base template intentionally
does **not** hardcode `service.type` (that hardcode was the bug in
[#926](https://github.com/hashgraph/solo-weaver/issues/926)); weaver owns the decision in one
place so it can defer to the chart's `ClusterIP` on the split path.

## Decision table (main service, when you don't set `service.type` yourself)

| `--load-balancer-enabled` | `loadBalancer.enabled` (chart split) | Main service `type` | MetalLB pool annotation on main service |
|---|---|---|---|
| `true` (default) | unset / `false` | `LoadBalancer` | injected (`public-address-pool`) |
| `true` | `true` (split) | `ClusterIP` (chart default) | none — the chart's `-external` LB carries the annotation |
| `false` | unset / `false` | `LoadBalancer` | none |
| `false` | `true` (split) | `ClusterIP` (chart default) | none |

An **explicit** `service.type` in your `-f` is always honored and never clobbered.

> **Gotcha — ClusterIP main service + `--load-balancer-enabled=true` fails the install.** On the
> non-split path, `--load-balancer-enabled=true` (the default) runs `verify-block-node-reachable`,
> which dials the main LoadBalancer's external IP. If you force `service.type: ClusterIP` there,
> weaver honors it and logs a warning, but the probe then finds no LoadBalancer Service and fails:
> ```
> common.illegal_state: no LoadBalancer Service found in namespace <ns>; cannot probe reachability
> ```
> To run a ClusterIP main service, also pass `--load-balancer-enabled=false` (which skips the
> probe) — see Showcase 3. On the **split** path this doesn't arise: the chart's `-external`
> LoadBalancer Service is what the probe dials, so the main service can stay `ClusterIP`.

## Showcase 1 — Default single-service (MetalLB present)

The common case: one `LoadBalancer` service, MetalLB assigns it an external IP from
`public-address-pool`.

```bash
solo-provisioner block node install ...        # --load-balancer-enabled defaults to true
```

No `service` or `loadBalancer` block needed in `-f`. Result:

```
NAME                           TYPE           EXTERNAL-IP    PORT(S)
block-node-block-node-server   LoadBalancer   192.168.99.0   40840,16007,40983
```

> The single `LoadBalancer` publishes all main-service ports (`40840` gRPC, `16007` metrics,
> `40983` plugin). If you only want the gRPC port externally reachable, use the split topology
> (Showcase 2).

## Showcase 2 — Split topology (chart-owned external LoadBalancer)

You want **only** the public gRPC port (`40840`) exposed externally, and the metrics/plugin
ports kept in-cluster. Enable the chart's own external LoadBalancer and leave the main service
on `ClusterIP`:

```yaml
# operator -f values
service:
  port: 40840
loadBalancer:
  enabled: true
  port: "40840"
  annotations:
    metallb.io/address-pool: "public-address-pool"
```

```bash
solo-provisioner block node install -f split-values.yaml ...   # --load-balancer-enabled stays true
```

Result — the main service is `ClusterIP` (no external IP; metrics/plugin ports stay internal),
and only the `-external` service is a `LoadBalancer`, exposing just `40840`:

```
NAME                                    TYPE           EXTERNAL-IP    PORT(S)
block-node-block-node-server            ClusterIP      <none>         40840,16007,40983
block-node-block-node-server-external   LoadBalancer   152.236.24.7   40840
```

You do **not** need to set `service.type: ClusterIP` yourself — weaver defers to the chart's
default. (Setting it explicitly also works and is honored.)

## Showcase 3 — No MetalLB

Cluster has no MetalLB (or any LoadBalancer controller). Disable the annotation injection:

```bash
solo-provisioner block node install --load-balancer-enabled=false ...
```

The main service stays `LoadBalancer` (weaver's default main-service shape) but carries **no**
pool annotation. Without a LoadBalancer controller the external IP never materializes and stays
`<pending>` — the service is still reachable in-cluster via its ClusterIP and via the
auto-allocated NodePort:

```
NAME                           TYPE           EXTERNAL-IP   PORT(S)
block-node-block-node-server   LoadBalancer   <pending>     40840:31234/TCP,...
```

If you want no external exposure at all in a no-MetalLB cluster, set `service.type: ClusterIP`
explicitly in your `-f` — weaver honors it and injects nothing.

## Upgrade behavior — existing broken installs are not auto-healed by a default upgrade

The [#926](https://github.com/hashgraph/solo-weaver/issues/926) fix corrects **fresh installs**.
It does **not** automatically fix an already-installed split-topology block node on a routine
upgrade, because of how `block node upgrade` reuses values:

- A block node installed with an **older** provisioner baked `service.type: LoadBalancer` into the
  release's stored user values (the old base template rendered it into the values file handed to
  `helm install`).
- `block node upgrade` defaults to reusing the previous release's values (Helm
  `ResetThenReuseValues`): chart defaults → **old release values (still `LoadBalancer`)** → the
  freshly rendered values on top. Since the #926 fix means weaver's rendered values no longer set
  `service.type`, nothing overrides the reused `LoadBalancer`, so the main service stays a
  LoadBalancer after the upgrade.

To flip an existing split install to `ClusterIP`, do **one** of:

- **Upgrade with `--no-reuse-values`** — resets to chart defaults and applies only the freshly
  rendered values (which omit `service.type`, so the chart's `ClusterIP` default stands):

  ```bash
  solo-provisioner block node upgrade --no-reuse-values -f split-values.yaml ...
  ```

  Re-supply your **full** `-f` on this path — `--no-reuse-values` does not carry forward values you
  set only at the original install time.

- **Re-assert `service.type: ClusterIP` explicitly** in your `-f`, which overrides the reused value
  on a normal (reuse-values) upgrade:

  ```yaml
  service:
    type: ClusterIP
    port: 40840
  loadBalancer:
    enabled: true
    port: "40840"
    annotations:
      metallb.io/address-pool: "public-address-pool"
  ```

The same reasoning applies to a no-MetalLB (`--load-balancer-enabled=false`) install upgraded from
an older provisioner: the reused `LoadBalancer` persists until you upgrade with `--no-reuse-values`
or set `service.type: ClusterIP` explicitly.
