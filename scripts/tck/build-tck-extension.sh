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
# Upstream gitignores gradle/wrapper/gradle-wrapper.jar so ./gradlew won't run
# from a fresh clone. Use system gradle (caller is responsible for installing
# it — the CI workflow uses gradle/actions/setup-gradle).
gradle --no-daemon build

ZIP=$(ls build/hivemq-extension/sparkplug-tck-*.zip 2>/dev/null | head -1)
if [ -z "$ZIP" ]; then
  echo "no extension zip in build/hivemq-extension/ — gradle output:"
  find build -maxdepth 3 -name '*.zip' 2>/dev/null || true
  exit 1
fi

mkdir -p "$OUT_DIR"
rm -rf "$OUT_DIR/sparkplug-tck"
unzip -q "$ZIP" -d "$OUT_DIR"
echo "Extension installed at $OUT_DIR/sparkplug-tck"
