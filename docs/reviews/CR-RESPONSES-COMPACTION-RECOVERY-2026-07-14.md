# Responses 压缩恢复与工具兼容验证

## 范围

本轮修复聚焦以下问题：

- `remote_compaction_v2` 上游在首个 SSE data 事件前长时间无响应时，允许按渠道配置提前终止原生压缩并进入合成摘要恢复。
- 远程压缩恢复中的合成摘要遇到 500、502、503、504 或 524 时，显式切换配置的摘要备用模型再试一次。
- OpenAI 兼容上游以结构化错误指出 `input[n].encrypted_content` 缺失时，精确移除对应 reasoning 历史项并重试，不删除 message、compaction 或其他输入项。
- default 与 classic 渠道配置页暴露首 data 超时设置，使用日志展示是否触发该恢复路径。

未改变的边界：

- 429 不进入摘要模型回退，仍使用渠道级限流重试策略。
- 含远程 opaque compaction 状态的请求不做本地合成恢复。
- 普通 Responses 请求的 500 或 502 不触发压缩专用回退。
- 所有恢复尝试仍保留错误日志和使用日志标记；最终失败不会被转换为伪成功。

## 代码验证

| 命令 | 结果 |
|---|---|
| 37 个含测试目录逐目录执行 `go test -count=1 -timeout=120s` | 全部通过 |
| `go test -race ./controller ./model ./relay/common ./relay/helper ./relay/channel/openai ./relay ./service -count=1 -timeout=600s` | 通过 |
| `GOTELEMETRY=off go vet ./...` | 通过 |
| Responses 规范化、桥接、tool search 能力路由相关目标测试 | 通过 |
| `bun run build`，目录 `web/default` | 通过，Safari 兼容检查通过 |
| `bun run build`，目录 `web/classic` | 通过，Safari 兼容检查通过 |
| 受影响前端文件的 ESLint、Prettier 与 i18n 同步检查 | 通过 |

`go list ./...` 曾在 macOS 宿主机进入不可中断 I/O 状态，未进入测试阶段；最终改为从测试文件清单生成 37 个目录逐一执行，全部通过。仓库级 default `typecheck` 和 `lint` 仍分别被未修改文件 `src/components/ui/markdown.tsx`、`src/components/long-text.tsx` 和 `src/features/system-settings/models/tool-price-settings.tsx` 的既有问题阻断，本轮受影响文件的定向检查均通过。

## 隔离环境

隔离 Compose 栈：

- 应用：`new-api-dev-isolated-new-api-1`，端口 `3001`
- PostgreSQL：`new-api-dev-isolated-postgres-1`，端口 `5433`
- Redis：`new-api-dev-isolated-redis-1`，端口 `6380`

最终源码构建为 `new-api-local:dev-fee879352` 并强制重建应用容器。验证时 build-info：

```text
version=v1.1.0
commit=fee8793528288d90584408bff753269abdbb1d43
date=2026-07-14T12:35:47Z
```

容器 health 为 `healthy`，`GET http://127.0.0.1:3001/api/status` 返回成功。

## 真实上游验证

隔离环境使用正式渠道的原样副本，测试固定渠道并使用 `/v1/responses`，不记录令牌、渠道密钥、完整上游地址或响应正文。

### 工具兼容

- 普通 Responses 和标准函数工具：117、155、160、178、204、206、208 全部通过。
- 自定义 `additional_tools` 与 namespace 工具：上述渠道均能返回正确工具调用；206 未表现出独有的丢工具问题。
- 2、150、155、160、169、178、180、199、204、205、206 使用“仅请求需要工具桥接时”规则，普通请求和压缩请求保持原生 Responses 路径。
- tool search 两轮回归：117、155、160、178、204、206、169、199、2、150、180 全部通过，第二轮均恢复为 `function_call read_file`，namespace 为 `workspace`。
- 208 的上游对 tool search 不兼容，能力注册表显式设置 `supports_tool_search=false`；不影响其原生普通工具、namespace 和压缩能力。

### 压缩恢复

- `context_compaction` 在渠道选择、协议桥接和上游请求前统一规范化为 `compaction`；空标记被移除，非法类型和仅空标记请求返回显式 400。
- 208 和 204 分别完成约 2.2 MB、超过 324k input tokens 的远程压缩并返回合法 `compaction`，否定“超过 272K 必然由 new-api 本地触发 502”的假设。
- 204、206 的真实加密 compaction 项分别使用 `context_compaction` 和标准 `compaction` 回放，均返回 `200 completed`。
- 169、199 使用 440 KB、约 68k input tokens 的输入均返回合法 `compaction`；169 一次回放遇到瞬时上游 502，随后连续两次成功。
- 2、150、180 在条件式工具 bridge 生效后仍保持原生压缩，220 KB 输入全部返回合法 `compaction`。
- 178 设置 `responses_remote_compaction_first_data_timeout_seconds=5`；首 data 超时和摘要备用模型路径有自动化测试及真实恢复日志覆盖。

此前复现的 502 包含上游 Cloudflare 源站失败、首 data 长时间无响应，以及旧 Codex `context_compaction` 兼容缺陷。本轮分别通过显式超时恢复、摘要备用模型和请求规范化处理；整个上游持续离线时仍返回真实失败，不伪造成功。

## 渠道专项结论

### 169 与 199

正式渠道当前均为手动禁用。使用正式配置原样复制到隔离环境后，两条渠道的普通请求、标准函数、自定义工具、namespace、tool search、远程压缩和两种 compaction 回放均通过。199 曾出现一次 60 秒上游 502，随后相同自定义工具场景连续两次成功，判定为瞬时上游波动。

### 117 低命中

117 与 155 在 `gpt-5.5` 上同为优先级 14、权重 0，同层选择时实际权重均为 10，不存在代码侧偏置。117 命中少有两个直接原因：

- 当前还有优先级 16 的 2、150、204、205，以及优先级 15 的 178、180；正常请求不会直接进入优先级 14。
- 117 使用 Responses -> Chat 路径，压缩请求会按能力约束跳过它；正式日志明确记录 `channel_skipped_unsupported_compaction` 后选择 155。

117 于 2026-07-13 重新启用，历史累计用量也不能直接与长期启用的 155 比较。

### 429

正式库过去 24 小时采样到 1086 条 429，涉及 756 个请求，其中 327 个经跨渠道重试最终成功。错误正文明确来自上游并发限制、RPM、日限额、pending request 限制或余额耗尽。当前重试链路有效，但上游容量不足仍是真实外部约束；本轮未通过禁用渠道或吞错来掩盖 429。

### Responses 历史 item ID 前缀

正式日志发现 Codex 完整历史回放携带 `id=item_*`，严格上游按 item 类型分别要求 `fc`、`msg`、`rs` 前缀并返回 502。30 天日志中共有：

- function/tool call 期望 `fc`：32 条，涉及 18 个请求。
- message 期望 `msg`：15 条，涉及 5 个请求。
- reasoning 期望 `rs`：9 条，涉及 4 个请求。

原版 QuantumNous/new-api 最新主分支提交 `a63364d156cf2a64f1c3d1ee4923d73d5f3222a1` 尚未处理该问题。Wei-Shaw/sub2api 已分别在 `fd64d07e61b4ae57830ede458069a002f824de60` 和 `4d4ba64bf7ba110241e0850bee2dd4180a6b3f49` 修复 function-call 与 message 的非法 `item_*` ID，策略是删除非法 `id`，不伪造新的服务端 ID。

本仓库提交 `f1fe96d3efb570eb654f36fefbfece4c22d14d61` 吸纳并扩展该策略：

- function/tool call 输入仅保留 `fc` 前缀 ID。
- message 仅保留 `msg` 前缀 ID。
- reasoning 仅保留 `rs` 前缀 ID。
- `call_id`、output item ID、合法 ID 和无约束 item 保持不变。
- 清理数量写入应用日志，不记录具体 ID 或用户内容。

隔离环境固定严格渠道 2，function-call、message、reasoning 三类 `item_*` 输入均从必现 502 变为 `200 completed`。正式发布后相同三类探针全部通过；真实长上下文每次清理 7 到 196 个非法 ID 后继续完成，未再出现 `invalid_id_prefix`。

## 正式发布状态

当前正式镜像：

```text
image=new-api-local:prod-20260714-item-id-f1fe96d3e
image_id=sha256:ca6c5e05609195a00ff21159de49739abf0d47d5b3e2ea04952759b7f30ec83f
commit=f1fe96d3efb570eb654f36fefbfece4c22d14d61
```

发布前完整 PostgreSQL 备份：

```text
.dev-docker/backups/new-api-prod-pre-f1fe96d3e-20260714-234502.dump
sha256=9bdd926e9c61d409b5ac367c3d083f570da464b37115b4d51b1f8f4a9547c21c
```

备份权限为 600，`pg_restore --list` 校验通过。仅重建 `new-api` 服务，PostgreSQL 和 Redis 未重建。

部署后验证：

- 容器 `running`、`healthy`、重启次数 0，build-info 与镜像提交一致。
- 普通自动 Responses、117 空 `context_compaction`、205 自定义工具和 namespace 工具均通过。
- 自动 tool search 修正配置前定位到 2、150 原生不支持；隔离验证并补齐规则后，正式连续 5 轮两阶段调用全部通过，日志实际覆盖 2、150、204、205。
- 204 对 220 KB 输入生成真实 `compaction`，随后 `context_compaction` 与标准 `compaction` 回放均 `200 completed`。
- `item_*` 前缀修复发布后，正式 function-call、message、reasoning 三类探针全部通过。
- 5 分钟滚动观察期间新增 101 条真实日志，`invalid_id_prefix`、500、429 均为 0；502 保持为部署初期同一请求的 3 次跨渠道通用上游错误，没有新增。
- 发布后日志未发现 panic、fatal、schema、deadlock 或 migration 失败。
