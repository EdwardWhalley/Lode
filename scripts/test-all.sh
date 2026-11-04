#!/usr/bin/env bash
# Runs the whole test suite against real database servers in Docker. --keep leaves them
# running afterwards; other arguments go to `go test`.
set -euo pipefail
cd "$(dirname "$0")/.."

if ! docker info >/dev/null 2>&1; then
  echo "Docker is not installed or not running. Start it, then run this again." >&2
  exit 1
fi

keep=0
args=()
for a in "$@"; do
  if [ "$a" = "--keep" ]; then keep=1; else args+=("$a"); fi
done

if [ "$keep" -eq 0 ]; then
  trap 'docker compose down' EXIT
fi

docker compose up -d --wait

export CGO_ENABLED=1
export LODE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:55432/postgres?sslmode=disable'
export LODE_TEST_MYSQL_DSN='mysql://root:lode-test@127.0.0.1:53306/'
export LODE_TEST_MARIADB_DSN='mariadb://root:lode-test@127.0.0.1:53307/'
export LODE_REQUIRE_SERVERS=1

go test -count=1 ${args[@]+"${args[@]}"} ./...
