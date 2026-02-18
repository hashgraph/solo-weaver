#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Initialize Vault with Alloy secrets for local development
set -euo pipefail

export VAULT_ADDR='http://localhost:8200'
export VAULT_TOKEN='devtoken'

echo "Waiting for Vault to be ready..."
echo "Checking Vault at: $VAULT_ADDR"

# Check if Vault container is running
if ! docker ps | grep -q solo-weaver-vault; then
  echo "❌ Error: Vault container is not running!"
  echo "Container status:"
  docker ps -a | grep vault || echo "No vault container found"
  echo ""
  echo "Container logs:"
  docker logs solo-weaver-vault 2>&1 | tail -20 || echo "Could not get logs"
  exit 1
fi

# Function to run vault commands (uses docker exec if vault CLI not available)
vault_cmd() {
  if command -v vault &>/dev/null; then
    vault "$@"
  else
    docker exec -e VAULT_ADDR="$VAULT_ADDR" -e VAULT_TOKEN="$VAULT_TOKEN" solo-weaver-vault vault "$@"
  fi
}

# Wait for Vault to be ready (with timeout)
MAX_ATTEMPTS=30
ATTEMPT=0
until vault_cmd status &>/dev/null; do
  ATTEMPT=$((ATTEMPT + 1))
  if [ $ATTEMPT -ge $MAX_ATTEMPTS ]; then
    echo "❌ Timeout waiting for Vault to be ready after ${MAX_ATTEMPTS} attempts"
    echo ""
    echo "Container logs:"
    docker logs solo-weaver-vault 2>&1 | tail -30
    echo ""
    echo "Vault status output:"
    vault_cmd status 2>&1 || true
    exit 1
  fi
  echo "  Vault is unavailable - sleeping (attempt $ATTEMPT/$MAX_ATTEMPTS)"
  sleep 2
done

echo "✓ Vault is ready!"

# Enable KV v2 secrets engine at secret/
echo "Enabling KV v2 secrets engine..."
vault_cmd secrets enable -version=2 -path=secret kv 2>/dev/null || echo "  KV engine already enabled"

# Create Alloy secrets for local development
echo "Creating Alloy secrets..."

# Create secrets for named remotes (used with --add-prometheus-remote=local:... and --add-loki-remote=local:...)
vault_cmd kv put secret/grafana/alloy/vm-cluster/prometheus/local \
  password="local-dev" \
  description="Prometheus credentials for 'local' remote"

vault_cmd kv put secret/grafana/alloy/vm-cluster/loki/local \
  password="local-dev" \
  description="Loki credentials for 'local' remote"

# Also create legacy single-remote secrets for backward compatibility
vault_cmd kv put secret/grafana/alloy/vm-cluster/prometheus \
  password="local-dev" \
  description="Prometheus credentials for legacy single remote"

vault_cmd kv put secret/grafana/alloy/vm-cluster/loki \
  password="local-dev" \
  description="Loki credentials for legacy single remote"

# Enable userpass auth
echo "Enabling userpass authentication..."
vault_cmd auth enable userpass 2>/dev/null || echo "  Userpass auth already enabled"

# Create a user for External Secrets Operator
echo "Creating ESO user..."
vault_cmd write auth/userpass/users/eso-user \
  password=eso-password \
  policies=eso-policy

# Create policy for ESO
echo "Creating ESO policy..."
cat > /tmp/eso-policy.hcl <<EOF
path "secret/data/grafana/alloy/*" {
  capabilities = ["read"]
}
path "secret/metadata/grafana/alloy/*" {
  capabilities = ["read", "list"]
}
EOF

if command -v vault &>/dev/null; then
  vault policy write eso-policy /tmp/eso-policy.hcl
else
  # Copy policy file into container, then use it
  docker cp /tmp/eso-policy.hcl solo-weaver-vault:/tmp/eso-policy.hcl
  docker exec -e VAULT_ADDR="$VAULT_ADDR" -e VAULT_TOKEN="$VAULT_TOKEN" solo-weaver-vault vault policy write eso-policy /tmp/eso-policy.hcl
fi

rm -f /tmp/eso-policy.hcl

echo ""
echo "✅ Vault initialization complete!"
echo ""
echo "Vault UI:  http://localhost:8200"
echo "Token:     devtoken"
echo "ESO User:  eso-user"
echo "ESO Pass:  eso-password"
echo ""
echo "Secrets created:"
echo "  - secret/grafana/alloy/vm-cluster/prometheus/local (password: local-dev)"
echo "  - secret/grafana/alloy/vm-cluster/loki/local (password: local-dev)"
echo "  - secret/grafana/alloy/vm-cluster/prometheus (password: local-dev) [legacy]"
echo "  - secret/grafana/alloy/vm-cluster/loki (password: local-dev) [legacy]"
echo ""

