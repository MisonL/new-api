# CR-RETRY-STATUS-CODES-2026-10-30

用户要求把 402（上游配额不足）与 400（invalid_request）纳入自动重试。两项拆成
两个原子提交（`3a5f9197c`、`a5deb1bcf`），每项都先写失败用例、确认 RED、实现、
反向验证，再单独提交。

## 1. 逐项修复

### 1.1 带内配额错误帧不再被钉成终态（3a5f9197c）

`OaiStreamHandler` 对 SSE 流内的 error 帧返回
`types.WithOpenAIError(..., types.ErrOptionWithSkipRetry())`。上游以 HTTP 200 +
帧内 error 报告 `insufficient_quota` 时，错误在 handler 层就被标成 skip-retry，
控制器的 `shouldRetry` 在 `types.IsSkipRetryError` 处直接返回 false，配置里的
重试状态码对这条路径完全失效。

修复：去掉该处的 `ErrOptionWithSkipRetry()`，只保留按上游错误内容映射的状态码
（`rate_limit` → 429、`insufficient_quota` → 402、其余 → 502），是否换渠道由
`shouldRetry` 按状态码策略决定。

重放安全由既有的 stream prelude 提交标记保证：`shouldRetry` 在
`c.Writer.Written() && !ContextKeyResponsesStreamPreludeRetrySafe` 时返回 false；
标记在流开始时置 true，`trackStreamPreludeCommit` 在首个带实质内容的帧
（content / reasoning_content / reasoning / refusal / tool_calls）到达时清为 false。
因此只发出过 role 或空 delta 帧时允许换渠道，内容已下发后仍然禁止重放。

### 1.2 默认自动重试状态码纳入 400（a5deb1bcf）

`AutomaticRetryStatusCodeRanges` 默认原为 `401-407`，400 不在其中。上游
400 `invalid_request` 可能是该渠道不支持某个参数或不接受该请求形态，换渠道仍可
成功；默认不重试会让这类请求钉死在单个渠道上。

- 后端默认范围改为 `400-407`（`setting/operation_setting/status_code_ranges.go`）
- 三处前端默认值字面量同步：`web/default/src/features/system-settings/integrations/index.tsx`、
  `web/classic/src/pages/Setting/Operation/SettingsMonitoring.jsx`、
  `web/classic/src/components/settings/OperationSetting.jsx`
- 未改动 `service/error.go` 里 5xx→400 的显式重映射：上游 5xx 但错误内容表明是
  请求参数问题时，仍按 400 + skip-retry 返回。这是刻意的语义（请求本身有问题，
  换渠道也不会成功），与本次"上游直接返回 400"的路径不同。

## 2. 反向验证记录

每项都在本轮执行并读取输出。

- 402（handler 层）：把 `types.ErrOptionWithSkipRetry()` 加回返回语句，
  `TestOaiStreamHandlerQuotaErrorFrameIsNotMarkedSkipRetry` 报
  `Should be false`（带内配额错误帧必须交给重试策略判断）。
- 402（端到端）：同样还原后，`TestRelayRetriesInBandQuotaFrameThroughAnotherChannel`
  报 `带内配额错误帧必须换到第二个渠道重试`，此时渠道 B 调用次数为 0。
- 402（重放边界）：`TestShouldRetryInBandQuotaFrameFollowsRetryPolicy` 的两个子用例
  分别锁定 prelude 提交前后；内容已下发时 `shouldRetry` 必须为 false。
- 400：把默认范围还原为 `401-407`，
  `TestShouldRetryByStatusCode_DefaultMatchesLegacyBehavior` 报
  `Should be true`（第 73 行，400），`TestShouldRetryUpstreamBadRequestByDefault`
  同样报 `Should be true`（上游 400 invalid_request 默认必须允许换渠道重试）。

## 3. 验证记录

以下命令均在本轮执行并读取输出。

```text
go build ./...                                                    exit 0
go vet ./controller ./relay/channel/openai ./setting/operation_setting
                                                                  exit 0
go test ./controller ./model ./relay/... ./setting/... ./service -count=1
                                                                  exit 0，无 FAIL
go test ./controller/ ./relay/channel/openai/ ./setting/operation_setting/ -count=1
                                                                  exit 0，3 个包 ok
cd web/default && bun run lint                                    0 errors，22 warnings
cd web/default && bun run build                                   exit 0
cd web/classic && bun x prettier --check <两个改动文件>            通过
```

`bun run lint` 的 22 个 warning 全部是既有的 `react-hooks/set-state-in-effect`，
本轮只改了一个字符串字面量，未新增告警。

## 4. 未覆盖边界

- 端到端用例用 httptest 上游与 SQLite 覆盖语义，未在隔离开发环境里对真实上游
  复现；真实上游的帧内错误形态可能带 `[DONE]` 或带 usage，两种都在既有帧级
  用例的覆盖范围内（终端错误帧无 `[DONE]` 的场景有
  `TestOaiStreamHandlerMapsTerminalErrorFrameWithoutDone`）。
- 402 的生产日志采样（本轮之前）显示：已观测到的 402 行没有 `stream_status`
  键，即它们走的是 HTTP 状态码路径；该路径本来就会按 `401-407` 重试。本次修复
  覆盖的是带内错误帧这条此前必然终态的路径。
- 400 的生产实例此前已手工把 `AutomaticRetryStatusCodes` 扩到 `400-407`，
  所以本次后端默认值改动影响的是全新安装与未保存过该选项的实例；前端三处
  字面量是页面在选项未加载时的兜底显示。
- 未在 MySQL / PostgreSQL 上重复；本轮改动是纯内存层的状态码判定，与方言无关。
- 只改后端重试策略与前端默认字面量，未触碰
  `web/classic/.../EditChannelModal.jsx` 与 `setting/sensitive_test.go`（属于
  并行会话的改动）。
