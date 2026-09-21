# CR: Classic 排行榜界面缺失 i18n 键

- 日期：2026-09-20
- 范围：`web/classic/src/i18n/locales/*.json`、`web/classic/src/pages/Rankings/index.jsx`、`web/classic/src/components/table/model-pricing/performance/*`
- 状态：已修复并验证

## 问题

Classic 主题新增的排行榜界面（`src/pages/Rankings/index.jsx`）以及模型性能徽标、性能详情
两个组件，共引用 36 个 `t()` 键。其中 35 个键在 7 个现行语言文件中全部缺失，另 1 个
（`占比`）在 HEAD 中即已存在但值为空串。

后果：

- 缺失键在运行时回落到键本身（中文原文），英文、日文等界面直接显示中文。
- `占比` 作为排行榜列头，在除 `zh-CN` 外的语言下渲染为空字符串，列头无文字。

`bun run i18n:lint` 门禁未能发现该问题：`web/scripts/i18nLintGate.mjs` 只检测源码中的
硬编码字符串，不做键的完整性校验，因此缺失键不会让门禁变红。

## 语言集合的事实

现行语言集合由两处共同确定，且一致：

- `web/classic/src/i18n/i18n.js` 的 `resources` 只注册 `en`、`zh-CN`、`zh-TW`、`fr`、`ru`、`ja`、`vi`。
- `web/classic/src/i18n/language.js` 的 `supportedLanguages` 为同一组 7 个。
- `web/classic/i18next.config.js` 的 `locales` 为同一组 7 个。

`src/i18n/locales/zh.json` 不在上述任何一处：无任何 `import` 引用、无 glob 动态导入、
`normalizeLanguage()` 把 `zh` 映射到 `zh-CN`。该文件是 2848 键的死文件（相对 `zh-CN`
缺 2202 键），不属于本次维护范围，保持原样未改动。

## 修复

按 classic 既有约定（中文键 + 各语言本地化值）为 7 个现行语言补齐 33 个排行榜相关键：

| 类别 | 键 |
|---|---|
| 榜单通用 | `排行榜`、`排名`、`供应商`、`Token 数量`、`占比`、`增长` |
| 时间范围 | `今日`、`本周`、`本月`、`今年`、`全部时间` |
| 排名变化 | `上升最快`、`下降最快` |
| 模型榜 | `热门模型`、`模型 Token 趋势`、`模型数`、`请求数`、`成功率`、`延迟`、`吞吐` |
| 供应商榜 | `供应商排行`、`供应商市场份额` |
| 性能摘要 | `平均延迟`、`平均 TTFT`、`上游首字节`、`上游总耗时`、`按分组性能`、`性能（24 小时）`、`最近 24 小时模型性能摘要`、`最近 24 小时暂无模型性能数据` |
| 说明与空态 | `基于平台实时用量统计模型和供应商热度。`、`暂无排行榜数据`、`暂无历史趋势数据`、`暂无排行变化`、`排行榜数据加载失败` |

其中 `占比` 在 HEAD 中已存在于 7 个语言文件，只是值为空串（`zh-CN` 为 `"占比"`）。
本次只填充空值，不新增键：

| 语言 | 值 |
|---|---|
| zh-TW | `佔比` |
| en | `Share` |
| fr | `Part` |
| ja | `割合` |
| ru | `Доля` |
| vi | `Tỷ lệ` |

插入位置遵循各文件既有的键排序（`i18next.config.js` 的 `sort: true`），`占比` 原地填值，
新键插入排序位置。

## 对既有工作区改动的处理

插入前该目录已带有 dev 分支未提交改动：每个语言文件相对 HEAD 多出 31 至 32 个键
（路由 Scope 工作台、Codex 加密上下文、渠道请求体大小限制等），与排行榜无关。
本次改动是在这些既有改动之上追加，未回退、覆盖或整理。

语义级对比（HEAD 解析结果 vs 当前解析结果）确认：

| 语言 | 丢失键 | 值被改坏 | 说明 |
|---|---|---|---|
| zh-CN | 0 | 0 | |
| zh-TW | 0 | 4 | HEAD 中空串被后出现的重复键覆盖为本地化值，解析结果为空串；当前为正确译文 |
| en | 0 | 1 | 同上（`不限`） |
| fr / ja / ru / vi | 0 | 各 2 | 同上（`暂无路由 Scope`、`路由 Scope 加载失败`） |

HEAD 的 `en.json` 等文件存在重复键（同键先空串、后译文）。JSON 解析取最后出现者，
故 HEAD 的有效值为译文；本次写入时做了去重，写出的值与 HEAD 有效值一致或更完整
（空串 -> 译文），不存在有效值被改坏的情况。各语言键数：

| 语言 | 当前键数 |
|---|---|
| zh-CN | 4938 |
| zh-TW | 4907 |
| en | 5144 |
| fr | 5009 |
| ja | 4934 |
| ru | 5060 |
| vi | 5475 |

## 验证

命令均在本轮运行并读取输出。

| 命令 | 结果 |
|---|---|
| `JSON.parse` 校验 8 个 locale 文件 | 全部 OK |
| `bun x prettier --check src/i18n/locales/` | All matched files use Prettier code style |
| `bun run lint` | exit 0 |
| `bun run i18n:lint` | exit 0（未发现超出基线的新问题） |
| `bun run build` | exit 0，`✓ built in 3m 29s`，`Safari compatibility check passed for dist` |

键完整性（对 `src/pages/Rankings/index.jsx`、`ModelPerformanceBadge.jsx`、
`ModelPerformanceDetails.jsx` 抽取的全部 36 个键）：

| 语言 | 缺失 | 空值 |
|---|---|---|
| zh-CN / zh-TW / en / fr / ja / ru / vi | 0 | 0 |

构建产物确认：`dist/assets/index-Cv0cqeB8.js` 内同时含 `排行榜`、`供应商排行`、
`平均 TTFT`、`排行榜数据加载失败`、`Provider market share` 以及 `"占比":"Share"`，
证明新键确实进入产物而非只停留在源文件。

## 边界

- `bun run build` 报出的 chunk 体积告警（`index-BslyKwaq.js` 等大于 2000 kB）为既有告警，
  与本次改动无关，未处理。
- `bun run i18n:lint` 通过只说明未新增硬编码字符串问题，不代表键完整性；键完整性由本
  记录的独立脚本校验，该脚本未纳入门禁。
- `zh.json` 为死文件，未修复也未删除，是否清理需单独评估。
- 未做浏览器实际渲染验证；覆盖的是键存在性、取值非空与产物包含，未覆盖 7 种语言下的
  实际界面排版。
