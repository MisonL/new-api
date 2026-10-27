# CR-AUDIT-FIX-ROUND-2026-10-27

范围：2026-10-26 至 2026-10-27 这一轮"全部优化好"审计修复的验证记录。
每条包含改动要点、验证命令、退出码与边界说明；未覆盖的部分单独列出。

## 1. 桌面端

### 1.1 就绪探针拆解 chunked 分帧（c1441cc63）

`/api/status` 响应体超过 Go 的 `bufferBeforeChunkingSize`（2048 字节），
服务端改用 `Transfer-Encoding: chunked`。裸 socket 探针把 `e93\r\n{...}`
直接当 JSON 解析必然失败，桌面端误判"端口被其他服务占用"，随后杀掉健康
的 sidecar 并无限重试。

验证：`cargo test --test runtime_support`（29 项）退出码 0，含新增的
chunked 正常、跨 chunk 分片、截断三种用例。

### 1.2 监听地址可用 HOST 收窄（568d31e9f）

`main.go` 原先固定 `Addr: ":" + port`，绑全部网卡；桌面端首次启动的
`/api/setup` 无鉴权，同网段可直接创建管理员。改为
`common.ResolveListenAddress(port)`，桌面端显式传 `HOST=127.0.0.1`。

验证：`go test ./common -run TestResolveListenAddress`，4 项通过。

### 1.3 打包版 ACL 远程上下文（1d43d1931）

主窗口 `WebviewUrl::External(http://127.0.0.1:PORT)` 在非 dev 构建下属于
Remote 上下文，原 capability 只有 `ExecutionContext::Local`，
`open_external_url` 会被 ACL 拒绝。新增 `main-window-remote` capability，
并用 `build.rs` 声明 app manifest。

验证：`cargo test --test acl_resolution`，3 项通过（含删除 remote
capability 与删除 app manifest 的反向用例）。

### 1.4 sidecar 退出（a1fe4a353、1aa06ee19）

- 先 SIGTERM 再回收，Go 侧的优雅停机才会跑完（插件 kill 在 Unix 上是
  SIGKILL）。
- `set_process_group(true)` 让插件层 kill 走 killpg，回收 sidecar 子进程。

验证：`cargo test`（lib + 三个集成测试）退出码 0。

### 1.5 密钥文件权限与损坏诊断（83cabb309）

- `desktop-secrets.json` 以 0600 创建、写入后 `sync_all` 再 rename；目录
  收紧到 0700。
- `analyze_startup_error` 新增密钥文件分支，明确文件路径，并说明删除重建
  会让旧 CRYPTO_SECRET 加密的数据不可解。

验证：`cargo test` 退出码 0；新增权限断言（0600）与两条诊断断言。
`cargo fmt --check` 干净。

### 1.6 服务管理页 CSP 与请求标记头（6db66dc6c）

`tauri.conf.json` 的 csp 只注入 `tauri://` 与 `data:` 资源，主窗口
（http://127.0.0.1）与服务管理窗口（自定义 scheme）都拿不到。服务管理页
改为在 HTML 响应上直接下发收紧的 CSP；有副作用的 POST 要求
`X-NewAPI-Service-Request` 标记头，阻断被注入脚本的 no-cors 简单请求。

验证：`cargo test` 退出码 0，新增 3 条授权判定单测。

边界：主窗口 CSP 仍归 Go 服务端/反代负责，`middleware/security_headers.go`
已记录该决定；本次未改。

### 1.7 旧 sidecar 退出事件误杀新进程（b935cb772）

监控线程收到 Terminated 无条件 take + kill；stop + 重试换新进程后，迟到的
旧事件会把新句柄杀掉。改为按 spawn 时记录的 pid 比对。

验证：`cargo test` 退出码 0，新增匹配/不匹配/空句柄三条用例。
边界：竞态依赖真实进程生命周期，接线部分仍需真机走一次"启动失败 → 重试"。

### 1.8 启动失败清理不再提前清 starting 标记

`spawn_bootstrap_desktop_runtime` 原先在 `handle_bootstrap_failure` 之前就
清 `startup_in_progress`。失败清理里的 `stop_sidecar` 会无条件取走当前
sidecar 句柄，提前清标记等于给重试开了一个窗口：重试刚起的新进程会被这次
失败清理顺手杀掉。改为成功路径才在这里清，失败路径由 `stop_sidecar` 内部
在清理末尾清。

验证：`cargo fmt --check` 干净，`cargo test` 全量通过。边界：时序问题
没有可断言的单测，需要真机复验。

## 2. 前端（web/default）

### 2.1 富文本净化（2316aa88c）

about、法律条款、footer_html 三处用 `dangerouslySetInnerHTML` 直接注入
管理员配置的 HTML，绕过了 Markdown 组件已有的 rehype-sanitize。

- 抽出 `lib/sanitize-schema.ts` 作为两条路径共用的白名单；原 `'class'`
  在 hast 中不生效（属性名是 `className`），实际一直被剥掉，一并修正。
- 新增 `lib/sanitize-html.ts`：parse5 解析 HTML 片段后净化，不做 Markdown
  重排（排版型 HTML 的空行/缩进会被 Markdown 解析成代码块，已实测）。
- 新增 `SanitizedHtml` 组件，把净化后的 hast 转成 React 元素，不再经过
  innerHTML。

验证：
- `bun test` 289 项全过，其中 `sanitize-html.test.ts` 8 项、
  `sanitized-html.test.tsx` 6 项。
- 反向验证：把 footer.tsx 改回 `dangerouslySetInnerHTML` 后源码守卫用例
  失败（`bypasses the sanitizer`），恢复后重新通过。
- `bun run typecheck` 退出码 0；`bun run lint` 22 条既有 warning、0 error；
  `bun run build` 退出码 0，产物体积 16953.6 kB（改动前 16954.9 kB）。

### 2.2 分页退化（16e5131a3）

users / redemption-codes 用 `manualPagination: !globalFilter`，带搜索词时
改由客户端二次切片，第 2 页恒空。改为 `manualPagination: true`。

验证：typecheck 0；lint 无新增；`bun test` 全过。

### 2.3 供应商下拉截断（29d22d698）

后端把 page_size 钳制在 100，原代码传 1000 只拿到第一页。新增
`getAllVendors()` 逐页拉取，`maxPages=20` 硬上限防止无界循环。

### 2.4 OAuth 意图标记（e0c267600）

绑定弹窗去掉 opener 后回调页无法区分绑定/登录。改为打开前写
`oauth:intent`，登录入口写 `login` 覆盖残留标记，回调页读标记，绑定结束
后清除；所有弹窗带 `noopener,noreferrer`。

### 2.5 网络抖动不再登出（96423bac3）

- `_authenticated/route.tsx`：只有 401 才清会话，网络错误放行本次渲染。
- `auth-store.ts` / `use-home-page-content.ts`：localStorage 读写包 try。
- `main.tsx`：500 只提示，不再整页跳 `/500`。

验证：typecheck 0；`bun test` 289 项全过；`bun run build` 退出码 0。

### 2.6 漏翻文案（a7e79649a）

账单弹窗、充值卡片、分页按钮、`formatTimestamp` 的 Never、三个内置
header profile 描述改为 `t()`；新增 10 个键按现有顺序插入 en/zh 并跑
`bun run i18n:sync` 同步其余语种。

验证：`bun run i18n:sync` 退出码 0；typecheck 0；lint 22 条既有 warning；
`bun test` 289 项全过（含同步更新的预置描述断言）。
边界：fr/ja/ru/vi 的新键暂时回落英文，已进 `_reports` 的 untranslated 清单。

## 3. 后端中继

### 3.1 AWS Converse 缓存与 thinking（482019440、904748b94）

- `cache_control` 映射为 `CachePointBlock`，cache token 落到 usage。
- 响应里的 thinking 不带 signature 时不再中断流，回放文本合法。

验证：`go test ./relay/channel/aws/` 通过，新增 6 条用例。

### 3.2 上游响应头过滤（0300c096d）

视频代理与 minimax TTS 直接复制上游全部响应头，会带出 Set-Cookie 与
hop-by-hop 头。改用 `service.CopyUpstreamHeaders`（同一份 denylist）。

验证：`go test ./service -run TestCopyUpstreamHeaders` 通过。

### 3.3 空回复自动重试（00a691d22）

生产日志（只读查询 `logs` 表）确认根因：渠道 218 的空回复全部是
`stream_status.end_reason=upstream_transport_interrupted`，错误为
`http2: server sent GOAWAY` / `unexpected EOF` / `stream disconnected
before completion`，`completion_tokens=0`，且 `upstream_ttfb_ms` 30-60s。
上游只发出 `message_start` 就断流，Claude 流式处理器补一个 `message_stop`
当成功返回，控制器不会换渠道重试。

- 判定提到 `relay/common`，OpenAI 侧改为调用同一份实现。
- 用 prelude 重试标记表达提交点：只转发 `message_start` 时可重试，首个
  `content_block_start`/`content_block_delta` 到达即清掉。
- 重试尝试跳过重复的 `message_start`，usage 照常解析。

验证：`go test ./relay/... ./controller ./service ./constant` 退出码 0；
新增 3 条用例（仅 prelude 中断可重试、内容已下发不重试、重试不重复
message_start 且内容正常下发）；`gofmt -l` 无输出，`go vet` 无输出。

### 3.4 OpenAI 格式上游同样可重试（c23149696）

Claude 侧修完后，OpenAI 兼容渠道存在同类缺口：中断判定要求
`ReceivedResponseCount == 0`，而上游通常会先发一帧 role/空 delta，计数已是 1，
断流仍被当成正常结束。

- 流开始时置 prelude 重试标记；`HandleStreamFormat` 逐帧判定，出现
  content / reasoning_content / reasoning / refusal / tool_calls 才算提交并
  清掉标记；标记清掉后不再解析后续帧。
- 扫描期间最后一帧被压后到流结束才下发，中断判定前补一次末帧提交检查，
  否则"末帧即内容"的单帧流会被误判为空。

验证：`go test ./relay/... ./controller ./service ./constant ./middleware`
退出码 0；新增 3 条用例（role 帧后断流可重试、内容帧后断流不重试、健康流
成功且标记已清）。

### 3.5 Gemini 格式上游同样可重试（1e7ea6119）

`geminiStreamHandler` 扫描结束后直接返回成功，不看 `StreamStatus`。原生
Gemini 与 Gemini→OpenAI/Claude 两条路径共用该函数，一处修复覆盖两处：
流开始时置 prelude 标记，分片出现 text / inlineData / functionCall 才算
提交并清标记，扫描结束后接入共享的中断判定。

验证：`go test ./relay/... ./controller ./service ./middleware` 退出码 0；
新增 3 条用例。

### 3.6 其他（9abfc0b21、3cbe3c75d）

- `common/page_info.go`：负数 page/page_size 归一化，避免负 LIMIT 退化成
  无上限查询。
- `controller/codex_usage.go`：用量查询不再刷新并持久化凭证，改为提示
  用户走显式刷新入口。

## 4. 已评估但不改

- 主窗口 `on_navigation` 限制：wry 的导航回调对子框架导航同样触发，按
  本地源拦截会一并挡掉关于页/自定义首页的管理员 iframe。主窗口 CSP 与
  导航策略留在 Go 服务端与部署层。
- `fatal_error_reported` 一次性闩锁：第一次致命错误后不再弹窗，避免失败
  循环刷屏；服务管理窗口本身有诊断横幅，信息不丢。保持现状。
- sidecar 未加 `env_clear()`：AGENTS.md §8 要求显式清空的变量已逐个覆盖，
  整体清空会影响 HOME/TMPDIR 等运行时依赖，收益不足。
- `common/rate-limit.go` 的 `Init` 双检 nil 未加原子操作：只在进程启动期
  调用，未观察到并发写入路径。

## 5. 未覆盖 / 未验证

- 桌面端 ACL、CSP、sidecar 生命周期改动均通过单元/集成测试与
  `cargo fmt --check`，未在打包后的真机安装包上复验。
- 前端改动通过 typecheck、lint、单测与生产构建，未在浏览器里逐页点检。
- 空回复重试只覆盖"首个内容帧前断流"这一种形态；上游返回 200 后
  正常结束但内容为空的其它形态未纳入。
- 生产环境仅做只读 SQL 查询，未重启、未改配置。
