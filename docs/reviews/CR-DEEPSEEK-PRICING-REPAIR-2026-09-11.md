# DeepSeek V4.1-Flash pricing and historical repair review

采样时间：2026-09-11 01:22-01:43（Asia/Shanghai）。本次范围是 DeepSeek
Flash、Flash legacy aliases、Flash vision alias 和 V4 Pro 的 token 计费。

## Official pricing basis

- https://api-docs.deepseek.com/quick_start/pricing
- https://api-docs.deepseek.com/news/news260910
- https://api-docs.deepseek.com/guides/thinking_mode

当前官方页面确认：

- `deepseek-flash` 实际版本为 DeepSeek-V4.1-Flash；legacy Flash names 继续接受并按 Flash 价格计费。
- Flash off-peak 为 input miss `$0.15`、cache hit `$0.003`、output `$0.60` per 1M tokens；peak 为 `$0.30`、`$0.006`、`$1.20`。
- peak 仅为工作日 UTC `01:00-04:00` 和 `06:00-10:00`。
- `deepseek-v4-pro` 在 2026-09-14 04:00 UTC 前按 Pro 阶梯价格，之后路由到 Flash 并按 Flash 价格计费。
- 官方未发布 cache write 单价，因此没有维护 `CreateCacheRatio`。

## Source changes

- `setting/billing_setting/tiered_billing.go` 增加 Flash 当前阶梯、Pro 路由边界和 timestamp-aware historical accessor。
- `bin/repair_channel_quota_logs.go` 识别当前表达式、旧 current 表达式，以及正式服务曾写入的 318/321/370/373 字符 month/day 快照。
- `bin/repair_channel_quota_logs_test.go` 覆盖旧快照在周末不应计 peak、Flash rollout 后重算和 Pro 路由边界。
- `setting/billing_setting/tiered_billing_test.go` 固定四种旧快照的持久化长度，防止以后静默跳过。

## Verification

| Command or check | Result |
| --- | --- |
| `go test -count=1 ./bin ./setting/reasoning ./setting/ratio_setting ./setting/billing_setting ./pkg/billingexpr ./pkg/pricingrepair` | exit 0 |
| `git diff --check` on the four pricing/repair files | exit 0 |
| `scripts/build-docker-local.sh new-api-local:dev` | exit 0 |
| isolated `new-api-dev-isolated-new-api-1` recreate, healthcheck and `/new-api --build-info` | healthy; commit `21bd76afd7429486668fdeaf1ac8c628300c6359-dirty` |
| production `nc -z -w 3 127.0.0.1 13000` | succeeded |
| production `/api/status` through `newapi` `api.js` | `success=true` |
| production `/api/pricing` through `newapi` `api.js` | Flash `0.075 / 4 / 0.02` with the current peak/off-peak expression; Pro `0.2175 / 2 / 0.008333333333333333` with the pre-2026-09-14 branch and Flash post-boundary branch |

## Historical repair

The Linux repair binary was rebuilt from the current source and executed inside
the production `new-api` container using the container's `SQL_DSN`,
`REDIS_CONN_STRING` and `CRYPTO_SECRET` environment references. The primary
DeepSeek run applied 74,069 rows with a total delta of `-2,061,058` internal
quota. User, token, channel and hourly `quota_data` updates were performed in
the same transaction, followed by Redis invalidation.

After the apply:

- a second full DeepSeek dry-run returned `repaired_logs=0` at 01:36:28;
- production SQL found zero rows with the stale 318/321/370/373 base64 snapshot lengths;
- zero tiered DeepSeek rows were missing `expr_b64`;
- the active DeepSeek model coverage query returned pricing for every enabled
  exact model name (`deepseek-v4-flash`, `DeepSeek-V4-Flash`,
  `deepseek-v4-pro`, `DeepSeek-V4-Pro`).

The current production hour can diverge again while requests are being served:
new logs are written continuously and `quota_data` is flushed asynchronously.
That is a live aggregation boundary, not a remaining historical stale-price
row. At one post-apply sample the current-hour mismatch was zero; later samples
showed only newly written requests in that same hour.

The formal `new-api` container was not restarted. Its pricing options were
already hot-synced and the production endpoint remained healthy throughout.
