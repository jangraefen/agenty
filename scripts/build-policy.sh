#!/bin/sh
# Compiles policies/ to WebAssembly: build/policy/policy.wasm (entrypoint agenty/authz/decision).
# The output lies outside policies/, so later opa runs never load the bundle's data.json.
set -eu

out=build/policy
rm -rf "$out"
mkdir -p "$out"
opa build -t wasm -e agenty/authz/decision --ignore '*_test.rego' -o "$out/bundle.tar.gz" policies
tar -xzf "$out/bundle.tar.gz" -C "$out" /policy.wasm
test -s "$out/policy.wasm"
