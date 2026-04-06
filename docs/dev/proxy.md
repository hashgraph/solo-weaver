# Proxy Support

This document describes how to use the proxy feature in solo-weaver to route network traffic through a
proxy. Common use cases include caching downloads for faster repeated deployments, routing through
corporate proxies for security or compliance, and supporting air-gapped or restricted network environments.

## Cache Proxy Infrastructure

The project ships a Docker Compose-based cache proxy stack in `test/cache-proxy/` with three services:

| Service | Port | Purpose |
|---------|------|---------|
| Squid HTTP/HTTPS proxy | `localhost:3128` | Caches binary downloads (Kubernetes, Helm, CRI-O) with SSL MITM interception |
| Docker Registry mirror | `localhost:5050` | Pull-through cache for `registry.k8s.io` container images |
| Go module proxy | `localhost:8081` | Caches Go module dependencies |

Start the infrastructure:

```bash
task proxy:start
```

For VMs, install the MITM CA certificate so HTTPS caching works:

```bash
task proxy:install-ca-cert
```

SSH into the VM **with proxy tunnels** (required for proxy to work inside the VM):

```bash
task vm:ssh:proxy
```

This sets up SSH reverse tunnels so ports 3128, 5050, and 8081 inside the VM reach the Docker
proxy containers on your macOS host. Using plain `task vm:ssh` will **not** have the tunnels.

Other proxy tasks: `proxy:stop`, `proxy:status`, `proxy:rebuild`.

## Configuration

Proxy is configured via the `proxy:` section in the weaver config YAML file:

```yaml
profile: local
proxy:
  enabled: true
  url: "127.0.0.1:3128"
  noProxy: "localhost,127.0.0.1,::1,.local,.svc,.cluster.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"  # optional, this is the default
  sslCertFile: "/etc/ssl/certs/ca-certificates.crt"
  containerRegistryProxy: "localhost:5050"
blockNode:
  namespace: block-node
```

A ready-to-use config for local development is available at `test/config/config_with_proxy.yaml`.

## How It Works

When proxy is activated (during CLI initialization, before any command runs):

1. **Environment variables** are set: `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, `SSL_CERT_FILE`
2. **CRI-O container registry proxy** configuration (`registries.conf`) is installed in the sandbox, so container
   image pulls go through the configured pull-through cache
3. Go's `http.ProxyFromEnvironment` in the downloader automatically picks up the proxy env vars
4. The `SSL_CERT_FILE` env var ensures Go's TLS stack trusts the MITM proxy CA certificate — no
   insecure TLS skip is needed

## Verifying Proxy Is Working

**Check Squid access logs and cache stats:**

```bash
task proxy:status
```

This shows recent requests, cache hit/miss ratios, and cache sizes. When solo-provisioner downloads
binaries through the proxy, you will see entries like:

```
TCP_MISS/200 https://cdn.dl.k8s.io/...    # First download (cache miss)
TCP_HIT/200  https://cdn.dl.k8s.io/...     # Subsequent download (cache hit)
```

**Check the solo-provisioner log file** for the activation message:

```bash
cat /opt/solo/weaver/logs/solo-provisioner.log | grep "Activating proxy"
```

**Watch Squid logs in real-time** while running solo-provisioner:

```bash
# On macOS (in a separate terminal):
docker exec solo-weaver-cache-proxy tail -f /var/log/squid/access.log
```

## Fields Reference

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | `bool` | Whether proxy mode is active |
| `url` | `string` | Proxy address as `host:port` (sets both `HTTP_PROXY` and `HTTPS_PROXY`) |
| `noProxy` | `string` | Comma-separated hosts/CIDRs to bypass proxy; defaults to localhost and private networks if omitted |
| `sslCertFile` | `string` | Path to CA certificate bundle for TLS verification (sets `SSL_CERT_FILE`) |
| `containerRegistryProxy` | `string` | Container image pull-through cache as `host:port` (configures CRI-O registry mirror) |

## Implementation

- Proxy activation: `internal/proxy/proxy.go` (`Activate()`, `InstallRegistriesConf()`)
- Config model: `pkg/models/config.go` (`ProxyConfig` struct)
- Config loading: `pkg/config/global.go` (`SetProxy()`, `IsProxyEnabled()`)
- Registry mirror template: `internal/templates/files/crio/registries.conf`
