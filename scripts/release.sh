#!/usr/bin/env bash
# Builds a self-contained release tarball for the Ubuntu VPS:
#   VITE_GOOGLE_CLIENT_ID=<id>.apps.googleusercontent.com GOARCH=amd64 npm run release
# Output: release/dietsis-<version>-linux-<arch>.tar.gz (+ .sha256). Nothing is uploaded anywhere.
set -euo pipefail
cd "$(dirname "$0")/.."

ARCH="${GOARCH:-amd64}"
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"

if [ -z "${VITE_GOOGLE_CLIENT_ID:-}" ] && [ "${ALLOW_PREVIEW:-0}" != "1" ]; then
  echo "VITE_GOOGLE_CLIENT_ID is empty: the build would run in local-only preview mode (no login, no sync)." >&2
  echo "Set it, or ALLOW_PREVIEW=1 to build anyway." >&2
  exit 1
fi

echo ">> checks"
npm run check

echo ">> frontend build"
npm run build

echo ">> backend build (linux/$ARCH, static)"
OUT="release/dietsis-$VERSION-linux-$ARCH"
rm -rf "$OUT" "$OUT.tar.gz" "$OUT.tar.gz.sha256"
mkdir -p "$OUT/bin" "$OUT/deploy"
(cd server && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "../$OUT/bin/dietsis-server" .)

cp -r dist "$OUT/www"
# Source maps would expose the original source; the app does not need them in production.
find "$OUT/www" -name '*.map' -delete
cp server/deploy/* "$OUT/deploy/"
echo "$VERSION" > "$OUT/VERSION"

tar -C release -czf "$OUT.tar.gz" "$(basename "$OUT")"
( cd release && shasum -a 256 "$(basename "$OUT").tar.gz" > "$(basename "$OUT").tar.gz.sha256" )
echo ">> $OUT.tar.gz"
cat "$OUT.tar.gz.sha256"
