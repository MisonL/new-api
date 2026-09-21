# CR-DESKTOP-OAUTH-HANDOFF-STORE-2026-04-17

## 审查范围

- `controller/oauth.go`
- `controller/oauth_desktop.go`
- `controller/oauth_desktop_store.go`
- `controller/oauth_desktop_test.go`
- `controller/oauth_desktop_store_test.go`
- `desktop/tauri-app/src-tauri/tests/runtime_support.rs`
- `.env.example`
- `AGENTS.md`
- `docs/superpowers/specs/2026-04-17-desktop-oauth-handoff-store-design.md`
- `docs/superpowers/plans/2026-04-17-desktop-oauth-handoff-store.md`

## 目标

- 让桌面端 OAuth handoff 在多实例部署下不依赖进程内内存
- 使用 Redis 作为共享状态面时，`start -> callback -> poll` 可跨实例闭环
- 保持普通 Web OAuth 和桌面单实例行为不回归

## 设计结论

- 主落点：状态面
- 次级影响：控制面
- 复杂性从进程内 map 转移到 Redis 短生命周期共享状态
- 不使用 sticky session 作为长期架构前提

## 代码审查结论

- `controller/oauth_desktop.go` 已不再直接持有进程内状态 map，状态访问统一走 store 包装函数
- `controller/oauth_desktop_store.go` 提供 memory 和 Redis 两套实现，默认在 `common.RedisEnabled && common.RDB != nil` 时使用 Redis
- Redis store 使用：
  - `new-api:desktop_oauth:v1:handoff:<handoff_token>`
  - `new-api:desktop_oauth:v1:state:<state>`
- Redis `Consume` 使用 Lua 原子读取并删除主记录与 state 索引
- `controller/oauth.go` 已为桌面 OAuth callback 链路补充 store 错误传播，不再吞掉状态面失败
- `runtime_support.rs` 已修复 `NEW_API_DESKTOP_PORT` 并发测试串扰，避免环境变量污染造成假失败
- `router/api-router.go` 已将 `/api/oauth/desktop/poll` 从 `CriticalRateLimit()` 中拆出，改为桌面 OAuth 轮询专用限流
- `middleware/rate-limit.go` 已新增按 `handoff_token` 分桶的轮询限流，避免正常桌面登录等待期间被 IP 级关键限流误伤

## 验证记录

### 1. Controller 定向验证

命令：

```bash
go test ./controller -run 'TestDesktopOAuth|TestHandleOAuthDesktop|TestHandleOAuthMarksDesktop' -count=1
```

结果：

- 退出码：0
- 结论：通过

覆盖点：

- memory store 生命周期
- Redis store 生命周期
- provider 错误回写 handoff 状态
- 浏览器已有 session 时桌面 login 不误走 bind
- Redis 下 callback + poll 登录闭环
- Redis 下 `start -> callback -> poll` 跨 router 生命周期闭环

### 2. Go 全量验证

命令：

```bash
go test ./...
```

结果：

- 退出码：0
- 结论：通过

### 3. 前端桌面运行时验证

命令：

```bash
bun test web/tests/desktopRuntime.test.mjs
```

结果：

- 退出码：0
- 结论：6/6 通过

### 3.1 轮询限流定向验证

命令：

```bash
go test ./middleware ./controller ./router -count=1
```

结果：

- 退出码：0
- 结论：通过

覆盖点：

- `/api/oauth/desktop/poll` 使用独立限流中间件
- 同一 IP 下不同 `handoff_token` 不共享轮询限流桶
- 单个 `handoff_token` 超出专用限流窗口后仍会返回 `429`

### 4. Tauri Rust 验证

命令：

```bash
cargo test --manifest-path desktop/tauri-app/src-tauri/Cargo.toml --test runtime_support --test window_bounds
```

结果：

- 退出码：0
- 结论：25/25 通过

额外发现：

- 初始失败根因不是业务回归，而是 `runtime_support` 测试间共享 `NEW_API_DESKTOP_PORT` 环境变量
- 已通过 `DesktopPortEnvGuard` + 互斥锁修复测试隔离

### 5. 双进程 live 验证

验证拓扑：

- 实例 A：本地进程，`PORT=3011`
- 实例 B：本地进程，`PORT=3012`
- 共享状态面：隔离开发环境 PostgreSQL `127.0.0.1:5433` + Redis `127.0.0.1:6380`
- 共享会话密钥：与隔离开发环境一致
- 对外回调口径：临时将 `ServerAddress` 切为 `http://127.0.0.1:13000`
- 外部 IdP：`dex-newapi-test`

临时验证配置：

- 在隔离开发库临时插入 `dex-local` 自定义 OAuth provider
- provider 配置：
  - `client_id=new-api-desktop`
  - `client_secret=dex-new-api-secret`
  - `authorization_endpoint=http://127.0.0.1:15556/dex/auth`
  - `token_endpoint=http://127.0.0.1:15556/dex/token`
  - `user_info_endpoint=http://127.0.0.1:15556/dex/userinfo`

验证步骤：

1. 调用实例 A：

```bash
GET http://127.0.0.1:3011/api/oauth/desktop/start?provider=dex-local&mode=login
```

得到：

- `state=EwDvgcXVmwhIkHE1P6n1Ytv1`
- `handoff_token=V8XBvBktJkOMQhtNKairxOIobW9NfdZN9TbKS2Yx`

2. 手工走 Dex 授权码流程：

- 使用 `dex-test@example.local / password`
- 通过 approval 页面拿到：
  - `code=darup7juym5zeg3auckxhjlcq`
  - `state=EwDvgcXVmwhIkHE1P6n1Ytv1`

3. 将 callback 显式打到实例 B 的后端接口：

```bash
GET http://127.0.0.1:3012/api/oauth/dex-local?code=darup7juym5zeg3auckxhjlcq&state=EwDvgcXVmwhIkHE1P6n1Ytv1
```

4. 回实例 A 轮询 handoff：

```bash
GET http://127.0.0.1:3011/api/oauth/desktop/poll?handoff_token=V8XBvBktJkOMQhtNKairxOIobW9NfdZN9TbKS2Yx
```

结果：

- callback 在实例 B 成功创建用户并完成桌面登录
- poll 在实例 A 返回成功登录结果：
  - `id=3`
  - `username=dex-local_3`
  - `email=dex-test@example.local`
- 再次 poll 返回：
  - `桌面端 OAuth 登录请求不存在或已过期`

结论：

- `start -> callback -> poll` 已在两个独立进程之间通过 Redis 共享状态闭环
- handoff 消费语义正确
- callback 真实后端入口应为 `/api/oauth/{provider}`；浏览器路由 `/oauth/{provider}` 只负责前端回调页

清理：

- 已停止两个本地验证实例
- 已从隔离开发库删除临时 `dex-local` provider
- 已恢复 `ServerAddress=http://127.0.0.1:3001`
- 已删除验证过程中创建的临时用户 `dex-local_3`

### 6. Docker 双容器 live 验证

验证拓扑：

- 实例 A：Docker 容器 `new-api-oauth-live-3111`，宿主端口 `3111`
- 实例 B：Docker 容器 `new-api-oauth-live-3112`，宿主端口 `3112`
- 容器内数据库连接：
  - `SQL_DSN=postgresql://root:123456@host.docker.internal:5433/new-api-dev`
- 容器内 Redis 连接：
  - `REDIS_CONN_STRING=redis://host.docker.internal:6380/0`
- 浏览器侧授权入口：
  - `authorization_endpoint=http://127.0.0.1:15556/dex/auth`
- 容器侧 token / userinfo 回源：
  - `token_endpoint=http://host.docker.internal:15556/dex/token`
  - `user_info_endpoint=http://host.docker.internal:15556/dex/userinfo`
- 对外回调口径：临时将 `ServerAddress` 切为 `http://127.0.0.1:13000`

先决修正：

- 初次 Docker 联调失败，根因不是 handoff 逻辑，而是容器内回调进程把 `127.0.0.1:15556` 视为容器自身，无法访问宿主机上的 Dex
- 修正方式是保持浏览器侧 `authorization_endpoint` 走宿主机 `127.0.0.1`，同时把容器内 `token_endpoint` 和 `user_info_endpoint` 改为 `host.docker.internal`

验证步骤：

1. 实例 A 创建 handoff：

```bash
GET http://127.0.0.1:3111/api/oauth/desktop/start?provider=dex-local&mode=login
```

得到：

- `state=J0Y311hPC1yplXpwK2lHkhDH`
- `handoff_token=d8XQ5ez1FAnWEpXtVjoXbnxnvWKx2SphGNL0Sz8j`

2. 通过 Dex 完成授权码流程：

- 使用 `dex-test@example.local / password`
- 最终浏览器重定向：
  - `http://127.0.0.1:13000/oauth/dex-local?code=dnxgvt7k5mcqiy26xmp3q3tci&state=J0Y311hPC1yplXpwK2lHkhDH`

3. 将 callback 显式打到实例 B 后端：

```bash
GET http://127.0.0.1:3112/api/oauth/dex-local?code=dnxgvt7k5mcqiy26xmp3q3tci&state=J0Y311hPC1yplXpwK2lHkhDH
```

结果：

- callback 返回成功登录结果：
  - `id=6`
  - `username=dex-local_6`
  - `display_name=dex-test`

4. 回实例 A 轮询 handoff：

```bash
GET http://127.0.0.1:3111/api/oauth/desktop/poll?handoff_token=d8XQ5ez1FAnWEpXtVjoXbnxnvWKx2SphGNL0Sz8j
```

结果：

- poll 返回与 callback 一致的用户：
  - `id=6`
  - `username=dex-local_6`
  - `display_name=dex-test`
- 再次 poll 返回：
  - `桌面端 OAuth 登录请求不存在或已过期`

额外发现：

- 两个容器首次并发启动时，实例 B 因数据库迁移与启动期查询竞争，直到 `2026-04-17 21:00:40` 才完全 ready
- 该现象不影响 handoff 正确性，但说明编排层健康检查不能假定容器启动后立即可用

结论：

- `start -> callback -> poll` 已在两个真实 Docker 容器之间通过 Redis 共享状态闭环
- handoff 消费语义在容器编排层同样正确
- 容器化部署时，涉及宿主机回源的 IdP 地址必须区分浏览器侧和容器侧可达性

清理：

- 已停止并删除 `new-api-oauth-live-3111`、`new-api-oauth-live-3112`
- 已删除临时镜像 `new-api-local:oauth-handoff-bin`
- 已从隔离开发库删除临时 `dex-local` provider
- 已恢复 `ServerAddress=http://127.0.0.1:3001`
- 已删除验证过程中创建的临时用户 `dex-local_6`

### 7. 反向代理入口层验证

验证拓扑：

- 统一入口代理：Nginx 容器 `new-api-oauth-proxy-13010`
- 入口地址：`http://127.0.0.1:13010`
- 后端实例：
  - `new-api-oauth-live-3111`
  - `new-api-oauth-live-3112`
- 浏览器侧重定向 URI 仍保持 Dex 已注册的：
  - `http://127.0.0.1:13000/oauth/dex-local`
- 实际验证的 API 入口全部经 `13010` 代理分发

验证步骤：

1. 经代理入口创建 handoff：

```bash
GET http://127.0.0.1:13010/api/oauth/desktop/start?provider=dex-local&mode=login
```

得到：

- `state=pelsTH5Q1SxJ1kn3bYyMBE8Y`
- `handoff_token=XcO2bBRMwEZ5V7gIUrjwUkzazRUchf4GCX2PSvQZ`

2. 完成 Dex 授权，得到浏览器最终跳转：

- `http://127.0.0.1:13000/oauth/dex-local?code=suzsygjn2ezg4gxjc4fr54k53&state=pelsTH5Q1SxJ1kn3bYyMBE8Y`

3. 经代理入口执行 callback：

```bash
GET http://127.0.0.1:13010/api/oauth/dex-local?code=suzsygjn2ezg4gxjc4fr54k53&state=pelsTH5Q1SxJ1kn3bYyMBE8Y
```

4. 经代理入口轮询 handoff：

```bash
GET http://127.0.0.1:13010/api/oauth/desktop/poll?handoff_token=XcO2bBRMwEZ5V7gIUrjwUkzazRUchf4GCX2PSvQZ
```

结果：

- callback 返回成功用户：
  - `id=7`
  - `username=dex-local_6`
  - `display_name=dex-test`
- poll 返回与 callback 一致的用户
- 二次 poll 返回：
  - `桌面端 OAuth 登录请求不存在或已过期`

分发证据：

- `3112` 日志命中：
  - `GET /api/oauth/desktop/start?...`
- `3111` 日志命中：
  - `GET /api/oauth/dex-local?...`
  - `GET /api/oauth/desktop/poll?...`

结论：

- 在统一代理入口下，请求被分发到不同后端实例时，桌面 OAuth handoff 仍能依赖 Redis 正确闭环
- 入口层不需要 sticky session

清理：

- 已停止并删除 `new-api-oauth-proxy-13010`
- 已停止并删除 `new-api-oauth-live-3111`、`new-api-oauth-live-3112`
- 已从隔离开发库删除临时 `dex-local` provider
- 已恢复 `ServerAddress=http://127.0.0.1:3001`

### 8. 真实前端回调页验证

验证环境：

- 使用本机桌面端 sidecar 已监听的 `http://127.0.0.1:13000`
- 在浏览器中真实打开 Dex 登录页和授权页
- 通过前端页面 `/oauth/dex-local` 触发 `OAuth2Callback.jsx` 中的实际 JS 回调逻辑

验证步骤：

1. 调用桌面端 sidecar 创建 handoff：

```bash
GET http://127.0.0.1:13000/api/oauth/desktop/start?provider=dex-local&mode=login
```

得到：

- `state=CWtP9dRr0SxfImhv1E8ycHCC`
- `handoff_token=JpZm06VKmZq4WZ09v39PSICxCKVIHwn5E2SMU6xb`

2. 在浏览器中：

- 先访问 `http://127.0.0.1:13000/login`，确保 `localStorage.status` 中已有 `dex-local` provider
- 再打开 Dex 授权 URL
- 使用 `dex-test@example.local / password`
- 点击 `Grant Access`

3. 浏览器实际跳回：

- `http://127.0.0.1:13000/oauth/dex-local?code=zwjtpextzd4ytmkus34iqdebr&state=CWtP9dRr0SxfImhv1E8ycHCC`

4. 前端回调页自动完成：

- `GET /api/status`
- `GET /api/oauth/dex-local?...`
- 登录成功后跳转到 `/console/token`

结果：

- 页面最终停在 `/console/token`
- 页面显示成功提示：
  - `登录成功！`
- 页面右上角用户态为：
  - `dex-local_3`
- 控制台检查：
  - 无 `error` / `warn`
- 网络请求检查：
  - `/oauth/dex-local?...` 返回 `200`
  - `/api/oauth/dex-local?...` 返回 `200`
  - 后续 `/api/user/self`、`/api/token/` 等页面初始化请求正常
- handoff 轮询结果：
  - 首次 poll 返回成功用户
  - 二次 poll 返回“请求不存在或已过期”

结论：

- 前端真实回调页 `/oauth/{provider}` 的 JS 链路可正常处理桌面 handoff 登录
- 不仅后端 callback 正常，前端回调页本身也已验证无控制台异常、无网络错误

清理：

- 已从桌面端本地 SQLite 删除本轮验证生成的测试用户 `dex-local_3`
- 已删除该测试用户对应的 `user_oauth_bindings` 绑定记录

### 9. Kubernetes Ingress 多副本验证

验证拓扑：

- 本地集群：`kind-newapi-oauth`
- Ingress 控制器：官方 `ingress-nginx` `controller-v1.15.1`
- 宿主机入口：`kubectl port-forward svc/ingress-nginx-controller 18081:80`
- 后端工作负载：
  - Deployment `new-api`
  - 副本数 `2`
- 集群内 Service：`new-api:80 -> pod:3000`
- 前端 / API 对外统一入口：
  - `http://127.0.0.1:18081`
- 外部 IdP：
  - 临时 Dex 容器 `dex-newapi-k8s-test`
  - 浏览器侧授权入口：
    - `http://127.0.0.1:15557/dex/auth`
  - 容器侧 token / userinfo 回源：
    - `http://host.docker.internal:15557/dex/token`
    - `http://host.docker.internal:15557/dex/userinfo`
- 对外回调口径：临时将 `ServerAddress` 切为 `http://127.0.0.1:18081`

入口层注意事项：

- 初始尝试使用 `18080` 失败，根因是宿主机该端口已被 Docker Desktop 占用
- 改为 `18081` 后，Ingress 入口与回调链路恢复正常

#### 9.1 API 闭环验证

验证步骤：

1. 经 K8s Ingress 入口创建 handoff：

```bash
GET http://127.0.0.1:18081/api/oauth/desktop/start?provider=dex-local&mode=login
```

得到：

- `state=OmFVn0v9z4i9CO3pV00c1izQ`
- `handoff_token=O0nxz5wPJGi29ff8JGCgciQGvpPQYAKQwc0pJvnm`

2. 完成 Dex 授权后得到浏览器最终跳转：

- `http://127.0.0.1:18081/oauth/dex-local?code=owwocitsumih2nx5gheulcztu&state=OmFVn0v9z4i9CO3pV00c1izQ`

3. 经 K8s Ingress 入口执行 callback：

```bash
GET http://127.0.0.1:18081/api/oauth/dex-local?code=owwocitsumih2nx5gheulcztu&state=OmFVn0v9z4i9CO3pV00c1izQ
```

4. 经 K8s Ingress 入口轮询 handoff：

```bash
GET http://127.0.0.1:18081/api/oauth/desktop/poll?handoff_token=O0nxz5wPJGi29ff8JGCgciQGvpPQYAKQwc0pJvnm
```

结果：

- callback 返回成功用户：
  - `id=8`
  - `username=dex-local_6`
  - `display_name=dex-test`
- poll 返回与 callback 一致的用户
- 二次 poll 返回：
  - `桌面端 OAuth 登录请求不存在或已过期`

Pod 分发证据：

- Pod `new-api-7b4d9bb5d9-7pdw8` 命中：
  - `GET /api/oauth/desktop/start?...`
- Pod `new-api-7b4d9bb5d9-54szs` 命中：
  - `GET /api/oauth/dex-local?...`
  - `GET /api/oauth/desktop/poll?...`
- 另一条二次 `poll` 落在不同 Pod，仍返回“请求不存在或已过期”

结论：

- 在 Kubernetes Ingress + 双副本 Deployment 下，`start -> callback -> poll` 已跨 Pod 闭环
- sticky session 不是必需条件

#### 9.2 真实前端回调页验证

验证步骤：

1. 经 K8s Ingress 入口创建 handoff：

```bash
GET http://127.0.0.1:18081/api/oauth/desktop/start?provider=dex-local&mode=login
```

得到：

- `state=rmikc6081PCMwJZe6yuMJvt3`
- `handoff_token=sthay4R9WF82eALyLXBcpLv0IYJiEHtOylcvZ5h1`

2. 浏览器访问：

- `http://127.0.0.1:18081/login`
- 再跳转到 Dex 授权 URL：
  - `redirect_uri=http://127.0.0.1:18081/oauth/dex-local`
- 使用 `dex-test@example.local / password`
- 点击 `Grant Access`

3. 浏览器实际回到：

- `http://127.0.0.1:18081/oauth/dex-local?code=s2qxqz6tvea7mj3luooikfjrq&state=rmikc6081PCMwJZe6yuMJvt3`

4. 前端回调页自动完成登录并跳转：

- 最终页面：
  - `/console/token`
- 页面右上角用户态：
  - `dex-local_6`

网络检查：

- `/oauth/dex-local?...` 返回 `200`
- `/api/status` 返回 `200`
- `/api/oauth/dex-local?...` 返回 `200`
- `/api/user/self`、`/api/user/self/groups`、`/api/token/` 返回 `200`

控制台检查：

- 无 `error` / `warn`

handoff 轮询结果：

- 首次 poll 返回成功用户
- 二次 poll 返回：
  - `桌面端 OAuth 登录请求不存在或已过期`

结论：

- K8s Ingress 下前端真实回调页 `/oauth/{provider}` 的 JS 链路正常
- 不仅后端 API handoff 可跨 Pod，前端页面级回调也已完成闭环

### 10. 2026-04-18 隔离单机补充验证（macOS）

验证拓扑：

- 隔离 new-api：`http://127.0.0.1:13100`
- 隔离 SQLite：`/tmp/newapi-mac-verify/data/new-api.db`
- mock 上游：`http://127.0.0.1:15566`
- 独立 cookie：`/tmp/newapi-mac-verify/cookies.txt`

关键发现：

- `GET /api/user/self` 在会话模式下还需要请求头 `New-Api-User`
- 缺少该头会返回：
  - `Unauthorized, New-Api-User header not provided`
- 补上 `New-Api-User: 1` 后，会话鉴权恢复正常

OAuth 闭环验证：

1. 创建 custom provider：`desktop-mock-oauth`（`oauth_code`）
2. 调用 `GET /api/oauth/desktop/start?provider=desktop-mock-oauth&mode=login`
3. 通过 mock 授权端点重定向到：
   - `GET /api/oauth/desktop-mock-oauth?code=mock-auth-code&state=<state>`
4. 调用 `GET /api/oauth/desktop/poll?handoff_token=<token>`

结果：

- 首次 poll 成功返回用户：
  - `id=2`
  - `username=mock-oauth-user`
- 二次 poll 返回：
  - `桌面端 OAuth 登录请求不存在或已过期`
- 证据文件：
  - `/tmp/newapi-mac-verify/oauth-callback-headers.txt`
  - `/tmp/newapi-mac-verify/oauth-callback-body.html`

SSE 闭环验证：

1. 新增隔离渠道：
   - 名称：`Mock OpenAI SSE`
   - 类型：`OpenAI`
   - `base_url=http://127.0.0.1:15566`
   - 模型：`gpt-4o-mini`
2. 创建隔离 token：`sse-verify-token`
3. 调用：
   - `POST /v1/chat/completions`，`stream=true`

结果：

- 流式 chunk 连续返回
- 输出包含 `data: [DONE]`
- relay 日志显示 `stream ended: reason=done`
- 证据文件：
  - `/tmp/newapi-mac-verify/sse-response.txt`

结论：

- 在隔离单机环境下，桌面 OAuth handoff 链路与 SSE 流式链路均已完成闭环
- 当前剩余未覆盖项集中在托盘相关 OS 级 GUI 交互

## 未覆盖项

- 未做真实云上 Kubernetes / 公网 Ingress / TLS / 多节点集群层的端到端联调

原因：

- 当前已完成本地双进程、Docker 双容器、反向代理入口层、真实前端回调页以及本地 kind Kubernetes Ingress 多副本验证
- 剩余未覆盖项集中在真实云环境的公网入口、TLS 终止、外部负载均衡器和多节点网络层

## 风险评估

- 当前主要残余风险在真实云上公网 Ingress / TLS / 外部负载均衡器 / 多节点网络层，而不是 controller / Redis 共享状态逻辑层
- 若未来 Docker/Kubernetes 部署误未启用 Redis，桌面 OAuth handoff 仍会退回单进程 memory 语义，不适合多实例
- 若外部 IdP 部署在宿主机或内网侧，必须明确浏览器侧和容器侧的可达地址映射，否则 token 交换会在 callback 阶段失败
- `.env.example` 与 `AGENTS.md` 已补充这一运行前提

## 审查结论

- 当前代码已满足“共享状态面替代进程内状态”的目标
- 离线测试、双 router 测试、本地双进程 live、Docker 双容器 live、反向代理入口层 live、真实前端回调页 live、本地 kind Kubernetes Ingress 多副本 live 均已通过
- 如需继续推进，下一层只剩真实云上入口和 TLS 场景
