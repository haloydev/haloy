#!/usr/bin/env bas

set -e

hcd $(git rev-parse --show-toplevel)
docker build -t haloyd-dev -f ./dev/haloyd/Dockerfile.dev .
