# CR-DEV-SERVICE-AUDIT-2026-10-08

最新源码与已部署隔离开发服务的全维度、全场景审查。

方法：6 个只读子代理（运行时与部署、提交 delta、在途改动红队、计费配额、
前端与 i18n、接口场景）分头取证，主代理逐条独立复现；子代理结论一律先
复现再采信，被推翻或收窄的结论单列第 4 节。全程未修改仓库、未写数据库、
未触碰生产容器 `new-api`。

## 1. 审查对象与范围

- 源码：分支 `dev`，时点 HEAD `fa211e334`；已部署基线提交
  `3e02d3fea`（旧开发镜像），delta 为 `3e02d3fea..fa211e334`（62 提交 /
  86 文件）；工作区另有其他会话的在途改动（快照时 369-502 项）。
- 服务：镜像 `new-api-local:dev`（`--build-info` = version v1.1.0、
  commit `4ba36471daec...-dirty`、date 2026-10-08T04:06:17Z），容器
  `new-api-dev-isolated-new-api-1`（端口 3001），同 compose 的
  `postgres:15`（15.17）与 `redis`。
- 采样时间 2026-10-08。在途改动随时变化，本文结论对应上述快照。

## 2. 部署事实（已验证）

- 容器 healthy、`RestartCount=0`、`OOMKilled=false`；挂载
  `.dev-docker/runtime/isolated/{data,logs}`，均 bind RW；`.dev-docker`
  已被 `.git/info/exclude` 忽略，不污染工作区。
- 启动日志：`migration started/finished main 1.754s`、Redis enabled、
  channels synced，窗口内无 error/warn/panic。数据库里唯一的 error 是审查
  探针自身写错的 SQL（`column "tablename" does not exist`），与服务无关。
- 端点：`GET /api/status` 200 JSON；`/api/healthz` 200 `ok`；
  `/api/pricing` 200 JSON；`/api/models` 401（未登录，符合鉴权）；
  `/api/uptime/status` 200；根页与全部被引用静态资源 200。
- 未认证 `POST /v1/chat/completions`（空 body、坏 JSON、正常小 body、
  64 KiB 受控 body）均在认证阶段返回 401 `authentication_error`，未触发
  上游；说明认证先于 body 解析。
- PostgreSQL 只读：库连通，核心表存在，`users 10 / channels 22 /
  tokens 17`；Redis 未认证访问返回 `NOAUTH`，认证后 `PONG`。
- 新鲜度：容器 BuildCommit `4ba36471d-dirty` 比当前 HEAD `fa211e334`
  落后 1 个提交；该提交只改 `tools/relaykit-dto-compat/`、
  `scripts/relaykit-*`、以及 `docs/superpowers/specs/` 设计文档，不触及
  Go 服务与前端产物，服务行为与 HEAD 等价。dirty 标记来自工作区里其他
  会话的在途改动，属预期。

## 3. 确认的缺陷与记录项

### 3.1 P1（计费正确性，配置门控）：Realtime WS 的 tiered 结算丢失细分 token

`service/quota.go:171-175` 的 `PostWssConsumeQuota` 只把总量传给表达式：

```go
tieredOk, tieredQuota, tieredRes := TryTieredSettle(relayInfo, billingexpr.TokenParams{
    P:   float64(usage.InputTokens),
    C:   float64(usage.OutputTokens),
    Len: float64(usage.InputTokens),
})
```

`CR`/`CC`/`CC1h`/`Img`/`ImgO`/`AI`/`AO` 全部缺省为 0，且 `P`/`C` 用的是
含音频的**总量**。对照其他结算路径：

- `service/tiered_settle.go:21-89` 的 `BuildTieredTokenParams` 会填入全部
  细分变量，并在表达式引用某子类时把 `P`/`C` 归一化为「未单独计价的
  部分」；
- 文本路径 `service/text_quota.go:895`、音频路径
  `service/quota.go:295`（`PostAudioConsumeQuota`）、渠道测试
  `controller/channel-test.go:666` 都走 `BuildTieredTokenParams`；
- 上游 usage 里细分字段确实存在：`dto/openai_response.go:280-293`
  的 `InputTokenDetails`/`OutputTokenDetails` 含 `CachedTokens`、
  `TextTokens`、`AudioTokens`、`ImageTokens`；
  `relay/channel/openai/relay-openai.go:512-523` 也逐轮把它们累加进了
  `usage`，只是结算时没传。

触发场景（表达式引用任一分量）：

```text
表达式 tier("standard", p * 0.75 + c * 4.5 + img * 1 + ai * 3 + ao * 12)
Realtime response.done: input_tokens=100, output_tokens=50,
  input_token_details.audio_tokens=1000, output_token_details.audio_tokens=200
实际传入: P=100, C=50, AI=0, AO=0
```

音频 token 既没进 `ai`/`ao` 项，还被按文本价计费，造成系统性少计费
（方向取决于表达式系数，本例为少收）。

归属：这是**预存在**缺陷，不在本轮 delta 内。`git log --oneline
3e02d3fea..HEAD -- service/quota.go service/tiered_settle.go
relay/websocket.go` 为空；`git blame` 显示该调用由 `f0589cc478`
（2026-03-16）引入，同一时期的 `5b03b39db` 把其余三处结算路径迁到
`BuildTieredTokenParams`，但未触及 `PostWssConsumeQuota`
（`git show 5b03b39db -- service/quota.go | grep -c PostWss` = 0）。
`service/realtime_billing_double_charge_test.go` 也不覆盖 tiered。

可达性：需要模型同时满足「走 `/v1/realtime` WS」
（`router/relay-router.go:82-84` → `RelayFormatOpenAIRealtime`
→ `relay.WssHelper`（`controller/relay.go:303`）→
`service.PostWssConsumeQuota`（`relay/websocket.go:52`））与
「`billing_expr` 引用子类变量」。出厂默认配置里存在这种组合
（`setting/billing_setting/tiered_billing.go:255,264-266,323,332-334`：
`gemini-3.1-flash-live-preview`、`gemini-robotics-er-*`、`gemini-3.5-live-*`
等映射到含 `ai`/`ao`/`img` 的表达式）。开发库 `billing_setting.billing_mode`
确有这些模型名（共 180 个模型），但当前 22 个渠道均为 OpenAI 型且没有
一个承载它们，故本环境暂不可达；生产配置未查（未触碰生产）。

**同日修复**（`4ea8c39ea`）：`PostWssConsumeQuota` 改为经
`buildTieredTokenParamsFromRealtime`（`service/tiered_settle.go`）复用
`BuildTieredTokenParams` 的归一化契约，`Len` 保持原始输入总量。新增两个
用例：`TestBuildTieredTokenParamsFromRealtime_NormalizesSubcategories`
钉住归一化（P/C 减去被引用的子类、子类保留），
`TestRealtimeBilling_TieredSettleChargesAudioSubcategory` 走完整结算
断言钱包扣款等于表达式值。反向验证：还原旧调用后结算用例失败
（999400 vs 990800，差额即被漏计的音频费）。`go build ./...` 与
`go test ./service/ ./relay/helper/ ./pkg/billingexpr/ -count=1` 通过。
本次修复未部署到开发容器（容器仍为 `4ba36471d-dirty`）。

### 3.2 P3：非规范前缀路径返回 200 SPA HTML，绕过 `/api` 的 404 JSON 契约

`router/web-router.go:80` 的 `shouldReturnRelayNotFound` 只按**原始 URI
前缀**判定，`//api/...`、`/./api/...`、`/API/...` 都不以 `/api` 开头，
于是落到 SPA 兜底分支，返回 `200 text/html`（index 页）而非 404 JSON。

实测（`curl --path-as-is`）：

| 路径 | 结果 |
| --- | --- |
| `/api/user/self`（无 token） | 401 JSON |
| `//api/user/self`（无 token） | 200 HTML（SPA index） |
| `//api/status` | 200 HTML |
| `//v1/models` | 200 HTML |
| `/api//status` | 404 JSON |
| `/API/status` | 200 HTML |
| `/./api/status` | 200 HTML |

**不是认证绕过**：没有任何 API handler 被执行，`//api/user/self` 的 body
是 SPA 的 index HTML，不返回用户数据；`/%61pi/status`（编码字母）经路由
解码后命中的是真正的 `/api/status`（200 JSON）。影响是：状态码上报错误
（应为 404/401 处返回了 200），会误导监控、爬虫与「只看状态码」的客户端。
属既有行为，非本轮改动引入。

**同日修复**（`0d66b45e7`）：`router/web-router.go` 的
`shouldReturnRelayNotFound` 改为对「原始 URI + `path.Clean(requestPath)`」
取并集分类（只新增分类、不回退既有分支）。修复后 `//api/...`、
`/./api/...`、`/%2f%2fapi/...` 返回 404 JSON；`/API/...` 大小写变体仍走
SPA——路由本身大小写敏感，这是刻意保留的行为。新增 6 个表驱动用例
（4 个在修复前失败）；`go test ./router/` 与 `go build ./...` 通过。
上游 `QuantumNous/new-api` 的 `router/web-router.go` 没有这个分类函数
（结构不同），本修复无上游对齐问题。

### 3.3 P3：安全响应头缺失

3001 响应未观察到 `X-Content-Type-Options`、`X-Frame-Options`、
`Content-Security-Policy`、`Referrer-Policy`、`Permissions-Policy`、
`Strict-Transport-Security`；仓库 Go 侧也没有任何安全响应头配置。生产
macOS 环境的 Caddy 代理（`deploy/proxy/macos-docker-tun/Caddyfile`）只做
`reverse_proxy`、未设置任何头，故也不存在外部代理层补齐。属真实的加固
候选，但下线需决策（加哪些头、CSP 与前端 inline script 的兼容），本轮
只记录。非本轮改动引入，也无证据表明当前构成漏洞。

### 3.4 P3（文档准确性）：在途 CORS 注释的前提不成立

`middleware/cors.go` 的在途 diff 把 `config.AllowCredentials = true`
删除，并加注释称「浏览器客户端用 bearer token 而非 cookie 认证」。结论
（删掉无效头）正确，但前提与代码不符：`middleware/auth.go:85` 的
`authHelper` 优先读 session（`session.Get("username")`），
`web/default/src/lib/api.ts:21` 用 `withCredentials: true` 且只带
`New-Api-User` 头、不发 `Authorization`。即后台 API 实际是 **cookie 会话
认证**。两处都不构成本次改动的回归（见 4.1），但注释应改写为该改动的真实
理由（`AllowAllOrigins=true` 硬编码 `ACAO: *`，跨源凭证请求本就无效；
要支持需回显具体 Origin）。

### 3.5 记录：普通 `/api` 路由与其预检的 CORS 头不一致

CORS 中间件在 `router/relay-router.go:14` 以 `engine.Use` 注册，晚于
`SetApiRouter`/`SetDashboardRouter` 的路由注册（`router/main.go:16-18`），
因此只覆盖其后注册的路由与 NoRoute 链。实测：`GET /api/status` 带
`Origin` 无任何 CORS 头，而 `OPTIONS /api/status` 预检返回
`ACAO: *`（走 NoRoute 链）。二者不一致属既有状态，非本轮改动；仅在
有跨源读取 `/api/*` 需求的部署下才有影响。

## 4. 被推翻或收窄的代理结论

### 4.1 推翻：CORS 移除 `AllowCredentials` 不是「新增中风险回归」

子代理报「cookie 会话跨源请求被 CORS 阻断」，中风险。复现后不成立：
`gin-contrib/cors v1.7.2` 在 `AllowAllOrigins=true` 时对预检与实际响应都
硬编码 `Access-Control-Allow-Origin: *`
（`utils.go:21-22,53-54`）。HEAD 的 `ACAO: * + ACA-Credentials: true` 对
带凭证的请求本身违反 fetch 规范，浏览器此前已拒绝；改动只是删掉这个无效
头，非凭证（bearer）请求行为不变。实测印证：当前 3001 的 OPTIONS 预检为
`ACAO: *` 且无 `ACA-Credentials`。真正的限制是通配符本身，不是本次改动。

### 4.2 收窄：`/api/uptime` 404 不是缺陷

实际路由是 `/api/uptime/status`（`router/api-router.go:26`），实测 200；
`/api/uptime` 本就不存在，404 符合契约。

### 4.3 收窄：双斜杠结果取决于斜杠位置

子代理记「双斜杠 404 JSON」只对中间双斜杠（`/api//status`）成立；**前导**
双斜杠（`//api/...`）是 200 HTML，见 3.2。

### 4.4 收窄：Creem webhook 原始 body 日志

子代理先报在途缺陷，后自行更正：相关日志行与 HEAD 一致，属既有问题，非
本次在途改动；本次只做了「部分日志改 digest」的不完整改造。

## 5. 无确认发现的维度

- 提交 delta `3e02d3fea..fa211e334`：62 提交 / 86 文件，未发现可定位到
  当前 HEAD 的高信号行为回归、安全缺陷或部署问题。逐项复核了
  `common.ReadAllWithLimit`、自建 SSE scanner 行上限、流结束状态分类、
  Docker/CI/依赖升级、systemd、classic `zh.json` 删除、前端去重与只读守卫。
- 在途改动红队（369 项）：除 4.1/4.4 外未确认凭据写入、权限检查移除、
  SSRF 绕过、破坏性 SQL、goroutine/响应体资源失控、三库不兼容或伪成功。
- 前端：`web/default` 与 `web/classic` 构建通过、Safari 兼容检查通过；
  3001 实际服务 classic，入口引用的 JS/CSS 均 200。无认证、只读守卫、
  i18n、暗色/移动端高信号回归（CSP 见 3.3）。
- 计费：`go test -count=1 ./pkg/billingexpr ./relay/helper ./service
  ./model ./relay/common` 通过；主代理复跑 `./relay/helper
  ./pkg/billingexpr` 通过。预扣/结算/退款、`quota_data` 聚合、Realtime
  双扣修复本身未发现新问题（3.1 是结算变量缺失，不是双扣）。

## 6. 覆盖边界

- 未做认证态端到端（无可用只读 token，未创建用户、未写库）；模型、控制台、
  真实上游调用未验证。
- 三数据库端到端计费事务未跑，仅源码审查（3.1 的缺失在 SQLite/MySQL/
  PostgreSQL 上表现一致，因为它发生在表达式求值之前）。
- 生产容器与生产库未读取，故 3.1 的生产可达性、3.3 的生产安全头状态均未
  核实。
- 3.1 的可达性以「渠道实际承载 realtime 模型」为前提，本环境未满足；
  生产是否满足需另行确认。
- 在途改动对应的其他会话仍在编辑，本文对在途部分的结论对应 2026-10-08
  快照，可能随其推进而过期。

索引说明：`docs/reviews/README.md` 当前被其他会话在途修改，为避免把他人的
未提交改动一并提交，本记录暂未加入索引。
