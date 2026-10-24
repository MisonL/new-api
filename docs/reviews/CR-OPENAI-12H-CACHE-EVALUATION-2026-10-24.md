# CR: 12h prompt cache 支持评估（2026-10-24）

## 上游变更

`e9d4d6353 feat(billing): bill OpenAI 12-hour prompt cache writes at their own price (#7965)`

OpenAI 于 2026-10-20 在 Responses API 上线 12 小时 prompt cache
（`prompt_cache_options.ttl = "12h"`，GPT-5.6 及以后）。12h 写入价格是输入价的
2.5 倍，是 30 分钟写入的两倍。上游在网关侧补齐了读取 `cache_write_tokens_details`、
按 cc12h 定价、表达式与前端展示。

## 本仓库现状

- 请求侧已转发 `prompt_cache_retention`（`dto/openai_request.go:85`、
  `service/openaicompat/responses_to_chat_request.go:823` 等），但**没有**
  `prompt_cache_options` 字段，也不读取 `cache_write_tokens_details`。
- `dto.InputTokenDetails` 只有 `cached_tokens` / `cached_creation_tokens`，
  没有 12h 写入计数。
- 计费：`service/text_quota.go` 与 `types.PriceData` 只支持 Claude 的 5m/1h
  split（`CacheCreation5mRatio` / `CacheCreation1hRatio`），没有 OpenAI 12h 维度。

结论：若上游真的按 12h 写入计费，本地会把 12h 写入当普通 cache write（比例 1x）
处理，**少计约一半到全部**。

## ROI 实测（生产库只读查询）

采样时间 2026-10-24，查询生产 `logs` 表：

```sql
-- 近 30 天含 cache 写字段的日志条数
select count(*) from logs
 where created_at > extract(epoch from now())::int - 2592000
   and (other::text ilike '%cache_creation%' or other::text ilike '%cache_write%');
-- 176932

-- 近 7 天日志中显式出现 12h 标记的条数
select count(*) from logs
 where created_at > extract(epoch from now())::int - 604800
   and other::text ilike '%12h%';
-- 0
```

近 30 天有大量 cache 写流量（176932 条），但没有一条带 12h 标记。结合生产
模型清单（未见 GPT-5.6 及以上），说明 12h TTL 当前**未被使用**。

## 决策

暂不实施。理由：

1. 当前无实际流量，改动收益为零。
2. 实施面跨 dto（新增拆分字段）、计费（新增 cc12h 维度）、表达式（billingexpr
   新增变量）、前端（定价编辑器、成本估算、日志详情）与 i18n 五套 locale，
   属独立功能需求而非缺陷修复。
3. 一旦开始使用 12h TTL，会先表现为"缓存写入少计费"，可通过日志中的
   `cache_write_tokens_details` 出现来监测。

## 触发条件

出现以下任一情况时立项实施：

- 生产日志出现 `cache_write_tokens_details` 或 `ttl: "12h"`。
- 渠道模型清单加入 GPT-5.6 或更新模型并启用 prompt cache 选项。
- 上游价格页出现 12h 写入单价。

## 实施要点（供后续参考）

- `dto.InputTokenDetails` 增加 12h 写入计数并贯通 streaming / compaction /
  格式转换。
- `types.PriceData` 增加 CC12h 比例；`service/text_quota.go` 按 2 倍 cache write
  比例定价。
- `pkg/billingexpr` 增加 `cc12h` 变量，并在表达式缺省时回退到 2 倍 cc。
- 前端定价编辑器、成本估算、日志详情与 7 个 locale 同步。
