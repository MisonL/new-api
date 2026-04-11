# CR-NIGHTLY-TIERED-BILLING-INTAKE-2026-04-11

## 背景

- 目标：在不破坏当前主线 SSO、协议转换、Responses 兼容与已验证 UI 的前提下，选择性吸纳 `upstream/nightly` 中与阶梯计费、工具定价相关的变更。
- 约束：仅在隔离 worktree `/tmp/new-api-nightly-intake` 内验证，不覆盖当前主工作区的未提交改动。

## 吸纳范围

已在隔离分支 `analysis/nightly-intake-20260411` 中吸纳以下 nightly 提交：

- `91ed4e19` tiered billing expression
- `f0589cc4` tiered billing UI
- `f6c0852d` quota per unit refactor
- `5b03b39d` tiered billing variable handling
- `c5405b2a` billing expression docs and logic
- `6e3ef48c` tool pricing settings UI
- `44fc10ba` pricing presets and expressions
- `d66311e9` Doubao Seed 1.8 pricing tier
- `0220df84` channel test support for tiered billing

## 发现的问题

### 1. 工具计费 API 断口

`service/text_quota.go` 调用了以下旧接口，但 `setting/operation_setting/tools.go` 只保留了新索引接口：

- `GetWebSearchPricePerThousand`
- `GetClaudeWebSearchPricePerThousand`
- `GetFileSearchPricePerThousand`

处理方式：

- 在 `setting/operation_setting/tools.go` 增加兼容 wrapper。
- wrapper 内部统一委托给 `GetToolPriceForModel` / `GetToolPrice`。
- `search-preview` 模型映射到 `web_search_preview`，其余 web search 走 `web_search`。

### 2. 前端渲染层存在运行时告警

`web/src/helpers/render.jsx` 在多个计费展示函数中对解构得到的常量再次赋值，Vite/esbuild 构建时给出明确告警。

处理方式：

- 引入 `normalizedCompletionRatio`、`normalizedAudioRatio`、`normalizedAudioCompletionRatio` 等局部派生变量。
- 保持计费公式与 UI 文案语义不变，仅消除运行时风险。

## 本次附加修补

- `setting/operation_setting/tools.go`
  - 新增工具计费兼容 wrapper。
- `setting/operation_setting/tools_test.go`
  - 新增兼容层单元测试，覆盖 `web_search_preview` override 和默认 `web_search` / `file_search` 价格。
- `web/src/helpers/render.jsx`
  - 修正常量重新赋值告警，保持展示逻辑等价。

## 验证记录

### Go

执行命令：

```bash
go test ./setting/operation_setting
go test ./service/... ./relay/helper/... ./controller/...
go test ./...
```

结果：

- 全部通过。

### Web

执行命令：

```bash
cd web
bun install
bun run eslint
bun run build
```

结果：

- ESLint 通过。
- 补充 `.eslintignore`，避免将 `dist/` 产物纳入源码门禁。
- 为 nightly 吸纳引入的前端源码文件补齐标准许可证头注释。
- 构建通过。
- 已消除 `render.jsx` 中的 `const` 重新赋值告警。
- 对 `App.jsx` 做页面级懒加载收口，避免控制台页面继续同步打进主入口。
- 对 `vite.config.js` 增加 `icons`、`markdown`、`charts` vendor 分包规则。
- 构建后主应用 chunk 从约 `7.48 MB` 降到约 `1.39 MB`，入口负担显著下降。
- 仍存在既有非阻塞告警：
  - `lottie-web` 使用 `eval`
  - `icons` / `charts` / `semi-ui` / `markdown` 等 vendor chunk 仍超过 500 kB，但已从主入口分离

## 结论

- 这批 nightly 阶梯计费 / 工具定价变更可以选择性吸纳。
- 当前主要风险已从“集成断裂”降为“常规前端包体优化”，不再阻塞合入。
- 正式吸纳时应保留此前冲突决策：
  - `relay/compatible_handler.go` 保留当前主线入口控制
  - 避免 nightly 在文本 relay 入口重新接管计费主路径
