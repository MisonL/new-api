# Responses namespace 兼容性验证

## 范围

- 仅将包含顶层 namespace 工具的请求路由到声明支持 namespace 的渠道。
- 将结构化的 `response.error` 和 `error` SSE 事件保留为上游错误。
- 上游明确返回针对 `input[index].namespace` 的 `400 unknown_parameter` 后，移除被拒绝的历史元数据字段并重试一次。
- 在请求上下文和用量日志元数据中记录该次重试。

## 行为契约

- 首次请求原样保留 `input[].namespace`。
- 仅当错误码为 `unknown_parameter`，且参数精确匹配 `input[index].namespace` 时允许重试。
- 重试时保留工具调用 ID、名称、输入和其他全部历史字段。
- 不静默改写上游不支持的顶层 namespace 工具定义。

## 验证记录

采样时间：`2026-07-10 13:42 Asia/Shanghai`。

该记录对应当时隔离环境采样，运行镜像为 `new-api-local:dev`，容器报告提交为 `51074ac3736288ee664966e0a2109e9580d86fcf-dirty`。后续基于 `919500d696501142c93e9480efeb68ec53e7bec7` 及更高提交的真实 Codex 复测记录见 `CR-GPT56-CODEX-DEV-CHANNEL-VERIFY-2026-07-10.md`。

```text
命令：go test ./controller ./relay/common ./relay/channel/openai ./relay ./service ./setting/ratio_setting -count=1
退出码：0
结果：所选包全部通过

命令：git diff --check
退出码：0
结果：未发现空白字符错误
```

隔离 Docker 服务已基于镜像 `new-api-local:dev` 强制重建。服务在 `3001` 端口进入健康状态，报告提交为 `51074ac3736288ee664966e0a2109e9580d86fcf-dirty`，并从 `/api/status` 返回 `success=true`。

自动化检查覆盖解析、路由、重试行为和隔离服务启动。本次验证未执行正式环境部署，也未向真实上游发送 namespace 请求。
