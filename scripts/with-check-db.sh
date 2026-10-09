#!/bin/sh
# Runs the given command with AGENTY_TEST_DATABASE_URL set to a database of
# its own: a compose project of its own, on a port Docker picks, removed with
# its data afterwards. So task check never collides with the development
# database, another check, or anything else listening on 5432.
set -eu
project="agenty-check-$$"
password="${POSTGRES_PASSWORD:-agenty-dev-password}"
down() {
  docker compose --project-name "$project" down --volumes --remove-orphans >/dev/null 2>&1 ||
    echo "with-check-db: cannot remove compose project $project" >&2
}
trap down EXIT
trap 'exit 130' INT TERM
POSTGRES_PORT=0 docker compose --project-name "$project" up --detach --wait --quiet-pull
port=$(docker compose --project-name "$project" port postgres 5432)
port=${port##*:}
export AGENTY_TEST_DATABASE_URL="postgres://agenty:$password@127.0.0.1:$port/agenty"
"$@"
