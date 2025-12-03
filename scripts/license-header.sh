#!/bin/bash
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Get the project root directory (parent of scripts directory)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Change to project root
cd "$PROJECT_ROOT"

# License header for different file types
GO_HEADER="// SPDX-License-Identifier: Apache-2.0"
SHELL_HEADER="# SPDX-License-Identifier: Apache-2.0"
YAML_HEADER="# SPDX-License-Identifier: Apache-2.0"

# Function to check if file has license header
has_license_header() {
    local file="$1"
    head -n 5 "$file" | grep -q "SPDX-License-Identifier: Apache-2.0"
}

# Function to add license header to Go files
add_go_header() {
    local file="$1"
    if ! has_license_header "$file"; then
        echo "Adding license header to: $file"
        # Create a temporary file with the header
        echo "$GO_HEADER" > "$file.tmp"
        echo "" >> "$file.tmp"
        cat "$file" >> "$file.tmp"
        mv "$file.tmp" "$file"
        return 0
    fi
    return 1
}

# Function to add license header to shell scripts
add_shell_header() {
    local file="$1"
    if ! has_license_header "$file"; then
        echo "Adding license header to: $file"
        # Check if file starts with shebang
        if head -n 1 "$file" | grep -q '^#!'; then
            # If shebang exists, insert license header after it
            {
                head -n 1 "$file"
                echo "$SHELL_HEADER"
                tail -n +2 "$file"
            } > "$file.tmp"
        else
            # Otherwise, add license header at the top
            {
                echo "$SHELL_HEADER"
                echo ""
                cat "$file"
            } > "$file.tmp"
        fi
        mv "$file.tmp" "$file"
        return 0
    fi
    return 1
}

# Function to add license header to YAML files
add_yaml_header() {
    local file="$1"
    if ! has_license_header "$file"; then
        echo "Adding license header to: $file"
        # Check if file starts with yaml directives (---)
        if head -n 1 "$file" | grep -q '^---'; then
            # If YAML directive exists, insert license header after it
            {
                head -n 1 "$file"
                echo "$YAML_HEADER"
                tail -n +2 "$file"
            } > "$file.tmp"
        else
            # Otherwise, add license header at the top
            {
                echo "$YAML_HEADER"
                echo ""
                cat "$file"
            } > "$file.tmp"
        fi
        mv "$file.tmp" "$file"
        return 0
    fi
    return 1
}

# Main function
main() {
    local action="${1:-check}"
    local count=0
    local total=0

    case "$action" in
        check)
            echo "Checking for missing SPDX license headers..."

            # Check Go files
            while IFS= read -r -d '' file; do
                total=$((total + 1))
                if ! has_license_header "$file"; then
                    echo "Missing license header: $file"
                    count=$((count + 1))
                fi
            done < <(find . -type f -name "*.go" \
                -not -path "./vendor/*" \
                -not -path "./bin/*" \
                -not -path "./.git/*" \
                -not -name "*_generated.go" \
                -not -name "mocks_generated.go" \
                -print0)

            # Check shell scripts
            while IFS= read -r -d '' file; do
                total=$((total + 1))
                if ! has_license_header "$file"; then
                    echo "Missing license header: $file"
                    count=$((count + 1))
                fi
            done < <(find . -type f -name "*.sh" \
                -not -path "./vendor/*" \
                -not -path "./bin/*" \
                -not -path "./.git/*" \
                -print0)

            # Check YAML files (excluding vendor and specific files)
            while IFS= read -r -d '' file; do
                # Skip config.yaml, integration-test.json, and other non-source files
                if [[ "$file" =~ (config\.yaml|integration-test\.json)$ ]]; then
                    continue
                fi
                total=$((total + 1))
                if ! has_license_header "$file"; then
                    echo "Missing license header: $file"
                    count=$((count + 1))
                fi
            done < <(find . -type f \( -name "*.yaml" -o -name "*.yml" \) \
                -not -path "./vendor/*" \
                -not -path "./bin/*" \
                -not -path "./.git/*" \
                -print0)

            echo ""
            echo "Summary: $count of $total files are missing license headers"

            if [ $count -gt 0 ]; then
                exit 1
            fi
            ;;

        add)
            echo "Adding SPDX license headers to source files..."

            # Add headers to Go files
            while IFS= read -r -d '' file; do
                if add_go_header "$file"; then
                    count=$((count + 1))
                fi
            done < <(find . -type f -name "*.go" \
                -not -path "./vendor/*" \
                -not -path "./bin/*" \
                -not -path "./.git/*" \
                -not -name "*_generated.go" \
                -not -name "mocks_generated.go" \
                -print0)

            # Add headers to shell scripts
            while IFS= read -r -d '' file; do
                if add_shell_header "$file"; then
                    count=$((count + 1))
                fi
            done < <(find . -type f -name "*.sh" \
                -not -path "./vendor/*" \
                -not -path "./bin/*" \
                -not -path "./.git/*" \
                -print0)

            # Add headers to YAML files
            while IFS= read -r -d '' file; do
                # Skip config.yaml and other non-source files
                if [[ "$file" =~ (config\.yaml|integration-test\.json)$ ]]; then
                    continue
                fi
                if add_yaml_header "$file"; then
                    count=$((count + 1))
                fi
            done < <(find . -type f \( -name "*.yaml" -o -name "*.yml" \) \
                -not -path "./vendor/*" \
                -not -path "./bin/*" \
                -not -path "./.git/*" \
                -print0)

            echo ""
            echo "Summary: Added license headers to $count files"
            ;;

        *)
            echo "Usage: $0 {check|add}"
            echo "  check  - Check for missing license headers (exits 1 if any are missing)"
            echo "  add    - Add license headers to files that are missing them"
            exit 1
            ;;
    esac
}

main "$@"

