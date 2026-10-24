# CR: JSON Schema 键序丢失问题与修复方案（2026-10-24）

## 缺陷背景

上游 `d0b840e75 fix(relay): keep the client's JSON Schema property order` 指出：本地有 schema → map[string]interface{} → Marshal 转换链，JSON 编码器对 map 的 key 排序导致 ordered schema 属性顺序丢失，破坏 structured output / tool parameters。

## 影响面审计

### A. dto 层定义（存在顺序风险的字段）

| 文件 | 字段 | 当前类型 | 风险等级 |
|---|---|---|---|
| `dto/claude.go:177` | `InputSchema` | `map[string]interface{}` | 🔴 高（Claude tools） |
| `dto/openai_response.go:147` | `Parameters` | `any` | 🟠 中（Responses API） |
| `dto/realtime.go:70` | `Parameters` | `any` | 🟠 中（Realtime WS） |
| `dto/openai_request.go:247` | `Parameters` | `any` | 🟠 中（Chat Completions） |
| `dto/openai_request.go:92` | `SearchParameters` | `json.RawMessage` | ✅ 已安全（透传） |

### B. 序列化处理点（decode→marshal）

| 文件 | 函数/位置 | 操作 | 影响 |
|---|---|---|---|
| `relay/channel/claude/relay-claude.go:237` | `normalizeClaudeInputSchema` | `Any2Type[map]` → `Marshal` (via `Tool.String`) | Claude tools |
| `relay/channel/ollama/relay-ollama.go:287,336` | `normalizeOllamaToolParameters` | `Unmarshal(map)` → `Marshal(schema)` | Ollama response format |
| `service/convert.go:1383` | `toolRequestFromOpenAIFunction` | `Unmarshal(map)` → `Marshal` | OpenAI→Claude 转换 |
| `service/openaicompat/responses_to_chat_request.go:208` | - | `Marshal(schemaMap)` | OpenAI Responses → Chat |
| `relay/channel/aws/converse.go:639` | Bedrock 参数包装 | `Marshal(schema)` | AWS Bedrock |

### C. 使用场景

- **Structured outputs**: GPT-4o/GPT-5 响应格式约束，键序丢失可能让模型输出未按 schema 定义的顺序填充值。
- **Tool parameters**: Claude/OpenAI/Bedrock 工具函数参数 schema，键序错乱导致解析失败或行为异常。
- **Ollama JSON schema**: Response format 约束校验失效。

## 修复方案对比

### 方案 1：全部改为 json.RawMessage（上游做法）

**步骤：**
1. `dto.Tool.InputSchema` → `json.RawMessage`
2. `dto.FunctionRequest.Parameters` → `json.RawMessage`
3. 每个调用点从 RawMessage 解包时避免转 map；如需访问结构用 `jsoniter` 或预解析模板。
4. 测试：添加 ordered schema 回归用例（验证 marshal 后保持原始属性顺序）。

**优点：** 完全保留客户端原始 JSON；与上游一致。
**缺点：** 改动量大（~30 个调用点 + dto 签名）；失去编译期类型安全；需全面重构 schema 构造逻辑。

**工作量预估：** 3-5 人日（含测试）。

### 方案 2：局部修复关键路径（最小干预）

只修复高风险点：
- Claude InputSchema (`dto.Tool`)：因 `Tool.String()` 直接序列化导致键序丢失，必须修。
- Ollama response format：若业务依赖 Ollama JSON schema 则修；否则可暂缓。
- 其他路径暂不修，观察无 bug 再决定。

**优点：** 改动可控；快速上线。
**缺点：** 遗留潜在风险；未来维护需持续关注。

**工作量预估：** 1-2 人日。

### 方案 3：等待 relaykit 迁移统一修

若计划做 relaykit 统一 DTO 重写，可在迁移时将 schema 字段全量改为 RawMessage，避免重复工作。

**优点：** 一次到位；符合技术演进方向。
**缺点：** 时间不可控；当前用户可能遇到有序 schema 相关 bug。

## 建议决策

| 业务优先级 | 推荐方案 | 理由 |
|---|---|---|
| **高**（已有结构化输出/工具调用需求） | 方案 1 或 2（立刻修） | 有序 schema 是核心正确性问题，早修早受益。 |
| **低**（仅简单文本生成） | 方案 3（暂缓） | 可按需排期，不阻塞发布。 |

若需立刻处理，建议：
1. 先用方案 2 修复 Claude InputSchema（最常用、影响最大）。
2. 补 ordered schema 回归测试（至少 2 条：reasoning first / answer last）。
3. 后续在 relaykit 迁移时整体升级到方案 1。

## 测试建议

**ordered schema regression test**：
```go
func TestSchemaKeepsPropertyOrder(t *testing.T) {
    // Given: schema 定义属性顺序为 ["reasoning", "answer", "thought"]
    raw := []byte(`{"type":"object","properties":{"reasoning":{...},"answer":{...},"thought":{...}}}`)

    // When: 经过我们的工具序列化为 JSON
    var schema map[string]any
    require.NoError(t, json.Unmarshal(raw, &schema))
    marshalled, err := json.Marshal(schema)

    // Then: 属性顺序仍保持原始顺序（通过 Regex 或 AST 比较）
    require.Regexp(`"reasoning".*"answer".*"thought"`, string(marshalled))
}
```

## 参考资料

- 上游 PR `d0b840e75` 完整实现
- `docs/superpowers/specs/2026-09-30-relaykit-migration-design.md` relaykit 迁移设计（含 Tool 重构时机）


---

## 2026-10-24 实施结果

### 已修复：Claude 工具 schema（提交 `420b20280`）

- `dto.Tool.InputSchema` 改为 `json.RawMessage`，原样透传客户端字节。
- `normalizeClaudeInputSchema` 返回 RawMessage；缺 `type` 时用文本拼接补
  `"type":"object"`，不走 map 重编码。
- 回归测试 `TestNormalizedClaudeToolParametersPreservesPropertyOrder`，
  反向验证：还原旧 map 实现后失败。

### 未修并说明理由：OpenAI `FunctionRequest.Parameters`

原计划一并改 `json.RawMessage`，审计后放弃，原因：

| 消费点 | 为什么必须保持结构化 |
|---|---|
| `relay/channel/gemini/relay-gemini.go:431-450` | Gemini 需要读取并删除 schema 中不支持的字段（`cleanFunctionParameters`），还要判断空 `properties` 时把整个 parameters 置 nil。改 RawMessage 需在 Gemini 内部重新解码再编码，仍然丢序，等于没修。 |
| `relay/channel/aws/converse.go:532` | 需要校验 `type` 并补齐默认值，走 map 后再交给 Bedrock SDK。 |
| `service/openaicompat/chat_to_responses.go:423` | 跨协议重组，本来就要改写结构。 |
| `dto/openai_request.go:194` | token 计数把参数当文本打印。 |

结论：这些路径的“改写 schema”是功能需求，不是序列化副作用。要做到上游那样
完全保序，需要把改写改成基于有序 AST（如保持原始 token 顺序的重写器），属独立
设计任务，收益主要在 Gemini/Bedrock 的 structured output 场景。

### 残留影响面

- Claude tools / Claude `output_config.format`：已保序。
- Gemini、Bedrock、openaicompat、Ollama：仍走 map，属性顺序会被字典序替代。
- 若后续确认上述渠道的 structured output 受影响，再按有序 AST 方案立项。
