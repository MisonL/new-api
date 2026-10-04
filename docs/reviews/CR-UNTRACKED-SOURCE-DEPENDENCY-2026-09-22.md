# CR: 未跟踪源文件被已跟踪文件依赖

- 日期：2026-09-22
- 范围：工作区未跟踪文件与已跟踪文件的依赖关系
- 状态：已核实，未处理（等待决定是否纳入版本控制）

## 问题

工作区有 60 个未跟踪文件（`git status --porcelain` 中 `??`），其中
**31 个是生产源码**，且被已跟踪文件 `import`。当前工作区能构建，只是因为
这些文件恰好存在于磁盘上；一旦执行 `git clean`、切换分支，或从该状态提交
后重新检出，**前后端都会直接构建失败**。

## 实证

两处都做了"移走文件即构建失败"的直接验证，验证后立即还原：

### 后端

```text
$ mv relay/common/responses_tool_names.go /tmp/rtn.go.bak
$ go build ./relay/...
# github.com/QuantumNous/new-api/relay
relay/responses_handler.go:1123:67: undefined: relaycommon.NormalizeResponsesToolNames
```

`relay/common` 包被 **227 个**已跟踪文件 import，因此该文件缺失的影响面是整个
relay 层。这是唯一一个未跟踪的**非测试** Go 源文件。

### 前端

```text
$ mv web/default/src/lib/legacy-route-rewrite.ts /tmp/lrr.bak
$ cd web/default && bun run build
error   Build error:
error   build failed in 1 m 58.8 s
error   Rspack build failed.
error: script "build" exited with code 1
```

`web/default/src/main.tsx:19` 有 `import { rewriteLegacyPath } from '@/lib/legacy-route-rewrite'`，
即应用入口直接依赖该未跟踪文件。

## 影响面清单

被已跟踪文件 import 的未跟踪文件：**30 个前端 + 1 个后端**。

前端引用密度最高的几个：

| 文件 | 被引用处数 |
| --- | --- |
| `web/default/src/hooks/use-persisted-table-visibility.ts` | 8 |
| `web/default/src/features/auth/components/telegram-auth.ts` | 4 |
| `web/default/src/lib/header-nav-access.ts` | 3 |
| `web/default/src/features/models/lib/deployment-channel.ts` | 3 |
| `web/default/src/features/usage-logs/components/filter-auto-complete.tsx` | 3 |
| `web/classic/src/helpers/headerNavModules.js` | 3 |

按目录归类：

- `web/default/src/features/`：usage-logs、channels、models、playground、
  wallet、auth、system-settings 等模块的组件与 lib
- `web/default/src/`：`lib/`、`hooks/`、`components/data-table/`
- `web/classic/src/`：`helpers/`、`hooks/model-pricing/`、`pages/Setting/`
- `relay/common/`：`responses_tool_names.go`（后端唯一一个）

## 未处理原因

用户明确"暂不提交"，因此本轮只做核实与记录，未执行 `git add`。是否纳入
版本控制、以及是否与其他改动分开提交，需要先确认。

## 提交时的顺序要求

若后续提交，必须注意：

1. `relay/common/responses_tool_names.go` 与其测试
   `relay/common/responses_tool_names_test.go` 必须与
   `relay/responses_handler.go` **同一次提交**，否则该提交单独编译失败。
2. 前端 30 个文件同理，须与引用它们的已跟踪文件同批提交。
3. 60 个未跟踪文件中还有约 29 个不是被引用的生产源码，需要分类判断
   哪些是源码、哪些是测试、哪些是构建产物或缓存（不应提交）。

## 未覆盖与边界

- 本次只验证了"移走即失败"这一条因果链，未逐个验证全部 31 个文件。
- 未检查这些文件是否已被 `.gitignore` 规则覆盖（部分可能是刻意不跟踪的
  本地配置）；分类时需一并核对。
- 未评估这些文件的内容质量或是否包含密钥，纳入版本控制前应做一次扫描。
