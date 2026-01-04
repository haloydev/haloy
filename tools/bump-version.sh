#!/usr/bin/env bash
#
# bump-version.sh - Bump the version of haloy and create a release tag
#
# Usage: ./tools/bump-version.sh <new-version>
#
# Examples:
#   ./tools/bump-version.sh 0.1.0-beta.14
#   ./tools/bump-version.sh 1.0.0
#   ./tools/bump-version.sh 2.0.0-rc.1
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# File paths
CONSTANTS_FILE="internal/constants/constants.go"

# Semver regex pattern (supports X.Y.Z and X.Y.Z-prerelease.N)
SEMVER_REGEX='^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9]+(\.[a-zA-Z0-9]+)*)?$'

# Print colored output
info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

success() {
    echo -e "${GREEN}[OK]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

# Show usage information
usage() {
    echo "Usage: $0 <new-version>"
    echo ""
    echo "Bump the version of haloy and create a release tag."
    echo ""
    echo "Arguments:"
    echo "  new-version    The new version (semver format)"
    echo ""
    echo "Examples:"
    echo "  $0 0.1.0-beta.14"
    echo "  $0 1.0.0"
    echo "  $0 2.0.0-rc.1"
    exit 1
}

# Validate semver format
validate_version() {
    local version=$1
    if [[ ! $version =~ $SEMVER_REGEX ]]; then
        error "Invalid version format: $version"
        echo ""
        echo "Version must be valid semver format:"
        echo "  - X.Y.Z (e.g., 1.0.0)"
        echo "  - X.Y.Z-prerelease (e.g., 0.1.0-beta.14, 2.0.0-rc.1)"
        exit 1
    fi
}

# Get current version from constants.go
get_current_version() {
    grep -E '^\s+Version\s+=' "$CONSTANTS_FILE" | head -1 | sed -E 's/.*"(.*)".*/\1/'
}

# Main script
main() {
    # Check for argument
    if [[ $# -lt 1 ]]; then
        error "Missing required argument: new-version"
        echo ""
        usage
    fi

    local new_version=$1

    # Validate version format
    validate_version "$new_version"

    # Ensure we're in the repo root
    if [[ ! -f "$CONSTANTS_FILE" ]]; then
        error "Must be run from the repository root (cannot find $CONSTANTS_FILE)"
        exit 1
    fi

    # Check we're in a git repo
    if ! git rev-parse --is-inside-work-tree > /dev/null 2>&1; then
        error "Not inside a git repository"
        exit 1
    fi

    # Check we're on main branch
    local current_branch
    current_branch=$(git branch --show-current)
    if [[ "$current_branch" != "main" ]]; then
        error "Must be on 'main' branch (currently on '$current_branch')"
        exit 1
    fi

    # Check for uncommitted changes
    if ! git diff-index --quiet HEAD --; then
        error "Working directory has uncommitted changes. Please commit or stash them first."
        exit 1
    fi

    # Check for untracked files in the constants directory
    if [[ -n $(git ls-files --others --exclude-standard internal/constants/) ]]; then
        error "There are untracked files in internal/constants/. Please commit or remove them first."
        exit 1
    fi

    # Get current version
    local current_version
    current_version=$(get_current_version)

    # Check if tag already exists
    if git tag -l "v$new_version" | grep -q "v$new_version"; then
        error "Tag v$new_version already exists"
        exit 1
    fi

    # Display what will happen
    echo ""
    echo "=========================================="
    echo "  Version Bump Summary"
    echo "=========================================="
    echo ""
    info "Current version: ${YELLOW}$current_version${NC}"
    info "New version:     ${GREEN}$new_version${NC}"
    info "Tag to create:   ${GREEN}v$new_version${NC}"
    info "Branch:          ${GREEN}$current_branch${NC}"
    echo ""
    echo "The following actions will be performed:"
    echo "  1. Run tests (make ci-test)"
    echo "  2. Update version in $CONSTANTS_FILE"
    echo "  3. git add $CONSTANTS_FILE"
    echo "  4. git commit -m \"chore: bump version to $new_version\""
    echo "  5. git push"
    echo "  6. git tag v$new_version"
    echo "  7. git push origin v$new_version"
    echo ""

    # Ask for confirmation
    read -rp "Proceed? [y/N] " confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        info "Aborted by user"
        exit 0
    fi

    echo ""

    # Step 1: Run tests
    info "Running tests (make ci-test)..."
    if ! make ci-test; then
        error "Tests failed. Aborting version bump."
        exit 1
    fi
    success "Tests passed"
    echo ""

    # Step 2: Update version in constants.go
    info "Updating version in $CONSTANTS_FILE..."
    sed -i.bak -E "s/(Version[[:space:]]+=[[:space:]]+\").*(\")/\1$new_version\2/" "$CONSTANTS_FILE"
    rm -f "${CONSTANTS_FILE}.bak"
    success "Version updated"

    # Step 3: Stage the file
    info "Staging changes..."
    git add "$CONSTANTS_FILE"
    success "Changes staged"

    # Step 4: Commit
    info "Creating commit..."
    git commit -m "chore: bump version to $new_version"
    success "Commit created"

    # Step 5: Push commit
    info "Pushing commit..."
    git push
    success "Commit pushed"

    # Step 6: Create tag
    info "Creating tag v$new_version..."
    git tag "v$new_version"
    success "Tag created"

    # Step 7: Push tag
    info "Pushing tag..."
    git push origin "v$new_version"
    success "Tag pushed"

    echo ""
    echo "=========================================="
    success "Version bumped to $new_version"
    echo "=========================================="
    echo ""
    info "Tag v$new_version has been pushed and should trigger the release workflow."
}

main "$@"
