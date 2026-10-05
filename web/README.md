# web

The web UI: a React + Vite single-page application built to static assets. It can be served by the `agenty` binary or hosted separately.

The web app is a pure API client: it uses only the public API, and only through the client generated from `api/`: `createApiClient` in `src/api/client.ts` (openapi-fetch) over the types in `src/api/schema.gen.ts`, which `task generate` produces with openapi-typescript from the `codegen/` workspace package. See [ARCHITECTURE.md §13.3](../docs/ARCHITECTURE.md).

## Development

Requires Node.js and pnpm (`task setup` checks the versions); this repository uses pnpm, never npm. The Task targets install the pinned packages with `pnpm install --frozen-lockfile` when needed:

- `task lint:web`: Biome (warnings fail, too) and the TypeScript type check
- `task test:unit:web`: Vitest (`src/test-setup.ts` cleans up rendered components after each test; stubbed globals and mocks are restored by configuration)
- `task build:web`: Vite production build into `web/dist`
- `task check:licenses:web`: dependency license allowlists (`scripts/check-licenses.mjs`) ([ARCHITECTURE.md §16](../docs/ARCHITECTURE.md))

`pnpm run dev` (in `web/`) starts the Vite dev server.
