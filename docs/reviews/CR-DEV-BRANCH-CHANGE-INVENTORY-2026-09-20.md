# dev 分支改动清单与候选制品梳理

采样时间：2026-09-20（Asia/Shanghai）
仓库：`/Volumes/Work/code/new-api`
分支：`dev`，`HEAD=5cda68077`，与 `origin/dev` 同步，比 `main` 多 7 个提交。

## 1. 已提交部分（dev 相对 main，7 个提交）

| 提交 | 主题 | 规模 |
| --- | --- | --- |
| `6f064edb0` | fix: align model pricing with official list prices | 11 文件，+3718/-243 |
| `f825f23d8` | fix Claude reasoning history conversion | 5 文件，+386/-13 |
| `21bd76afd` | fix(relay): enforce serial tool calls for Codex Responses Lite | 4 文件，+103 |
| `a2724e3ff` | feat: harden routing protocols billing and backend tests | 417 文件，+73985/-9552 |
| `cac5eea9e` | feat: improve dashboard logs and routing observability | 153 文件，+31298/-7941 |
| `904a5a976` | docs: harden deployment guidance and verification records | 64 文件，+4701/-1720 |
| `5cda68077` | test: expand operational script contracts and fixtures | 43 文件，+7808/-1100 |

## 2. 未提交部分（候选制品本体）

工作区：191 个已修改、4 个已删除、62 个未跟踪（含本文档）。

### 2.1 规模

| 范围 | 已跟踪改动 |
| --- | --- |
| 后端（relay/service/controller/setting/dto/constant） | +1469 / -318（46 文件） |
| 前端（web/default + web/classic） | +14192 / -3637（142 文件） |
| 脚本与文档 | +149 / -92（7 文件） |
| 未跟踪新增源码 | 前端 7142 行 + Go 260 行 |

### 2.2 后端分组

| 分组 | 文件数 | 规模 | 主要内容 |
| --- | --- | --- | --- |
| Responses/Codex 协议 | 16 | +706/-143 | 流终态校验、观测日志上限、工具名归一化、折叠 SSE、request id 捕获 |
| 计费与价格 | 4 | +149/-97 | cache ratio 对齐、model ratio |
| service 层 | 4 | +156/-3 | 上游 request id 捕获、Claude effort 透传、任务轮询 |
| controller 层 | 10 | +201/-29 | 重试判定、流错误、日志自动补全开关、视频代理 |
| 通用 handler | 12 | +241/-46 | 流扫描器、helper、各协议 handler 小改 |

关键新增文件（未跟踪，必须 `git add`）：

- `relay/common/responses_tool_names.go`（260 行）—— 导出 `NormalizeResponsesToolNames`，被已跟踪的 `relay/responses_handler.go:1123` 调用。漏 add 会导致编译失败。

### 2.3 前端分组

default（107 文件，+13302/-3405）：features 下 playground(23)、channels(19)、usage-logs(18)、system-settings(16)、models(9)、auth(7)、wallet(6)、profile(6)。

classic（35 文件，+890/-232）：components/table(18)、hooks、pages/Setting、helpers。

未跟踪前端源码 36 个（7142 行），全部被已跟踪代码引用，同属构建必需项。

### 2.4 已删除

`web/default/src/i18n/locales/_reports/{fr,ja,ru,vi}.untranslated.json` —— 由 `sync-i18n.mjs:548` 在 `untranslatedCount === 0` 时主动删除，与先例提交 `c72c357c5 chore(i18n): clean stale translation reports` 行为一致。

## 3. 提交拆分建议

按原子任务分组，每组单独复核与提交：

1. Responses/Codex 协议（16 文件 + `relay/common/responses_tool_names.go`）
2. 计费与价格（4 文件）
3. service 层（4 文件 + `service/convert_effort_test.go`）
4. controller 层（10 文件）
5. 通用 handler（12 文件）
6. 前端 default（107 文件 + 36 未跟踪）
7. 前端 classic（35 文件 + 6 未跟踪）
8. 运维脚本与测试（7 文件）
9. 审计文档（11 文件）

每组提交前必须确认该组自洽可编译。**禁止只 `git commit -a`**：第 1、3、6、7 组都含未跟踪的新源码文件，漏 add 会产生编译或构建失败的提交。

## 4. 本轮已确认的验证结果

| 检查 | 结果 |
| --- | --- |
| `go build ./...` | exit 0 |
| `go vet ./...` | exit 0 |
| `go test ./... -count=1` | exit 0，60 包，0 FAIL，0 cached |
| `gofmt -l <源码目录>` | 输出为空 |
| `cd web/default && bun run typecheck` | exit 0 |
| `cd web/default && bun run lint` | exit 0 |
| `cd web/default && bun test` | 220 pass / 0 fail / 866 expect |
| `git diff --check` | exit 0 |

## 5. 边界说明

- 上述 Go 与前端结果在**当前 dirty 工作区**取得，不代表任何拆分后的单个提交自洽。
- 隔离 `3001` 容器构建于 2026-09-20 01:24（CST），早于本轮 `controller/misc.go`（10:12）与 `setting/ratio_setting/cache_ratio.go`（10:30）的格式化修改，因此当前容器不再反映工作区最新状态，需要重建后才可作为验证依据。
- 未做真实上游推理请求；真实渠道行为仍需在发布窗口单独采集。
