#!/usr/bin/env bash
# Build the Sparkplug TCK HiveMQ extension from source.
#
# The Eclipse Sparkplug project does not publish binary TCK artifacts to
# GitHub releases — the extension zip is produced by the gradle build. This
# script clones the repo at a pinned ref, runs the build, and unpacks the
# extension into a directory suitable for mounting into HiveMQ CE at
# /opt/hivemq/extensions/sparkplug-tck.
#
# Usage:
#   build-tck-extension.sh <output-dir>
# Env:
#   TCK_REPO    default: https://github.com/eclipse-sparkplug/sparkplug.git
#   TCK_REF     default: master   (override with a tag/SHA for reproducibility)

set -euo pipefail

OUT_DIR="${1:?usage: build-tck-extension.sh <output-dir>}"
TCK_REPO="${TCK_REPO:-https://github.com/eclipse-sparkplug/sparkplug.git}"
TCK_REF="${TCK_REF:-master}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "Cloning $TCK_REPO @ $TCK_REF ..."
git clone --depth 1 --branch "$TCK_REF" "$TCK_REPO" "$WORK/sparkplug" \
  || { git clone "$TCK_REPO" "$WORK/sparkplug" && git -C "$WORK/sparkplug" checkout "$TCK_REF"; }

cd "$WORK/sparkplug/tck"
chmod +x ./gradlew
./gradlew --no-daemon hivemqExtensionZip

ZIP=$(ls build/hivemq-extension/sparkplug-tck-*.zip | head -1)
[ -z "$ZIP" ] && { echo "no extension zip produced"; exit 1; }

mkdir -p "$OUT_DIR"
rm -rf "$OUT_DIR/sparkplug-tck"
unzip -q "$ZIP" -d "$OUT_DIR"
echo "Extension installed at $OUT_DIR/sparkplug-tck"
