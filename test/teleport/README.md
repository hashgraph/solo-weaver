# Teleport Local Development Setup

Local Teleport server for testing the Teleport Kubernetes agent integration with Solo Weaver.

**Components:**
- Teleport Server - Authentication, proxy, and audit logging
- Teleport Web UI - Access management interface

## 🎯 Architecture Overview

Teleport provides **two types of agents**:

### 1. Host-Level Agent (Node Agent)
Installed directly on the host machine for SSH access to the node itself.

| Environment | Method | Notes |
|-------------|--------|-------|
| Local Dev | `weaver --teleport-node-agent-proxy` | Requires `task teleport:start` to trust the server cert |
| Production | `weaver --teleport-node-agent-token` | Uses `hashgraph.teleport.sh` |

> **Local dev note:** The `task teleport:start` command automatically extracts the Teleport
> server's certificate and adds it to the system trust store. This allows the node agent
> to connect to the local Teleport server with self-signed certificates.


### 2. Kubernetes Agent
Installed via Helm chart to provide secure access to the Kubernetes cluster.

```
Local Dev (Docker):                 Production (Teleport Cloud):
Teleport Server (Docker)            hashgraph.teleport.sh
     ↓                                   ↓
Teleport Kube Agent (Helm)    ←→   Teleport Kube Agent (Helm)
     ↓                                   ↓
Kubernetes API Access              Kubernetes API Access
+ Audit Logging                    + Audit Logging
```

---

## 🚀 Quick Start - Local Development

### Prerequisites: Build Weaver

From your Mac, ensure you have the latest build:
```bash
task build
```

### Step 1: Start Teleport Server

Start and SSH into a fresh VM:
```bash
task vm:ssh
```

Then, from within the VM, start the Teleport server:
```bash
task teleport:start
```

This single command will:
- Generate Teleport server configuration
- Start the Teleport server container
- Wait for it to be ready
- Detect the node IP address
- Generate a join token
- Create `/tmp/teleport-values-configured.yaml` with correct values

### Step 2: Generate Node Agent Token

Generate a fresh token for the node agent:
```bash
task teleport:node-agent-token
```

This will output the token and node IP. **Note these values for Step 3.**

### Step 3: Install Cluster with Teleport Agents

Copy the weaver binary:
```bash
cp /mnt/solo-weaver/bin/weaver-linux-arm64 ~/.

sudo ~/weaver-linux-arm64 install
```

Install the cluster with both Kubernetes and Node agents:
```bash
sudo weaver block node install \
  --profile=local \
  --teleport-enabled \
  --teleport-values=/tmp/teleport-values-configured.yaml \
  --teleport-node-agent-token=<TOKEN> \
  --teleport-node-agent-proxy=<NODE_IP>:3080
```

> **Note:** Replace `<TOKEN>` and `<NODE_IP>` with values from Step 2.

This installs:
- **Kubernetes Agent** - Secure kubectl access via Teleport
- **Node Agent** - SSH access to the host via Teleport


### Step 4: Create Admin User

Inside the VM, create an admin user:
```bash
docker exec solo-weaver-teleport tctl users add admin --roles=editor,access --logins=root
```

This command will output a **signup link** like:
```
User "admin" has been created but requires a password. Share this URL with the user to complete user setup:
https://<NODE_IP>:3080/web/invite/<token>

NOTE: Make sure <NODE_IP>:3080 points at a Teleport proxy that users can access.
```

To access the signup URL from your Mac:

1. Start port forwarding (from your Mac, in a new terminal):
   ```bash
   task vm:teleport-forward
   ```

2. Open the signup URL in your browser, replacing `<NODE_IP>` with `localhost`:
   ```
   https://localhost:3080/web/invite/<token>
   ```

3. Complete the signup:
   - Set your password
   - Configure your second factor (OTP authenticator app like Google Authenticator, Authy, etc.)

### Step 5: Access Teleport Web UI

From your Mac:
```bash
task vm:teleport-forward
```

Open https://localhost:3080 and login with:
- **Username:** `admin` (or whatever username you created)
- **Password:** The password you set during signup
- **Second Factor:** Code from your authenticator app

Navigate to:
- **Resources** → **Kubernetes** to see your cluster
- **Resources** → **Servers** to see your node (SSH access)


---

## 🧹 Cleanup

```bash
# Stop Teleport server
task teleport:stop

# Remove all data
task teleport:clean
```

---

## 📋 What Gets Configured

**Teleport Server (Docker):**
- ✅ Auth service for certificate-based authentication
- ✅ Proxy service for secure access
- ✅ Web UI for management
- ✅ Audit logging enabled

**Teleport Kubernetes Agent (Helm):**
- ✅ Registers cluster with Teleport server
- ✅ Enables secure kubectl access
- ✅ Full command audit logging

**Teleport Node Agent (host-level):**
- ✅ SSH access to the host via Teleport
- ✅ Session recording enabled
- ✅ Role-based access control

**Teleport Node Agent (Optional):**
- ✅ SSH access to the host node
- ✅ Session recording
- ✅ Machine identity management

---

## 🔧 Advanced

### Generate New Join Token

If your Kubernetes agent token expires (24h TTL):
```bash
task teleport:kube-token
```

### View Teleport Logs

```bash
task teleport:logs
```

### Check Status

```bash
task teleport:status
```

### Manual Token Generation

```bash
docker exec solo-weaver-teleport tctl tokens add --type=kube --ttl=24h
```

---

## 🔍 Troubleshooting

### Node agent not showing in Teleport UI

1. **Check if the node agent service is running:**
   ```bash
   sudo systemctl status teleport
   ```

2. **Check the node agent logs:**
   ```bash
   sudo journalctl -u teleport -f
   ```

3. **Check if the config file exists:**
   ```bash
   cat /etc/teleport.yaml
   ```

4. **Restart the node agent:**
   ```bash
   sudo systemctl restart teleport
   ```

5. **If the token expired**, generate a new one and reinstall:
   ```bash
   task teleport:node-agent-token
   # Then re-run weaver install with the new token
   ```

### Agent can't connect to server

1. **Check the proxy address** - The agent needs to reach the Teleport server on the node IP:
   ```bash
   # Get node IP
   kubectl get nodes -o wide
   
   # Verify Teleport is listening
   curl --insecure https://<NODE_IP>:3080/webapi/ping
   ```

2. **Check the join token** - Tokens expire after 24h:
   ```bash
   task teleport:new-token
   ```

3. **Check agent logs**:
   ```bash
   kubectl logs -n teleport-agent -l app=teleport-kube-agent
   ```

### "context deadline exceeded" errors

This usually means the agent can't reach the Teleport server. Verify:
- Teleport server is running: `docker ps | grep teleport`
- Port 3080 is accessible from the node IP
- The values file has the correct `proxyAddr`

---

## 🏭 Production Setup

For production with Teleport Cloud/Enterprise (e.g., `hashgraph.teleport.sh`):

### Step 1: Install Host-Level Agent (Optional)

On each node that needs SSH access, provide the join token from Teleport Cloud:
```bash
sudo weaver block node install \
  --teleport-enabled \
  --teleport-values=/path/to/values.yaml \
  --teleport-node-agent-token="58566f1f672c0db769bf1fe7681121dc"
```

Or in config file:
```yaml
teleport:
  enabled: true
  valuesFile: "/path/to/teleport-values.yaml"
  nodeAgentToken: "58566f1f672c0db769bf1fe7681121dc"
```

The URL is automatically constructed as:
```
https://hashgraph.teleport.sh/scripts/<token>/install-node.sh
```

### Step 2: Create Values File

See `teleport-values-prod-example.yaml` for a complete example.

---

## 📁 Files

| File | Purpose |
|------|---------|
| `docker-compose.yml` | Teleport server container definition (local dev) |
| `teleport-values-local.yaml` | Template for local dev Helm values |
| `teleport-values-prod-example.yaml` | Example production Helm values |
| `/tmp/teleport-values-configured.yaml` | Generated values with real IP and token (created by `task teleport:start`) |

