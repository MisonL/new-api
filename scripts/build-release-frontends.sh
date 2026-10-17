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

# 产物版本门禁：bun audit 只证明 lockfile 干净，证明不了产物里打包的是哪个版本
# （overrides 不清理嵌套副本，手工 vendor 的预构建产物完全不在依赖树里）。
# 发布物与镜像都经此脚本构建，所以检查放在这里，而不是只放在 PR 门禁里。
# 用 bun 而不是 node：调用方（release.yml）不安装 node，依赖 runner 预装版本会
# 让门禁行为随 runner 镜像漂移；bun 由 .bun-version 钉死，与 Dockerfile 同源。
bun scripts/frontend-bundle-versions.mjs

echo "All frontends built successfully for $version: ${frontends[*]}"
