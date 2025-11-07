#!/usr/bin/env bash

set -e

VERSION=$(date +%Y%m%d%H%M%S)

docker build -t my-nginx:latest -t my-nginx:v${VERSION} .

echo "Built my-nginx:latest and my-nginx:v${VERSION}"

