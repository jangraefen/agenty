#!/bin/sh
# Compiles policies/ to WebAssembly: build/policy/policy.wasm (entrypoint agenty/authz/decision).
# The output lies outside policies/, so later opa runs never load the bundle's data.json.
set -eu

out=build/policy
rm -rf "$out"
mkdir -p "$out"
opa build -t wasm -e agenty/authz/decision --ignore '*_test.rego' -o "$out/bundle.tar.gz" policies
# The member is "/policy.wasm" or "policy.wasm" depending on the opa version; look it up so
# GNU tar and bsdtar both match it. GNU tar warns about the leading slash on stderr; failures still surface via the checks below.
member=$(tar -tzf "$out/bundle.tar.gz" 2>/dev/null | grep -E '^/?policy\.wasm$' | head -n 1)
test -n "$member"
tar -xzOf "$out/bundle.tar.gz" "$member" 2>/dev/null > "$out/policy.wasm"
test -s "$out/policy.wasm"
