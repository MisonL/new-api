# new-api Codex Bridge

从 `new-api` OpenAI 兼容网关生成本机 Codex App 配置，让
`new-api` 模型出现在 Codex App 的模型下拉菜单中。

```bash
cd tools/codex-bridge
node ./bin/new-api-codex-bridge.js
```

当前工具先按源码本地验证，`package.json` 也标记为 `private`，暂不发布 npm 包。
默认命令会进入交互式配置向导。
它会询问 Codex 主目录、`new-api` URL、
API Key 和要展示的模型。配置完成后需要重启 Codex App，因为
`model_catalog_json` 在 Codex 启动时加载。

默认认证模式是 `provider-token`：API Key 写在 `[model_providers.new-api]`
的 `experimental_bearer_token` 中，不覆盖 Codex 官方登录使用的 `auth.json`。
如果你明确需要旧行为，可以传 `--auth-mode auth-json`；如果你会用环境变量启动
Codex，可以传 `--auth-mode env --env-key NEW_API_KEY`。
`env` 模式只会在 `config.toml` 写入 `env_key`，不会保存 API Key；启动
Codex App 的进程环境中必须已经存在这个变量。

脚本用法建议通过环境变量传 key，避免明文 key 留在 shell 历史记录：

```bash
export NEW_API_KEY=sk-your-key
cd tools/codex-bridge
node ./bin/new-api-codex-bridge.js setup \
  --base-url https://new-api.example.com \
  --api-key-env NEW_API_KEY \
  --auth-mode provider-token \
  --default-model gpt-5.5 \
  --all-models \
  --yes
```

常用命令：

- `setup`：交互式或参数式写入配置；不带子命令时默认执行。
- `print`：预览将生成的文件，`auth.json` 和含 `experimental_bearer_token`
  的 `config.toml` 会被脱敏。
- `doctor`：检查 `config.toml`、`auth.json`、模型目录和回执。
- `refresh`：重新拉取 `/v1/models`，回执不保存 API Key。
- `restore --backup <id>`：从配置或刷新时生成的备份恢复。

WSL 注意事项：WSL 内的 Codex CLI 和 Windows 侧 Codex App 通常不是同一个
`~/.codex` 目录。在 WSL 中运行本工具时，应明确选择目标。

发布前说明：包名预留为个人 npm 作用域 `@mison/codex-bridge`。正式公开发布前，
不要在产品界面或文档中把 `npx @mison/codex-bridge` 当作可执行命令使用。
要公开发布时，先移除 `private: true`，再补回 `publishConfig.access = "public"`。
