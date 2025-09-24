#!/bin/bash

# Unified debug script for solo-provisioner
# Usage: ./debug.sh app [args...]        - Debug application
#        ./debug.sh test [package]       - Debug tests (default: ./pkg/software)

set -e

CONTAINER_NAME="solo-provisioner-debug"
DEBUG_PORT="2345"

# Show usage if no arguments
if [ $# -eq 0 ]; then
    echo "Usage:"
    echo "  ./debug.sh app [args...]        - Debug provisioner application"
    echo "  ./debug.sh test [package]       - Debug tests in package (default: all packages)"
    echo ""
    echo "Examples:"
    echo "  ./debug.sh app cluster deploy --help"
    echo "  ./debug.sh test                 - Debug all tests"
    echo "  ./debug.sh test ./pkg/semver    - Debug specific package tests"
    echo "  ./debug.sh test ./internal/workflows"
    exit 1
fi

MODE="$1"
shift  # Remove first argument

# Check if container exists
if ! docker ps -q -f name=$CONTAINER_NAME | grep -q .; then
    echo "❌ Container $CONTAINER_NAME not found. Please run ./debug-setup.sh first."
    exit 1
fi

case "$MODE" in
    "app")
        echo "🚀 Starting application debug server in Docker container..."
        
        # Build the application first with debug symbols
        echo "🏗️ Building provisioner with debug symbols..."
        docker exec -it $CONTAINER_NAME bash -c "cd /app && go build -gcflags='all=-N -l' -o ./bin/provisioner-linux-amd64-debug ./cmd/provisioner"
        
        # Start debug server for application
        echo "🔧 Starting Delve debug server for application on port $DEBUG_PORT..."
        echo "💡 Arguments passed: $*"
        
        ARGS="$*"
        docker exec -it $CONTAINER_NAME bash -c "
            cd /app && 
            pkill dlv 2>/dev/null || true &&
            dlv exec ./bin/provisioner-linux-amd64-debug --headless --listen=:$DEBUG_PORT --api-version=2 --accept-multiclient --continue=false -- $ARGS
        "
        ;;
        
    "test")
        # Handle package specification
        if [ $# -eq 0 ]; then
            echo "🧪 No package specified for debugging."
            echo "📋 Available test packages:"
            docker exec $CONTAINER_NAME bash -c "
                cd /app && 
                find . -name '*_test.go' -exec dirname {} \; | sort | uniq | sed 's|^|    |'
            "
            echo ""
            echo "💡 Usage: ./debug.sh test <package>"
            echo "   Example: ./debug.sh test ./pkg/fsx"
            echo "   Example: ./debug.sh test ./internal/workflows"
            exit 1
        fi
        
        PACKAGE="$1"
        
        echo "🧪 Starting test debug server in Docker container..."
        echo "📦 Package: $PACKAGE"
        
        # Validate package exists (skip validation for Go patterns like ./pkg/...)
        if [[ "$PACKAGE" != *"..."* ]]; then
            docker exec $CONTAINER_NAME bash -c "
                cd /app && 
                if [ ! -d \"$PACKAGE\" ]; then
                    echo \"❌ Package directory '$PACKAGE' not found\"
                    exit 1
                fi
            " || exit 1
        fi
        
        # Start debug server for tests
        echo "🔧 Starting Delve debug server for tests on port $DEBUG_PORT..."
        docker exec -it $CONTAINER_NAME bash -c "
            cd /app && 
            pkill dlv 2>/dev/null || true &&
            dlv test $PACKAGE --headless --listen=:$DEBUG_PORT --api-version=2 --accept-multiclient
        "
        ;;
        
    *)
        echo "❌ Invalid mode: $MODE"
        echo "Use 'app' or 'test'"
        exit 1
        ;;
esac