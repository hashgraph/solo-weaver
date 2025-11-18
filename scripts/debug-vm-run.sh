#!/bin/bash

# This script runs a remote debug session on a VM
set -e

# --- Configuration ---
VM_NAME="solo-weaver-debian"
VM_USER="${VM_USER:-weaver}"

# Get the absolute path to the project root (parent of scripts directory)
SCRIPT_DIR="$(dirname "$(realpath "${BASH_SOURCE[0]}")")"
SSH_PRIVATE_KEY="${SSH_PRIVATE_KEY:-${SCRIPT_DIR}/../.ssh/id_rsa_vm}"

if [ -z "$VM_HOST" ]; then
  echo "VM_HOST not set, trying to get it from utmctl..."
  VM_HOST=$(utmctl ip-address "$VM_NAME" 2>/dev/null | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' | head -1)
fi

if [ -z "$VM_HOST" ]; then
  echo "Error: VM_HOST is not set. Please run 'task vm:start' to ensure VM is running and .env file is created."
  exit 1
fi

VM_PROJECT_PATH="/mnt/solo-weaver"
SSH_OPTS="-i $SSH_PRIVATE_KEY -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"

# --- Script ---

if [ "$#" -lt 1 ]; then
    echo "Usage: $0 <test|app> [args...]"
    echo "Examples:"
    echo "  $0 test ./pkg/fsx"
    echo "  $0 app cluster deploy --help"
    exit 1
fi

COMMAND=$1
shift
ARGS=("$@")

echo "🚀 Starting debug session on VM for '$COMMAND'..."

# Kill any existing processes using port 2345
echo "🔍 Checking for existing processes on port 2345..."
ssh $SSH_OPTS "$VM_USER@$VM_HOST" "lsof -nP -t -iTCP:2345 -sTCP:LISTEN | xargs -r kill 2>/dev/null || true" || true
echo "✅ Port 2345 cleanup completed"

# Construct the dlv command
DLV_COMMAND="cd $VM_PROJECT_PATH && source /etc/profile.d/go.sh && unset GOFLAGS && export CGO_ENABLED=0 && "
if [ "$COMMAND" == "test" ]; then
    # Build test binary manually with debug symbols and use dlv exec to support breakpoints
    DLV_COMMAND+="go test -c -gcflags='all=-N -l' -o /tmp/test-binary ${ARGS[0]} && sudo ~/go/bin/dlv exec /tmp/test-binary --listen=:2345 --headless=true --api-version=2 --accept-multiclient --continue=false -- -test.v"
elif [ "$COMMAND" == "app" ]; then
    # Use exec mode instead of debug mode to avoid runtime issues
    # First build the binary, then run it with dlv exec
    DLV_COMMAND+="go build -ldflags='-compressdwarf=false' -o /tmp/weaver-debug ./cmd/weaver && sudo ~/go/bin/dlv exec /tmp/weaver-debug --listen=:2345 --headless=true --api-version=2 --accept-multiclient -- ${ARGS[@]}"
else
    echo "Invalid command: $COMMAND. Use 'test' or 'app'."
    exit 1
fi

# Execute the command on the VM via SSH
echo "Executing: ssh $SSH_OPTS $VM_USER@$VM_HOST \"$DLV_COMMAND\""
ssh -L 2345:127.0.0.1:2345 $SSH_OPTS "$VM_USER@$VM_HOST" "$DLV_COMMAND"
