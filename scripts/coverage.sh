#!/bin/sh
# IDEA.md: the gateway, policy and secret redaction are where security lives
# and get full coverage. Fails unless each package's statements are all
# covered by its own tests.
set -eu
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
for pkg in toolgateway policy secret; do
  go test -coverprofile="$dir/$pkg.out" "./internal/$pkg/" >/dev/null
  total=$(go tool cover -func="$dir/$pkg.out" | awk '/^total:/ {print $3}')
  echo "$pkg coverage: $total"
  [ "$total" = "100.0%" ] || exit 1
done
