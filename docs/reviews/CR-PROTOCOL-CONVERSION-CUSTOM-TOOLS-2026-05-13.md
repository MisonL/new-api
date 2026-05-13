# CR-PROTOCOL-CONVERSION-CUSTOM-TOOLS-2026-05-13

## Summary

采样日期：2026-05-13。

本轮记录 `main` 上 Responses 到 Chat Completions 协议转换的自定义工具桥接能力，以及对应的 `web/default` 系统设置页、i18n 同步和隔离开发环境验证。

结论：

- `main` 与 `origin/main` 已对齐到 `c72c357c59f4660d03f9b5a24a28be8726ffb2b1`。
- 隔离开发容器 `new-api-dev-isolated-new-api-1` 运行同一提交。
- `3001` `/api/status` 返回 `success:true`，容器健康状态为 `healthy`。
- `web/default` 通过 Vite 代理到 `3001` 后，系统设置页可真实登录并进入 `/system-settings/models/global`。
- “协议转换兼容”区块可见，点击“填充自定义工具示例”后可填入带 `options.enable_custom_tool_bridge: true` 的策略 JSON。
- 浏览器控制台未发现 error。

## Commits

- `e5b4225d3` `feat: bridge responses custom tools to chat`
- `36157788b` `feat(web): add protocol conversion policy examples`
- `c72c357c5` `chore(i18n): clean stale translation reports`

## Scope

本轮覆盖：

- Responses 请求转 Chat Completions 请求时的自定义工具桥接。
- Chat Completions 自定义工具结果转 Responses 输出项。
- 流式和非流式兼容路径。
- 全局协议转换策略中的规则化方向配置。
- `web/default` 全局模型配置页的策略示例、文案和多语言翻译。
- i18n 同步报告中过期 untranslated report 的自动清理。

本轮不覆盖：

- 真实上游模型调用。
- 生产 `3000` 环境。
- 对用户现有策略的自动迁移或写库变更。

## Code Facts

后端主要文件：

- `service/openaicompat/policy.go`
- `service/openaicompat/responses_to_chat_request.go`
- `service/openaicompat/chat_to_responses_response.go`
- `service/openai_chat_responses_compat.go`
- `service/openai_chat_responses_mode.go`
- `relay/responses_handler.go`
- `relay/responses_via_chat.go`
- `relay/channel/openai/responses_via_chat.go`
- `setting/model_setting/global.go`
- `dto/openai_response.go`

后端测试文件：

- `service/openaicompat/compat_test.go`
- `relay/responses_handler_test.go`
- `relay/responses_via_chat_test.go`
- `relay/channel/openai/responses_via_chat_test.go`
- `setting/model_setting/global_test.go`

前端和 i18n 文件：

- `web/default/src/features/system-settings/models/global-settings-card.tsx`
- `web/default/src/i18n/locales/en.json`
- `web/default/src/i18n/locales/zh.json`
- `web/default/src/i18n/locales/fr.json`
- `web/default/src/i18n/locales/ja.json`
- `web/default/src/i18n/locales/ru.json`
- `web/default/src/i18n/locales/vi.json`
- `web/default/scripts/sync-i18n.mjs`
- `web/default/src/i18n/locales/_reports/_sync-report.json`

## Policy Example

系统设置页“填充自定义工具示例”会写入以下规则形态：

```json
{
  "rules": [
    {
      "name": "responses-to-chat-codex-custom-tools",
      "enabled": true,
      "source_endpoint": "responses",
      "target_endpoint": "chat_completions",
      "all_channels": false,
      "channel_ids": [1],
      "model_patterns": ["^gpt-5.*$"],
      "options": {
        "enable_custom_tool_bridge": true
      }
    }
  ]
}
```

行为边界：

- 未显式开启 `enable_custom_tool_bridge` 时，不桥接 Responses 自定义工具。
- 启用后，Responses 自定义工具会映射到 Chat Completions 兼容的工具调用结构。
- Responses 原生但 Chat 兼容路径暂不适用的工具会被过滤，不走伪成功。
- JSON 编解码仍应遵守仓库约束，业务代码中新增编解码走 `common.Marshal` / `common.Unmarshal`。

## Verification

### Go

命令：

```bash
go test ./relay/... ./service/... ./setting/...
```

结果：

- 退出码 `0`。
- 相关 relay、service、setting 包通过。

### Frontend

命令：

```bash
cd web/default
bun run i18n:sync
bun run lint
bun run build
```

结果：

- 三个命令退出码均为 `0`。
- `_sync-report.json` 中所有 locale 的 `missingCount`、`extrasCount`、`untranslatedCount` 均为 `0`。

### Diff Hygiene

命令：

```bash
git diff --check
```

结果：

- 退出码 `0`。

## Gemini Review

使用 Gemini CLI 以只读方式复审当前 diff 和桌面/移动截图。

结论：

- Blocker：无。
- 主要 non-blocker 是非英文 locale 中新增文案仍保留英文。
- 后续已补齐 `fr`、`ja`、`ru`、`vi` 翻译，并修复 i18n report stale 文件清理。

## Isolated Dev Deployment

构建与重建命令：

```bash
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
```

运行态确认：

```bash
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
docker inspect -f '{{.State.Status}} {{.State.Health.Status}} {{.Config.Image}} {{.Image}}' new-api-dev-isolated-new-api-1
curl -fsS http://127.0.0.1:3001/api/status
```

关键结果：

```text
version=v1.1.0
commit=c72c357c59f4660d03f9b5a24a28be8726ffb2b1
date=2026-05-13T14:31:01Z
source=https://github.com/MisonL/new-api
```

```text
running healthy new-api-local:dev sha256:5da2e8d89205092a8db32b10e7581947089275a654fd0393236e728e618bc648
```

`/api/status` 返回 `success:true`。

## Browser Verification

验证方式：

- 启动 `web/default` dev server：

```bash
cd web/default
VITE_REACT_APP_SERVER_URL=http://127.0.0.1:3001 bun run dev --host 127.0.0.1 --port 5176
```

- 在浏览器访问 `http://127.0.0.1:5176/system-settings/models`。
- 使用隔离 dev 数据库中的临时 smoke admin 真实登录。
- 登录后进入 `http://127.0.0.1:5176/system-settings/models/global`。
- 检查完成后删除临时 smoke admin 和本地临时登录文件。

验证结果：

- 页面显示“协议转换兼容”。
- 页面显示“填充自定义工具示例”。
- 点击按钮后，策略 JSON 中出现：
  - `responses-to-chat-codex-custom-tools`
  - `source_endpoint: "responses"`
  - `target_endpoint: "chat_completions"`
  - `options.enable_custom_tool_bridge: true`
- 浏览器 console error 数量为 `0`。
- 临时 smoke admin 清理后，隔离 dev 数据库中 `codex-smoke-%` 用户数量为 `0`。

## Operational Notes

- `3001` 容器入口当前可能按系统配置加载 classic 主题；它证明后端服务和镜像已同步，不等价于 `web/default` 管理页视觉验证。
- 验证 `web/default` 管理页时，应使用 Vite dev server 代理到 `3001`。
- 受 `middleware.UserAuth()` 保护的后台接口，仅有 Cookie 不够，直接接口探测还需要 `New-Api-User` 请求头。
- 本轮没有向生产 `3000` 环境写入数据。

## Final State

- `main` 与 `origin/main` 对齐。
- 工作区无未提交改动。
- 最新提交已推送到 `origin/main`。
- 隔离 dev `3001` 运行最新提交并保持健康。
