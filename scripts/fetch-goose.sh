#!/usr/bin/env sh
set -eu
root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
dir="$root/third_party"
mkdir -p "$dir"
ver="v3.24.1"
base="https://github.com/pressly/goose/releases/download/${ver}"
for arch in arm64 x86_64; do
  out="$dir/goose_linux_${arch}"
  if [ -x "$out" ]; then
    echo "exists: $out"
    continue
  fi
  echo "fetch: goose_linux_${arch}"
  curl -fsSL -o "$out" "${base}/goose_linux_${arch}"
  chmod +x "$out"
done
