# CR: model_mapping 字面量 null 缺陷修复与静默失败诊断

- 日期：2026-09-22
- 范围：`model/channel_mapping.go`、`model/ability.go`
- 状态：代码修复完成并通过单元测试；生产数据未改动

## 缺陷

`channels.model_mapping` 存字面量 `null` 时，`parseChannelModelMappingEntries`
走 JSON token 解析，顶层 token 是 `null` 而非 `{`，直接返回
`unmarshal_model_mapping_failed`，被包装为 `ErrChannelModelMapping`。

后果分两层：

1. **渠道被静默排除出路由**。该渠道不再作为该模型的候选，请求落到
   `formatNoAvailableChannelErrorMessage`，返回 503
   `ErrorCodeGetChannelFailed`，文案是"分组 X 下模型 Y 的可用渠道不存在（retry）"。
   运维看到的是"没渠道"，真实原因是某个渠道存了脏 mapping。
2. **V2 路由迁移被永久阻塞**。`ApplyRouteFieldsBackfill` 在
   `len(MappingConflicts) > 0` 时 fail closed，返回
   `ErrRouteScopeMigrationConflicts`，迁移无法完成。

## 触发条件与现状

生产 `channels` 表共 78 行，`model_mapping` 取值分布：

| 形态 | 行数 | 说明 |
| --- | --- | --- |
| `has-content` | 33 | 正常映射 |
| `empty` (`''`) | 32 | 规范的空值形态 |
| `literal-null` | 12 | **缺陷数据** |
| SQL `NULL` | 1 | GORM 扫成 `""`，无害 |

12 行字面量 `null` 中仅 4 行是启用态（id 35、36、37、72），其余 8 行为
`status=2`（禁用）。全部为 `len=4`，即恰好是 `null` 四个字符。

**当前无用户可见影响**：查询"仅由这 4 个启用脏渠道提供、无任何干净渠道兜底"
的模型，返回 0 行。即每个模型都还有其它可用渠道。

**为什么现在是潜伏而非活跃故障**：生产 `CHANNEL_ROUTING_V2_ENABLED=false`，
且 `channel_route_scopes` 表**不存在**（迁移从未执行），因此请求走 legacy 缓存路径。
legacy 路径在映射错误时会置 `cacheComplete=false` 并回退数据库选择器，不会
静默丢弃候选。只有在以下情况才会命中缺陷路径：

- 打开 `CHANNEL_ROUTING_V2_ENABLED`
- 或某个模型**只**挂在 35/36/37/72 上

## 七个处理点的语义不一致

全仓对 `IsChannelModelMappingError` 有 7 处处理，修复前语义不统一：

| 位置 | 处理方式 | 可诊断性 |
| --- | --- | --- |
| `channel_route_scope.go:595` | 跳过 + `SysError` | 有日志 |
| `channel_route_scope.go:636` | 跳过 + `SysError` | 有日志 |
| `channel_cache.go:429` | `cacheComplete=false` 回退 DB | 有信号 |
| `channel_cache.go:488` | `cacheComplete=false` 回退 DB | 有信号 |
| `channel_health.go:908` | 专用 `InvalidRoute` 决策 | 有专门处理 |
| `ability.go:429` | `continue` | **完全静默**（本轮已补日志） |
| `channel_route_selector.go:481` | `continue` | **完全静默**（本轮已补日志） |

`loadRouteCandidateSelections` 里静默 `continue` 会让该渠道从候选集消失，函数
返回 `(nil, nil)`，与"该模型本就没有渠道""渠道被禁用"**完全同形**，调用方
无法区分。

## 修复

三处，共 17 行新增、0 行删除。

### 1. 字面量 null 归一化为"无映射"（`model/channel_mapping.go`）

```go
if strings.EqualFold(modelMapping, "null") {
    return nil, nil
}
```

理由：SQL `NULL` 经 GORM 已扫成 `""`，`""` 是既有的规范空值形态（生产 32 行），
字面量 `null` 与它们语义相同，都属于"没有映射"，不是"非法映射"。修复范围
**精确限定在这一个 token**，不放宽整体语法。

### 2. 静默跳过补诊断（`model/ability.go`、`model/channel_route_selector.go`）

```go
// model/ability.go（legacy 数据库路径）
common.SysError(fmt.Sprintf("route candidate skipped invalid model mapping: group=%s model=%s channel_id=%d error=%v", ability.Group, ability.Model, ability.ChannelId, mappingErr))

// model/channel_route_selector.go（V2 数据库路径）
common.SysError(fmt.Sprintf("route candidate skipped invalid model mapping: group=%s model=%s channel_id=%d error=%v", group, candidate.model, channel.Id, mappingErr))
```

保留"丢弃候选"的正确行为（mapping 非法则不可信），只补上日志，使"因映射非法
而跳过"与"本来没渠道"可区分。两条路径都要补，否则同一缺陷在 V2 开启/关闭时
的诊断能力不一致。

## 验证

新增测试 4 个文件，全部先验证"撤销修复即失败"。

```text
go build ./...      exit=0
go vet ./...        exit=0
go test ./model/    ok  7.286s
go test ./relay/... ./service/ ./controller/   全部 ok
```

### 新增测试与反向验证

| 测试 | 撤销修复后 |
| --- | --- |
| `TestParseChannelModelMappingAcceptsLiteralNull` | FAIL（`unmarshal_model_mapping_failed`） |
| `TestResolveChannelModelMappingTreatsLiteralNullAsEmpty` | FAIL（同上） |
| `TestBuildRouteCandidateCacheKeepsChannelWithLiteralNullMapping` | FAIL |
| `TestLoadRouteCandidateSelectionsLogsInvalidMapping` | FAIL（日志为空串） |
| `TestRouteV2EntriesFromDatabaseLogsInvalidMapping` | FAIL（日志为空串） |

反向验证命令：临时移除对应修复代码后重跑，确认测试失败，再恢复。

### 修复不放松语法边界

`TestParseChannelModelMappingStillRejectsNonObjectJSON` 覆盖 `[]`、`"null"`
（带引号的字符串）、`123`、`true`、`["a","b"]`、`{`、`not json` 七种输入，
全部仍被拒绝。`TestBuildRouteCandidateCacheStillDropsMalformedMapping` 确认
真正畸形的 mapping（`{"broken":`）仍被丢弃，且现在会留下日志。

## 端到端验证（隔离环境）

dev 环境启用 `CHANNEL_ROUTING_V2_ENABLED=true`，用渠道 9213（`agnes-*` 五个模型，
该模型仅此一个渠道）做同渠道、同请求、仅 `model_mapping` 不同的对照。每次改库后
重启进程，使 `InitChannelCache` 重建缓存——**这一步是必须的**，见下方"测法陷阱"。

| mapping | HTTP 响应 | 内部归属 | 错误类型 |
| --- | --- | --- | --- |
| `'null'`（已修复） | 503，渠道被选中并转发上游 | `channel error (channel #9213, status code: 503)` | `AgnesAI_error`（上游透传） |
| `'{"broken":'`（畸形） | 503，本地判定无可用渠道 | `No available channel for model ... (distributor)` | `new_api_error` |

判据是**内部归属**而非响应体：两种情况下响应体都含
`No available channel for model agnes-1.5-flash under group default (distributor)`，
因为渠道 9213 自身是链式 new-api，它把上游的同类错误原样透传。只有
`channel error (channel #9213, ...)` 这一行才能证明渠道被选中并转发。

路由域层对照（`/api/channel-route-scopes/migration/preview`）：

| mapping | `scope_count` |
| --- | --- |
| `''`（基线） | 2185 |
| `'null'` | 2185（与基线一致） |
| `'{"broken":'` | 2180（少 5，正好是渠道 9213 的 5 个模型） |

畸形 mapping 还产生 5 行诊断日志，逐模型指明渠道与原因：

```text
[SYS] failed to resolve model mapping for route cache: channel_id=9213
      group=default requested_model=agnes-1.5-flash
      error=channel model mapping configuration error: unmarshal_model_mapping_failed
```

### 测法陷阱（本轮踩到并更正）

首轮中继对照得出"两种 mapping 结果相同"的错误结论，原因是：

1. dev 设了 `REDIS_CONN_STRING`，`main.go:82-84` 在 Redis 可用时强制
   `common.MemoryCacheEnabled = true`；
2. dev 的 `CHANNEL_CACHE_SYNC_ENABLED=false`，渠道缓存**永不刷新**；
3. 直接改数据库不会进入内存缓存，中继读到的仍是进程启动时的旧快照。

因此任何"改库 → 直接发中继请求"的对照都无效，必须重启进程让缓存重建。这一点
同样适用于生产排查。

## 未覆盖与边界

- **生产数据未改动**。12 行字面量 `null` 仍在生产库中。修复后新代码会正确
  处理它们，但历史数据本身未归一化。
- **未在生产验证**。生产 `CHANNEL_ROUTING_V2_ENABLED=false`，缺陷路径不活跃，
  无法在生产观察修复前后的行为差异。上表全部来自隔离环境。
- **七个处理点本轮全部补齐日志**，但只对 `ability.go:429`、
  `channel_route_selector.go:481` 两处新增了日志；其余五处原本已有日志或
  已有专门信号，未改动。
- **根因未确证**。判断为 2026-04-07~11 期间的历史遗留写入，当前代码不存在
  产生该值的写入方，但未定位到具体引入提交。
- 单测用 SQLite 内存库（`openTestDB`），未覆盖 PostgreSQL/MySQL 的列类型差异；
  本缺陷不依赖方言，风险低，但未在真实三库上跑过。
