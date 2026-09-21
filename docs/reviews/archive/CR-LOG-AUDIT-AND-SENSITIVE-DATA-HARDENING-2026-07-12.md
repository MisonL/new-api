# 正式服务日志审计与敏感信息加固记录

## 审计范围

- 采样时间：2026-07-12 16:05 至 16:40，时区为 Asia/Shanghai。
- 正式服务：`new-api`，构建提交 `56db36536a493af242a95fc80d476b89c79952b9`。
- 开发服务：`new-api-dev-isolated-new-api-1`，端口 `3001`。
- 数据源：Docker 标准输出、PostgreSQL `logs` 表、PostgreSQL 和 Redis 容器日志、Docker 健康状态与资源统计。

## 审计结论

- 正式服务和开发服务均为健康状态，无重启、OOM、panic 或 fatal。
- 正式服务升级后未再出现 `input[*].arguments` 类型错误、`input[*].namespace` 错误或上下文窗口错误。
- 自动 compact 已产生 2 条 `mid_turn`、`remote_v2`、`memento` 成功日志，最大输入 333192 token；高缓存请求最大缓存读取 322304 token。
- PostgreSQL 无等待锁和超过 5 秒的长查询；PostgreSQL、Redis 自正式服务重启后无自身错误日志。
- 渠道 `205` 在采样窗口内成功 0 次、524 错误 28 次。全局自动禁用关闭，因此该渠道不会自动退出路由。
- `grok-4.5` 没有启用渠道，6 次请求按设计显式返回 503，没有发生隐藏模型回退。

## 发现与修复

发现两类敏感信息落盘风险：

1. Gin 访问日志记录完整查询参数，后台按密钥搜索渠道时会把密钥写入日志。
2. 渠道测试成功后直接记录完整上游响应，可能包含 instructions、输入和输出正文。

修复内容：

- 访问日志保留请求路径和查询参数名，但统一将所有查询参数值替换为 `REDACTED`。
- 渠道测试日志只记录渠道 ID、HTTP 状态码和响应字节数，不再记录响应正文。

## 验证结果

以下命令均退出码为 0：

```bash
go test ./controller ./middleware ./model ./relay/common ./relay/helper ./service
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
```

Docker 镜像第二次构建时曾因 Docker Hub 元数据请求返回 EOF 而失败；未忽略该失败，立即重试后构建退出码为 0，并使用重试成功的镜像重新创建开发容器。

开发环境真实请求：

```text
GET /api/channel/search?group=REDACTED&keyword=REDACTED&p=REDACTED
```

原始伪密钥未出现在重建后的开发服务访问日志中。

## 边界

- `previous_response_id` 工具回填测试因上游响应使用 `store=false` 而返回“找不到工具调用”；显式携带完整工具历史的真实请求已成功返回 `MULTI_TURN_OK`。
- 对象类型 `arguments` 的真实请求未出现协议 400，而是在渠道连续 502 后耗尽重试。该结果证明类型兼容层已执行，但不能把上游可用性表述为已通过。
- `gpt-5.6-sol` 仍存在少量上游 502、524、并发限制和流中断，属于渠道可用性问题，不属于本次日志加固或 Responses 类型转换回归。

## 正式部署与历史清理

- 正式部署时间：2026-07-12 17:11，时区为 Asia/Shanghai。
- 正式镜像：`new-api-local:prod-20260712-log-redaction-884791965`。
- 正式构建提交：`884791965535bc55e634eba833279e22eb701d1c`。
- 升级前数据库备份：`backups/new-api-before-compact-control-plane-v2-20260712T091055Z.sql`。
- 备份 SHA-256：`a51eed729a57deb4c75b5eb932e34313bd8d0d1c6eee6c51a32235e377c95eb8`。
- 正式升级脚本完成构建信息、状态端点和近期错误扫描，升级结果为 `deployed`。
- 渠道 `205` 在正式重启前后持续 502、524 且成功为 0，已通过管理 API 手动禁用；数据库状态为 `2`，启用能力数为 0。
- 历史日志中 106 行长 `sk-` 值已替换为固定占位符，557 行完整 Responses JSON 已替换为审计标记。
- 清理后重新扫描：长 `sk-` 值 0 行、完整 Responses JSON 0 行、Bearer token 0 行、access token 0 行、session cookie 0 行、PostgreSQL DSN 0 行、Redis DSN 0 行。
