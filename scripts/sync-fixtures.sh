#!/usr/bin/env bash
# Vendor the rendered kaliki-template examples (backend/ and api/services/ only)
# into testdata/kaliki-template/. Usage: scripts/sync-fixtures.sh <path-to-kaliki-template>
set -euo pipefail
src="${1:?usage: sync-fixtures.sh <path-to-kaliki-template>}"
here="$(cd "$(dirname "$0")/.." && pwd)"
dest="$here/testdata/kaliki-template"
rm -rf "$dest"
mkdir -p "$dest"
for ex in minimal jwt-full-otp kitchen-sink; do
  from="$src/examples/rendered/$ex"
  mkdir -p "$dest/$ex/api"
  rsync -a --exclude 'node_modules' "$from/backend" "$dest/$ex/"
  rsync -a "$from/api/services" "$dest/$ex/api/"
done
git -C "$src" rev-parse HEAD > "$dest/SOURCE"
echo "synced $(cat "$dest/SOURCE")"
