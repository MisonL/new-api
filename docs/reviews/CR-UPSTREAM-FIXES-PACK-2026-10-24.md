# CR: 上游 P0/P1 修复批量移植（2026-10-24）

采样时间：2026-10-24。任务来源：用户要求"全部优化好"，指代评估清单中的未合入上游修复项。

## 移植范围

按优先度从高到低移植 7 个原子修复（每个提交一个原子包），并更新过时文档声明。已排除的
项目及其理由已在下方"已知遗留"列出。

| # | 上游 Commit | 本地提交 | 内容摘要 | 级别 |
|---|---|---|---|---|
| 1 | `1b63c28dc` | 7275c69e1 | Claude cache-only 用量与显式零 usage 计费修正 | P0 |
| 2 | `e2a0c4b16` | 55f76fa52 | Redis limiter NOSCRIPT 回退到 EVAL | P0 |
| 3 | `45fbb149f` | 6def28cd2 | 分组限流按 userId:group 独立计数 | P1 |
| 4 | `55bb6b903` | 078a90632 | 通知限流窗口不再随整点重置 + 非正数回退 | P1 |
| 5 | `09109ec31` | c3f47288b | 过载哨兵改用容器 cgroup 口径 + 连续性与新鲜度 | P0 |
| 6 | - | 4757072bb | 倍率同步 envelope 伪成功判定修正（适配本地） | P1 |
| 7 | 文档修订 | docs | 更新 CR-MULTI-ROUND-AUDIT §11.8 过时说明 | P2 |

## 关键实现要点

### 1. Claude cache-only 用量计费（P0）
- `service/text_quota.go` 新增 `hasBillableUsage()`：将 `CacheTokens`、`CacheCreation*` 纳入可计费用量判断；同时新增 `hasClaudeCacheOnlyUsage()` 供 PostTextConsumeQuota 区分“全落在 cache”而非“上游无回报”。
- `relay/channel/claude/HandleStreamFinalResponse`：用 `buildOpenAIStyleUsage.PromptTokens == 0` 作为缺失输入判据（包含 cache），仅当 input+cache 全为 0 才回填；输出同理仅在确实流出内容而 token 为 0 时回填，显式 0 保持。

测试：4 个 cache-only 用例与 3 个显式零回归用例全绿；前后对比实测（探针返回 Quota=0 → 正确值）。

反向验证：不修即失败（之前探针已证实缓存全命中时 Quota=0）。

### 2. Redis limiter NOSCRIPT 回退（P0）
- `common/limiter/limiter.go`：收到 "NOSCRIPT " 前缀错误时，改调 Eval 载入脚本并复用结果，避免进程内只缓存 SHA 导致后续所有限流请求以 rate_limit_check_failed（500）终结。
- miniredis 回归：ScriptFlush 后先断言 SCRIPT EXISTS=false，再执行 Allow，期望成功且 SCRIPT EXISTS=true。

隔离 dev Redis 实测复现过：SCRIPT FLUSH → EVALSHA 报 NOSCRIPT → 修复后正常；运行后已 SCRIPT LOAD 恢复脚本缓存。

### 3. 分组限流独立计数（P1）
- `middleware/model-rate-limit.go`：新增 `rateLimitSubject(c)`，有独立限流配置（GetGroupRateLimit found=true）的分组返回 `userId:group`，否则返回 `userId`；redis/memory 处理器改为接收 subject 字符串并使用它作为 key 前缀。

端到端测试构造场景：loose(3) 打满后 strict(2) 的首个请求应为 200；旧逻辑会因共用 userId 计数变成 429。反向验证通过。

### 4. 通知限流窗口整点重置（P1）
- `service/notify-limit.go`：key 去掉小时后缀 `:2006010215`；Redis 路径改同一事务的 SETNX+INCR（SETNX 设过期时间，INCR 保留 TTL）；非正数分钟回退默认 10。内存路径同样去掉后缀。
- test：miniredis 用 FastForward 推进时间跨越窗口边界，验证两小时前的用量仍阻断（TTL 仍在）、新窗口打开、新窗口内第二次允许、第三次阻断、超配置时长后恢复。

反向验证：带 hour 后缀时在 FastForward 后会立即失效。

### 5. 过载哨兵容器口径 + 连续性（P0）
- `common/container_usage.go`（新建）：按 cgroup v2/v1 读取配额与用量；CPU 计算在 quota/cpuset 较小者上，内存计算为 working_set = usage - inactive_file（含 fallback 策略）。容器口径不可用时调用方回退宿主机口径。
- `common/system_monitor.go`：采样增加 `SampledAt` 字段；引入 `overloadDetector` 累计连续 3 次（约 15s）阈值超限，任意回落清零；`systemStatusMaxAge=30s`，过期采样不再熔断。
- `common.CheckSystemOverload`：消费 status 的 Overloaded 标记并复核阈值，返回 *SystemOverload 或 nil。
- `middleware/performance.go`：消费 CheckSystemOverload，删除内联重复逻辑。
- tests：port upstream container_cpu/memory tests 与连续采样/过期采样检查。

注意：本地生产环境与 dev 环境 `performance_setting.monitor_enabled` 均为 false（只读查询确认），本修复处于关闭状态，但代码与数据结构已就绪。

### 6. ratio_sync envelope 伪成功（P1）
- `controller/ratio_sync.go`：Success 由 bool 改为 *bool；抽取 `upstreamEnvelopeError(success,message)`：nil 或 true → ""（继续解析），false → message（为空则合成描述）。
- 同文件注释称“若 Data 为空将继续按 type1 解析”，但实际一直尝试解析空 body.Data。修改为 data 缺失时把整个响应体当 type1 载荷，静态裸数据得以正确识别。

测试：envelope 四分支 + 端到端 HTTP 服务器覆盖两种裸 payload。反向验证：helper 还原成旧 bool 语义后立即失败。

### 7. 文档修订（P2）
- CR-MULTI-ROUND-AUDIT 记录 §11.8 声称"剩余违规 0"但与现状不符的过时声明。本次迁移已完成，该节应标注"已完成：1c02b5c7b"。

## 未覆盖与已知遗留

以下项目在本轮中 deliberately not done：

| 主题 | 原因 | 建议下一步 |
|---|---|---|
| stream write deadline（`7e6596728`） | 本地架构从未设置 WriteTimeout（main.go 注释明确），不存在上游那种“每次写固定 30s 切断慢客户端”的缺陷形态。移植需新增预算机制与 SetWriteDeadline，属于行为增强而非修复。 | 如需要保护慢客户端，可在独立任务中设计并实验预算策略。 |
| Responses false disconnect（`0c2af9780`） | 本地已有 sr.Done() + IsNormalEnd 处理，行为是否等价需更深度复现。 | 如出现具体 bug 再针对性分析。 |
| keepalive handoff race（`25372e30b`） | local pinger & scanner 使用不同 mutex；理论存在竞争但未复现。 | 在 `-race` 下压测后决定是否修补。 |
| OAuth 身份列并发唯一（11.7） | 审计 doc 给出明确的"不修原因"：指针改造（empty string → NULL）波及全库，且 SQLite 无 ALTER COLUMN；半途修复会破坏三库兼容性。 | 作为架构级任务单独立项规划。 |
| 文档中已过时的部分（如 11.7 OAuth concurrency） | 这些条目是历史决策的长期记录，不应被“修掉”。 | 保持归档，按需更新结论行。 |
| json migration | 已由 `1c02b5c7b` 完成（业务代码 0 处直接调用）；`dto/openai_request.go` 剩的 7 处在块注释死代码里，不影响运行。 | 可清理死代码但不影响功能。 |

## 验证记录（本轮实跑并读取输出）

后端：
- `go build ./...` exit 0
- `go vet ./...` exit 0
- `go test ./service ./controller ./middleware ./common ./relay/channel/claude -count=1` all pass
- `GOTOOLCHAIN=go1.27.3 govulncheck ./...` 0 个可达漏洞（前置依赖升级已验证）

前端：未变动，保持之前验证结果。

## 部署提示

- 容器重建前请确认 `performance_setting.monitor_enabled` 期望值（dev/prod）。若启用，确保容器 cgroup 可用（Docker Compose 默认已启用 v2 限制）。
- Redis 重启或主从切换后不再需要重启 new-api：NOSCRIPT 回退机制自动恢复脚本加载。
- ratio_sync 前端差异表显示可能变化：不带 success 字段的静态载荷此前被记录为 error，现在转为 success+differences。

## 参考链接

- CR-DEPS-LATEST-RELEASE-2026-10-24.md（依赖升级）
- CR-AUDIT-ROUND-2026-10-19.md（问题发现与评估）
- CR-MULTI-ROUND-AUDIT-2026-10-16.md（详细清单）
