# Monitoring the traffic shaper

How to see from Grafana, without SSH, when a block node's traffic shaper, or the host
firewall it runs with, stops doing its job. Ops own the alert rules and dashboards; this page
lists the signals, gives example rules, and says what to check when one fires.

The approach:

- **Alert on the block node's own metrics first.** If consensus nodes can't publish, the
  block node's publisher metrics drop, whatever the cause: an empty policy set, a firewall
  rule, or a consensus node misconfiguration.
- **Use the daemon's logs in Loki to find out why.** Every failure path logs a stable `reason`.
- **Alert on the daemon's tick line going missing.** It catches a stopped daemon or a stuck
  statusz loop.

## What each host needs

1. **Traffic shaping on.** `block node install` or `block node reconfigure` with
   `--traffic-shaping-enabled` turns on the policy plane, tc shaping and the daemon with its
   traffic-shaper monitor (`components.block_node.monitors.traffic_shaper: true` in
   `daemon.yaml`). See
   [Traffic shaping](commands/block-node.md#traffic-shaping---traffic-shaping-enabled).
2. **Alloy, after the block node, with both a Prometheus remote and a Loki remote:**

   ```bash
   sudo solo-provisioner alloy cluster install \
     --cluster-name=<name> \
     --add-prometheus-remote=name=<p>,url=<prometheus write URL>,username=<user> \
     --add-loki-remote=name=<l>,url=<loki push URL>,username=<user> \
     --monitor-block-node
   ```

| Piece | What it sends |
|---|---|
| Prometheus remote | Host metrics (node-exporter), kubelet, cAdvisor |
| Loki remote | journald, which includes the daemon (`unit="solo-provisioner-daemon.service"`) |
| `--monitor-block-node` | Block node metrics (`/metrics`) and block node pod logs |

Watch out for:

- `--monitor-block-node` needs **both** remote types. With only one, block node metrics are
  not collected, and the command does not say so.
- Each run replaces the whole remote list. Pass every remote every time. See
  [Changing remotes](commands/alloy.md#changing-remotes).
- Passwords come from the `grafana-alloy-secrets` Secret. See
  [Secret key naming](commands/alloy.md#secret-key-naming).
- If the host firewall is on, it drops new connections from pods to node-exporter on port
  9100, so that target's `up` goes to 0. Open the port to the cluster:

  ```bash
  sudo solo-provisioner network firewall add --in-cluster-port 9100
  ```

## Check that the signals arrive

Before writing alerts, run these in Grafana Explore for one host. `<name>` is the host's
`--cluster-name`.

```logql
{unit="solo-provisioner-daemon.service", cluster="<name>"} |= "TrafficShaperStatuszTick"
```

Expect at least one line per poll interval (default 5 minutes), each a JSON object that
starts with `{"level":"info"`.

```promql
up{cluster="<name>", job="block-node-block-node-server"}
up{cluster="<name>", job=~".*node-exporter.*"}
```

Expect `1` for each.

| You see | Likely cause |
|---|---|
| No daemon lines at all | No Loki remote, or Alloy is not running on the host |
| Daemon lines, but no tick lines | Traffic shaping is off on the host, or the daemon predates the tick line |
| Lines with colour codes such as `[90m` and `INF` | The daemon predates JSON logging; upgrade it |
| No block node `up` series | `--monitor-block-node` missing, or only one remote type given |
| node-exporter `up` is `0` | The host firewall blocks port 9100 (above) |

## Labels

| Label | Where it comes from | Use |
|---|---|---|
| `cluster` | `alloy cluster install --cluster-name`. Always set | One value per host. Group and filter alerts by it |
| `unit` | journald's systemd unit | `solo-provisioner-daemon.service` selects the daemon's logs |
| `syslog_identifier` | journald | `solo-provisioner-daemon` on the daemon's own lines. Add it to the selector to drop the `sudo` lines journald files under the same unit |
| `job` (Prometheus) | The scrape target | Block node: its Service name, `block-node-block-node-server` with the default release. node-exporter: contains `node-exporter` |

A remote with `labelProfile=ops` also adds `environment`, `instance`, `instance_type`,
`inventory_name` and `ip`. See [Label profiles](commands/alloy.md#label-profiles).

## Failure and signal

| Failure | What you see | Where |
|---|---|---|
| Consensus nodes not in `bn-publisher` | Publisher connections fall to 0; items received stop rising | Prometheus |
| The block node asked for no publishers | Tick line has `desired_bn_publisher=0` | Loki |
| Daemon down, or statusz loop stuck | Tick line stops | Loki |
| Daemon can't find a block node pod | Tick line has `outcome=idle_no_endpoint` | Loki |
| Statusz loop failing | Tick line has `outcome=failed` and `error`; `TrafficShaperResponsibilityFaulted` | Loki |
| A policy set was emptied | `TrafficShaperMembershipCleared` | Loki |
| A name in statusz didn't resolve | `TrafficShaperStatuszNameUnresolved` | Loki |
| Firewall, policy or shaper drift not repaired | `NetworkReassertWorkerFailed` | Loki |
| Firewall or policy table removed by someone else, then restored | `NetworkArtifactReasserted` | Loki |
| Ingress shaping not re-attached after a pod restart | `TrafficShaperTCAttachFailed`, `TrafficShaperVethResolveFailed` | Loki |
| Firewall blocks a scrape | `up == 0` for that target | Prometheus |

## The tick line

The daemon logs one `TrafficShaperStatuszTick` line at `info` each time its statusz loop
runs, on every path.

The statusz loop runs only when the daemon's traffic-shaper monitor is on. A host with it off
logs no tick lines, so scope tick alerts to hosts that run the traffic shaper.

The daemon writes JSON to journald, one object per line. A tick that applied, with the build
and caller fields left out:

```json
{"level":"info","reason":"TrafficShaperStatuszTick","monitor":"bn-traffic-shaper-monitor","outcome":"applied","statusz_url":"http://10.244.0.12:40983","desired_bn_backfill":2,"desired_bn_partner_out":2,"desired_bn_publisher":4,"desired_bn_restricted":0,"withheld":"","message":"statusz tick"}
```

| Field | Meaning |
|---|---|
| `outcome` | `applied`, `unchanged`, `idle_no_endpoint` or `failed` (below) |
| `statusz_url` | The statusz endpoint used. Empty when idle |
| `error` | Why the tick failed. Only on `failed` |
| `desired_<policy>` | How many elements statusz wants in that policy set, with `-` written as `_`, as this tick's check computed it. `0` means apply empties the set. The apply fetches statusz again, so if the roster changes in between, the next tick shows the new count |
| `withheld` | Comma-separated membership policies whose statusz call reported nothing. Apply leaves those sets as they are, so they have no `desired_` count. Listener-port sets (`<name>_ports`) are not listed; they ride on the inbound call and are left alone with it |

| Outcome | Meaning |
|---|---|
| `applied` | The daemon applied the desired membership |
| `unchanged` | Desired membership hasn't changed since the last apply; nothing written |
| `idle_no_endpoint` | No ready block node pod found and no `base_url` set; nothing checked |
| `failed` | The check or the apply failed. The loop retries after a back-off |

Counts and `withheld` appear only when the check ran: on `applied`, `unchanged`, and on
`failed` when the apply failed after a good check.

How often: at least once per poll interval (default 5 minutes,
`components.block_node.statusz.poll_interval`). After a failure the retry back-off adds up
to 5 more minutes. A 30-minute window covers both.

If the CLI on the host is older than the daemon, tick lines have no `withheld` field and the
daemon logs `TrafficShaperStatuszWithheldUnreported` once.

## Stable reason codes

Alert rules may depend on these names, the outcome values and the tick fields above. They
don't change between releases.

| Reason | Level | Meaning |
|---|---|---|
| `TrafficShaperStatuszTick` | info | One statusz loop run (above) |
| `TrafficShaperResponsibilityFaulted` | warn | The statusz loop or pod watcher failed; retrying after back-off |
| `TrafficShaperMembershipCleared` | warn | An apply emptied one or more policy sets |
| `TrafficShaperStatuszNameUnresolved` | warn | A name in statusz did not resolve |
| `TrafficShaperTCAttachFailed` | warn | Ingress shaping could not be attached to a pod |
| `TrafficShaperVethResolveFailed` | warn | The pod's host-side interface could not be found |
| `NetworkReassertWorkerFailed` | error | The drift-repair worker failed |
| `NetworkArtifactReasserted` | warn | A firewall or policy table was missing and was restored |

## Example alerts

Block node metric names start with `blocknode_` from block node 0.40, and with
`hiero_block_node_` before that. Use the prefix of the version you run.

```promql
# Consensus nodes can't publish (any cause). For 5m.
blocknode_publisher_open_connections == 0
rate(blocknode_publisher_block_items_received_total[5m]) == 0

# A scrape is blocked.
up{job=~".*node-exporter.*"} == 0
```

```logql
# Tick stopped: daemon down or loop stuck. Only for hosts with the traffic shaper on.
# absent_over_time drops labels, so write one rule per host with its cluster label.
absent_over_time({unit="solo-provisioner-daemon.service", cluster="<name>"} |= "TrafficShaperStatuszTick" [30m])

# Loop idle or failing on several ticks.
sum by (cluster) (count_over_time({unit="solo-provisioner-daemon.service"}
  |= "TrafficShaperStatuszTick" | json outcome="outcome"
  | outcome=~"idle_no_endpoint|failed" [30m])) >= 3

# The block node wants no publishers.
max by (cluster) (max_over_time({unit="solo-provisioner-daemon.service"}
  |= "TrafficShaperStatuszTick" | json publishers="desired_bn_publisher"
  | unwrap publishers | __error__="" [15m])) == 0

# Repair or attach problems.
sum by (cluster) (count_over_time({unit="solo-provisioner-daemon.service"}
  |~ "NetworkReassertWorkerFailed|NetworkArtifactReasserted|TrafficShaperTCAttachFailed" [15m])) > 0
```

journald also files `sudo` lines from the daemon's child commands under the daemon's unit.
They are not JSON, so filter on the reason before `| json`, as these examples do.

A drop from N publishers to N-1 needs the expected count. The tick line's
`desired_bn_publisher` is that number, but comparing it with the block node's gauge takes a
rule across Loki and Prometheus.

## When an alert fires

Ops read the host's latest tick line in Loki first. The host's operator runs the commands
below on the host.

### Consensus nodes can't publish

Look at `desired_bn_publisher` and `withheld` on the latest tick line:

- **`desired_bn_publisher=0`:** the block node reports no publishers, and apply has emptied
  `bn-publisher`. Check the block node's publisher roster.
- **`bn-publisher` in `withheld`:** the block node's inbound statusz reported nothing, so the
  set was left as it was. Check what statusz returns:

  ```bash
  solo-provisioner block node reconcile-shaper --statusz-url <statusz_url> --check
  ```

- **A count above 0:** the set has members. Compare them with the consensus nodes'
  addresses. If they match, the cause is outside the traffic shaper: the consensus node's
  configuration or the network between them.

  ```bash
  sudo nft list set inet weaver-workload-policy bn-publisher
  ```

### Tick stopped

```bash
sudo solo-provisioner daemon service check
sudo journalctl -u solo-provisioner-daemon -n 50
```

If the daemon runs but logs no tick, the loop is stuck in a check or an apply. The last
`TrafficShaperStatuszPolling` (debug) or `TrafficShaperStatuszApplying` line names the URL.
`sudo solo-provisioner daemon service stop` and then `start` restarts it.

### Repeated `idle_no_endpoint`

The daemon finds no ready block node pod. Check the pods in the block node's namespace with
`kubectl get pods`. If statusz is served somewhere the daemon can't discover, set it with
`block node reconfigure --statusz-base-url=<url>`.

### Repeated `failed`

Read the `error` field. To reproduce the check as the daemon's user:

```bash
sudo -u weaver solo-provisioner block node reconcile-shaper --statusz-url <statusz_url> --check --output json
```

If the check passes, the apply failed. Run it as root to see why:

```bash
sudo solo-provisioner block node reconcile-shaper --statusz-url <statusz_url>
```

### Repair or attach problems

- `NetworkReassertWorkerFailed`: run `sudo solo-provisioner network reassert` and read its
  report. See [`network reassert`](commands/network/reassert.md).
- `NetworkArtifactReasserted`: something outside solo-provisioner removed a firewall or
  policy table. Find what did, such as a firewall service restart or an `nft` flush.
- `TrafficShaperTCAttachFailed` or `TrafficShaperVethResolveFailed`: read `error`. The pod
  has no ingress shaping until its next pod event.

## See also

- [Alloy commands](commands/alloy.md)
- [`block node reconcile-shaper`](commands/block-node.md#reconcile-shaper--sync-policy-sets-from-statusz)
- [Troubleshooting](troubleshooting.md)
