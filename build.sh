#!/usr/bin/env bash
# Cross-compile always-green into dist/ for macOS and Linux
set -euo pipefail

root="$(cd "$(dirname "$0")" && pwd)"
cd "$root"

mkdir -p dist

targets=(
  darwin/arm64
  darwin/amd64
  linux/amd64
  linux/arm64
)

for pair in "${targets[@]}"; do
  os="${pair%/*}"
  arch="${pair#*/}"
  out="dist/always-green-${os}-${arch}"
  echo "building $out"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags="-s -w" -o "$out" ./cmd/always-green
done

echo
ls -lh dist/always-green-*
