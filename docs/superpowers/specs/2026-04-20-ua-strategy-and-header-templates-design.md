# UA 策略与请求头模板设计

日期：2026-04-20

## 控制合同

- Primary Setpoint：渠道级和标签级都能配置多选 `User-Agent`，并在真实转发链路中按 `轮询` 或 `随机` 策略生效。
- Acceptance：
  - 渠道编辑与标签编辑都支持多选 UA、策略选择、优先级选择。
  - 请求真正发往上游时，`User-Agent` 按配置池和策略选出最终值。
  - 标签级规则与渠道级规则可按系统默认或局部覆盖的优先级决策。
  - 用户可保存“完整请求头 JSON 模板”为私有模板，并在后续编辑中复用。
  - 非法模板和非法请求头内容不给生效，且给出明确提示。
- Guardrails：
  - 普通 `header_override` 静态覆写能力保持可用。
  - 不把 UA 策略做成任意请求头规则引擎。
  - 不允许静默回退、静默忽略非法配置、或生成假成功链路。
  - 必须兼容 SQLite、MySQL、PostgreSQL。
- Sampling Plan：
  - 先做纯逻辑测试和 API 测试，再做前端交互验证，最后做 `new-api -> 上游` 的真实链路验证。
- Rollback Trigger：
  - 普通 `header_override` 回归。
  - 渠道/标签优先级失真。
  - 多实例或多请求场景下轮询出现池外值、空值或双重推进。

## 当前代码事实

### 现有渠道配置

- 渠道已有独立的 `header_override` 字段：`model.Channel.HeaderOverride`。
- 渠道还有 `settings` JSON 字段，对应 `dto.ChannelOtherSettings`，适合承载新的结构化策略配置。
- 渠道已有多 key 轮询状态：`ChannelInfo.MultiKeyPollingIndex`，说明仓库已经接受“运行时游标状态”这类设计。

### 现有标签编辑

- 当前没有独立的 `Tag` 模型或 `tag` 表。
- `EditTagModal` 对应后端 `PUT /api/channel/tag`，本质是按 tag 批量修改命中的渠道。
- 这意味着“标签编辑”目前只是批量写渠道字段，而不是一个真正的标签级运行时实体。

### 由代码事实推导出的设计约束

- 若继续把标签请求头能力实现成“批量写渠道字段”，就无法支撑：
  - 标签级独立优先级。
  - 标签级独立 `User-Agent` 池。
  - 标签级独立轮询游标。
- 因此，本功能必须新增“标签级请求头策略存储层”，不能只复用现有批量修改语义。

## 目标与非目标

### 目标

- 为渠道和标签提供统一语义的 `User-Agent` 策略配置。
- 在真实运行时基于策略选出最终 `User-Agent` 并写入标准请求头。
- 提供“当前用户私有”的完整请求头模板能力。
- 提供严格的格式校验和明确错误提示。

### 非目标

- 不扩展到任意 header 的多值轮询/随机。
- 不提供全局公共模板市场。
- 不在本轮引入复杂的表达式规则引擎。
- 不修改现有模型映射、分组、模型列表等非请求头相关的标签批量编辑语义。

## 总体方案

采用“三层配置 + 一层运行时状态”的方案：

1. 渠道级请求头配置
   - 继续使用渠道已有 `header_override`。
   - 在 `dto.ChannelOtherSettings` 中新增结构化 `ua_strategy` 以及优先级相关字段。

2. 标签级请求头策略
   - 新增独立表 `tag_request_header_policies`。
   - 该表承载标签级 `header_override`、`ua_strategy`、局部优先级覆盖。

3. 系统默认优先级
   - 使用现有 `Option` / `OptionMap` 机制保存全局默认值。

4. 运行时游标状态
   - 新增独立表 `request_header_strategy_states`。
   - 统一承载渠道级和标签级 `round_robin` 游标，避免把状态混入配置表。

## 数据模型

### 渠道级 `ua_strategy`

新增到 `dto.ChannelOtherSettings`：

- `HeaderPolicyMode string`
  - 值：`system_default` / `prefer_channel` / `prefer_tag` / `merge`
- `OverrideHeaderUserAgent bool`
  - 是否用策略结果覆盖静态 `header_override.User-Agent`
- `UserAgentStrategy *UserAgentStrategy`

结构 `UserAgentStrategy`：

- `Enabled bool`
- `Mode string`
  - 值：`round_robin` / `random`
- `UserAgents []string`

### 标签级策略表

新增模型 `TagRequestHeaderPolicy`：

- `Tag string`
  - 主键，长度与现有渠道 `tag` 对齐
- `HeaderOverride string`
  - 标签级静态请求头覆写
- `HeaderPolicyMode string`
- `OverrideHeaderUserAgent bool`
- `UserAgentStrategyJSON string`
  - 存结构化 JSON
- `CreatedAt int64`
- `UpdatedAt int64`

说明：

- 该表只管理“请求头相关”的标签级策略。
- `EditTagModal` 中模型、分组、参数覆盖等既有 bulk edit 行为保持原样。
- `EditTagModal` 中“请求头覆盖”和“UA 策略”改为读写此表，而不是批量改渠道字段。

### 运行时状态表

新增模型 `RequestHeaderStrategyState`：

- `ScopeType string`
  - 值：`channel` / `tag`
- `ScopeKey string`
  - 渠道使用 `channel:<id>`，标签使用 `tag:<tag>`
- `RoundRobinCursor int64`
- `Version int64`
- `UpdatedAt int64`

联合主键：

- `ScopeType`
- `ScopeKey`

用途：

- 只为 `round_robin` 模式保存游标。
- `random` 模式不写该表。
- 通过 `Version` 做乐观并发控制，避免多实例下游标乱跳。

### 用户私有模板表

新增模型 `UserHeaderTemplate`：

- `Id int`
- `UserId int`
- `Name string`
- `Content string`
- `CreatedAt int64`
- `UpdatedAt int64`

约束：

- `UserId + Name` 唯一。
- 只保存合法、已通过校验的完整 JSON 对象模板。
- 应用模板时仍做二次校验，防止历史脏数据或手工写库。

## 系统默认项

新增系统配置项，使用现有 `Option` 机制：

- `RequestHeaderPolicyDefaultMode`
  - 默认值：`prefer_channel`

系统设置页新增对应的管理员入口。渠道和标签都可以选择：

- 跟随系统默认
- 渠道优先
- 标签优先
- 合并

## 运行时决策链

每次请求真正发往上游前，按以下顺序处理：

1. 读取渠道现有静态 `header_override`。
2. 若渠道有 tag，则查询该 tag 的 `TagRequestHeaderPolicy`。
3. 读取系统默认优先级。
4. 结合渠道局部配置和标签局部配置，算出最终优先级模式。
5. 得到最终静态请求头源：
   - `prefer_channel`：优先渠道静态 header。
   - `prefer_tag`：优先标签静态 header。
   - `merge`：标签为基底，渠道覆盖冲突键。
6. 得到最终 `UserAgentStrategy`：
   - `prefer_channel`：优先渠道策略。
   - `prefer_tag`：优先标签策略。
   - `merge`：UA 池按“标签在前、渠道在后”合并去重；策略模式以高优先级一方为准。
7. 若最终策略启用：
   - `round_robin`：读取并推进 `request_header_strategy_states` 中对应 scope 的游标。
   - `random`：从最终 UA 池中随机选择。
8. 将选出的 UA 写入标准 `User-Agent`。
9. 若策略未启用，则保留静态 header 中的 `User-Agent` 或保持现有行为。

说明：

- 本功能只负责标准 `User-Agent` 头，不扩展到任意自定义头。
- 这更符合真实使用场景，也避免把功能做成失控的规则引擎。

## 标签编辑语义调整

`EditTagModal` 拆成两块语义：

1. 现有 bulk edit 区
   - 模型
   - 模型映射
   - 分组
   - 参数覆盖
   - 其他已有标签批量字段

2. 新的标签级请求头策略区
   - 标签级 `header_override`
   - 标签级 `ua_strategy`
   - 标签级优先级覆盖

结论：

- `EditTagModal` 的“请求头覆盖”不再是“把同一 JSON 批量写进所有渠道”。
- 它变成真正的标签级策略配置。
- 这是本功能成立的必要行为变更，必须在 UI 文案和帮助说明中说清楚。

## 前端交互

渠道编辑和标签编辑都新增独立区块：`User-Agent 策略`

字段：

- 是否启用 UA 策略
- 策略模式：`轮询` / `随机`
- 预置多选
- 自定义 UA 输入
- 已选 UA 列表
- 优先级模式
- 是否覆盖静态 `User-Agent`

请求头模板区：

- 保存当前 `header_override` 为模板
- 查看当前用户模板列表
- 应用模板
- 删除模板

模板只作用于 `header_override`，不承载 `ua_strategy`。

## 校验规则

### `header_override`

- 必须是合法 JSON。
- 必须是 JSON 对象。
- key 不能为空字符串。
- value 必须是字符串、数字或布尔值。
- 非法时可以留在编辑区继续修改，但“保存模板”和“应用模板”都不生效。

### `ua_strategy`

- 启用时，`UserAgents` 不能为空。
- 自定义 UA 需要 `trim`、去重、去空字符串。
- `Mode` 必须是允许的枚举。
- 合并后若 UA 池为空，则整条策略视为无效。

### 模板

- 保存前必须通过 `header_override` 全量校验。
- DB 中不保存非法模板。
- 应用模板时再次校验；若失败，保持当前编辑区不变并提示。

## API 设计

### 复用与扩展现有渠道接口

- 渠道更新接口继续复用现有更新 API。
- `ua_strategy` 与 `HeaderPolicyMode` 进入渠道 `settings` JSON。

### 标签策略接口

新增：

- `GET /api/channel/tag-policy?tag=<tag>`
- `PUT /api/channel/tag-policy`

其中 `PUT` 只处理：

- `tag`
- `header_override`
- `ua_strategy`
- `header_policy_mode`
- `override_header_user_agent`

这样可避免把“标签 bulk edit”与“标签运行时策略”混成一个高耦合控制器。

### 用户模板接口

新增：

- `GET /api/user/header-templates`
- `POST /api/user/header-templates`
- `PUT /api/user/header-templates/:id`
- `DELETE /api/user/header-templates/:id`

权限：

- 仅当前登录用户可见自己的模板。
- 不提供管理员跨用户查看入口。

## 并发与一致性

`round_robin` 不依赖单进程内存游标。

实现口径：

- 读状态行。
- 根据 `Version` 做乐观更新。
- 更新失败则重试有限次数。
- 仍失败则显式报错，不静默回退到 `random` 或固定值。

这样兼容：

- SQLite
- MySQL
- PostgreSQL

并且支持多实例部署。

## 验证策略

### L0 纯逻辑测试

- 优先级解析
- 标签/渠道合并
- UA 池去重
- 轮询索引推进
- 模板 JSON 校验

### L1 API 测试

- 用户模板 CRUD 与权限隔离
- 标签策略 CRUD
- 非法 payload 被拒绝

### L2 运行时集成测试

- 渠道级轮询
- 标签级轮询
- 合并模式
- `prefer_channel`
- `prefer_tag`
- `random` 只从池内取值

### L3 真实链路验证

- 使用隔离开发环境
- 让请求经过 `new-api -> 上游`
- 观察真实发出的 `User-Agent`
- 至少验证渠道级、标签级、随机、轮询四类场景

## 风险与缓解

- 风险：标签请求头语义变化可能让老用户误以为仍是 bulk edit。
  - 缓解：UI 上明确标注“标签级运行时策略”，并在发布说明中写清。

- 风险：多实例下轮询状态竞争。
  - 缓解：使用独立状态表 + 乐观并发更新。

- 风险：历史同 tag 渠道可能已有不同 `header_override`，无法自动推导唯一标签级规则。
  - 缓解：不做自动回填；新标签策略由用户显式保存生效。

- 风险：模板数量持续增长。
  - 缓解：按用户维度分页，限制单用户模板数量和名称长度。

## 最终建议

本功能按以下边界落地：

- 只把“动态多 UA 策略”做到 `User-Agent`，不泛化到任意头。
- 用独立 `TagRequestHeaderPolicy` 解决标签级运行时语义缺失问题。
- 用独立 `RequestHeaderStrategyState` 解决多实例轮询状态一致性问题。
- 模板只服务于静态 `header_override`，按用户私有隔离。

这是当前仓库结构下，最小且可验证的真实落地方案。
