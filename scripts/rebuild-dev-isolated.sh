#!/usr/bin/env bash
# 重建隔离开发环境并验证容器运行的代码与 Git HEAD 一致。
#
# 用法：scripts/rebuild-dev-isolated.sh [镜像标签]
# 默认标签 new-api-local:dev。
#
# 步骤：
#   1. 构建 Docker 镜像（失败立即退出，不重建容器）。
#   2. 强制重建 dev-isolated 的 new-api 容器。
#   3. 比对容器 --build-info 的 commit 与 git rev-parse HEAD（-dirty 后缀可接受）。
#   4. curl /api/status 确认服务健康。
set -euo pipefail

cd "$(dirname "$0")/.."

IMAGE="${1:-new-api-local:dev}"
COMPOSE_FILE="deploy/compose/dev-isolated.yml"
ENV_FILE="deploy/env/dev-isolated.env"
CONTAINER="new-api-dev-isolated-new-api-1"

echo "==> 构建镜像 ${IMAGE}"
scripts/build-docker-local.sh "${IMAGE}"

echo "==> 重建容器"
docker compose -f "${COMPOSE_FILE}" --env-file "${ENV_FILE}" up -d --no-deps --force-recreate new-api

echo "==> 等待容器就绪"
for i in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:3001/api/status >/dev/null 2>&1; then
    break
  fi
  if [ "$i" = "30" ]; then
    echo "错误：容器 30 次探测后仍未就绪" >&2
    exit 1
  fi
  sleep 2
done

GIT_HEAD="$(git rev-parse HEAD)"
CONTAINER_COMMIT="$(docker exec "${CONTAINER}" /new-api --build-info | sed -n 's/^commit=//p' | sed 's/-dirty$//')"

echo "git HEAD:            ${GIT_HEAD}"
echo "container build-info commit: ${CONTAINER_COMMIT}"

if [ -z "${CONTAINER_COMMIT}" ]; then
  echo "错误：无法读取容器 build-info" >&2
  exit 1
fi

if [ "${GIT_HEAD}" != "${CONTAINER_COMMIT}" ]; then
  echo "错误：容器运行的代码与 Git HEAD 不一致（healthy 不等于代码最新）" >&2
  exit 1
fi

echo "==> 代码一致性确认通过"
curl -fsS http://127.0.0.1:3001/api/status | head -c 200
echo
echo "完成：${IMAGE} 已重建并确认与 HEAD 一致。"
