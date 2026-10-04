# api

Contracts shared between components:

- **OpenAPI 3.1** for the public REST API: [`openapi.yaml`](openapi.yaml).
- **Protobuf / Connect** for the protocol between the server and `agenty-sandbox` (not yet written).

API and protocol changes start here. `task generate` generates code from these contracts into the components that use them:

| Contract | Generator | Output |
|---|---|---|
| `openapi.yaml` | `oapi-codegen` with [`oapi-codegen.yaml`](oapi-codegen.yaml) | Gin server interfaces and models: `server/internal/httpapi/openapi.gen.go` |
| `openapi.yaml` | `openapi-typescript` (run from `web/codegen`) | TypeScript types: `web/src/api/schema.gen.ts`, used by `createApiClient` in `web/src/api/client.ts` |

Generated code is never edited by hand: change the contract, run `task generate`, and commit the result. `task check` fails (via `task check:generated`) when generated code is out of date. See [ARCHITECTURE.md §4 and §13.1](../docs/ARCHITECTURE.md).
