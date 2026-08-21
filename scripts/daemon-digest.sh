#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
#
# Print the SHA-256 of a file, or fail loudly.
#
# Used by build:cli to stamp the co-released daemon's digest into the CLI, and by
# verify:cli to check the pairing before a release. Both callers must treat a
# digest they could not compute as fatal, which is why this refuses to emit
# anything it has not validated:
#
#   - an empty pin builds a CLI whose daemon download silently falls back to the
#     published-checksum path instead of the compiled-in digest;
#   - an empty needle makes verify:cli's `grep -F` match every file, so the
#     release guard would report success on any CLI at all.
#
# sha256sum is coreutils (Linux/CI); macOS ships shasum instead.

set -euo pipefail

if [ $# -ne 1 ]; then
  echo "usage: $(basename "$0") <file>" >&2
  exit 2
fi

file="$1"

if [ ! -f "$file" ]; then
  echo "error: $file is not a regular file" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  digest="$(sha256sum "$file" | cut -d' ' -f1)"
else
  digest="$(shasum -a 256 "$file" | cut -d' ' -f1)"
fi

if ! printf '%s' "$digest" | grep -Eq '^[0-9a-f]{64}$'; then
  echo "error: could not compute a sha256 digest for $file (got '$digest')" >&2
  exit 1
fi

printf '%s\n' "$digest"
