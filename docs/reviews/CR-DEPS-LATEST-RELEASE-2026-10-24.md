# CR: 四端依赖升级到最新 release（2026-10-24）

采样时间：2026-10-24。任务来源：用户要求「所有依赖、框架全部采用最新 release 版本」。

## 范围

四端全量升级并修掉破坏性变更：Go 后端、web/default、web/classic、Tauri 桌面端。

## 提交

| 提交 | 内容 |
| --- | --- |
| 459d5c900 | Go 工具链钉点统一到 1.27.3（Dockerfile、Dockerfile.dev、4 个 workflow、AGENTS.md） |
| a292dc2fc | 后端依赖与 Go 工具链升级（go 1.27.3、gin 1.12.0、gorm 1.31.2、go-webauthn 0.18.2、pgx 5.11.0、aws-sdk-go-v2 1.47.4 等） |
| c68a7e3d3 | Tauri 依赖升级（tauri 2.12.5、tauri-build 2.7.4、@tauri-apps/cli 2.12.4 等） |
| 3bbe95d92 | web/default 依赖升级（react 19.3.1、vite 8.3.5、@tanstack/react-router 1.170.46、i18next 26.5.6、ai 7.0.150 等） |
| 44c8c7ef8 | web/classic 依赖升级（react 19.3.1、vite 8.3.5、tailwindcss 4.3.3、semi-ui 2.104.0、@lobehub/ui 6.7 等） |

## 关键破坏性变更与处理

后端：

- gorm 1.31：`Schema.ParseIndexes()` 返回 `[]*schema.Index`，`model/log_index_creation.go` 三个函数签名与两个测试辅助改指针遍历。
- go-webauthn 0.18：`Credential.Descriptor()` 变指针方法，`controller/passkey.go` 先赋值再调用。

web/default：

- TanStack Table v9 重写：新建 `src/lib/table-compat.ts` 兼容桥（re-export legacy 的 `useLegacyTable as useReactTable`、`get*RowModel`、`LegacyColumnDef` 等），54 个文件改从 `@/lib/table-compat` 导入；`VisibilityState` 在 `@tanstack/table-core` 改名 `ColumnVisibilityState`。
- TypeScript 7 移除 `baseUrl`：两个 tsconfig 删除该字段。typescript-eslint 8.72 不支持 TS 7（issue #10940），TypeScript 钉 `^6.0.3`。
- ai SDK v7：`usage.reasoningTokens` 迁到 `outputTokenDetails.reasoningTokens`，`cachedInputTokens` 迁到 `inputTokenDetails.cacheReadTokens`（`components/ai-elements/context.tsx`）。
- react-day-picker v10：`ClassNames.table` 改名 `month_grid`（`components/ui/calendar.tsx`）。
- react-hooks 7.1.1 把 `set-state-in-effect` 并入 recommended，21 处既有惯用法降级为 warn 并注明理由。

web/classic：

- Tailwind 3 升 4：postcss 插件换 `@tailwindcss/postcss`，`index.css` 改 CSS-first（`@import 'tailwindcss'` + `@source` 替代 content），104 个 color 与 6 个 radius token 迁入 `@theme`。
- Vite 8 `manualChunks` 只接受函数：对象映射改按 `/node_modules/` 前缀归类的函数。
- Semi UI 2.104 exports 不含 `./dist/*`：Vite alias 指向 `dist/css/semi.css` 绝对路径。
- react-icons 5.7 移除 `SiLinkedin`/`SiSlack`：换 `react-icons/fa` 的 `FaLinkedin`/`FaSlack`。
- shiki：`@shikijs/themes` 4.5.0 才有 `horizon-bright`，钉 `^4.5.0`。

安全 overrides：web/default `ip-address ^10.7.4`、web/classic `dompurify ^3.4.17`。

## 验证（本轮实际运行并读取输出）

| 命令 | 结果 |
| --- | --- |
| `go build ./...` | exit 0（需先构建 web/classic 生成 embed dist） |
| `go vet ./...` | exit 0 |
| `go test ./... -count=1` | 66 包 ok |
| `GOTOOLCHAIN=go1.27.3 govulncheck ./...` | 0 个可达漏洞 |
| `cd web/default && bun run typecheck` | 0 error |
| `cd web/default && bun run lint` | exit 0（22 warning，含 set-state-in-effect 降级项） |
| `cd web/default && bun run build` | exit 0 |
| `cd web/default && bun test` | 275 pass / 0 fail |
| `cd web/default && bun audit` | 0 漏洞 |
| `cd web/classic && bun run build` | 通过（3m10s） |
| `cd web/classic && bun run lint` | exit 0 |
| `cd web/classic && bun test` | 218 pass / 0 fail |
| `cd web/classic && bun audit` | 0 漏洞（969 包） |
| `cargo check`（desktop/tauri-app/src-tauri） | exit 0 |
| `cargo test --test runtime_support` | 22 passed |

## 已知遗留与边界

- `bun run knip` 仍 exit 1，但其未用导出清单全部指向升级前就存在的文件，本次未引入新条目；未修，属独立任务。
- 未覆盖：真实浏览器打开两套前端做视觉回归（升级含 React 19、Tailwind 4 主题重排），仅覆盖 build/typecheck/lint/test；生产容器 `new-api` 未重建、未重启。
- 上游 QuantumNous/new-api 的 105 个提交中若干 P0/P1 修复（Redis NOSCRIPT、notify 限流窗口、cgroup 过载哨兵等）在本轮被依赖升级推迟，尚未移植。
- 仓库为 shallow clone，`git merge-base HEAD upstream/main` 失败，无法据提交距离断言哪些上游修复已合入。
