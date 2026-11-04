# CR-MULTI-AGENT-AUDIT-FIXES-2026-11-04

## 1. 范围与方法

- 审查对象：`dev` 分支最新源码与隔离开发环境（`new-api-dev-isolated-*`，端口 3001/5433/6381）。
- 审查方式：多路子代理并行审查 + dsh headless + codex，共 6 路发现逐条人工复核。子代理的多条 P0/P1 断言被复现实验推翻（详见第 5 节）。
- 生产容器 `new-api`（13000）、`postgres`、`redis` 全程只读：仅 SELECT、`docker logs`、`docker inspect`。
- 本文件只记录本轮修复与验证；审查发现的完整清单以会话内逐条复核为准，不重复展开。

## 2. 本轮修复（按提交）

| 提交 | 修复 | 验证 |
| --- | --- | --- |
| `a733776d6` | classic 半色工具类全量失效：`@theme` 声明在 `:root` 读不到 `body` 上的 Semi 变量，改 `@theme inline` 让工具类直出 `var(--semi-*)`；`--radius-*: initial` 清掉的默认圆角刻度（xs-sm-md-lg-xl-2xl-3xl-4xl 及裸 `--radius`）显式补回 | 构建产物检查 `.bg-semi-color-bg-1{background-color:var(--semi-color-bg-1)}`、`.rounded-lg{border-radius:.5rem}`；浏览器实测 `bg-semi-color-bg-1` 计算值从 `rgba(0,0,0,0)` 变 `rgb(35,36,41)`，`rounded-lg` 实测 8px |
| `0493a5181` | models/vendors 升级遗留的旧复合唯一索引 `uk_*_name_delete_at`：非 MySQL 分支 AutoMigrate 后 DROP，MySQL 分支生成列索引建好后 DROP；对未删除行无约束力、白耗写入开销、且两行软删除时间戳相同时错误阻止同名重建 | SQLite 单测 3 条 + 反向验证；真实 MySQL 8.4（`na-audit-mysql2`，端口 13400，库 `newapi_test_d`）F1 用例 + 反向验证；真实 PG 15.17（隔离实例 5433，schema `meta_drop_test_*`）新增 `TestLegacySoftDeleteCompositeIndexDroppedOnPostgres` + 反向验证；PG 日志可见真实执行 `DROP INDEX IF EXISTS "uk_model_name_delete_at"` |
| `4df0582d2` | AWS AKSK 流（`awsStreamHandler`）自管扫描器不经过 `StreamScannerHandler`：健康流零值 EndReason 被记成 error，渠道健康探针永远无法恢复；中途断流静默补 message_stop、空回复按成功计费。补齐：入口 prelude 置位、attempt 级 `ReceivedResponseCount`（入口清零、逐帧自增）、`finishAwsStream` 收尾（见过 message_stop 记正常结束；未见到记上游中断，仅 prelude 阶段转可重试错误，内容已下发则部分成功不重放；`stream.Err()` 真实错误写入 EndError） | 新增 5 条单测 + 反向验证（回退后全 FAIL）；`go test ./relay/channel/aws -shuffle=on` 通过 |
| `132cc1e86` | `TryUserAuth` 只透传会话 id 不校验状态：禁用用户在会话有效期内仍能以自己分组拿 `/api/pricing`、`/api/perf-metrics`、`/api/user/groups`。改为经 `loadSessionUserIdentity` 回读（内存缓存优先），仅启用用户才设置 id；接口本身公开，状态非法按未登录处理不中断 | 新增 `TestTryUserAuthRejectsDisabledUserSession` + 反向验证（回退后 FAIL）；真实生产链路核对：ManageUser disable 走 `user.Update(false)` → `updateUserCache`，缓存回读方案在生产生效 |
| `cb6ca002a` | F1 MySQL 回归补旧索引删除断言（information_schema.statistics 计数为零）；新增 PG 门控用例 | 见 `0493a5181` 行 |

## 3. 评估后不修（含理由）

- **P2-2 默认调色板（gray/slate/blue 等）**：旧 Tailwind 3 配置 `theme.colors` 完全替换默认色板，生产构建同样 0 个 gray 类，不是回归；恢复反而让 `dark:` 变体（跟随 `prefers-color-scheme`）在 `html.dark` 下产生深字深底。
- **P2-3 `dark:` 变体语义**：`html.dark` 下 `prefers-color-scheme` 实测 false，Tailwind 3 时代同样如此，属既有设计。
- **P3-5 `sendResponsesStreamData` 无条件清 prelude**：Responses 路径已有内容级 prelude 缓冲（`isResponsesStreamPreludeEvent` 判定 + `flushResponsesStreamPrelude` + 缓冲上限 8），首个语义事件到达才写线并清标记；无条件清零只在缓冲溢出路径提前发生，窗口极窄且保守方向正确（不重试比重放内容安全）。不动近期加固过的逻辑。
- **P3-6 空 method 不对称**：classic 侧测试明确文档化了宽松行为，仅文档性差异。

## 4. 复核中被推翻的子代理断言

- `SkipDefaultTransaction` 下 gorm `Raw` 无事务保护 —— 实际 Raw 回调只注册 `gorm:raw`，不存在所谓"丢事务"差异。
- `GenerateAccessToken` 跨用户写入 —— 重复时提前 return，不可达。
- `BootstrapRecovery` 状态未清零 —— `ResetBillingMetadata` 已清零。
- classic `text-semi-color-danger` 失效 —— 源码从未使用该类，Tailwind 按需生成故不存在；换源码真实使用的类后验证通过。

## 5. 已知边界

- `go build ./...` 在多会话并行时会被构建缓存锁卡住（实测 20 分钟只有约 20 秒 CPU、无子进程，属等待而非编译）；本轮以 `go build ./relay/... ./controller/...` 与目标测试包代替，AGENTS.md §6 规定的测试集全部实跑通过。
- `./model` 在 `-shuffle=on` 高并发并行跑时偶发 `TestCacheUpdateChannelStatusReplacesCachedSnapshot` 失败（8+ 次复现尝试仅 1 次命中，单测 50 连跑、定 seed 重放、隔离新测试文件对照均无法复现）。与本轮改动文件无交集（channel_cache.go / channel_other_settings_test.go 本轮未动），记录为既有偶发，不在本任务内扩大修改。
- MySQL 用例门控 `NEW_API_TEST_MYSQL_DSN`，PG 用例门控 `NEW_API_POSTGRES_TEST_DSN`；未设置时跳过（CI 与本地行为不同属预期）。

## 6. 验证命令与结果（2026-11-04 实跑）

```text
go build ./relay/... ./controller/...            BUILD_OK（exit 0）
go test ./controller ./model ./relay/common ./relay/helper ./relay/channel/aws ./middleware ./service -count=1
  -> TEST_EXIT=0，7 包全 ok（controller 20.6s / model 9.6s / relay/common 0.2s / relay/helper 22.0s / aws 0.2s / middleware 5.3s / service 26.4s）
go vet ./model ./relay/channel/aws ./middleware  exit 0
NEW_API_TEST_MYSQL_DSN=... go test ./model -run 'TestF1(Model|Vendor)MetaMySQLBackwardCompat|Test(Model|Vendor)NameUniqueAmongLiveRowsOnMySQL|TestLegacy...' 
  -> 8 用例全 PASS（MySQL 8.4.11 / PG 15.17 真实实例）
go test ./model -run 'TestLegacy|TestF1' -count=1（无 DSN）-> ok（PG 用例按设计 SKIP）
git status --short --branch -> 仅剩并行会话文件（EditChannelModal.jsx 修改、setting/sensitive_test.go 未跟踪），未触碰
```
