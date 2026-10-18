#!/bin/sh
# new-api 容器入口：以 root 启动时交接运行时目录，再用 su-exec 降权运行。
#
# 不直接写死 USER 的原因：/data 与日志目录是宿主机绑定挂载，镜像构建期的
# chown 会被挂载遮蔽。Linux 上宿主机目录通常属于 root，写死 USER 会让既有
# 部署在升级后无法写 SQLite 与日志。这里在启动时处理属主，处理完立即降权，
# 服务进程本身始终是非 root。
set -eu

APP_UID=1000
APP_GID=1000

# 显式放弃降权（例如只读文件系统或 NFS root_squash 无法 chown 的部署）。
# 这是显式开关，不是静默降级：开启后容器内进程就是 root，状态可从 ps 看到。
if [ "${NEW_API_RUN_AS_ROOT:-0}" = "1" ]; then
    exec /new-api "$@"
fi

# 编排里已经指定了 user: 时，容器本来就不是 root，无需也无法降权。
if [ "$(id -u)" != "0" ]; then
    exec /new-api "$@"
fi

# 需要写入的目录：工作目录（SQLite 与 pprof 落在这里）以及 --log-dir 指定的
# 日志目录。默认值与 compose 里的 --log-dir /app/logs 一致。
dirs="/data /app/logs"
prev=""
for arg in "$@"; do
    if [ "$prev" = "--log-dir" ]; then
        dirs="$dirs $arg"
    fi
    case "$arg" in
        --log-dir=*) dirs="$dirs ${arg#--log-dir=}" ;;
    esac
    prev="$arg"
done

# 判定标准是"目标用户能否真的写入"，而不是"属主看起来对不对"。
# Docker Desktop 的 virtiofs 会把挂载目录在容器内一律显示为 0:0，按属主判断
# 会让每次启动都跑一遍 chown -R 却什么也改不了。
can_write_as_app_user() {
    # shellcheck disable=SC2016 # 探测命令里的 $1/$$ 必须由内层 sh 展开
    su-exec "$APP_UID:$APP_GID" sh -c 'p="$1/.new-api-write-probe.$$"; : >"$p" 2>/dev/null && rm -f "$p"' sh "$1" 2>/dev/null
}

# 去重后再处理：--log-dir /app/logs 会让同一目录出现两次，
# 重复的探测与告警会让人以为有两个目录出了问题。
seen=""
for dir in $dirs; do
    case " $seen " in
        *" $dir "*) continue ;;
    esac
    seen="$seen $dir"
    if [ ! -d "$dir" ]; then
        if ! mkdir -p "$dir" 2>/dev/null; then
            echo "new-api: 无法创建运行时目录 $dir" >&2
            exit 1
        fi
    fi
    if can_write_as_app_user "$dir"; then
        continue
    fi
    if ! chown -R "$APP_UID:$APP_GID" "$dir" 2>/dev/null; then
        echo "new-api: 无法把 $dir 的属主改为 $APP_UID:$APP_GID；若该目录是只读挂载或 NFS root_squash，请设置 NEW_API_RUN_AS_ROOT=1" >&2
    elif ! can_write_as_app_user "$dir"; then
        # 不静默吞掉：属主改了仍写不进去，进程随后会失败并留下明确的日志，
        # 但先在这里说清楚原因，避免升级后只看到 "failed to open log file"。
        echo "new-api: 目录 $dir 在 chown 后仍不可写（可能是挂载选项或 ACL 限制），请设置 NEW_API_RUN_AS_ROOT=1" >&2
    fi
done

exec su-exec "$APP_UID:$APP_GID" /new-api "$@"
