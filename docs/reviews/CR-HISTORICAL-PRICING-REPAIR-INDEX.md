# 索引：历史价格修正记录

本页只做索引与主次标注，不合并、不改写正文。两次修正的范围与模型不同，
都是独立的过程证据。

## 记录

| 日期 | 记录 | 定位 |
| --- | --- | --- |
| 2026-09-12 | [CR-HISTORICAL-PRICING-REPAIR-2026-09-12.md](CR-HISTORICAL-PRICING-REPAIR-2026-09-12.md) | **主**：较新一次修正 |
| 2026-08-29 | [CR-HISTORICAL-PRICING-REPAIR-2026-08-29.md](CR-HISTORICAL-PRICING-REPAIR-2026-08-29.md) | 次：较早一次修正 |

## 阅读建议

历史消费日志金额修正的**现行口径**见 `docs/operations/pricing-maintenance.md`，
那里是权威说明。本目录的两份记录只作为过程证据，用于追溯某次修正命中了哪些
模型、差额多少、备份表叫什么名字。

按 AGENTS.md 第 5 节，涉及 `ModelRatio`、`CompletionRatio`、`quota_data` 聚合
修正或用量回算时，必须同时读 `docs/operations/pricing-maintenance.md`，
不能只看本目录的记录。
