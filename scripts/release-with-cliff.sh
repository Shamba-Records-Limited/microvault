#!/bin/bash

# Usage: ./scripts/release-with-cliff.sh v0.1.0-beta.1

cd "$(dirname "$0")/.." || exit 1

VERSION=$1

if [ -z "$VERSION" ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 v0.1.0-beta.1"
    exit 1
fi

# Check if git-cliff is installed
if ! command -v git-cliff &> /dev/null; then
    echo "git-cliff is not installed. Installing..."
    echo "Run: cargo install git-cliff"
    echo "or: npm install -g git-cliff"
    exit 1
fi

# Fetch tags from remote so we can determine the previous release
git fetch --tags --quiet

# Get the previous tag
PREVIOUS_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "")

# Generate changelog for this release only (since last tag)
if [ -z "$PREVIOUS_TAG" ]; then
    # First release - include all commits
    RELEASE_NOTES=$(git-cliff --unreleased --tag "$VERSION" --strip all)
else
    # Generate notes between previous tag and HEAD
    RELEASE_NOTES=$(git-cliff $PREVIOUS_TAG..HEAD --tag "$VERSION" --strip all)
fi

echo "Creating GitHub release for $VERSION..."
echo ""
echo "Release notes:"
echo "$RELEASE_NOTES"
echo ""

# Determine if this is a prerelease
PRERELEASE_FLAG=""
if [[ $VERSION == *"beta"* ]] || [[ $VERSION == *"alpha"* ]] || [[ $VERSION == *"rc"* ]]; then
    PRERELEASE_FLAG="--prerelease"
fi

# Create the release
gh release create "$VERSION" \
    --title "$VERSION" \
    --notes "$RELEASE_NOTES" \
    $PRERELEASE_FLAG

echo "✅ Release created successfully!"
