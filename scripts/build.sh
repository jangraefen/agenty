#!/bin/sh
# Production build shared by `task build` and the Dockerfile. Produces a self-contained
# .next/standalone directory: server, static assets, policy wasm and the SQL migrations the
# server applies on start.
set -eu

sh scripts/build-policy.sh
pnpm exec next build

out=.next/standalone
mkdir -p "$out/.next"
rm -rf "$out/.next/static" && cp -R .next/static "$out/.next/static"
if [ -d public ]; then rm -rf "$out/public" && cp -R public "$out/public"; fi
# Next copies local .env files into the output and loads them at runtime; configuration must
# come from the real environment, and dev credentials must not travel with the build.
rm -f "$out"/.env "$out"/.env.*

# Read at runtime by src/instrumentation.ts; file tracing cannot see them.
rm -rf "$out/src/server/db/migrations" && mkdir -p "$out/src/server/db"
cp -R src/server/db/migrations "$out/src/server/db/migrations"

if [ ! -s "$out/build/policy/policy.wasm" ]; then
  echo "build: build/policy/policy.wasm is missing from $out (check outputFileTracingIncludes)" >&2
  exit 1
fi
