# CR-CODEX-HEADER-PROFILE-PRESET-2026-05-19

## 范围

- 修正 Codex 请求头预置口径。
- 保持 `codex-cli` 固定快照指向交互式 TUI 的 `codex-tui` 请求身份。
- 新增 `codex-desktop` 固定快照，使用 Codex Desktop 真实请求 User-Agent 样例。
- Codex CLI / Codex Desktop 透传模板补齐 `User-Agent` 与 `Originator`，用于保留真实客户端身份。

## 验证

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `go test -timeout 60s ./dto ./service ./controller -run 'HeaderProfile\|CliHeaderPassthroughTemplateDefinitions\|BuildFetchModelsHeaders' -count=1` | 0 | Header Profile、Codex Desktop 固定快照、Codex 透传清单相关 Go 测试通过 |
| `node --test web/classic/src/components/table/channels/modals/headerProfile.helpers.test.js web/tests/headerOverridePolicy.test.mjs` | 0 | classic Header Profile、旧 UA 预置、Codex Desktop 预置相关前端测试通过 |
| `rg -n "codex-cli@\|codex_exec\|source=exec\|Codex Desktop\|codex-tui/0.130.0\|CODEX_CLI_HEADER_PASSTHROUGH_HEADERS\|CodexCliPassThroughHeaders" dto setting service controller docs/channel web/classic/src/components/table/channels/modals web/classic/src/constants web/classic/src/helpers web/default/src/features/channels web/default/src/features/system-settings/general/channel-affinity web/tests -g '!node_modules'` | 0 | `codex-cli@...` 仅保留在版本化 profile ID 测试语义中；未作为 User-Agent 模板使用 |

## 结论

本轮已覆盖静态 Header Profile、旧版 User-Agent 预置、Codex 请求头透传模板和文档口径。未做真实 Codex Desktop 客户端联调，当前结论基于用户提供的抓包样例、代码常量和本地测试。

## 高级规则预置模板补充

- 移除 `Codex 透传 + 移除图片工具` 组合预置，保留 `Codex Header Passthrough` 与 `Remove Image Generation Tool` 两个单独模板。
- 将 `AWS Bedrock Claude Compat` 拆成 `AWS Bedrock Claude Beta Header` 与 `AWS Bedrock Remove Input Examples` 两个单独模板。
- 新增 `web/tests/paramOverridePresetContracts.test.mjs`，防止高级参数覆盖预置重新暴露组合模板。

补充验证：

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `node --test web/classic/src/components/table/channels/modals/headerProfile.helpers.test.js web/tests/headerOverridePolicy.test.mjs web/tests/paramOverridePresetContracts.test.mjs` | 0 | Header Profile、旧 UA 预置、高级参数覆盖预置单 operation 契约通过 |
| `rg -n "<combined preset markers>" web/default/src web/classic/src docs -g '!node_modules' -g '!**/*.test.*'` | 1 | 生产代码与文档中无组合预置残留 |
