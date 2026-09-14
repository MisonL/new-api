# GPT Image 2.5 pricing review

采样时间：2026-09-11（Asia/Shanghai）。本次核对 OpenAI 官方模型页、图像生成指南、价格页，以及正式 New API `13000` 的模型和价格接口。

## 官方结论

OpenAI 没有发布可直接调用的通用模型 ID `gpt-image-2.5`。官方发布的精确模型为：

- `gpt-image-2.5-sunburst`
- `gpt-image-2.5-flare`

两者官方标准价格均为每 1M tokens：

| 模态 | 输入 | 缓存输入 | 输出 |
| --- | ---: | ---: | ---: |
| 图像 | `$8` | `$2` | `$30` |
| 文本 | `$5` | `$1.25` | 不适用 |

## New API 维护口径

两种变体与现有 `gpt-image-2` 价格完全一致，按标准倍率维护：

| 模型 | `ModelRatio` | `CompletionRatio` | `CacheRatio` | `ImageRatio` |
| --- | ---: | ---: | ---: | ---: |
| `gpt-image-2.5-sunburst` | `2.5` | `6` | `0.25` | `1.6` |
| `gpt-image-2.5-flare` | `2.5` | `6` | `0.25` | `1.6` |

不配置 `ModelPrice`，因为官方按 token 计费；不配置 `CreateCacheRatio`，因为官方未公布缓存写入价格。

源码 `FormatMatchingModelName` 将两个精确变体归一到 `gpt-image-2`，并增加了倍率、缓存倍率、图像倍率和输出倍率回归覆盖。没有创建未被官方公布的 `gpt-image-2.5` 通用别名。

## 正式服务状态

正式 `13000` 当前 active channel 没有这两个精确模型，`/api/user/models` 和 `/api/pricing` 均未返回 `gpt-image-2.5` 相关条目。正式 options 已合并预置两个精确模型的 `ModelRatio`、`CompletionRatio`、`CacheRatio` 和 `ImageRatio`，不改变现有渠道或其他模型价格。当前没有对应历史消费日志，因此不执行历史账单回算。

## 验证

| 检查 | 结果 |
| --- | --- |
| 官方 `gpt-image-2.5-sunburst` 模型页 | HTTP 200，确认精确 ID和图像模型用途 |
| 官方 `gpt-image-2.5-flare` 模型页 | HTTP 200，确认精确 ID和图像模型用途 |
| 官方价格页 | 确认图像 `$8/$2/$30`、文本 `$5/$1.25/不适用` |
| 通用 `gpt-image-2.5` 模型页 | HTTP 404，未创建虚构通用 ID |
| `go test -count=1 ./setting/ratio_setting` | 通过 |
| 正式 `/api/user/models`、`/api/pricing` | 成功，当前无 2.5 变体 active channel |

来源：

- https://developers.openai.com/api/docs/pricing
- https://developers.openai.com/api/docs/models/gpt-image-2.5-sunburst
- https://developers.openai.com/api/docs/models/gpt-image-2.5-flare
- https://developers.openai.com/api/docs/guides/image-generation
