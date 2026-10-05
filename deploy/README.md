# deploy

Deployment artifacts: Dockerfiles for `agenty` and `agenty-sandbox`, and a Docker Compose setup that runs them together with PostgreSQL. Helm charts come later. See [ARCHITECTURE.md §14](../docs/ARCHITECTURE.md).

## Images

`agenty.Dockerfile` and `agenty-sandbox.Dockerfile` build the two binaries from the repository root:

```sh
task build:images   # builds agenty:<version> and agenty-sandbox:<version>
task test:images    # builds them, then checks --version, the version label, and the non-root user
```

Both run as the non-root user 65532 on a distroless static base image. Base images are pinned by digest; when bumping Go, update `go.work` and the `golang` image in both Dockerfiles together (a unit test checks that they match).

## Local PostgreSQL

`compose.yaml` runs PostgreSQL with pgvector for local development, using the same image as the tests:

```sh
docker compose -f deploy/compose.yaml up -d --wait
# postgres://agenty:agenty-dev-password@localhost:5432/agenty?sslmode=disable
```

Set `AGENTY_POSTGRES_PORT` to use another host port. `docker compose -f deploy/compose.yaml down -v` removes the container and its data.
