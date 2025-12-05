#!/usr/bin/env bash
set -euo pipefail

REPO="hashgraph/solo-weaver"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"
TAG_NAME="${1:-}"  # optional release tag argument

ARCH_RAW="$(uname -m)"
case "$ARCH_RAW" in
  x86_64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "❌ Unsupported architecture: $ARCH_RAW"
    exit 1
    ;;
esac

TMP_JSON="$(mktemp)"
trap 'rm -f "$TMP_JSON"' EXIT

# ----------------------------
# Fetch release JSON
# ----------------------------
if [[ -z "$TAG_NAME" ]]; then
    API_URL="https://api.github.com/repos/$REPO/releases/latest"
    echo "🔍 Fetching latest release info..."
else
    API_URL="https://api.github.com/repos/$REPO/releases/tags/$TAG_NAME"
    echo "🔍 Fetching release info for tag $TAG_NAME..."
fi

CURL_API_ARGS=(--location -s -w "%{http_code}" -o "$TMP_JSON")
if [[ -n "$GITHUB_TOKEN" ]]; then
    CURL_API_ARGS+=(--header "Authorization: Bearer $GITHUB_TOKEN")
    echo "🔍 Using GitHub token for API access..."
fi

HTTP_CODE=$(curl "${CURL_API_ARGS[@]}" "$API_URL")
if [[ "$HTTP_CODE" != "200" ]]; then
    echo "❌ Failed to fetch release info (HTTP $HTTP_CODE)"
    cat "$TMP_JSON"
    exit 1
fi

if [[ -z "$TAG_NAME" ]]; then
    TAG_NAME=$(grep -oP '"tag_name":\s*"\K(.*?)(?=")' "$TMP_JSON")
    echo "🔖 Latest release: $TAG_NAME"
fi

# ----------------------------
# Extract asset IDs reliably
# ----------------------------
if ! command -v jq >/dev/null; then
  echo "❌ Please install jq first: sudo apt install -y jq"
  exit 1
fi

BINARY_ID=$(jq -r --arg arch "$ARCH" '.assets[] | select(.name == "weaver-linux-\($arch)") | .id' "$TMP_JSON")
CHECKSUM_ID=$(jq -r --arg arch "$ARCH" '.assets[] | select(.name == "weaver-linux-\($arch).sha256") | .id' "$TMP_JSON")

echo "Binary ID: $BINARY_ID"
echo "Checksum ID: $CHECKSUM_ID"

if [[ -z "$BINARY_ID" ]]; then
    echo "❌ Could not find binary asset for weaver-linux-$ARCH"
    exit 1
fi
if [[ -z "$CHECKSUM_ID" ]]; then
    echo "❌ Could not find checksum asset for weaver-linux-$ARCH"
    exit 1
fi

BINARY_FILE="weaver-linux-$ARCH"
CHECKSUM_FILE="weaver-linux-$ARCH.sha256"

echo "Binary file: $BINARY_FILE"
echo "Checksum file: $CHECKSUM_FILE"

# ----------------------------
# Download assets using GitHub API
# ----------------------------
if [[ -z "$GITHUB_TOKEN" ]]; then
    echo "❌ GITHUB_TOKEN is required to download private release assets."
    exit 1
fi

echo "⬇️ Downloading binary (asset ID $BINARY_ID)..."
curl -L -H "Authorization: Bearer $GITHUB_TOKEN" \
     -H "Accept: application/octet-stream" \
     "https://api.github.com/repos/$REPO/releases/assets/$BINARY_ID" \
     -o "$BINARY_FILE"

echo "⬇️ Downloading checksum (asset ID $CHECKSUM_ID)..."
curl -L -H "Authorization: Bearer $GITHUB_TOKEN" \
     -H "Accept: application/octet-stream" \
     "https://api.github.com/repos/$REPO/releases/assets/$CHECKSUM_ID" \
     -o "$CHECKSUM_FILE"

chmod +x "$BINARY_FILE"

# ----------------------------
# Verify SHA256
# ----------------------------
EXPECTED_SHA=$(awk '{print $1}' "$CHECKSUM_FILE")
CALCULATED_SHA=$(sha256sum "$BINARY_FILE" | awk '{print $1}')

echo "🔐 Verifying SHA256..."
if [[ "$EXPECTED_SHA" != "$CALCULATED_SHA" ]]; then
    echo "❌ SHA256 mismatch!"
    echo "Expected: $EXPECTED_SHA"
    echo "Calculated: $CALCULATED_SHA"
    rm -f "$BINARY_FILE" "$CHECKSUM_FILE"
    exit 1
fi
echo "✅ Checksum OK"

# ----------------------------
# Install
# ----------------------------
echo "Installing Solo Weaver..."

# First check if we can actually run sudo without a password prompt (non-interactive environments)
if ! sudo -n true 2>/dev/null; then
    echo "This script needs to run some commands with sudo."
    echo "You may be prompted for your password now."
fi

sudo ./"$BINARY_FILE" install

# Cleanup
rm -f "$BINARY_FILE" "$CHECKSUM_FILE"

weaver -h
echo "🎉 Solo Weaver installed successfully!"

