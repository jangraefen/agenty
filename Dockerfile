# syntax=docker/dockerfile:1

ARG NODE_VERSION=24

FROM node:${NODE_VERSION}-bookworm-slim AS base
ENV PNPM_HOME=/pnpm PATH=/pnpm:$PATH NEXT_TELEMETRY_DISABLED=1
RUN corepack enable
WORKDIR /app

FROM base AS deps
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --frozen-lockfile

FROM base AS build
ARG OPA_VERSION=v1.21.1
ARG TARGETARCH
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
  && rm -rf /var/lib/apt/lists/*
# opa compiles policies/ to WASM during the build; checksum-verified static binary.
RUN set -eu; asset="opa_linux_${TARGETARCH}_static"; \
  base="https://github.com/open-policy-agent/opa/releases/download/${OPA_VERSION}"; \
  curl -fsSL -o /tmp/opa "$base/$asset"; \
  echo "$(curl -fsSL "$base/$asset.sha256" | cut -d ' ' -f 1)  /tmp/opa" | sha256sum -c -; \
  install -m 0755 /tmp/opa /usr/local/bin/opa; rm /tmp/opa; opa version
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN sh scripts/build.sh

FROM node:${NODE_VERSION}-bookworm-slim AS runtime
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 HOSTNAME=0.0.0.0 PORT=3000
WORKDIR /app
COPY --from=build --chown=node:node /app/.next/standalone ./
USER node
EXPOSE 3000
CMD ["node", "server.js"]
