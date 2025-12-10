#!/usr/bin/env bash
set -euo pipefail

REPO="hashgraph/solo-weaver"
TAG_NAME="${1:-}"  # user may optionally pass v0.3.0 etc.

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

# ------------------------------------
# Function: extract asset id (to avoid the need for jq)
# ------------------------------------
extract_asset_id() {
  local file="$1"
  local target="$2"
  local in_assets=0
  local id=""
  local name=""

  while IFS= read -r line; do
    # enter assets array
    if [[ $in_assets -eq 0 && $line =~ \"assets\"[[:space:]]*:[[:space:]]*\[ ]]; then
      in_assets=1
      continue
    fi

    if [[ $in_assets -eq 1 ]]; then
      # match id
      if [[ $line =~ \"id\"[[:space:]]*:[[:space:]]*([0-9]+) ]]; then
        id="${BASH_REMATCH[1]}"
      fi

      # match name
      if [[ $line =~ \"name\"[[:space:]]*:[[:space:]]*\"([^\"]+)\" ]]; then
        name="${BASH_REMATCH[1]}"
      fi

      # when both found, check match
      if [[ -n "$id" && -n "$name" ]]; then
        if [[ "$name" == "$target" ]]; then
          printf '%s' "$id"
          return 0
        fi
        id=""
        name=""
      fi

      # exit assets array
      if [[ $line =~ \] ]]; then
        break
      fi
    fi
  done < "$file"

  return 1
}

# ------------------------------------
# Function: Fetch release JSON
# ------------------------------------
fetch_release_json() {
  if [[ -z "$TAG_NAME" ]]; then
      API_URL="https://api.github.com/repos/$REPO/releases/latest"
      echo "🔍 Fetching latest release info..."
  else
      API_URL="https://api.github.com/repos/$REPO/releases/tags/$TAG_NAME"
      echo "🔍 Fetching release info for tag $TAG_NAME..."
  fi

  CURL_API_ARGS=(--location -s -w "%{http_code}" -o "$TMP_JSON")

  if [[ -n "${GITHUB_TOKEN:-}" ]]; then
      CURL_API_ARGS+=( -H "Authorization: Bearer $GITHUB_TOKEN" )
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
}

# ------------------------------------
# Function: Download asset by ID
# ------------------------------------
download_asset() {
  local asset_id="$1"
  local output_file="$2"

  echo "⬇️ Downloading asset $asset_id → $output_file ..."
  local ARGS=(-L -H "Accept: application/octet-stream")

  if [[ -n "${GITHUB_TOKEN:-}" ]]; then
      ARGS+=( -H "Authorization: Bearer $GITHUB_TOKEN" )
  fi

  curl "${ARGS[@]}" \
    "https://api.github.com/repos/$REPO/releases/assets/$asset_id" \
    -o "$output_file"
}

# ------------------------------------
# Function: Verify SHA256
# ------------------------------------
verify_checksum() {
  local bin_file="$1"
  local checksum_file="$2"

  local EXPECTED_SHA
  EXPECTED_SHA=$(awk '{print $1}' "$checksum_file")

  local CALCULATED_SHA
  CALCULATED_SHA=$(sha256sum "$bin_file" | awk '{print $1}')

  echo "🔐 Verifying SHA256..."
  if [[ "$EXPECTED_SHA" != "$CALCULATED_SHA" ]]; then
      echo "❌ SHA256 mismatch!"
      echo "Expected: $EXPECTED_SHA"
      echo "Calculated: $CALCULATED_SHA"
      exit 1
  fi

  echo "✅ Checksum OK"
}

# ------------------------------------
# Function: Install Weaver
# ------------------------------------
install_weaver() {
  local file="$1"

  echo "Installing Solo Weaver..."

  # Allow sudo prompt if needed
  if ! sudo -n true 2>/dev/null; then
      echo "This script needs sudo; you may be asked for your password."
  fi

  sudo "./$file" install
  echo "🎉 Solo Weaver installed successfully!"
}

# ======================================================================
# MAIN SCRIPT FLOW
# ======================================================================

fetch_release_json

TARGET_BINARY="weaver-linux-$ARCH"
TARGET_CHECKSUM="$TARGET_BINARY.sha256"

BINARY_ID=$(extract_asset_id "$TMP_JSON" "$TARGET_BINARY" || true)
CHECKSUM_ID=$(extract_asset_id "$TMP_JSON" "$TARGET_CHECKSUM" || true)

echo "Binary ID: $BINARY_ID"
echo "Checksum ID: $CHECKSUM_ID"

if [[ -z "$BINARY_ID" ]]; then
    echo "❌ Could not find asset: $TARGET_BINARY"
    exit 1
fi
if [[ -z "$CHECKSUM_ID" ]]; then
    echo "❌ Could not find asset: $TARGET_CHECKSUM"
    exit 1
fi

# Download files
download_asset "$BINARY_ID" "$TARGET_BINARY"
download_asset "$CHECKSUM_ID" "$TARGET_CHECKSUM"

chmod +x "$TARGET_BINARY"

# Checksum verify
verify_checksum "$TARGET_BINARY" "$TARGET_CHECKSUM"

# Install
install_weaver "$TARGET_BINARY"

# Cleanup
rm -f "$TARGET_BINARY" "$TARGET_CHECKSUM"

# Test run
weaver -h
