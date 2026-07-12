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

- 本次代码修复只影响后续日志，不会自动改写既有日志文件。
- 正式服务尚未部署本次敏感日志加固代码，正式服务当前仍运行提交 `56db36536a493af242a95fc80d476b89c79952b9`。
- 渠道 `205` 的禁用属于正式配置变更，未在本次代码修复中静默执行。
