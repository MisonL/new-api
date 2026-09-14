# Responses 兼容折中方案

## 目标
在保留 chat 兼容能力的前提下，按上游官方协议保留可公开重放的 reasoning 文本，并明确拒绝无法无损转换的 opaque reasoning 状态。

## 官方依据
- DeepSeek Thinking Mode：携带工具的后续请求必须完整回传历史 `reasoning_content`，否则上游返回 400。
- DeepSeek Responses API：`reasoning` 输入使用 `content[]`，元素类型为 `reasoning_text`；流式事件使用 `response.reasoning_text.delta` 和 `response.reasoning_text.done`。
- Anthropic Extended Thinking：工具续轮必须保留完整 thinking 或 redacted thinking 块；无法无损转换的 `redacted_thinking` 不得伪装为普通文本。
- OpenAI Reasoning：reasoning items 属于 opaque 状态，应原样回传相关 reasoning items、function calls 和 outputs；不向原生 OpenAI Responses 注入 DeepSeek 专用纯文本扩展。

文档入口（2026-08-31 核验）：
- <https://api-docs.deepseek.com/guides/thinking_mode>
- <https://api-docs.deepseek.com/guides/responses_api>
- <https://api-docs.deepseek.com/api/create-response>
- <https://docs.anthropic.com/en/docs/build-with-claude/extended-thinking>
- <https://platform.openai.com/docs/guides/reasoning>

## 原则
- `reasoning.encrypted_content` 是上游 opaque 状态，只能用于原生 Responses 恢复、native opaque restore 或 affinity fingerprint，不能写入 chat message、system prompt、user prompt 或普通 `content`。
- 可读 reasoning summary 可以映射到 chat `reasoning_content`；当前 `dto.Message` 和 stream delta 都已有 `reasoning_content` 字段，不需要把 summary 塞进普通 `content`。
- 需要状态保真的请求优先保留原生 Responses；无法保真的 chat compat 只能显式降级或显式拒绝，不能静默当作已恢复。

## 任务
- [x] 识别所有会走 `Responses -> chat compat` 的入口，标出允许保真、可摘要降级和必须拒绝的分支 -> 验证：路径清单覆盖 `FindResponsesViaChatRule`、`responsesViaChat`、`buildOpenAIResponsesRequestBody`、stream/non-stream response handler。
- [x] 在 `FindResponsesViaChatRule` 入口先处理状态型请求：普通 `previous_response_id`、remote compaction、native opaque restore 不进入 chat compat；明确允许的 local synthetic cleanup 例外进入 chat compat -> 验证：这些请求不会调用或只在 synthetic cleanup 场景调用 `ResponsesRequestToChatCompletionsRequest`，需要保真的请求走原生 Responses 或 synthetic compact 恢复。
- [x] 在 `buildOpenAIResponsesRequestBody` 保留 native opaque restore 的边界检查：如果 native opaque reference 被强制转换路径处理，必须显式拒绝或强制保真，不能把 opaque reference 发送到 chat 上游 -> 验证：native opaque reference 不会经由 chat compat 转成 chat request。
- [x] 对 chat compat 中不可恢复的 `reasoning.encrypted_content` 只做两件事：计算 affinity fingerprint 和记录降级；存在可读 summary 时才映射到 `reasoning_content` -> 验证：无 opaque 字段进入正式 prompt、chat message 或 `metadata` 明文字段。
- [x] 补齐非流式 `Responses -> chat` 的可读 reasoning summary 映射，包括 `resp.Reasoning.Summary` 和 output reasoning summary part/text -> 验证：`ResponsesResponseToChatCompletionsResponse` 生成的 assistant message 携带 `reasoning_content`，并覆盖“只有 summary 没有 output_text”的响应。
- [x] 补齐流式 `Responses -> chat` 的 summary 事件覆盖，处理 `response.reasoning_summary_text.delta`；对 `response.reasoning_summary_part.added/done` 明确启用并补齐 key/delta 去重 -> 验证：不同事件形态都能输出 chat `delta.reasoning_content` 且不会重复。
- [x] 对官方 DeepSeek `reasoning_text` 增量按原文映射到 chat `reasoning_content`，并对 `done`、output item 和 terminal response 做去重；官方 OpenAI opaque reasoning 仍不转成纯文本 -> 验证：DeepSeek reasoning text 可见且不会重复，opaque 字段不进入 chat。
- [x] 对 chat compat 降级补充日志/计费上下文标记，区分 `reasoning_encrypted_content_ignored`、`reasoning_summary_mapped`、`previous_response_id_preserved_or_rejected`、`native_responses_preserved`、`affinity_fingerprint_preserved`、`affinity_fingerprint_missing` -> 验证：使用日志详情能判断本次请求的处理分支和是否发生能力折损。
- [x] 补充回归测试，覆盖 `include=reasoning.encrypted_content`、普通 `previous_response_id`、native opaque restore、synthetic compact、remote compaction input、non-stream reasoning summary、summary-only response、stream summary text delta、stream summary part、chat compat 降级、affinity fingerprint 保留和退化 -> 验证：`go test ./service/openaicompat ./service ./relay/channel/openai ./relay` 通过。

## 完成标准
- [x] 兼容模式下不再把 opaque reasoning 当成可恢复状态。
- [x] 需要保真的场景全部走原生 Responses 或 synthetic compact 恢复。
- [x] 可读 reasoning summary 和官方 DeepSeek `reasoning_text` 在 stream 和 non-stream 两条路径都能保留为 `reasoning_content`。
- [x] `responses_encrypted_content` affinity 在能保留 fingerprint 的场景继续生效；不能保留时有明确日志标记且可回退到后续 key source。
- [x] 所有降级行为可从日志或上下文标记中确认。
- [x] 回归测试能稳定区分“可恢复”、“可摘要降级”和“必须拒绝”的行为。

## 本轮验证
- 修复 `ExtractOutputTextFromResponses`：`reasoning` output 不再进入普通 assistant `content` 兜底，summary 只映射到 `reasoning_content`。
- Chat 到 DeepSeek Responses 的历史 reasoning 现在使用官方 `content:[{type:reasoning_text,text:...}]` 形状；原生 OpenAI Responses 默认不注入该扩展。
- 新增 Chat/Responses 流式 reasoning text 增量、terminal snapshot 去重和无 `item_id` 到带 `item_id` 的事件衔接测试。
- 新增非流式回归：summary-only `resp.Reasoning.Summary`、reasoning output `content[].summary_text` 均保留为 `reasoning_content`。
- 新增流式回归：`response.reasoning_summary_text.delta` 输出 chat `delta.reasoning_content`，并保留 summary part 去重覆盖。
- 已执行：`go test ./service/openaicompat ./relay/channel/openai ./relay -run 'Responses.*Chat|OaiResponsesToChat|FindResponsesViaChatRule|ResponsesViaChat|ReasoningSummary|Encrypted|Compaction|PreviousResponseID' -count=1`。
- 已执行：`go test ./service/openaicompat ./service ./relay/channel/openai ./relay -count=1`。
