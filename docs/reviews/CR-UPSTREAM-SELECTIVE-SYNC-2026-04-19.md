# 上游剩余项选择性吸纳复核（2026-04-19）

## 结论

本轮在 `chore/selective-upstream-sync-2026-04-19` 分支中，已吸纳对本仓库影响最大的剩余上游改动：

- 管理日志隐私修复
- 充值日志审计字段补全
- 旧充值日志展开兼容
- `NODE_NAME` 节点标识支持
- Compose 默认 Redis 鉴权模板加固

## 已吸纳项

- `c31343ac7` 管理日志不再向普通用户泄露管理员身份
- `209d90e86` 充值日志增加管理员可见审计信息
- `6ff8c7ab0` 旧版充值日志保留可展开并提示升级
- `209645e26` 审计日志支持 `NODE_NAME`
- `d75a04679` Compose 默认 Redis 模板增加密码

说明：

- 吸纳时做了本仓库口径适配，而不是机械照搬上游 patch。
- Redis 默认密码改为通过环境变量统一驱动，避免硬编码在多处散落。
- 开发隔离 Compose 也同步采用同一口径，避免正式与开发模板分叉。

## 已处理但不会在 `git cherry` 中显示为等价的项

- `3cad6b9d7` Claude 空字符串内容转换问题

说明：

- 本仓库已通过手工合并方式提前吸纳该修复。
- 因 patch 形态不同，`git cherry` 仍会显示 `+`，但语义已覆盖。

## 明确保留不吸纳项

- `dd57eeb51` `pgx` 升级

原因：

- 本仓库当前 `go.mod` 已使用更高版本 `github.com/jackc/pgx/v5 v5.9.1`，不应回退到上游较低版本口径。

- `f995a868e` `waffo-pay` 大功能合并

原因：

- 该提交属于支付能力扩展，不是安全/稳定性修复。
- 影响范围横跨后端支付链路、设置页、前端充值流程、测试与配置项。
- 在当前“剩余高影响项收口”任务中直接并入，风险高于收益。
- 如要吸纳，应单独开专题分支做全链路验证，而不是在本轮混入。

## 本轮验证

- `go test ./...`
- `cd web && bun run build`
- `SESSION_SECRET=test-session CRYPTO_SECRET=test-crypto docker compose config`
- `DEV_SESSION_SECRET=test-session DEV_CRYPTO_SECRET=test-crypto DEV_NEW_API_DATA_DIR=/tmp/newapi-dev-data DEV_NEW_API_LOG_DIR=/tmp/newapi-dev-logs DEV_POSTGRES_DATA_DIR=/tmp/newapi-dev-pg docker compose -f deploy/compose/dev-isolated.yml config`

## 风险结论

- 当前剩余未吸纳项中，没有比本轮已吸纳项更高优先级的安全或稳定性缺口。
- `waffo-pay` 属于可选能力增强，应独立推进，不应作为本轮“必须补齐项”强行并入。
