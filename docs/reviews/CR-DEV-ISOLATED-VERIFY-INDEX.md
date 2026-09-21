# 索引：隔离开发环境验证记录

本页只做索引与主次标注，不合并、不改写任何一次验证的正文。
各次记录都是独立的过程证据，保留原样。

## 记录

| 日期 | 记录 | 定位 |
| --- | --- | --- |
| 2026-09-20 | [CR-DEV-ISOLATED-VERIFY-2026-09-20.md](CR-DEV-ISOLATED-VERIFY-2026-09-20.md) | **主**：最新全量验证，含 46 个启用渠道可达性探测、五条协议路径、ping 与审计 writer、classic 排行榜 i18n、`web/default` 三浏览器双视口渲染 |
| 2026-05-11 | [archive/CR-DEV-ISOLATED-VERIFY-2026-05-11.md](archive/CR-DEV-ISOLATED-VERIFY-2026-05-11.md) | 次：早期验证 |
| 2026-05-07 | [archive/CR-DEV-ISOLATED-VERIFY-2026-05-07.md](archive/CR-DEV-ISOLATED-VERIFY-2026-05-07.md) | 次：最早一次 |

## 阅读建议

接手时只读 2026-09-20 那一份即可，它已覆盖前两次的验证维度。需要追溯某项
能力的首次验证结论时，再按日期回查早期记录。

## 边界

2026-09-20 记录中明确列出的未完成项（`web/default` 登录态端到端、
隔离库 V2 路由未启用、真实 code CLI 客户端、图片/视频/任务类渠道）
不会因为本页而被视为已覆盖。
