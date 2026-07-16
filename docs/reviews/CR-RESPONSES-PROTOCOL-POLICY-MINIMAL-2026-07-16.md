# Responses 协议转换策略最小化审计

采样日期：2026-07-16（Asia/Shanghai）

## 结论

正式环境原先的三组规则已收敛为两条按能力触发的例外规则：

- 渠道 117、模型 `gpt-5.6-sol`：仅当请求包含上游声明不支持的 namespace tool 时，才将 Responses 请求转为 Chat Completions。
- 渠道 2、模型 `gpt-5.5`：仅当请求包含上游声明不支持的 custom tool 时，才将 Responses 请求转为 Chat Completions。

两条规则均使用 `only_when_tool_bridge_required=true`，不会把普通 Responses 请求转换为 Chat Completions。其余原规则覆盖的渠道已经移除，原生 Responses 请求保持原协议直达上游。

## 规则来源与时间边界

协议转换编辑器和 Responses 到 Chat 的桥接能力分别在 2026-04-10 的策略 UI 合入、2026-06-28 的 `Responses -> Chat` 支持提交后形成。生产中的 `legacy-chat-to-responses` 规则最早可由 2026-04-30 的会话记录确认；当前无法从数据库证明更早的精确写入时间，因为策略值没有独立更新时间字段。

截图中的 `current-gpt-custom-tools-via-chat` 是前一轮 Codex 会话在 2026-07-14 22:16（UTC+8）写入并逐步扩展渠道范围的临时兼容规则。它原本覆盖 `[2,150,155,160,169,178,180,199,204,205,206]`，范围明显超过已验证的能力缺口。

## 删除依据

开发隔离环境在同一请求场景下验证了以下行为：

- 渠道 117 的普通请求、函数工具、grammar custom tool、tool search 在原生 Responses 下通过。
- 渠道 117 只有 namespace tool 在能力声明为不支持时需要 Chat 桥接，桥接后通过。
- 渠道 2 的普通请求可原生通过，grammar custom tool 在能力声明为不支持时需要 Chat 桥接，桥接后通过。
- 渠道 204 的普通请求和 grammar custom tool 原生 Responses 通过，不需要加入转换规则。
- 渠道 169、199 的主要失败是 `Upstream access forbidden`，不是协议转换失败，不能用转换规则掩盖上游权限或账号问题。

因此，不能保留“当前启用渠道全部转 Chat”的宽规则，也不能因为某个渠道的上游 502 就把它标记为协议不兼容。

## 正式环境变更

变更前策略覆盖三条规则：

1. 117、146 的 Responses -> Chat 规则。
2. 3 的 Chat -> Responses 规则（当前渠道停用）。
3. `current-gpt-custom-tools-via-chat`，覆盖 `[2,150,155,160,169,178,180,199,204,205,206]`。

变更后只保留：

```json
{
  "enabled": true,
  "all_channels": false,
  "rules": [
    {
      "name": "channel-117-namespace-via-chat",
      "enabled": true,
      "source_endpoint": "responses",
      "target_endpoint": "chat_completions",
      "all_channels": false,
      "channel_ids": [117],
      "model_patterns": ["^gpt-5[.]6-sol$"],
      "options": {
        "enable_custom_tool_bridge": true,
        "only_when_tool_bridge_required": true
      }
    },
    {
      "name": "channel-2-custom-via-chat",
      "enabled": true,
      "source_endpoint": "responses",
      "target_endpoint": "chat_completions",
      "all_channels": false,
      "channel_ids": [2],
      "model_patterns": ["^gpt-5[.]5$"],
      "options": {
        "enable_custom_tool_bridge": true,
        "only_when_tool_bridge_required": true
      }
    }
  ]
}
```

同时在渠道能力注册表中记录：117 不支持 namespace tools，2 不支持 custom tools。该声明只影响对应工具类型，不影响普通 Responses 请求。

正式数据库变更前备份：

- 文件：`backups/new-api-pre-protocol-policy-minimal-20260716-123952.dump`
- SHA-256：`61afd7a5a5d0a0189e593da74a5596df6c32e434bca755a1143cb62672772a5c`

## 重启后观测

正式容器仍为旧生产镜像 `new-api-local:prod-codex-lite-8d454008`，重启后健康检查通过。近两小时日志中，`gpt-5.6-sol` 成功请求的 `request_conversion` 均为 `OpenAI Responses`，`request_path` 和 `upstream_request_path` 均为 `/v1/responses`；没有证据表明普通请求被转换为 Chat。

同一时间窗口的错误按原因分组如下：

- 渠道 117：主要是余额不足，另有少量上游 502。
- 渠道 169、199：主要是 `Upstream access forbidden`，属于上游权限或账号问题。
- 渠道 204：大部分原生 Responses 成功，少量 `stream_read_error`。
- 渠道 2：存在上游 502、429 和少量历史 `encrypted_content` 参数错误；当前日志不足以证明这些错误由协议转换造成。

这些错误必须继续按上游状态、请求体形状和渠道能力分别诊断，不能通过扩大协议转换范围处理。

## 边界

本次生产验证确认了规则加载、原生路径和错误分类，但受可用 token 的计费/预检限制，未对 117 和 2 在生产上重新发送完整工具场景请求。完整工具矩阵已在隔离开发环境通过；生产端仍需使用有效余额和目标渠道可用账号进行低频验证。
