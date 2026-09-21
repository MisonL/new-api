# CR: Responses 工具名别名在引用位置被误判为冲突

- 日期：2026-09-20
- 范围：`relay/common/responses_tool_names.go`
- 状态：已修复并验证

## 问题

`relay/common` 的工具名别名注册表只有一个 `normalize`，被三个位置共用：

- `tools[]` 工具声明
- `input[]` 历史调用（`function_call`、`tool_call`、`custom_tool_call`、`mcp_*` 等）
- `tool_choice`

该函数没有"别名方向"判断。当引用位置携带的是同请求内某次声明已经生成的别名时，
它把别名当作新的原名再走一遍归一化，于是 `originalByAlias[alias]` 里已属他人，
报出伪冲突。

复现：

```
tools: [{"type":"function","name":"my tool!"}]        # 声明，生成别名
input: [{"type":"function_call","name":"newapi_tool_my_tool_96caa15f0b", ...}]   # 引用该别名
=> responses tool name "newapi_tool_my_tool_96caa15f0b" normalizes to
   "newapi_tool_my_tool_96caa15f0b", conflicting with "my tool!"
```

该形态在多轮对话中可达：响应侧不把别名还原为原名，客户端拿到别名后再放回
`input[]` 即为引用位置。

## 修复

对照既有兄弟实现 `service/openaicompat/responses_tool_state.go`，它本就按角色分开：

| 角色 | 函数 | 别名方向检查 |
|---|---|---|
| 声明位置 | `normalizeDeclaration` | 无，声明别名即冲突（fail-closed） |
| 引用位置 | `normalizeReference` | 有，别名解析为自身 |

`relay/common` 缺失这一拆分。修复内容：

1. 拆出 `normalizeDeclaration`（原逻辑）与 `normalizeReference`（新增别名方向判断）。
2. `normalizeResponsesToolNameField` 增加 `position` 参数，按位置分派：
   - `tools[]` 及其嵌套对象 -> `toolNameDeclaration`
   - `input[]` 历史调用、`tool_choice` -> `toolNameReference`

## 一次被测试拦下的错误修复

最初的修复方案是直接给唯一的 `normalize` 加别名方向检查。该方案被既有测试
`TestNormalizeResponsesToolNamesRejectsAliasCollision` 拦下并失败：

```
An error is expected but got nil.
```

该测试位于 `relay/common/responses_tool_names_test.go`，是 2026-09-17 的工作区改动
（未跟踪，早于本轮会话），它刻意固定了"两个声明归一化到同一别名必须冲突"这一契约。
直接加方向检查会弱化声明位置的冲突检测，属于错误方向。正确做法是按角色拆分，
使声明位置保持 fail-closed、仅引用位置接受别名。

## 验证

| 命令 | 结果 |
|---|---|
| `go test ./relay/common/ -count=1` | ok |
| `go test ./relay/... -count=1` | 37 包 ok |
| `go test ./controller ./model ./relay/common ./relay/helper ./service -count=1` | 全部 ok |
| `go build ./...` | exit 0 |
| `gofmt -l`（改动文件） | 无输出 |

新增回归测试（追加至 `responses_tool_names_test.go`）：

- `TestNormalizeResponsesToolNamesResolvesReferenceToOwnAlias`
- `TestNormalizeResponsesToolNamesResolvesToolChoiceAlias`
- `TestNormalizeResponsesToolNamesStillRejectsDeclarationCollision`

该文件既有 5 个用例保持通过，合计 8 个用例全通过。

变异验证（逐个撤掉修复，测试必须失败）：

| 撤掉 | 结果 |
|---|---|
| 声明位置的冲突检测（弱化为接受别名） | FAIL（既有冲突测试） |
| 引用位置的别名方向判断 | FAIL（新增引用测试） |

## 边界

- 本次修复只调整声明与引用两种角色的分派，不改变归一化算法本身
  （`dto.NormalizeToolName` 未改动）。
- 未连接真实上游；覆盖的是请求改写语义，未覆盖上游对别名的实际接受度。
