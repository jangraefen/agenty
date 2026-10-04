# api

Contracts shared between components:

- **OpenAPI 3.1** for the public REST API.
- **Protobuf / Connect** for the protocol between the server and `agenty-sandbox`.
- Go and TypeScript code generated from these contracts.

API and protocol changes start here. Generated code is never edited by hand: change the contract and regenerate. See [ARCHITECTURE.md §4 and §13.1](../docs/ARCHITECTURE.md).
