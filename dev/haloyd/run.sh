#!/usr/bin/env bash

set -e

DOCKER_NETWORK=haloy-public

cd $(git rev-parse --show-toplevel)

CERT_STORAGE=$(mktemp -d /tmp/haloy-cert-storage-XXXXXX)
LOGS=$(mktemp -d /tmp/haloy-logs-XXXXXX)
DB_STORAGE=$(mktemp -d /tmp/haloy-db-storage-XXXXXX)
DEV_API_TOKEN="test-123"

cleanup() {
  rm -rf "$CERT_STORAGE" "$LOGS" "$DB_STORAGE"
}
trap cleanup EXIT

if ! docker network inspect "$DOCKER_NETWORK" >/dev/null 2>&1; then
  echo "Creating Docker network: $DOCKER_NETWORK"
  docker network create "$DOCKER_NETWORK"
else
  echo "Docker network $DOCKER_NETWORK already exists"
fi

docker run -it --rm \
  --name haloyd-dev \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$CERT_STORAGE":/cert-storage:rw \
  -v "$LOGS":/logs:rw \
  -v "$DB_STORAGE":/db:rw \
  -v $(pwd):/src \
  --network haloy-public \
  -p 9999:9999 \
  -e HALOY_DEBUG=true \
  -e HALOY_API_TOKEN="${DEV_API_TOKEN}" \
  haloyd-dev
