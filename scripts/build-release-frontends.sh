#!/usr/bin/env bash
set -euo pipefail

version="${1:?usage: build-release-frontends.sh <version>}"
build_commit="${BUILD_COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
build_date="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
frontends=(
  web/default
  web/classic
)

build_frontend() {
  local frontend_dir="$1"
  local build_version="${2:?missing version for $frontend_dir}"

  if [ ! -d "$frontend_dir" ]; then
    echo "Missing directory: $frontend_dir" >&2
    exit 1
  fi

  echo "==> Building frontend: $frontend_dir"
  (
    cd "$frontend_dir"
    bun install --frozen-lockfile
    DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION="$build_version" bun run build
  )
  # 门禁先于 metadata：与 Dockerfile 一致。产物不合格时不写发布元数据，
  # 避免在中止的构建里留下看似有效的 release 描述。frontend_dir 即 CHECKS
  # 里的 label（web/default、web/classic），dist 路径用脚本内默认值。
  bun scripts/frontend-bundle-versions.mjs --only "$frontend_dir"
  sh scripts/write-frontend-release-metadata.sh \
    "${frontend_dir#web/}" \
    "$frontend_dir/dist" \
    "$build_version" \
    "$build_commit" \
    "$build_date"
}

for frontend in "${frontends[@]}"; do
  build_frontend "$frontend" "$version"
done

echo "All frontends built successfully for $version: ${frontends[*]}"
