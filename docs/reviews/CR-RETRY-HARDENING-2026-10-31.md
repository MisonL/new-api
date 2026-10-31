# CR-RETRY-HARDENING-2026-10-31

接 `CR-RETRY-STATUS-CODES-2026-10-30.md`（402/400 纳入自动重试）之后，复检同一
条重试链路时发现三处问题并全部修复。三项各一个原子提交，先写失败用例、确认
RED、实现、反向验证，再单独提交。

## 1. 复检发现的三个问题

### 1.1 内容策略拒绝被当成渠道级暂时故障重放（66e68350f）

`3a5f9197c` 去掉了 `OaiStreamHandler` 在 SSE 错误帧上的 skip-retry，让帧内错误
统一交给 `shouldRetry` 按状态码判定。副作用：内容策略/安全拒绝（映射为 502）
也变成"可重试"，于是被逐渠道重放。

这类拒绝由请求内容本身决定——同一个 prompt 换到任何渠道都会得到同样的拒绝。
重放只会白跑重试预算，并让每个被尝试的渠道各记一次健康冷却，把好渠道也拖凉。

修复：新增 `isDeterministicContentPolicyRefusalError`（`controller/relay.go`），
按 `OpenAIError` 的 type / code / message 识别内容策略与安全拒绝，在 `shouldRetry`
里与 `isDeterministicModelNotFoundError` 并列判为终态。判定放在控制器，非流式、
OpenAI SSE 帧、Responses 事件所有路径最终都汇聚到 `shouldRetry`，一处即对全部
协议生效；handler 里不再重复做协议特定判定。

识别的标记：`content_policy_violation`、`content policy violation`、
`content_policy`、`content policy`、`content filter`、`content_filter`、
`safety system`、`prompt is blocked`。

判定的边界是刻意收缩的：普通 400 `invalid_request`（渠道可能只是不接受某个参数）、
402 `insufficient_quota`（账号配额）都保持可重试——它们不是由请求内容决定的。

### 1.2 状态码规则存在真实数据竞争（59ff7e9d9）

`AutomaticRetryStatusCodeRanges` / `AutomaticDisableStatusCodeRanges` 在中继热
路径被 `ShouldRetryByStatusCode` / `ShouldDisableByStatusCode` 无锁读取，而管理端
保存设置经 `Automatic*StatusCodesFromString` 对整个包级切片赋值。切片头是三字
（指针 / 长度 / 容量），裸赋值与并发裸读构成数据竞争，撕裂读可能把新指针与旧
长度拼在一起。

复现：`-race` 下并发读写，得到 8 条 `WARNING: DATA RACE`。

修复：改为与 `tool_price_setting` 一致的 RCU 发布模式。写方解析出新切片后经
`atomic.Pointer[[]StatusCodeRange]` 发布，读者只 `Load` 当前指针；旧切片替换后
不再被改写，读端无需加锁。出厂规则拆成 `defaultAutomatic*StatusCodeRanges`，
只在包初始化构造、之后不再改写，供尚未发布过任何规则时兜底；显式发布空规则
存成空切片而非留在 nil，空切片不会被误当成"未配置"而回退到出厂规则。

公开 API 由变量改为函数：`AutomaticRetryStatusCodeRanges()` /
`SetAutomaticRetryStatusCodeRanges(...)`，禁用侧同理。调用方
（`relay_quota_frame_retry_test.go`、`channel_auto_disable_test.go`、
`status_code_ranges_test.go`）同步改为 getter/setter。

### 1.3 全新安装默认不重试（baaf3b32c）

`common.RetryTimes` 出厂值为 0，`shouldRetry` 在 `retryTimes <= 0` 处直接返回
false。结果是全新安装不手动把这个值调大，就没有任何渠道 failover：402/400 可
重试、自动重试状态码、渠道健康冷却观测整条链路全部空转。

修复：默认改为 3，与生产校准过的取值对齐。数据库里保存过 `RetryTimes` 的存量
部署走数据库值，不受代码默认值影响；影响面是全新安装与未保存过该选项的实例。
两处前端默认字面量（`web/default` 与 `web/classic`）同步为 3，作为选项未加载
时的兜底显示。

## 2. 反向验证记录

每项都在本轮执行并读取输出。

- 1.1：临时去掉 `shouldRetry` 里的 `isDeterministicContentPolicyRefusalError`
  分支，`TestShouldRetryRejectsContentPolicyRefusal` 的四个子用例全部报
  `Should be false`（内容策略/安全拒绝必须保持终态）；还原后四个子用例通过，
  且 `TestShouldRetryNonContentPolicyErrorsStillRetry`（普通 400 与 402）保持
  为 True——证明不是把 400/402 一起误杀。
- 1.2：在 atomic 改造前的直接赋值实现下，`-race` 跑
  `TestStatusCodeRangesDoNotRaceWithReads` 报 8 条 DATA RACE；改造后同一用例
  在 `-race` 下无告警通过。
- 1.3：把 `RetryTimes` 还原为 0，`TestDefaultRetryTimesEnablesFailover` 报
  `RetryTimes 默认值 = 0，全新安装将没有任何渠道 failover`。

## 3. 验证记录

以下命令均在本轮执行并读取输出。

```text
go build ./setting/operation_setting                              exit 0
go test -count=1 ./controller ./model ./service ./relay/... \
  ./setting/... ./common ./middleware                              exit 0，无 FAIL
go test -count=1 -run 'TestShouldRetryRejectsContentPolicyRefusal|\
  TestShouldRetryNonContentPolicyErrorsStillRetry' ./controller/   ok
go test -count=1 -run TestDefaultRetryTimesEnablesFailover ./common/  ok
go test -race -count=1 \
  -run TestStatusCodeRangesDoNotRaceWithReads ./setting/operation_setting/  ok
go test -count=1 -run 'TestOaiStreamHandlerGenericErrorFrameFollowsRetryPolicy|\
  TestOaiStreamHandlerQuotaErrorFrameIsNotMarkedSkipRetry' \
  ./relay/channel/openai/                                          ok
gofmt -l <改动文件>                                                无输出
```

## 4. 未覆盖边界

- 三项都是纯内存层的判定与发布逻辑，与数据库方言无关，未在 MySQL / PostgreSQL
  上重复；1.2 的并发性质用 `-race` 覆盖，不依赖真实数据库。
- 1.1 的内容策略标记是按上游错误文本匹配，未穷举所有厂商的措辞。未命中的
  措辞会退回"按状态码可重试"，即恢复到修复前的行为，不会比原来更差。
- 1.3 改的是代码默认值；生产实例此前已把 `RetryTimes` 存为 3，本次改动对生产
  运行值无影响，影响面为全新安装与未保存过该选项的实例。
- 只改重试链路相关文件，未触碰 `web/classic/.../EditChannelModal.jsx` 与
  `setting/sensitive_test.go`（属并行会话的改动）。

## 5. 复检中另外发现、本任务外、未修复
`029f3ae87` 曾为"空数组 `[]`"增加过显式处理（`NormalizeHTTPStatusCodeRanges`，
`input == "[]"` 返回空规则），后由 `a2724e3ff` 移除。当前
`AutomaticRetryStatusCodesFromString("[]")` 报
`invalid http status code rules: []`，即管理端若提交 `[]` 会保存失败而非清空重试
规则。与本轮三项修复无关，未在本次改动，仅记录。清空规则应提交空字符串。
