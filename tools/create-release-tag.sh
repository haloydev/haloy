#!/usr/bin/env bash
#
# create-release-tag.sh - Validate the repo state and push a release tag
#
# Usage: ./tools/create-release-tag.sh <version-or-tag>
#        ./tools/create-release-tag.sh --next [channel]
#
# Examples:
#   ./tools/create-release-tag.sh v0.1.0-beta.43
#   ./tools/create-release-tag.sh 0.1.0
#   ./tools/create-release-tag.sh 2.0.0-rc.1
#   ./tools/create-release-tag.sh --next
#   ./tools/create-release-tag.sh --next beta
#

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

SEMVER_REGEX='^v?[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9]+(\.[a-zA-Z0-9]+)*)?$'
CHANNEL_REGEX='^[a-zA-Z0-9-]+(\.[a-zA-Z0-9-]+)*$'

info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

success() {
    echo -e "${GREEN}[OK]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

usage() {
    echo "Usage: $0 <version-or-tag>"
    echo "       $0 --next [channel]"
    echo ""
    echo "Create and push a release tag without editing tracked files."
    echo ""
    echo "Arguments:"
    echo "  version-or-tag  Release version like 0.1.0 or tag like v0.1.0-beta.43"
    echo "  --next [channel]  Increment the latest prerelease tag, optionally for a specific channel"
    exit 1
}

normalize_tag() {
    local input=$1
    if [[ ! $input =~ $SEMVER_REGEX ]]; then
        error "Invalid version format: $input"
        echo ""
        echo "Expected semver, with or without a leading v:"
        echo "  - 1.0.0"
        echo "  - v1.0.0"
        echo "  - 0.1.0-beta.43"
        echo "  - v2.0.0-rc.1"
        exit 1
    fi

    if [[ $input == v* ]]; then
        echo "$input"
    else
        echo "v$input"
    fi
}

validate_channel() {
    local channel=$1
    if [[ ! $channel =~ $CHANNEL_REGEX ]]; then
        error "Invalid prerelease channel: $channel"
        echo ""
        echo "Expected a semver prerelease identifier such as:"
        echo "  - beta"
        echo "  - rc"
        echo "  - alpha.preview"
        exit 1
    fi
}

fetch_remote_tags() {
    if git remote get-url origin > /dev/null 2>&1; then
        git fetch --tags origin --quiet
    fi
}

latest_tag() {
    git tag --sort=-version:refname | head -n 1
}

find_latest_prerelease_tag() {
    local channel=${1:-}

    if [[ -n $channel ]]; then
        git tag --sort=-version:refname | grep -E "^v[0-9]+\.[0-9]+\.[0-9]+-${channel//./\\.}\.[0-9]+$" | head -n 1
        return
    fi

    git tag --sort=-version:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+-.+\.[0-9]+$' | head -n 1
}

increment_prerelease_tag() {
    local input=$1

    if [[ ! $input =~ ^v([0-9]+\.[0-9]+\.[0-9]+)-(.+)\.([0-9]+)$ ]]; then
        error "Cannot auto-increment non-prerelease tag: $input"
        echo ""
        echo "Use an explicit tag like:"
        echo "  $0 v1.2.3-beta.1"
        exit 1
    fi

    local base_version=${BASH_REMATCH[1]}
    local channel=${BASH_REMATCH[2]}
    local prerelease_number=${BASH_REMATCH[3]}

    echo "v${base_version}-${channel}.$((prerelease_number + 1))"
}

resolve_version_tag() {
    local mode=$1
    local value=${2:-}

    case "$mode" in
        explicit)
            normalize_tag "$value"
            ;;
        next)
            local latest_prerelease
            latest_prerelease=$(find_latest_prerelease_tag "$value")
            if [[ -z $latest_prerelease ]]; then
                if [[ -n $value ]]; then
                    error "No existing prerelease tags found for channel '$value'"
                    echo ""
                    echo "Create the first tag in that series explicitly, for example:"
                    echo "  $0 v1.2.3-$value.1"
                else
                    error "No existing prerelease tags found"
                    echo ""
                    echo "Create the first prerelease tag explicitly, for example:"
                    echo "  $0 v1.2.3-beta.1"
                fi
                exit 1
            fi

            increment_prerelease_tag "$latest_prerelease"
            ;;
    esac
}

main() {
    local mode=""
    local explicit_version=""
    local next_channel=""

    if [[ $# -lt 1 ]]; then
        error "Missing required argument"
        echo ""
        usage
    fi

    case "$1" in
        --next)
            mode="next"
            if [[ $# -gt 2 ]]; then
                error "Too many arguments"
                echo ""
                usage
            fi
            if [[ $# -eq 2 ]]; then
                next_channel=$2
                validate_channel "$next_channel"
            fi
            ;;
        -h|--help)
            usage
            ;;
        *)
            mode="explicit"
            if [[ $# -ne 1 ]]; then
                error "Too many arguments"
                echo ""
                usage
            fi
            explicit_version=$1
            ;;
    esac

    if ! git rev-parse --is-inside-work-tree > /dev/null 2>&1; then
        error "Not inside a git repository"
        exit 1
    fi

    local repo_root
    repo_root=$(git rev-parse --show-toplevel)
    cd "$repo_root"

    fetch_remote_tags

    local current_branch
    current_branch=$(git branch --show-current)
    if [[ "$current_branch" != "main" ]]; then
        error "Must be on 'main' branch (currently on '$current_branch')"
        exit 1
    fi

    if [[ -n $(git status --short) ]]; then
        error "Working tree is not clean. Commit, stash, or remove local changes before tagging."
        exit 1
    fi

    local current_tag
    current_tag=$(latest_tag)

    local version_tag
    version_tag=$(resolve_version_tag "$mode" "${next_channel:-$explicit_version}")

    if git rev-parse "$version_tag" > /dev/null 2>&1; then
        error "Tag $version_tag already exists"
        exit 1
    fi

    echo ""
    echo "=========================================="
    echo "  Release Tag Summary"
    echo "=========================================="
    echo ""
    info "Current latest tag:   ${YELLOW}${current_tag:-none}${NC}"
    info "New release tag:      ${GREEN}$version_tag${NC}"
    info "Branch:               ${GREEN}$current_branch${NC}"
    echo ""
    echo "The following actions will be performed:"
    echo "  1. Run tests (make ci-test)"
    echo "  2. Create annotated tag $version_tag on HEAD"
    echo "  3. git push origin $version_tag"
    echo ""

    read -rp "Proceed? [y/N] " confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        info "Aborted by user"
        exit 0
    fi

    echo ""
    info "Running tests (make ci-test)..."
    make ci-test
    success "Tests passed"
    echo ""

    info "Creating annotated tag $version_tag..."
    git tag -a "$version_tag" -m "Release $version_tag"
    success "Tag created"

    echo ""
    info "Pushing tag..."
    git push origin "$version_tag"
    success "Tag pushed"

    echo ""
    echo "=========================================="
    success "Release tag pushed: $version_tag"
    echo "=========================================="
    echo ""
    info "GitHub Actions should now build and publish the release from $version_tag."
}

main "$@"
