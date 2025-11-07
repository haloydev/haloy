#!/usr/bin/env bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

CONSTANTS_FILE="$SCRIPT_DIR/../internal/constants/constants.go"

version=$(awk -F'"' '/Version.*=.*"/ {print $2; exit}' "$CONSTANTS_FILE" 2>/dev/null)

# Fallback if version not found
if [ -z "$version" ]; then
    echo "Warning: Could not extract version from constants.go, using 'dev'" >&2
    version="dev"
fi

echo "$version"
