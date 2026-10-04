# deploy

Deployment artifacts: Dockerfiles for `agenty` and `agenty-sandbox`, and a Docker Compose setup that runs them together with PostgreSQL. Helm charts come later. See [ARCHITECTURE.md §14](../docs/ARCHITECTURE.md).

## Local PostgreSQL

`compose.yaml` runs PostgreSQL with pgvector for local development, using the same image as the tests:

```sh
docker compose -f deploy/compose.yaml up -d --wait
# postgres://agenty:agenty@localhost:5432/agenty?sslmode=disable
```

Set `AGENTY_POSTGRES_PORT` to use another host port. `docker compose -f deploy/compose.yaml down -v` removes the container and its data.
