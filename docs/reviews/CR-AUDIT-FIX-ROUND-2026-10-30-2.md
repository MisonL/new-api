# CR-AUDIT-FIX-ROUND-2026-10-30-2

在 `CR-AUDIT-FIX-ROUND-2026-10-30.md`（第三轮）之后再扫一轮，用户要求把列出的
四类问题全部修掉。本轮拆成 4 个原子提交（`432f5c7c0`、`da3429933`、
`18c3a7152`、`81cb2c712`，另有 OAuth 一项 `3ae2623a2`），每项都先写失败用例、
确认 RED、实现、反向验证，再单独提交。

## 1. 逐项修复

### 1.1 剩余 19 个注册模块的配置读取竞争（432f5c7c0）

`ConfigManager.Update` 在写锁内按字段重写已注册结构体；上一轮
（`d8572f81f`）只把 `global / gemini / qwen / fetch_setting` 与
`general_setting` 的读取改成锁内快照。其余 19 个模块的读取访问器仍直接读
包级变量。

复现方式：对每个模块并发执行 `config.GlobalConfig.Update(模块, ...)` 与它的
读取访问器，`-race` 下 19 个模块全部报 `DATA RACE`（`group_ratio_setting`
除外，字段是 `*types.RWMap`，访问走 RWMap 自己的锁）。

修复：

- `setting/config` 新增 `ReadLiveAs[T]` 与 `SnapshotOf[T]` 泛型助手，在管理器
  读锁内取值；模块未注册时由调用方回退到包级变量。
- `SnapshotOf` 刻意不接收"默认值"参数：参数在调用前求值，那次无锁读本身
  就是竞争。调用方拿到 `ok=false` 后再回退。
- 每个模块保留指针 getter（测试播种用，与既有约定一致），新增
  `XSettingsSnapshot()` / `readXSettings()` 供生产读取。
- 迁完全部生产读取点，含中继计价热路径（`billing_setting.GetBillingMode` /
  `GetBillingExpr`、`quota_setting` 免费模型预扣）、Claude 中继的
  `max_tokens` 与 thinking 预算、OAuth 登录（oidc/discord/passkey）、支付
  下单、签到、控制台状态、日志保留、性能指标。
- 同一请求只取一份快照：Claude 转换、充值页的金额档位与折扣，若各取一次，
  热更新会让同一次请求用上两代配置。
- `tools.RebuildToolPriceIndex` 在 `Update` 返回之后重建索引，读 `Prices`
  同样改为锁内快照。
- `monitor_setting.GetMonitorSetting` 原来每次调用都解析 `CHANNEL_TEST_FREQUENCY`
  并就地写包级结构体，两个并发读者互相写同一字段；改为解析结果只施加在快照
  上。环境变量每次重读而不是在包 init 缓存：`.env` 由 `main()` 加载，晚于
  包 init。

### 1.2 定价刷新与 root 建号吞掉数据库错误（da3429933）

`updatePricing` 的两处预加载（模型元数据、供应商）用 `_ =` 丢弃错误：查询
失败时把空结果当成事实继续构建，用一份缺少描述与供应商的列表覆盖上一份正确
缓存。`createRootAccountIfNeed` 忽略 `DB.Create` 的错误，建号失败仍返回 nil。

- 预加载失败时记录错误、保留旧缓存并返回；失败的刷新记录时间戳，使已发布过
  缓存的实例按正常 1 分钟 TTL 重试，而不是每个请求重打数据库。
- root 建号失败向上返回错误。
- 测试 fixture 补齐 `vendors` 表：真实库一定有它，缺表会让刷新在供应商查询处
  失败，测试就测不到定价构建本身。

### 1.3 上游模型预览的本地查询失败显式报错（18c3a7152）

`SyncUpstreamPreview` 用 `_ =` 读本地模型与供应商：查询失败时 locals 为空，
预览把上游模型全部报成"缺失"，运营照此导入会给每条已有模型再建一份。

### 1.4 OAuth 绑定身份检查 fail-closed（3ae2623a2）

`IsProviderUserIdTaken` 与 `CreateUserOAuthBindingWithTx` 都丢弃了 Count 的
错误：计数失败时 count 为 0，返回"未被占用"。同仓库的 `identityInUse` 已
明确 fail-closed，这两处没有对齐。改为计数失败时判为已占用 / 返回显式错误。

### 1.5 删除无引用函数（81cb2c712）

`updateUserUsedQuota` / `updateUserRequestCount` 在收敛到
`UpdateUserUsedQuotaAndRequestCount` 之后已无调用方。

## 2. 反向验证记录

每项都在本轮执行并读取输出。

- 配置读取：分别还原 `quota_setting`、`oidc`、`billing_setting`、`tool_price`
  的访问器为裸读包级变量，四条子用例均报 `WARNING: DATA RACE`。
- 定价刷新：恢复两处 `_ = DB.Find(...).Error`，两条用例分别报
  `a failed metadata query must not be treated as 'no metadata'` 与
  `a failed vendor query must not wipe the published vendor list`。
- root 建号：恢复吞错误写法，用例报 `An error is expected but got nil`。
- 模型预览：恢复 `_ =` 写法，用例报响应为 `"success":true`。
- OAuth：恢复原实现，用例报 `Should be true` 与
  `does not contain "failed to check existing OAuth binding"`。

## 3. 验证记录

以下命令均在本轮执行并读取输出。

```text
go build ./...                                                    exit 0
go vet ./...                                                      exit 0
go test ./controller ./model ./service ./relay/... ./setting/... -count=1
                                                                  exit 0，51 个包 ok，无 FAIL
go test -race -count=1 ./setting/... ./model ./service ./relay ./relay/helper
                                                                  exit 0，13 个包 ok，无 DATA RACE
```

## 4. 未覆盖边界

- 配置竞争用 SQLite 与 `-race` 复现并验证；未在 MySQL / PostgreSQL 上重复。
  这类竞争是纯内存层的，与数据库方言无关。
- 定价刷新失败注入用的是删表；真实环境里的失败形态（连接中断、锁超时）语义
  一致，但未在真实 MySQL/PostgreSQL 上复现。
- `model_sync` 预览用例只覆盖本地模型查询失败；供应商查询失败走同一模式，未
  单独构造用例。
- 本轮只改后端，未触碰前端；`web/classic/.../EditChannelModal.jsx` 与
  `setting/sensitive_test.go` 属于并行会话的改动，未纳入提交。
