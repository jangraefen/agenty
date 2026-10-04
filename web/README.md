# web

The web UI: a React + Vite single-page application built to static assets. It can be served by the `agenty` binary or hosted separately.

The web app is a pure API client: it uses only the public API, and only through the client generated from `api/`. See [ARCHITECTURE.md §13.3](../docs/ARCHITECTURE.md).

## Development

Requires Node.js and pnpm (`task setup` checks the versions); this repository uses pnpm, never npm. The Task targets install the pinned packages with `pnpm install --frozen-lockfile` when needed:

- `task lint:web`: Biome and the TypeScript type check
- `task test:unit:web`: Vitest
- `task build:web`: Vite production build into `web/dist`
- `task check:licenses:web`: dependency license allowlists (`scripts/check-licenses.mjs`) ([ARCHITECTURE.md §16](../docs/ARCHITECTURE.md))

`pnpm run dev` (in `web/`) starts the Vite dev server.
