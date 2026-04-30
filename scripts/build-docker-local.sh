#!/usr/bin/env sh
set -eu

image="${1:-${NEW_API_IMAGE:-new-api-local:prod-main}}"
version="$(tr -d '\n\r' < VERSION)"
commit="$(git rev-parse HEAD)"
if ! git diff --quiet || ! git diff --cached --quiet || [ -n "$(git ls-files --others --exclude-standard)" ]; then
  commit="${commit}-dirty"
fi
build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
source_url="${SOURCE_URL:-https://github.com/MisonL/new-api}"

docker build \
  --build-arg APP_VERSION="$version" \
  --build-arg VCS_REF="$commit" \
  --build-arg BUILD_DATE="$build_date" \
  --build-arg SOURCE_URL="$source_url" \
  -t "$image" \
  .

echo "built image=$image version=$version commit=$commit date=$build_date"
