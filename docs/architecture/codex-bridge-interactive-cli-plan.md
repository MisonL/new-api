# Codex Bridge 交互式 CLI

## 背景

`new-api` 已经能提供 OpenAI 兼容的 `/v1/models` 和
`/v1/responses`。这足够让 Codex CLI 在用户手动配置提供方时访问
第三方模型，但不能自动让第三方模型出现在 Codex App 的模型下拉菜单中。

本任务的目标效果是用户截图中的 Codex App 模型选择体验：通过 `new-api`
中转的第三方模型能出现在 App 模型菜单里，用户可以在界面上手动切换。

当前 Codex 可行路径是本机配置注入：

- `~/.codex/config.toml` 可以配置 `model`、`model_provider`、
  `model_catalog_json` 和 `[model_providers.<id>]`。
- `model_catalog_json` 是 Codex 启动时加载的本地 JSON 模型目录。
- `model_catalog_json` 应写相对文件名，不写绝对路径，避免 WSL、symlink、
  跨盘路径在 Codex App/CLI 中失效。
- 自定义提供方使用 OpenAI 兼容的 Responses API 时，应配置
  `wire_api = "responses"`。
- 提供方和认证配置属于用户级 Codex 配置，不应写到项目级
  `.codex/config.toml`。

在本机 `codex-cli 0.142.1` 上验证得出：模型目录条目不是松散的
`{ model, displayName }` 结构。能被当前 Codex 接受的最小模型条目需要使用
Codex 的 snake_case 字段，例如 `slug`、`display_name`、
`supported_reasoning_levels`、`base_instructions`、
`supports_reasoning_summaries`、`support_verbosity`、工具能力字段和上下文窗口
元数据。

## 目标

构建跨平台 Codex Bridge CLI。当前阶段先以源码本地运行验证，不发布 npm 包。
用户在启动或重启 Codex App 前运行该工具；工具从 `new-api` 发现模型，生成
Codex 模型目录，安全写入本机 Codex 配置，并给出清晰的成功或诊断结果。

完成 setup 后，Codex App 应能加载 `new-api` 模型并在模型下拉菜单中展示，
用户可以在 App UI 中手动切换。

## 范围

包含：

- 在仓库中新增独立 Node ESM CLI 包。
- 默认无参数进入交互式配置向导。
- 支持高级用户通过参数进行非交互式配置。
- 生成或更新 `config.toml`、`new-api-model-catalog.json` 和不含密钥的
  回执；仅在兼容模式下更新 `auth.json`。
- 提供 `setup`、`print`、`doctor`、`refresh`、`restore` 命令。
- 在 `web/default` 的 API Key/token 工作流附近增加最小指引。
- 增加中文用户文档和验证说明。

不包含：

- 本任务不修改 `new-api` relay 行为。
- 不在 CLI 中实现 Chat Completions 到 Responses 的协议转换。
- 不把 API Key 写入模型目录、回执、文档、测试数据或日志。
- 不让服务端 `new-api` 写用户本机 Codex 配置文件。

## 用户流程

默认流程：

1. 用户运行 `node ./bin/new-api-codex-bridge.js`。
2. 配置向导询问 Codex 主目录、`new-api` 基础 URL、API Key、默认模型和模型范围。
3. 工具请求 `{baseUrl}/v1/models`。
4. 工具预览目标文件和模型选择。
5. 工具备份已有文件，原子写入新文件，并在支持的平台设置安全权限。
6. 用户重启 Codex App。
7. Codex App 加载 `new-api-model-catalog.json`，模型出现在模型菜单中。

高级用法：

```bash
cd tools/codex-bridge
node ./bin/new-api-codex-bridge.js setup \
  --base-url https://new-api.example.com \
  --api-key-env NEW_API_KEY \
  --codex-home ~/.codex \
  --default-model gpt-5.5 \
  --all-models \
  --yes
```

## 命令

- `setup`：交互式或参数式配置，并写入文件。
- `print`：只预览生成内容，不写文件。
- `doctor`：检查 Codex 配置、提供方、模型目录、认证和回执是否一致。
- `refresh`：基于回执和当前认证重新拉取 `/v1/models`。
- `restore`：从工具生成的备份恢复配置。
- `help`：显示简短帮助。

## 文件

目标 Codex home 默认规则：

- 显式设置 `CODEX_HOME` 时优先使用它。
- Windows：`%USERPROFILE%\.codex`。
- macOS/Linux：`~/.codex`。
- WSL：询问目标是 WSL 内的 Codex 还是 Windows 侧 Codex App，不自动同时写两边。

生成文件：

- `config.toml`：默认模型、提供方、模型目录文件名和提供方配置块。
- `auth.json`：仅 `auth-json` 兼容模式使用，写入必要 API Key 信息，并保留
  用户已有无关字段。
- `new-api-model-catalog.json`：根据 `/v1/models` 生成的 Codex 模型目录。
- `new-api-codex-bridge-receipt.json`：不含密钥的配置元数据。
- `backups/new-api-codex-bridge/<backup-id>/`：写入前的文件快照。

`config.toml` 目标片段：

```toml
model = "gpt-5.5"
model_provider = "new-api"
model_catalog_json = "new-api-model-catalog.json"

[model_providers.new-api]
name = "new-api"
base_url = "https://new-api.example.com/v1"
wire_api = "responses"
experimental_bearer_token = "<redacted>"
```

默认认证模式是 `provider-token`，会在提供方配置块中写入
`experimental_bearer_token`。这是为了让 Codex App 重启后能直接使用第三方
提供方，同时避免覆盖 Codex 官方登录使用的 `auth.json`。

可选认证模式：

- `provider-token`：默认模式，key 写入 `[model_providers.new-api]` 的
  `experimental_bearer_token`，Unix-like 下 `config.toml` 设为 `0600`。
- `auth-json`：兼容旧工具行为，写入 `auth.json` 的 `OPENAI_API_KEY`，并在
  provider 中配置 `requires_openai_auth = true`。
- `env`：提供方中只写 `env_key`，不写 key 文件；适合命令行和受控启动环境，
  但 Codex App 需要从带环境变量的进程启动。

写入器应尽量保留无关已有配置。它可以在备份后替换 bridge 管理块和顶层 Codex
默认项。遇到冲突时，交互模式必须明确提示；非交互模式必须通过 `--yes` 或
明确参数才能继续。

## 模型目录模板

每个 `/v1/models` 返回的 id 映射为一个模型条目。生成器使用稳定的
Codex 兼容模板，只替换模型相关字段。

已通过 `codex debug models` 验证的结构：

```json
{
  "models": [
    {
      "slug": "model-id",
      "display_name": "model-id",
      "description": "Provided by new-api.",
      "default_reasoning_level": "medium",
      "supported_reasoning_levels": [
        { "effort": "low", "description": "Fast" },
        { "effort": "medium", "description": "Balanced" },
        { "effort": "high", "description": "Deep" }
      ],
      "shell_type": "shell_command",
      "visibility": "list",
      "supported_in_api": true,
      "priority": 50,
      "additional_speed_tiers": [],
      "service_tiers": [],
      "availability_nux": null,
      "upgrade": null,
      "base_instructions": "You are Codex, a coding agent.",
      "model_messages": {
        "instructions_template": "You are Codex, a coding agent.\n\n{{ personality }}"
      },
      "supports_reasoning_summaries": true,
      "default_reasoning_summary": "none",
      "support_verbosity": true,
      "default_verbosity": "low",
      "apply_patch_tool_type": "freeform",
      "web_search_tool_type": "text_and_image",
      "truncation_policy": { "mode": "tokens", "limit": 10000 },
      "supports_parallel_tool_calls": true,
      "supports_image_detail_original": true,
      "context_window": 200000,
      "max_context_window": 200000,
      "effective_context_window_percent": 95,
      "experimental_supported_tools": [],
      "input_modalities": ["text", "image"],
      "supports_search_tool": true,
      "use_responses_lite": false
    }
  ]
}
```

## 安全规则

- 交互式 API Key 输入必须隐藏回显。
- 推荐通过交互提示或环境变量提供 key，避免在 shell 历史记录中留下明文 key。
- 不把 API Key 写入模型目录或回执。
- 不在日志、错误、测试、文档或最终输出里打印 API Key。
- 默认不更新 `auth.json`，避免破坏 Codex 官方登录状态。
- `auth-json` 模式更新 `auth.json` 时保留已有认证字段。
- Unix-like 系统下，含密钥的 `config.toml` 或 `auth.json` 权限设置为 `0600`。
- 写文件前先备份，写入使用原子替换。
- 网络、认证、解析错误必须显式失败；不 mock 成功。

## 跨平台规则

- 使用 Node 标准库 path API，不硬编码路径分隔符。
- 即使目标 Codex home 是绝对路径，`model_catalog_json` 也只写
  `new-api-model-catalog.json`。
- WSL 检测应结合环境变量和内核信息。
- WSL 中只有在能识别 `/mnt/c/Users/...` 或用户明确提供路径时，才提供
  Windows App 目标。
- 不假设 Windows Codex App 与 WSL Codex CLI 共用同一个配置目录。

## 实施任务

- [x] 创建零依赖 Node ESM CLI 包和 `bin` 入口。
  验证：`node <bin> --help` 能输出命令帮助。
- [x] 实现 URL 规范化和 `/v1/models` 发现。
  验证：测试覆盖带 `/v1` 和不带 `/v1`、认证失败、404、非 JSON、空模型列表。
- [x] 实现模型目录生成。
  验证：生成的模型目录能在临时 `CODEX_HOME` 下通过 `codex debug models`。
- [x] 实现配置、三种认证模式、回执、备份、原子写入和恢复。
  验证：临时目录测试覆盖配置、检查、恢复、认证字段保留和默认不碰
  `auth.json`。
- [x] 实现交互式向导和非交互参数。
  验证：无参数进入配置；TTY 模式下 API Key 输入不回显。
- [x] 实现 `print`、`doctor`、`refresh`、`restore`。
  验证：命令测试覆盖成功路径和失败诊断。
- [x] 在 `web/default` 增加 API Key/token 最小指引。
  验证：`bun run typecheck` 和相关 lint 通过。
- [x] 增加中文文档，说明 Codex App 重启、WSL 目标选择和安全用 key。
  验证：文档不含真实 key，示例可复制。
- [x] 运行最终验证。
  验证：CLI 测试通过、Codex 模型目录冒烟验证通过、前端检查通过，
  `git diff --name-only` 只包含任务范围文件和已知既有 dirty 文件。

## 发布口径

- 当前阶段不发布 npm 包；`tools/codex-bridge/package.json` 保持 `private: true`。
- 后续若要公开发布，再补回 npm 发布配置并重新核对包内容。

## 已验证结果

- `cd tools/codex-bridge && npm test`：38 项通过。
- `cd tools/codex-bridge && npm pack --dry-run`：打包清单已排除测试文件。
- `cd web/default && bun test tests/cc-switch-url.test.ts`：4 项通过。
- `cd web/default && bun run lint`：通过。
- `cd web/default && bun run typecheck`：通过。
- 临时 `CODEX_HOME` 下 `codex debug models`：能识别生成的 `gpt-5.5` 模型。

## 验收标准

- `node ./bin/new-api-codex-bridge.js` 可以不输入长参数完成交互式 setup。
- 生成的配置使用 `wire_api = "responses"` 和 `/v1` 提供方基础 URL。
- 生成的模型目录能被 `codex debug models` 接受。
- `model_catalog_json` 始终为相对文件名。
- `doctor` 能发现缺少配置、缺少模型目录、缺少认证和提供方不匹配。
- `refresh` 能更新模型列表，且回执不存 API Key。
- 默认模式不覆盖 Codex 官方 `auth.json`。
- `restore` 能恢复备份文件。
- macOS、Linux、Windows、WSL 路径行为有测试覆盖。
- WebUI 指引存在，且不鼓励用户把 key 放入 shell 历史记录。
