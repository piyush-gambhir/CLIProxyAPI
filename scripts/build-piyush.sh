#!/bin/sh
# Build a traceable release from the committed fork checkout.
set -eu
cd "$(dirname "$0")/.."
version=${1:-piyush-$(git rev-parse --short=12 HEAD)}
case "$version" in ''|*[!a-zA-Z0-9._-]*) echo 'Invalid release name' >&2; exit 2;; esac
if [ -n "$(git status --porcelain)" ]; then
  echo 'Commit or stash changes before building a release.' >&2
  exit 1
fi
commit=$(git rev-parse HEAD)
output="bin/$version"
if [ -e "$output" ]; then echo "Release output already exists: $output" >&2; exit 1; fi
mkdir -p "$output"
built_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
go build -trimpath -ldflags "-s -w -X main.Version=$version -X main.Commit=$commit -X main.BuildDate=$built_at" -o "$output/cliproxyapi" ./cmd/server
printf '{"version":"%s","commit":"%s","repository":"https://github.com/piyush-gambhir/CLIProxyAPI","builtAt":"%s"}\n' "$version" "$commit" "$built_at" > "$output/manifest.json"
echo "Built $output/cliproxyapi ($commit)"
