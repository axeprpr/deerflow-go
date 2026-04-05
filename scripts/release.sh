#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-dev}"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
COMMIT="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
LDFLAGS="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}"
DIST_DIR="$REPO_ROOT/dist"
BIN_DIR="$REPO_ROOT/bin"

mkdir -p "$DIST_DIR" "$BIN_DIR"

DEERFLOW_UI_DIR="${DEERFLOW_UI_DIR:-${UI_DIR:-}}" "$REPO_ROOT/scripts/build_ui.sh"

build_target() {
  local goos="$1"
  local goarch="$2"
  local ext="$3"
  local out="$BIN_DIR/deerflow-${goos}-${goarch}${ext}"
  echo "building ${goos}/${goarch} -> ${out}"
  (cd "$REPO_ROOT" && GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    PATH=/usr/local/go/bin:$PATH go build -ldflags "$LDFLAGS" -o "$out" ./cmd/langgraph)
}

package_linux() {
  local arch="$1"
  local name="deerflow-go_${VERSION}_linux_${arch}"
  local stage="$DIST_DIR/$name"
  rm -rf "$stage"
  mkdir -p "$stage"
  cp "$BIN_DIR/deerflow-linux-${arch}" "$stage/deerflow"
  cp "$REPO_ROOT/.env.example" "$stage/.env.example"
  cp "$REPO_ROOT/README.md" "$stage/README.md"
  tar -C "$DIST_DIR" -czf "$DIST_DIR/${name}.tar.gz" "$name"
  rm -rf "$stage"
}

package_windows() {
  local arch="$1"
  local name="deerflow-go_${VERSION}_windows_${arch}"
  local stage="$DIST_DIR/$name"
  rm -rf "$stage"
  mkdir -p "$stage"
  cp "$BIN_DIR/deerflow-windows-${arch}.exe" "$stage/deerflow.exe"
  cp "$REPO_ROOT/.env.example" "$stage/.env.example"
  cp "$REPO_ROOT/README.md" "$stage/README.md"
  (cd "$DIST_DIR" && zip -qr "${name}.zip" "$name")
  rm -rf "$stage"
}

build_target linux amd64 ""
build_target windows amd64 ".exe"

package_linux amd64
package_windows amd64

(
  cd "$DIST_DIR"
  sha256sum *.tar.gz *.zip > checksums.txt
)

echo "release artifacts written to $DIST_DIR"
