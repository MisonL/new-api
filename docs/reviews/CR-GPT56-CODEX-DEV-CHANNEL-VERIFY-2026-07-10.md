# GPT-5.6 Codex 开发环境渠道验证记录

## 背景

为验证正式服务中 `gpt-5.6-sol` 的 Codex 调用链路，在不升级正式服务的前提下，将正式库渠道 `72`、`180` 复制到隔离开发库，并在 `127.0.0.1:3001` 上执行真实调用测试。

## 环境

- 开发服务：`new-api-dev-isolated-new-api-1`
- 开发入口：`http://127.0.0.1:3001`
- 开发构建提交：`919500d696501142c93e9480efeb68ec53e7bec7`
- 正式服务：未升级，仍为 `51074ac3736288ee664966e0a2109e9580d86fcf`

## 数据准备

- 从正式 PostgreSQL 复制 `channels` 表中 `id in (72, 180)` 的完整行到开发 PostgreSQL。
- 从正式 PostgreSQL 覆盖开发库中 `channel_id in (72, 180)` 的 `abilities`。
- 复制后开发库中：
  - 渠道 `72` 启用，模型包含 `gpt-5.6-sol`、`gpt-5.6-luna`、`gpt-5.6-terra`。
  - 渠道 `180` 启用，模型包含 `gpt-5.6-sol`、`gpt-5.6-luna`、`gpt-5.6-terra`。
  - `gpt-5.6-sol` 对应启用能力记录数量为 `2`。

## 验证项目

1. 直接 `/v1/responses` 冒烟验证：
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
   - 说明：该手写输入不能作为 namespace 重试的正向验证；代码层回归测试已覆盖 `code:null,param:""` 与标准 `param` 两种错误体。

4. Codex 命令行真实编码任务：
   - CLI：`codex-cli 0.144.1`
   - 模型：`gpt-5.6-sol`
   - 临时工作区任务：新增 `multiply(a,b)`，更新测试并运行 `npm test`。
   - 结果：Codex 退出码 `0`，目标文件修改正确。
   - 日志：生成 13 条成功的流式 `/v1/responses` 请求，均命中渠道 `180`。

5. Codex 自动压缩：
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
   - 输出类型：`message`、`compaction`
   - 结果包含非空加密内容。
   - 日志：`request_path=/v1/responses/compact`，`responses_compact_mode=native`，`responses_compact_route_decision=native_opaque_recorded`。

7. Codex 多轮真实编码任务与 CompactV2 续跑：
   - 临时工作区：`/tmp/newapi-gpt56-compactv2-real-task`
   - 模型：`gpt-5.6-sol`
   - 任务内容：让 Codex 修复订单 CSV、折扣、税费汇总测试；第二轮追加分类报表功能测试，要求续跑并完成 `renderCategoryReport`。
   - 第一轮：原始 `npm test` 为 `7` 个测试中 `3` 过 `4` 失败；Codex 修改 `src/orders.js` 后 `7` 个测试全部通过。
   - 第二轮：追加 `test/report.test.js` 后进入续跑，多次触发长会话和压缩提示；中途一次因 Codex 沙箱只读导致写入被拒，随后使用同一开发环境重新以可写权限续跑。
   - 最终结果：`node --test test/report.test.js` 退出码 `0`，`1` 个测试通过；`npm test` 退出码 `0`，`8` 个测试全部通过。
   - 证据文件：
     - `/tmp/newapi-gpt56-compactv2-real-task/evidence/turn1.jsonl`
     - `/tmp/newapi-gpt56-compactv2-real-task/evidence/turn2.jsonl`
     - `/tmp/newapi-gpt56-compactv2-real-task/evidence/turn2-continue.jsonl`
     - `/tmp/newapi-gpt56-compactv2-real-task/evidence/turn2-continue-write.jsonl`
   - `turn2-continue-write.jsonl` 末尾包含 `TURN2_DONE`。
   - `rg -c 'Long threads and multiple compactions|compaction|compact'` 命中统计：`turn1=25`、`turn2=21`、`turn2-continue=2`、`turn2-continue-write=2`。
   - 开发日志中 token `40`、`41`、`42` 均为 `gpt-5.6-sol`，均命中渠道 `180`，请求数分别为 `40`、`37`、`9`。
   - 开发日志中 `Unknown parameter` 命中 `0`，`expected an object` 命中 `0`。
   - 开发日志中 `namespace` 字符串命中来自 `other` 字段里的 `channel_capability_snapshot.compact_mode_effective` 等压缩能力快照，不是上游 `input[index].namespace` 参数错误。

8. 高上下文、高缓存读、自动压缩场景：
   - 临时工作区：`/tmp/newapi-gpt56-compactv2-real-task`
   - 模型：`gpt-5.6-sol`
   - 渠道：`180`
   - 任务内容：在接近 `353K` 上下文窗口时让 Codex 继续真实编码任务，先修复空行和注释行报表测试，再多轮续跑测试，等待自动压缩。
   - token `45` 结果：
     - 高输入请求：`318233`、`322332`、`333238`、`334509` prompt tokens。
     - 高缓存读请求：`317184`、`321280`、`332544` cache tokens。
     - 自动压缩请求：`prompt_tokens=331289`、`cache_tokens=6912`、`responses_compact_mode=codex_local`、`responses_compact_trigger=auto`、`responses_compact_phase=mid_turn`、`responses_compact_strategy=memento`、`stream_status.status=ok`。
     - compact 后请求降到约 `53K-56K` prompt tokens。
   - token `46` 结果：
     - warm-up 后固定大前缀缓存生效，高缓存读请求包括 `prompt_tokens=297561/cache_tokens=296704`、`300031/298752`、`303815/302848`。
     - 多轮真实测试 resume 后，高缓存读持续增长到 `328K` 级别仍稳定返回 `ok`。
     - 手动补充少量上下文压力后，请求 `856080` 达到 `prompt_tokens=341193`、`cache_tokens=328448`，随后 `856081` 自动压缩。
     - 自动压缩请求 `856081` 为 `prompt_tokens=337808`、`cache_tokens=6912`、`responses_compact_mode=codex_local`、`responses_compact_trigger=auto`、`responses_compact_phase=mid_turn`、`responses_compact_strategy=memento`、`stream_status.status=ok`。
     - compact 后请求 `856082` 降到 `prompt_tokens=55535`、`cache_tokens=11008`。
   - 结论：已验证高缓存读请求之后自动压缩能成功完成；当前观察到的压缩请求本身仍是低缓存读，不是高缓存读。
   - 失败边界：过大 prompt 会返回上游 `400 input exceeds context window`，这属于上下文越界，不是 `arguments` 或 `namespace` 参数适配错误。
   - 最终本地验证：`npm test` 退出码 `0`，`10` 个测试全部通过。

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

追加多轮任务临时 token 日志统计：

- `id=40`：`40` 条请求，模型 `gpt-5.6-sol`，渠道 `180`。
- `id=41`：`37` 条请求，模型 `gpt-5.6-sol`，渠道 `180`。
- `id=42`：`9` 条请求，模型 `gpt-5.6-sol`，渠道 `180`。
- `id in (40,41,42)` 的 `Unknown parameter` 计数为 `0`。
- `id in (40,41,42)` 的 `expected an object` 计数为 `0`。

高上下文追加测试临时 token 日志统计：

- `id=45`：验证 10% 上下文余量附近真实编码任务、自动压缩和压缩后降低上下文。
- `id=46`：验证高缓存读请求序列、自动压缩和压缩后降低上下文。
- `id in (45,46)` 的 `Unknown parameter` 计数为 `0`。
- `id in (45,46)` 的 `expected an object` 计数为 `0`。
- `id=46` 中有 1 条 `canceled`，来自测试过程中手动中断批量后续循环，不是上游参数错误。

## 清理

- 临时开发 token `id=39` 已删除。
- 临时开发 token `id=40`、`id=41`、`id=42` 已删除。
- 临时开发 token `id=43`、`id=44`、`id=45`、`id=46` 已删除。
- 对应 Redis token 缓存已删除。
- 临时 Codex 工作区保留在 `/tmp/newapi-gpt56-compactv2-real-task`，用于复核证据。
- 临时 `CODEX_HOME` 中的 `config.toml` 仅引用 `env_key = "NEWAPI_CODEX_TOKEN"`，不包含 token 明文。

## 边界

- 本次未升级正式服务。
- 本次未输出渠道密钥或 API token 明文，未将密钥写入仓库或验证记录。
- 渠道 `72`、`180` 的配置与 abilities 已保留在隔离开发库，用于后续继续复测。
