# macOS Docker TUN Hardening Review

## Scope

本轮修复并复核以下边界：LaunchAgent plist 渲染转义、Caddy 重启失败阈值、watchdog 快照新鲜度、正式部署 dirty worktree 门禁，以及 recovery/monitor/log-retention 安装原子替换。

未启动、重建、重启或升级正式 Docker、Caddy、PostgreSQL、Redis 或生产端口。

## Changes

- XML 转义结果在进入 `sed` replacement 前额外转义反斜杠、`&` 和 `|`；路径包含特殊字符时仍能生成正确 plist。
- Caddy 入口失败触发门槛使用 `NEW_API_RECOVERY_CADDY_RESTART_FAILURE_THRESHOLD`。旧的 `NEW_API_RECOVERY_PUBLIC_FAILURE_THRESHOLD` 仅在专用变量未设置时作为兼容别名。
- monitor 默认只接受最近 600 秒内的合法 UTC watchdog 快照。快照缺失、时间戳非法、来自未来或过期时返回失败，并不继续使用旧探针字段。
- 正式 `--apply` 在镜像 guard 阶段拒绝任何 tracked 或 untracked dirty worktree；`--check-only` 仍可执行只读预检。
- 三个安装器先生成临时脚本和 plist，完成占位符及 plist 校验后再替换活动文件；替换或 LaunchAgent 操作失败时恢复旧安装。

## Verification

以下命令均在当前开发工作区执行并退出码为 `0`：

```text
bash -n scripts/install-macos-docker-tun-recovery.sh scripts/install-macos-docker-tun-monitor.sh scripts/install-macos-docker-tun-log-retention.sh scripts/macos-docker-tun-recovery-watchdog.sh scripts/macos-docker-tun-monitor.sh scripts/deploy-macos-docker-tun.sh scripts/macos-docker-tun-recovery-watchdog.test.sh scripts/macos-docker-tun-monitor.test.sh scripts/install-macos-docker-tun-recovery.test.sh scripts/install-macos-docker-tun-monitor.test.sh
shellcheck scripts/install-macos-docker-tun-recovery.sh scripts/install-macos-docker-tun-monitor.sh scripts/install-macos-docker-tun-log-retention.sh scripts/macos-docker-tun-recovery-watchdog.sh scripts/macos-docker-tun-monitor.sh scripts/deploy-macos-docker-tun.sh scripts/macos-docker-tun-recovery-watchdog.test.sh scripts/macos-docker-tun-monitor.test.sh scripts/install-macos-docker-tun-recovery.test.sh scripts/install-macos-docker-tun-monitor.test.sh
bash scripts/macos-docker-tun-recovery-watchdog.test.sh
bash scripts/macos-docker-tun-monitor.test.sh
bash scripts/macos-docker-tun-log-retention.test.sh
bash scripts/install-macos-docker-tun-recovery.test.sh
bash scripts/install-macos-docker-tun-monitor.test.sh
python3 scripts/macos-docker-tun-caddy-log-report.test.py
python3 scripts/macos-docker-tun-monitor-alert.test.py
python3 scripts/macos-docker-tun-port-audit.test.py
python3 scripts/macos-docker-tun-time-machine-report.test.py
python3 scripts/compose-healthcheck-contract.py
git diff --check
```

新增回归覆盖：特殊字符 plist 路径、渲染失败时旧脚本/plist 保持不变、Caddy 专用阈值延迟重启、陈旧或非法 watchdog 快照 fail-closed。

## Boundary

本轮未执行真实 macOS launchd、Caddy 端口切换或正式 Compose apply；这些行为仍需在独立维护窗口按既有发布门禁验收。
