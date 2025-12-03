#!/bin/bash
# SPDX-License-Identifier: Apache-2.0

# This script sets up remote debugging for the Ubuntu container
set -e

CONTAINER_NAME="solo-weaver-debug"
DEBUG_PORT="2345"

echo "🐳 Setting up Docker container for debugging..."

# Clean up any existing container
echo "📦 Cleaning up existing containers..."
docker stop $CONTAINER_NAME 2>/dev/null || true
docker rm $CONTAINER_NAME 2>/dev/null || true

# Build the image
echo "🏗️  Building Docker image..."
cd "$(dirname "$0")"  # Ensure we're in the build directory
docker build -t local/solo-weaver-local:latest -f Dockerfile.local .

echo "🚀 Starting debug container..."
# Start container with debug port exposed (2345 for both tests and app)
docker run -d --name $CONTAINER_NAME --privileged --cap-add=ALL \
  -v "/sys/fs/cgroup:/sys/fs/cgroup:rw" \
  -v "$(pwd)"/..:/app \
  -p $DEBUG_PORT:$DEBUG_PORT \
  local/solo-weaver-local:latest tail -f /dev/null

# Wait for container to be ready
sleep 5

echo "⚙️  Setting up debugging environment in container..."
# Install Delve and setup environment
docker exec $CONTAINER_NAME bash -c "
  cd /app && 
  apt-get update -qq && 
  go install github.com/go-delve/delve/cmd/dlv@latest &&
  echo 'Delve installed successfully'
"

echo "✅ Container setup complete!"
echo ""
echo "📋 Next steps:"
echo "1. Debug tests: ./debug-run.sh test [package]"
echo "2. Debug app: ./debug-run.sh app [args...]"
echo ""
echo "💡 Examples:"
echo "   ./debug-run.sh test ./pkg/fsx"
echo "   ./debug-run.sh app cluster deploy --help"
echo ""
echo "💡 See build/README.md for detailed debugging instructions"
echo ""
echo "🛑 To stop: docker stop $CONTAINER_NAME"
