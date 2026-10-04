# web

The web UI: a React + Vite single-page application built to static assets. It can be served by the `agenty` binary or hosted separately.

The web app is a pure API client: it uses only the public API, and only through the client generated from `api/`. See [ARCHITECTURE.md §13.3](../docs/ARCHITECTURE.md).

## Development

Requires Node.js with npm (`task setup` checks the version). The Task targets install the pinned npm packages with `npm ci` when needed:

- `task lint:web`: Biome and the TypeScript type check
- `task test:unit:web`: Vitest
- `task build:web`: Vite production build into `web/dist`
- `task check:licenses:web`: npm license allowlists ([ARCHITECTURE.md §16](../docs/ARCHITECTURE.md))

`npm run dev` (in `web/`) starts the Vite dev server.
