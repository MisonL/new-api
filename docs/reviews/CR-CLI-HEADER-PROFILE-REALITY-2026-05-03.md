# CR-CLI-HEADER-PROFILE-REALITY-2026-05-03

## 范围

- 仓库：`/Volumes/Work/code/new-api`
- 目标：清理 `Header Profile` 内 AI Coding CLI 模板中的假值，改成有证据支撑的固定请求头快照，并补齐需要 `pass_headers` 的客户端

## 证据来源

### 1. 本机已安装 CLI 版本

- `codex --version` -> `codex-cli 0.128.0`
- `claude --version` -> `2.1.126 (Claude Code)`
- `node -p "require('@google/gemini-cli/package.json').version"` -> `0.40.1`
- `node -p "require('@qwen-code/qwen-code/package.json').version"` -> `0.15.6`
- `amp version` -> `0.0.1777753404-g60c948`
- `droid --version` -> `0.115.0`

### 2. 真实请求抓包 / 安装产物提取

- Codex CLI 本机真实请求抓包：
  - 路径：`/v1/responses`
  - `User-Agent: codex_exec/0.128.0 (Mac OS 15.7.3; x86_64) ghostty/1.3.1 (codex_exec; 0.128.0)`
  - `originator: codex_exec`
  - 同时存在：
    - `x-codex-beta-features`
    - `x-codex-turn-metadata`
    - `x-codex-window-id`
    - `x-client-request-id`
    - `session_id`
- Qwen Code 本机真实请求抓包：
  - 路径：`/chat/completions`
  - `User-Agent: QwenCode/0.15.6 (darwin; x64)`
  - 同时存在：
    - `X-Stainless-Lang`
    - `X-Stainless-Package-Version`
    - `X-Stainless-Os`
    - `X-Stainless-Arch`
    - `X-Stainless-Runtime`
    - `X-Stainless-Runtime-Version`
    - `X-Stainless-Retry-Count`
- Claude Code 本机真实请求抓包：
  - 路径：`/v1/messages?beta=true`
  - `User-Agent: claude-cli/2.1.126 (external, sdk-cli)`
  - 同时存在：
    - `X-Claude-Code-Session-Id`
    - `X-Stainless-Arch`
    - `X-Stainless-Lang`
    - `X-Stainless-Os`
    - `X-Stainless-Package-Version`
    - `X-Stainless-Retry-Count`
    - `X-Stainless-Runtime`
    - `X-Stainless-Runtime-Version`
    - `X-Stainless-Timeout`
    - `Anthropic-Beta`
    - `Anthropic-Dangerous-Direct-Browser-Access`
    - `Anthropic-Version: 2023-06-01`
    - `X-App: cli`
- Gemini CLI 本机真实请求日志：
  - 目标 URL：`https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse`
  - `User-Agent: GeminiCLI/0.40.1/gemini-3.1-pro-preview (darwin; x64; terminal) google-api-nodejs-client/9.15.1`
  - 同时存在：
    - `x-goog-api-client: gl-node/24.14.0`
  - 结论：
    - 该命令链路实际走 `cloudcode-pa.googleapis.com`，不是简单的 `GOOGLE_GEMINI_BASE_URL` 覆盖路径
    - 当前可实锤的默认固定头只有 `User-Agent` 和 `x-goog-api-client`
- OpenCode 本机二进制提取：
  - 默认参数中存在 `--user-agent=opencode/1.1.14`
- OpenCode 本机 live capture：
  - 使用隔离目录 `/tmp/opencode-capture-chat` 与 `/tmp/opencode-capture-responses`，只写临时 `opencode.json`，不修改仓库和 `~/.config/opencode`
  - `@ai-sdk/openai-compatible` provider 命中 `POST /v1/chat/completions`
  - 该请求 `User-Agent: ai-sdk/openai-compatible/1.0.29 ai-sdk/provider-utils/3.0.19 runtime/bun/1.3.5`
  - `@ai-sdk/openai` provider 命中 `POST /v1/responses`
  - 该请求 `User-Agent: ai-sdk/openai/2.0.71 ai-sdk/provider-utils/3.0.17 runtime/bun/1.3.5`
  - 两条可控 upstream 请求都未出现 `opencode/1.1.14` 或 `X-Session-Affinity`

### 3. 未纳入固定模板的客户端

- `droid`
- `amp`
- `opencode`

原因：

- 当前本机只能确认 CLI 自身版本和前置服务层存在，未拿到可审计的真实上游固定请求头值
- 为避免继续向用户暴露 `Droid/1.0`、`AmpCLI/1.0` 这类伪值，本轮先从 shipped preset 中移除
- OpenCode 虽已完成 live capture，但真实 upstream UA 来自 AI SDK provider，而不是 OpenCode 自身；不同 provider 对应 `/chat/completions` 与 `/responses` 两种路径，不适合继续作为唯一内置 OpenCode 固定模板

## 继续研究补充

### 1. Claude Code

- 当前结论：
  - 通过 `--bare --settings /tmp/claude-capture-settings.json` 强制指向本地捕获端点后，已实锤真实上游请求
  - 旧的 `claude-code/2.1.126` 只适合作为安装产物版本线索，不应继续当作固定 `User-Agent` 模板
  - 当前应以 live capture 的 `claude-cli/2.1.126 (external, sdk-cli)` 为固定快照，并把 `X-Claude-Code-Session-Id` 纳入透传白名单

### 2. Amp

- 实测阻塞：
  - 注入 `AMP_URL=http://127.0.0.1:<capture-port>` 与 `AMP_API_KEY=test-key` 后，CLI 直接返回 `Error: User not found`
  - 本地捕获端点没有收到任何请求
- 当前结论：
  - 这轮只能确认 `amp` 在进入可控 upstream capture 之前就被服务侧身份校验挡住
  - 仍不能把某个 `User-Agent` 或透传头当作已确认值写回模板

### 3. Droid

- 实测阻塞：
  - 即使通过 `--settings /tmp/droid-capture-settings.json` 指向本地捕获端点并设置 `FACTORY_API_KEY=test-key`
  - CLI 仍直接返回 `Authentication failed. Please log in using /login or set a valid FACTORY_API_KEY environment variable.`
  - 本地捕获端点没有收到任何请求
- 当前结论：
  - 这轮只能确认它被平台鉴权前置挡住，尚未进入可控 upstream capture
  - 仍不能把某个固定 `User-Agent` 或透传头写回模板

### 4. OpenCode

- 官方配置与本机二进制中可确认：
  - 自定义 provider 可通过 `@ai-sdk/openai-compatible` 和 `@ai-sdk/openai` 配置 `options.baseURL`
  - 存在 `--user-agent=opencode/1.1.14`
  - 代码路径显式识别：
    - `/v1/responses`
    - `/chat/completions`
- 本机二进制中未检出：
  - `X-Session-Affinity`
  - `x-session-affinity`
  - `session affinity`
- 本机 live capture 可确认：
  - `@ai-sdk/openai-compatible` 命中 `/v1/chat/completions`
  - `@ai-sdk/openai` 命中 `/v1/responses`
  - 两种路径的 upstream `User-Agent` 均为 AI SDK provider 运行时 UA，不是 `opencode/1.1.14`
  - 两种路径均未出现 `X-Session-Affinity`
- 当前结论：
  - `opencode/1.1.14` 只能说明 CLI 自身默认参数，不能作为模型 upstream 固定请求头模板
  - 不能把 `X-Session-Affinity` 当作默认透传模板对外推荐
  - 本轮移除 OpenCode 内置 Header Profile 和对应透传白名单

### 5. Gemini CLI

- 当前结论：
  - `gemini -p 'say ok' --yolo` 已给出 live request 错误日志，能直接读取真实目标 URL 和请求头
  - 旧的 `gemini-2.5-pro` 默认模型推断不再适合作为固定 UA 模板
  - 当前应以 live request 的 `GeminiCLI/0.40.1/gemini-3.1-pro-preview (darwin; x64; terminal) google-api-nodejs-client/9.15.1` 为固定快照
  - `X-Goog-Api-Version` 与 `X-Goog-User-Project` 本轮未实抓，不应继续放在必需透传白名单里

## 代码调整

- `dto/header_profile.go`
  - Claude Code 固定 UA 改成 live capture 的 `claude-cli/2.1.126 (external, sdk-cli)`
  - Gemini CLI 固定 UA 改成 live capture 的 `gemini-3.1-pro-preview` + `google-api-nodejs-client/9.15.1`
  - Qwen Code 描述改成 live capture 的 `/chat/completions` 口径
  - 移除 OpenCode 内置 Header Profile，避免把 CLI 默认参数误当作 upstream 请求头
- `setting/operation_setting/channel_affinity_setting.go`
  - Claude CLI 透传白名单新增 `X-Claude-Code-Session-Id`
  - Gemini CLI 透传白名单收紧为 `User-Agent` + `X-Goog-Api-Client`
  - Qwen Code 透传白名单移除未实抓的 `X-Stainless-Timeout`
  - 移除 OpenCode 默认透传白名单
- `web/src/components/table/channels/modals/headerProfile.constants.js`
  - 同步前端 builtin profile
- `web/src/constants/channel-affinity-template.constants.js`
  - 同步前端透传白名单收紧结果
- `web/src/helpers/headerOverrideUserAgent.js`
  - 同步旧 UA 选择菜单中的真实固定值
  - Claude / Gemini 改为 live capture 的真实固定 UA
  - 移除 OpenCode 固定 UA 选项

## 已执行验证

### 1. 前端 helper 测试

```bash
node --test web/src/components/table/channels/modals/headerProfile.helpers.test.js
```

- 退出码：`0`

### 2. 后端定向测试

```bash
go test ./controller ./service -run 'HeaderProfile|CliHeaderPassthroughTemplateDefinitions' -count=1
```

- 退出码：`0`

### 3. 后端回归测试

```bash
go test ./controller ./service -count=1
```

- 退出码：`0`

### 4. 前端生产构建

```bash
cd web && bun run build
```

- 退出码：`0`
