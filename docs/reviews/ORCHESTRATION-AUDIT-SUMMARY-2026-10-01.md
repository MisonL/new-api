# 编排式复核总结：管理员豁免全局限流

**复核日期**: 2026-10-01
**编排运行**: `run_7b7371d11c39`（Orca）
**复核对象**: 管理员豁免全局限流功能及其后续修复
**复核状态快照**: 工作区 HEAD 已包含 `02ab2cd0f`、`104a9ae44` 等修复

---

## 1. 复核范围与方式

通过 Orca 编排在同一 Run 下派出 5 个只读复核 worker，每个负责一个维度。
所有 worker 的约束一致：只读、不改仓库文件、每条结论必须附 `file:line` 或命令输出、
必须在 `/tmp` 独立复现而非静态推理。

| 任务 ID | 维度 | 状态 | 真实耗时 | 结论摘要 |
|---------|------|------|---------|---------|
| `task_3b04fc9fc48f` | 信任代理实现 | completed | 约 10 分钟 | 逻辑正确，测试全覆盖，无 P0 |
| `task_20e20c78b211` | 限流豁免中间件 | completed | 约 41 分钟 | 豁免逻辑正确，测试对文件外限流器有盲区 |
| `task_7f796f9354b5` | i18n 与选项归一化 | completed | 约 22 分钟 | 两处修复已验证，发现同类残留一处 |
| `task_917d6609e194` | 前端改动 | completed | 约 42 分钟 | 无缺陷，发现一处既有的无关缺陷 |
| `task_6f76493eeea5` | compose 透传审计 | **failed** | 约 1 分钟 | **未产出任何结论** |

耗时取自 `orca orchestration worker-show` 的 `dispatchedAt` / `completedAt`，不是估算。

**重要边界**：compose 透传维度（`task_6f76493eeea5`）启动后约 1 分钟即失败，
没有产出报告。因此本文档不含任何来自该维度的结论。compose 与 `.env.example`
相关的问题由本会话另行独立复现和修复，不属于本轮编排复核的产出，见
`CR-ADMIN-RATE-LIMIT-EXEMPT-2026-09-29.md`。

---

## 2. 各维度真实结论

### 2.1 信任代理实现（`task_3b04fc9fc48f`）

worker 原始结论（原文照录）：

> 已完成 TRUSTED_PROXIES 信任代理的独立复核。所有输入边界（空串、auto、逗号列表、无效条目）
> 行为已验证，裸机/容器两条分支逻辑正确，容器判定依据可靠（标记文件+cgroup 片段），
> ownIPv4Subnets 过滤规则覆盖 VPN/多网桥/--network-host。6 个变异测试全部被捕获，
> 测试独立于生产常量，无自引用盲区。无 P0 缺陷，有一个文档级建议：VPN 网卡会被包含，
> 若 VPN 对端可达容器则可伪造 X-Forwarded-For。

要点：

- 无 P0 缺陷。
- 6 个变异全部被捕获，测试不再自我引用 `loopbackProxies` 常量。
  这与本会话早先在 `02ab2cd0f` 中修复的盲区一致——worker 复核的是修复后的状态。
- 唯一建议是文档级：VPN 网卡会进入信任集合，若 VPN 对端可达容器则可伪造 XFF。
  该建议已写进 `.env.example` 的 `TRUSTED_PROXIES` 安全边界说明。

### 2.2 限流豁免中间件（`task_20e20c78b211`）

worker 原始结论（原文照录）：

> 只读复核了管理员豁免全局限流的 7 个提交，在 /tmp 复制仓库做了 6 组变异实验，
> 未改动仓库任何文件。结论：exemptableLimiter 恰好包装 GlobalWeb/GlobalAPI/Search/Suggestion
> 四个限流器，CriticalRateLimit 未被包装（登录/注册/找回密码/支付仍受保护），
> adminRateLimitExempt 在 session 缺失/role 非 int/status 非 int/账号禁用时全部 fail closed，
> main.go:219 引擎级 session 先于 router/api-router.go:21 的 group 级限流器执行，
> 静态资源短路顺序与重构前行为等价（192/192 组合矩阵逐行相同），11 轮 -shuffle 全绿。
> 发现一处测试覆盖盲区：TestEveryExportedLimiterIsDocumented 只解析 rate-limit.go，
> 对 EmailVerificationRateLimit、ModelRequestRateLimit 等其它文件里的限流器无感知，
> 变异 E/F 证明新增或误包装这类限流器时两个测试都不会失败（原文均为 ok）；
> 这是测试盲区而非运行缺陷。工作区未提交的 abortRateLimited 重构只改限流命中后的响应形态，
> 不触及豁免逻辑，不影响以上任何结论。

要点：

- 豁免逻辑本身正确，无运行缺陷；fail-closed 的四条路径都被独立验证。
- 真实盲区：`TestEveryExportedLimiterIsDocumented` 只解析 `middleware/rate-limit.go`，
  不覆盖 `model-rate-limit.go`、`email-verification-rate-limit.go` 里的限流器。
  属于测试盲区，不是运行缺陷。
- 确认工作区另一会话未提交的 `abortRateLimited` 重构与豁免逻辑正交。

### 2.3 i18n 与选项归一化（`task_7f796f9354b5`）

worker 原始结论（原文照录）：

> 只读复核了 83a134885（i18n 未初始化回退）与 3a273494a（admin-exempt 归一化），
> 全部结论在 /tmp 用外部模块 + go test -overlay 独立复现，未改动仓库任何文件。
> A 组：修复后 Translate/T/GetLocalizer 在 bundle 缺失时返回 key/返回 nil；
> 修复前实测 nil pointer panic（栈顶 localizer.go:193）；Init 的 sync.Once 使第二次调用
> 返回 nil 而非首次 error，损坏 overlay 会造成 bundle 非 nil 但 localizers 为空的半初始化；
> 回归测试单跑/全包/-shuffle 均 PASS，换回修复前 i18n.go 则 SIGSEGV FAIL，是真实回归测试。
> B 组：normalize 的返回值确实被用于校验、入库与 updateOptionMap（被丢弃的只是 validate
> 内部自算值），修复后 TRUE/True/1/t 等入库均为 true 且运行时生效；修复前遗留大写值重启后
> OptionMap 与运行时不一致（'1' 会造成 UI 显示开启但功能不生效），修复后 DB 保留历史原值
> 但读入内存即归一；NormalizeRateLimitAdminExempt 两处调用点均已接入管线，非死代码。
> 残留：validate-only 的 log_retention_setting 布尔/整数项在输入带首尾空白时与 Fix B 同源
> （校验通过但消费端未生效），其余 validate-only 键（billing_mode/expr、tool_price prices、
> RequestHeaderPolicyMaxMode）无此问题。

要点：

- `83a134885` 与 `3a273494a` 两处修复都经独立复现验证有效。
- 换回修复前版本会 SIGSEGV FAIL，说明回归测试是真实的，不是摆设。
- 残留同类问题一处：`log_retention_setting` 的 validate-only 布尔/整数项在输入带首尾
  空白时校验通过但消费端未生效。这是与本轮修复同源的问题，未在本轮修复。

### 2.4 前端改动（`task_917d6609e194`）

worker 原始结论（原文照录）：

> 只读复核了 classic SettingsRequestRateLimit.jsx 与 web/default request-limits/，
> 逐条独立复现 5 个问题：字段清单含该键、两个保存按钮按字段收窄（MODEL 5 个 / GLOBAL 1 个）、
> compareObjects 布尔路径类型一致、props.options 缺键时 setValues 不再重置 Switch
> （104a9ae44 已修，HEAD 祖先已确认）、default 侧字段名与 schema 一致。
> 追踪两条提交链路证明后端兼容：classic 发字符串 "true"、default 发 JS 布尔 true，
> axios 1.19.0 实测序列化不同，但 controller/option.go:116 走 common.DecodeJson 而非
> PostForm/ShouldBind，bool 与 string 都经 Interface2String 归一后由 strconv.ParseBool 接受
> （Go 探针四种形态全部 err=<nil>）。i18n 新增键在 classic 7 语言与 default 6 语言均存在
> （default 的 fr/ja/ru/vi 键值仍为英文原文），CR 文档的前端段落有真实浏览器交互描述
> 但仓库内无可复现脚本或产物，且该验证早于 104a9ae44，未覆盖 options 未加载路径。
> 唯一发现的真实缺陷是既有的数字字段类型错配（加载完成前触碰数字框即发 PUT，
> 与本次改动无关），无待办事项，未修改仓库任何文件。

要点：

- 前端改动本身无缺陷，`104a9ae44` 的修复被确认为 HEAD 祖先并有效。
- 两套前端提交格式不同（字符串 vs 布尔）但后端都兼容，经 Go 探针实测。
- 既有的无关缺陷：数字字段类型错配，加载完成前触碰数字框即发 PUT。与本次改动无关，
  未在本轮修复。
- 遗留项：`web/default` 的 fr/ja/ru/vi 翻译仍是英文原文。

---

## 3. 本轮编排复核的净结论

| 项 | 结论 |
|----|------|
| 豁免逻辑正确性 | 正确，fail-closed，无运行缺陷 |
| 信任代理实现 | 正确，无 P0，测试无自引用盲区 |
| i18n 与选项归一化修复 | 两处均已验证有效 |
| 前端改动 | 无缺陷 |
| 编排层面的真实产出 | 4 个维度完成，1 个维度（compose）失败无产出 |

发现的真实遗留项（均为低优先级，非本轮功能缺陷）：

1. ~~`TestEveryExportedLimiterIsDocumented` 只解析单文件，跨文件限流器无覆盖（测试盲区）。~~
   **已修复**（提交 `b0f0bfb4e`）：枚举改为扫描所有引用限流原语的文件，exempt/limited 两组集合
   同时驱动行为探针。变异验证：把 `EmailVerificationRateLimit` 包进 `exemptableLimiter` 报
   `got 200`，摘掉 `SearchRateLimit` 的包装报 `got 401`。
2. ~~`log_retention_setting` 的 validate-only 项在带首尾空白时与本次归一化修复同源。~~
   **已修复**（同一次提交）：`ValidateValue` 先 trim 再校验、而入库用的是未 trim 的值，
   `strconv.ParseBool(" true ")` 失败后静默 `continue`。新增 `NormalizeValue` 返回规范形式并
   接入选项管线。真实 HTTP 实测：`PUT /api/option/` 写 `" true "` 落库为 `true`。
3. ~~`web/default` 的 fr/ja/ru/vi 翻译仍为英文原文。~~ **部分修复**（提交 `d871b6d73`）：
   补译 94 条字符串（fr 35、ja 3、ru 3、vi 53），集中在渠道路由 Scope 工作台与管理员
   限流豁免开关。当时的顾虑（这三个文件正被另一会话大规模改写）经核查成立，因此只用
   index-only 方式暂存 HEAD 已存在的键，工作区未改动，未与另一会话的在途键冲突。
   **残留（实测，非推测）**：以该提交自身的代码树运行 `i18n:sync`，四个 locale 均仍报
   未翻译——fr 5、ja 36、ru 35、vi 5；其中 ja 与 ru 另各缺 9 个键（en.json 有、这两个
   locale 没有）。这些键属于同批新增的渠道路由 Scope 工作台，译文已存在于工作区，但
   属于另一会话的在途改动，本提交未纳入。**因此 HEAD 上这四个 locale 尚未翻译完整，
   该提交的信息初稿曾误称 "i18n:sync reports zero untranslated entries"，已更正。**
4. ~~既有的数字字段类型错配（前端，与本次改动无关）。~~ **未复现，撤回**：审计描述的是
   「加载完成前触碰数字框即发 PUT」，但 `web/default` 与 `web/classic` 两套前端的数字输入
   都没有 `onChange` 直接触发保存、也没有值变化时保存的 effect，提交都走显式 Save 按钮。
   该描述与代码不符，不作为遗留项。

**顺带发现并修复（超出审计点名范围）**：审计只点了 6 个支付类 `*Enabled` 键，实测发现
`updateOptionMap` 用 `value == "true"` 消费**整个布尔选项家族**，而接口接受任何
`strconv.ParseBool` 认得的拼写，于是 `"True"`/`"1"`/`" t "` 都是存进去、报成功、运行时读回
false。已用 `isBooleanOptionKey` 统一归一化（提交 `de563706d`），真实 HTTP 端到端验证。


---

## 4. 记录边界

- 本文档只记录 `run_7b7371d11c39` 中 worker 实际产出的结论，结论内容照录 worker 报告。
- compose 透传维度失败，其相关问题（`TRUSTED_PROXIES` 默认值、`CRYPTO_SECRET` 声明、
  healthcheck 动态端口、`STREAMING_TIMEOUT`/`SQLITE_PATH` 透传）由本会话独立复现并修复，
  记录在 `CR-ADMIN-RATE-LIMIT-EXEMPT-2026-09-29.md`，不归入本轮编排产出。
- worker 耗时取自 Orca dispatch 时间戳，不是估算值。
