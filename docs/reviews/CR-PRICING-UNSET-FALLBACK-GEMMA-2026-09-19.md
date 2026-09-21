# 正式服务未定价回退与 Gemma 免费模型复核

采样时间：2026-09-19 12:56（Asia/Shanghai）。本轮处理了未维护价格模型的放行口径，以及 Gemma 4 免费模型的输出倍率错误显示为 `1`。

## 证据与修复

- 正式服务 `http://127.0.0.1:13000` 的 `/api/status` 返回 `success=true`、版本 `v1.1.0`。
- 正式 `/api/option/` 更新前快照保存为 `/tmp/new-api-pricing-options-before-20260919T005804.json`，包含 `ModelRatio`、`CompletionRatio`、`CacheRatio`、`CreateCacheRatio` 和 `ModelPrice` 五个价格选项，未覆盖其他配置。
- 正式 `SelfUseModeEnabled` 已通过管理 API 设置为 `true`；`/api/user/models` 回读成功，审计中发现的 `step-router-v1`、`auto`、Embedding、视频/第三方别名等未维护价格模型均可见。
- 正式 `CompletionRatio` 中 `google/gemma-4-31b-it`、`google/gemma-4-26b-a4b-it` 已更新为 `0`，键数量保持为 `539`；写入后通过 `/api/pricing` 回读为 `0/0/0/0`，四项依次为 `ModelRatio`、`CompletionRatio`、`CacheRatio` 和 `CreateCacheRatio`。大小写变体 `gemma-4-31B-it` 也解析为零价。
- 正式用户列表共 4 个账户。管理员 `id=1` 的个人回退设置已清除；正式服务改为通过全局 `SelfUseModeEnabled=true` 放行未维护价格模型，因此所有用户均使用统一兜底口径。
- 未维护价格的 token 模型沿用现有兜底：`ModelRatio=37.5`（输入约 `$75 / 1M tokens`）、`CompletionRatio=1`、`CacheRatio=1`、`CreateCacheRatio=1.25`。这不是官方价格，后续补齐官方价后应覆盖对应模型键。
- 源码 `setting/ratio_setting/model_ratio.go` 将两个 Gemma 4 默认 `CompletionRatio` 改为 `0`，并同步 `setting/ratio_setting/model_ratio_test.go` 和 `relay/helper/price_test.go` 回归测试，防止新部署恢复为 `0/1/0`，同时验证自用模式下未定价模型可调用。

## 验证

```text
gofmt -w setting/ratio_setting/model_ratio.go setting/ratio_setting/model_ratio_test.go relay/helper/price_test.go: exit 0
git diff --check（目标源码和测试）: exit 0
go test -count=1 -timeout=180s ./setting/ratio_setting: exit 0
go test -count=1 -timeout=180s ./relay/helper ./controller: exit 0
正式 /api/status: success=true
正式 /api/pricing: Gemma 4 目标条目为 0/0/0
```

## 边界

- 未回算历史日志、用户余额或 `quota_data`：本轮没有确认新的官方价格生效时间，且 Gemma 近期实际日志已为零价。
- 动态路由、Embedding、视频或其他缺少官方单价的模型在生产中会使用上述统一兜底，不代表已经确认其官方价格；补价后必须覆盖对应模型键。
- 生产运行态通过管理 API 启用全局自用模式并修正 Gemma 价格；源码默认值将在后续按正常发布流程进入新镜像，本轮未重启或发布正式容器。
