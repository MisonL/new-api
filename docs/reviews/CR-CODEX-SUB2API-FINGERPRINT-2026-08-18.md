# CR-CODEX-SUB2API-FINGERPRINT-2026-08-18

## 范围

本记录核对 new-api 的 Codex CLI 与 Codex Desktop Header Profile，确保其可适配上游原版 Sub2API 的 `codex_cli_only_engine_fingerprint_signals` 请求头匹配功能。正式服务、正式数据库、Redis、Caddy 和生产容器均未修改、重启或重建。

## 上游规则

依据 Sub2API 提交 `1870b58` 的 `engine_fingerprint_signal.go`：

- 默认必选信号是至少一个值非空的 `x-codex-*` 请求头前缀。
- `session-id`/`session_id`、`thread-id`/`thread_id` 和 `client_metadata.x-codex-window-id`/`client_metadata.x-codex-installation-id` 只是可选预填信号。
- 必选信号之间是 AND，同一信号的 `match` 项之间是 OR。
- 请求体中的 `client_metadata` 不能由 Header Profile 伪造或补写。

## 实现

- 后端内置 `codex-cli` 与 `codex-desktop` Profile 标记为 `PassthroughRequired`，静态模板只保留有证据的 `User-Agent` 和 `Originator`，不包含任何静态 `x-codex-*` 值。
- default/classic 前端在选择 Codex Profile 时，自动把动态请求头名称合并进无条件 `pass_headers`；编辑旧渠道提交时也会补齐该预设，同时保留已有的其他 `param_override.operations`。
- 动态清单包含 `Originator`、session/thread 标识、`X-Codex-*` 信号和 `X-Client-Request-Id`。透传操作只保存名称，运行时值必须来自当前真实 Codex 请求。
- 后端保存校验只要求 Codex Profile 的无条件 `pass_headers` 至少包含一个 `x-codex-*` 名称；运行时继续核对真实请求头值是否原样透传。
- 未加入不存在于上游匹配规则的 `X-Codex-Compatibility`，也未合成请求体中的 Codex metadata。

## 验证

| 检查 | 结果 |
| --- | --- |
| `go test ./dto ./controller ./relay/common ./relay/channel` | 退出码 0 |
| `bun test tests/channel-header-profile-strategy.test.ts` | 30 pass |
| `node --test src/components/table/channels/modals/headerProfile.helpers.test.js` | 106 pass |
| `bun run typecheck` | 退出码 0 |
| `bun run lint` | 退出码 0，5 条已有的未使用 eslint-disable 警告 |
| `git diff --check` | 退出码 0 |

## 未覆盖边界

本轮只完成源码和隔离/离线验证，没有使用真实上游渠道做生产验收。只有当前请求确实携带非空 `x-codex-*` 动态头时，上游的默认指纹规则才会命中；模板本身不会伪造这些值。正式服务仍需按既有发布审批流程单独验收。
