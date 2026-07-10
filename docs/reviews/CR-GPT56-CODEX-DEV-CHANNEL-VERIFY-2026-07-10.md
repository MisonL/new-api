# GPT-5.6 Codex 开发环境渠道验证记录

## 背景

为验证正式服务中 `gpt-5.6-sol` 的 Codex 调用链路，在不升级正式服务的前提下，将正式库渠道 `72`、`180` 复制到隔离开发库，并在 `127.0.0.1:3001` 上执行真实调用测试。

## 环境

- 开发服务：`new-api-dev-isolated-new-api-1`
- 开发入口：`http://127.0.0.1:3001`
- 开发 build：`919500d696501142c93e9480efeb68ec53e7bec7`
- 正式服务：未升级，仍为 `51074ac3736288ee664966e0a2109e9580d86fcf`

## 数据准备

- 从正式 PostgreSQL 复制 `channels` 表中 `id in (72, 180)` 的完整行到开发 PostgreSQL。
- 从正式 PostgreSQL 覆盖开发库中 `channel_id in (72, 180)` 的 `abilities`。
- 复制后开发库中：
  - 渠道 `72` 启用，模型包含 `gpt-5.6-sol`、`gpt-5.6-luna`、`gpt-5.6-terra`。
  - 渠道 `180` 启用，模型包含 `gpt-5.6-sol`、`gpt-5.6-luna`、`gpt-5.6-terra`。
  - `gpt-5.6-sol` 对应 enabled ability 数量为 `2`。

## 验证项目

1. 直接 `/v1/responses` smoke：
   - 模型：`gpt-5.6-sol`
   - 结果：HTTP `200`
   - 输出：`{"ok":true,"task":"gpt56-smoke"}`
   - 日志：命中渠道 `180`

2. `function_call.arguments` 字符串历史输入：
   - 模型：`gpt-5.6-sol`
   - 输入包含历史 `function_call.arguments` 字符串对象。
   - 结果：HTTP `200`
   - 输出：`args-history-ok`
   - 说明：真实上游未再返回 `Invalid type for input[index].arguments`。

3. 手写 `namespace` 历史输入：
   - 模型：`gpt-5.6-sol`
   - 结果：上游返回 HTML `502`，不是 `Unknown parameter: input[index].namespace`。
   - 说明：该手写输入不能作为 namespace retry 的正向验证；代码层回归测试已覆盖 `code:null,param:""` 与标准 `param` 两种错误体。

4. Codex CLI 真实 coding 任务：
   - CLI：`codex-cli 0.144.1`
   - 模型：`gpt-5.6-sol`
   - 临时工作区任务：新增 `multiply(a,b)`，更新测试并运行 `npm test`。
   - 结果：Codex 退出码 `0`，目标文件修改正确。
   - 日志：生成 13 条成功的流式 `/v1/responses` 请求，均命中渠道 `180`。

5. Codex 自动 compact：
   - 通过临时 Codex 配置降低 `model_auto_compact_token_limit` 触发。
   - 日志中 6 条请求包含：
     - `responses_compact_client=codex`
     - `responses_compact_mode=codex_local`
     - `responses_compact_phase=mid_turn`
     - `responses_compact_trigger=auto`
     - `responses_compact_strategy=memento`
   - 结果：所有相关请求 `stream_status.status=ok`。

6. `/v1/responses/compact` 端点：
   - 模型：`gpt-5.6-sol`
   - 结果：HTTP `200`
   - output types：`message`、`compaction`
   - 结果包含非空 encrypted content。
   - 日志：`request_path=/v1/responses/compact`，`responses_compact_mode=native`，`responses_compact_route_decision=native_opaque_recorded`。

## 统计

临时开发 token `id=39` 下测试日志统计：

- `total=20`
- `success=15`
- `errors=5`
- `compact_meta=6`
- `endpoint_compact=1`
- `arguments_retry=0`
- `namespace_retry=0`

错误 5 条均来自手写 namespace 输入触发的上游 `502`，不是本次生产截图中的 `Unknown parameter` 或 `Invalid type`。

## 清理

- 临时开发 token `id=39` 已删除。
- 对应 Redis token 缓存已删除。
- 临时 Codex 工作区与临时 `CODEX_HOME` 已删除。

## 边界

- 本次未升级正式服务。
- 本次未输出渠道密钥或 API token 明文，未将密钥写入仓库或验证记录。
- 渠道 `72`、`180` 的配置与 abilities 已保留在隔离开发库，用于后续继续复测。
