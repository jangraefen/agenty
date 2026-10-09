#!/bin/sh
# Production build shared by `task build` and the Dockerfile. Produces a self-contained
# .next/standalone directory: server, static assets, policy wasm and the migration runner.
set -eu

sh scripts/build-policy.sh
pnpm exec next build

out=.next/standalone
mkdir -p "$out/.next"
rm -rf "$out/.next/static" && cp -R .next/static "$out/.next/static"
if [ -d public ]; then rm -rf "$out/public" && cp -R public "$out/public"; fi

# Migration runner: bundled, because file tracing does not include drizzle's migrator.
pnpm exec esbuild scripts/migrate.mjs --bundle --platform=node --format=esm --target=node24 \
  --outfile="$out/scripts/migrate.mjs" --log-level=warning
rm -rf "$out/src/server/db/migrations" && mkdir -p "$out/src/server/db"
cp -R src/server/db/migrations "$out/src/server/db/migrations"

if [ ! -s "$out/build/policy/policy.wasm" ]; then
  echo "build: build/policy/policy.wasm is missing from $out (check outputFileTracingIncludes)" >&2
  exit 1
fi
