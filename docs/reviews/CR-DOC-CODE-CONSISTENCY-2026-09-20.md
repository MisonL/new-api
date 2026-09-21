# 文档与代码一致性修复审计

采样时间：2026-09-20（Asia/Shanghai）
仓库：`/Volumes/Work/code/new-api`，分支 `dev`，`HEAD=5cda68077`。

本轮对全仓文档与运维脚本做一致性复核，逐条对照代码、脚本行为和运行态，修复以下不一致。
所有结论均由实际命令输出或代码引用支持，未采用推断。

## 1. 运维脚本拒绝局域网绑定（已修复）

问题：`scripts/deploy-macos-docker-tun.sh` 把后端发布地址硬编码为 loopback，
`PORT_MAPPING` 固定为 `127.0.0.1:${LOOPBACK_PORT}:${CONTAINER_PORT}`，并要求
`NEW_API_PORT_MAPPING` 与之完全相等。生产 `.env` 使用 `0.0.0.0:13000:3000`
（局域网访问为明确需求），因此预检直接失败：

```text
[macos-tun] ERROR: NEW_API_PORT_MAPPING=0.0.0.0:13000:3000 does not match derived 127.0.0.1:13000:3000
```

修复：

- 新增 `NEW_API_BIND_ADDRESS`，取值限定为 `127.0.0.1`、`0.0.0.0`、`::1`、`::`。
- 未显式设置时，从 `NEW_API_PORT_MAPPING` 的主机部分推导，因此现有生产 `.env`
  无需改动即可工作；无映射时回退到 `127.0.0.1`。
- TUN Compose 渲染校验与运行态端口占用检查改为比对 `BIND_ADDRESS`，不再固定 `127.0.0.1`。
- `NEW_API_BIND_ADDRESS` 加入 TUN env 允许键白名单。

验证（三种配置实测）：

| 配置 | 结果 |
| --- | --- |
| `NEW_API_BIND_ADDRESS=127.0.0.1` | 端口校验通过 |
| `NEW_API_BIND_ADDRESS=0.0.0.0` | 端口校验通过 |
| `NEW_API_BIND_ADDRESS=192.168.1.5` | 拒绝：`must be 127.0.0.1, 0.0.0.0, ::1, or ::` |

`--check-only` 现停在镜像 provenance 门禁（`existing new-api image revision is missing or not a clean commit`）。
该门禁在修复前即存在，要求正式镜像 revision 为 40 位干净 commit；当前正式镜像带
`-litefix2` 后缀，因此被拒属预期行为，且只在 `--apply` 时生效，不影响日常 Compose 操作。

## 2. `AGENTS.md` 前端技术栈与实际不符（已修复）

原文写“前端：React 18、Vite、Semi Design”。实测：

- `web/default/package.json`：`react ^19.2.4`、`@rsbuild/core ^2.0.1`，构建命令为 `rsbuild dev` / `rsbuild build`。
- `web/classic/package.json`：`react ^18.2.0`、`vite ^6.4.3`，构建命令为 `vite`。

已改为分别描述两个前端，避免把 `web/default` 的构建工具写成 Vite。

## 3. `AGENTS.md` 只读联调命令无效（已修复）

原文给出：

```bash
VITE_DEV_PROXY_TARGET=http://127.0.0.1:3300 \
VITE_REACT_APP_READONLY_MODE=true \
bun run dev --host 0.0.0.0 --port 5173
```

全仓检索确认这两个变量没有任何消费者。`web/default/rsbuild.config.ts` 的 devProxy
target 来自 `VITE_REACT_APP_SERVER_URL`（`process.env` 或 `loadEnv` 的 `VITE_` 前缀）。
只读代理是监听 `3300` 的 nginx（`deploy/compose/frontend-readonly-proxy.yml`），
因此正确写法是把该变量指向代理：

```bash
VITE_REACT_APP_SERVER_URL=http://127.0.0.1:3300 \
bun run dev --host 0.0.0.0 --port 5173
```

已按此修正。

## 4. 未维护价格模型的兜底口径写错（已修复）

`docs/operations/pricing-maintenance.md` 原称未匹配 `ModelRatio` 的模型按
`ModelRatio=37.5`、`CompletionRatio=1`、`CacheRatio=1`、`CreateCacheRatio=1.25` 计费。
实测只有 `ModelRatio`、`CacheRatio`、`CreateCacheRatio` 是固定值：

- `GetModelRatio`（`setting/ratio_setting/model_ratio.go`）未命中时返回 `37.5`。
- `GetCacheRatio` / `GetCreateCacheRatio`（`setting/ratio_setting/cache_ratio.go`）返回 `1` / `1.25`。
- `CompletionRatio` 走 `getHardcodedCompletionModelRatio`，按模型名前缀返回不同值：
  `gpt-5.x` 为 `6`、无点号 `gpt-5*` 为 `8`、`gpt-4o` 为 `4`、`gpt-4`/`gpt-4.5-preview` 为 `2`、
  `o1`/`o3` 前缀为 `4`、`claude-3`/`claude-sonnet-4`/`claude-opus-4`/`claude-haiku-4` 为 `5`。

已把该段改为逐项列表并标注代码位置，明确 `CompletionRatio` 不是固定值，
核对费用时必须读取实际生效值。

## 5. `CHANGELOG.md` 声称与仓库不符（已修复）

原文声称“README 与渠道配置说明补充…`pass_headers` 白名单，以及 OpenCode…”。
实测 `docs/channel/other_setting.md` 确实包含（`Header Profile` 7 处、`pass_headers` 6 处、
`OpenCode` 1 处），但 `README.md` 中 `pass_headers` 与 `OpenCode` 均为 0 处，
且 `desktop/` 下不存在任何 README。已把声称限定为渠道配置说明，并移除对桌面端 README 的表述。

## 6. 失效文档引用（已修复）

`docs/reviews/archive/CR-MAIN-PAYMENT-SECURITY-GAP-2026-04-19.md` 引用 `model/payment_method_guard.go`，
该文件当前不存在，且 `git log --all` 无记录。同段的
`model/subscription_payment_guard.go` 存在并提供
`CompleteSubscriptionOrderWithPaymentMethod` 与 `ExpireSubscriptionOrderWithPaymentMethod`。
已改为指向现存文件并注明原文件职责已并入。

## 7. 验证记录

```text
bash scripts/install-macos-docker-tun-monitor.test.sh          PASS
bash scripts/install-macos-docker-tun-recovery.test.sh         PASS
bash scripts/macos-docker-tun-monitor.test.sh                  PASS
bash scripts/macos-docker-tun-recovery-watchdog.test.sh        PASS
bash -n scripts/deploy-macos-docker-tun.sh                     exit 0
git diff --check                                               exit 0
```

## 8. 未覆盖边界

- 未执行 `--apply`；镜像 provenance 门禁要求干净 commit 的正式镜像，当前正式镜像不满足，
  因此本模式的实际切换路径未在本轮验证。
- 未在真实局域网客户端上验证 `13000` 直连；`0.0.0.0` 绑定仅由 Compose 渲染与脚本校验确认。
- 修改的是本地被 gitignore 的 `deploy/env/macos-docker-tun.env` 与 `.env`，不入库。
