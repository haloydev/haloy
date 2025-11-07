#!/usr/bin/env bash

set -e

DEPLOYMENT_ID=$(date +%Y%m%d%H%M%S)
DEPLOYMENT_ID_STATIC=20250318152205

docker run --name my-nginx-container-two-${DEPLOYMENT_ID} \
  --network haloy-public \
  -l "haloy.appName=my-nginx-container" \
  -l "haloy.deployment-id=${DEPLOYMENT_ID}" \
  -l "haloy.acme.email=test@haloy.dev" \
  -l "haloy.domain.0=domain.com" \
  -l "haloy.domain.0.alias.0=www.domain.com" \
  -l "haloy.health-check-path=/health" \
  -l "haloy.port=80" \
  -l "haloy.role=app" \
  -e "NODE_ENV=production" \
  my-nginx

