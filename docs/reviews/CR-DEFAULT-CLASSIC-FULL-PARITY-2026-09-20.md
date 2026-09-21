# Classic 与 Default 前端完整功能复核

采样时间：2026-09-20（Asia/Shanghai）

## 结论

本轮逐项对照 `web/classic` 的路由、控制台入口、请求 API、表单能力、弹窗操作、个人设置、OAuth 绑定、Playground、日志、计费、模型部署、渠道高级配置和系统设置，确认发现的真实功能缺口只有 Default 微信绑定流程，已完成补齐。未发现其他需要新增页面或后端接口的 Classic-only 功能。

## 已补齐

- Default 微信绑定弹窗从占位提示改为真实二维码、验证码输入、提交、加载、错误、成功和关闭状态。
- 微信绑定请求改为后端契约要求的 `POST /api/oauth/wechat/bind`，请求体为 `{ code }`。
- 账户绑定列表显示已绑定的 `wechat_id`，并沿用现有刷新回调。
- Default 内置 GitHub、Discord、OIDC、Linux DO 和自定义 OAuth 绑定统一使用绑定模式和浏览器能力判断；不支持浏览器绑定的 API-only Provider 显式禁用按钮。
- 新增文案已同步到 Default 的 `en`、`zh`、`fr`、`ja`、`ru`、`vi` 六个 locale。

## 对照结果

- 控制台导航、旧 Classic 路由重写、Dashboard、Chat、Playground、Chat2Link、Token、日志、任务和绘图日志：已存在且行为一致。
- 钱包、充值、兑换码、订阅、用户、渠道、模型、模型部署：已存在；渠道模型测试运行配置、Header Profile、参数覆盖、代理、模型映射和响应协议能力已对齐。
- 系统设置、维护、OAuth、Passkey、2FA、签到、通知、语言和个人账户绑定：已存在；微信绑定缺口已修复。
- `web/classic` 的 `ModelsList.jsx` 当前无页面引用，属于历史死代码，不能作为 Default 缺少可用模型页面的证据。
- API 路径名称或实现栈差异均已按功能核对；剩余差异是路径规范化、Default 新增能力或 React/Semi 与 React/Radix 实现差异，不是缺失功能。

## 验证

| 命令 | 结果 |
| --- | --- |
| `cd web/default && bun run i18n:sync` | 通过；925 个源码文件、4632 个键、0 个缺失键 |
| `cd web/default && bun test` | 通过；220 pass、0 fail、866 expect calls |
| `cd web/default && bun run typecheck` | 退出码 0 |
| `cd web/default && bun run lint` | 退出码 0 |
| Default 目标文件 `bun prettier --check ...` | 通过 |
| `cd web/default && bun run build` | 通过；Rsbuild、Safari compatibility rewrite/check 和浏览器兼容检查通过 |
| `cd web/classic && bun run test:node` | 通过；137 pass、0 fail |
| `git diff --check` | 通过 |
| `docker compose ... up -d --no-deps --force-recreate new-api` | 隔离容器重建成功 |
| `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` | `v1.1.0`，当前 dirty commit `5cda680770543782d5d0a757b31f4e783443dc03` |
| `curl --connect-timeout 3 --max-time 10 http://127.0.0.1:3001/api/status` | HTTP 200，`X-New-Api-Version: v1.1.0` |
| `curl --connect-timeout 3 --max-time 10 http://127.0.0.1:13000/api/status` | HTTP 200，正式服务只读探针 |

## 边界

- 工作区包含此前任务留下的大量 dirty 修改，本轮未回退、覆盖或清理无关文件。
- 隔离环境已重建并验证当前源码；正式 `13000` 只做了带超时的只读健康检查，未重启、重建、写入数据库或 Redis，也未宣称本轮 Default 修复已发布到正式环境。
- 本轮没有修改后端代码；微信绑定依赖正式后端已有的 `POST /api/oauth/wechat/bind` 契约，真实微信扫码和验证码链路仍需在启用微信配置的环境中进行业务验收。
