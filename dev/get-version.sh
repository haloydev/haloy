#!/usr/bin/env bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$SCRIPT_DIR/.."

version=$(git -C "$REPO_ROOT" describe --tags --dirty --always --match 'v*' 2>/dev/null || true)
if [ -z "$version" ]; then
    version="dev"
fi

echo "$version"
