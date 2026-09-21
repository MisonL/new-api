# CR: 渠道批量操作开关默认值与 classic 相反

- 日期：2026-09-20
- 范围：`web/default/src/features/channels/components/channels-provider.tsx`、`web/default/src/i18n/locales/*.json`
- 状态：已修复并验证

## 问题

`web/default` 渠道页新增的批量操作开关，与 `web/classic` 渠道页共用同一个
localStorage 键 `enable-batch-delete`，但对"未设置"状态给出了相反的默认值：

| 位置 | 读取方式 | 未设置时 |
| --- | --- | --- |
| `web/classic/src/hooks/channels/useChannelsData.jsx:259` | `localStorage.getItem('enable-batch-delete') === 'true'` | `false`（不启用） |
| `web/default/src/features/channels/components/channels-provider.tsx:80`（修复前） | `stored === null ? true : stored === 'true'` | `true`（启用） |

两侧写回也用同一个键：

- `web/classic/src/components/table/channels/ChannelsActions.jsx:278`
- `web/default/src/features/channels/components/channels-primary-buttons.tsx:64`

因此同一个键会被两个主题互相覆盖，而"未设置"在两侧含义不同：

1. 用户从未动过该开关时，classic 渠道页不显示行选择列，default 渠道页显示。
2. 用户在 classic 打开该开关（写入 `"true"`），切到 default 仍是启用，看似一致；
   但在 default 关闭（写入 `"false"`）后回到 classic，classic 读到 `"false"` 为不启用，
   一致。真正的分歧只在**键不存在**时。

该开关控制的是行选择与批量操作入口：

- `channels-table.tsx:365` `enableRowSelection: enableBatchOperations && ...`
- `channels-table.tsx:383` 关闭时清空 `rowSelection`
- `channels-table.tsx:570` 开启时才渲染 `DataTableBulkActions`

即它决定批量入口是否出现，而批量入口包含删除类操作。

## 修复

1. 默认值与 classic 对齐：未设置即不启用。

```ts
const [enableBatchOperations, setEnableBatchOperations] = useState(() => {
  // Same key as the Classic channel page, which reads it as a plain
  // 'true' comparison, so an unset value means disabled there. Keep the
  // default identical: one key shared by both themes cannot carry two
  // different meanings for the unset state.
  return localStorage.getItem('enable-batch-delete') === 'true'
})
```

2. 开关文案对齐 classic 同键措辞。该开关实际控制"行选择/批量选择"而非批量操作本身，
   classic 的对应文案是 `开启批量操作`（`en`: `Enable batch selection`）。default 原为
   `Batch operations`，按 classic 的英文措辞改为 `Enable batch selection`，其余语言同步：

| 语言 | 修复前 | 修复后 |
| --- | --- | --- |
| en | `Batch operations` | `Enable batch selection` |
| zh | `批量操作` | `开启批量操作` |
| fr | `Opérations par lots` | `Activer la sélection par lots` |
| ja | `一括操作` | `一括選択を有効化` |
| ru | `Пакетные операции` | `Включить пакетный выбор` |
| vi | `Thao tác hàng loạt` | `Bật chọn hàng loạt` |

`Batch operations` 是本次 dev 分支新增键（HEAD 中 6 个语言文件均无此键），改动只替换值，
不增删键。

## 验证

命令均在本轮运行并读取输出。

| 命令 | 结果 |
|---|---|
| `cd web/default && bun run typecheck` | exit 0 |
| `cd web/default && bun run lint` | exit 0 |
| `cd web/default && bun test` | 220 pass, 0 fail |
| `cd web/default && bun run i18n:sync` | exit 0，`Source i18n scan: 925 files, 4658 keys, 0 missing.` |
| `cd web/default && bun run build` | exit 0，`Safari compatibility check passed for dist` |

locale 改动核查：6 个语言文件键数均为 5173 不变，除 `Batch operations` 外无其它值变化，
文件缩进与行数（5178 行）与修改前一致。

`bun run i18n:sync` 会按设计重写 `web/default/src/i18n/locales/_reports/`（刷新
`missingCount` / `untranslatedCount` 并删除已清空的 `*.untranslated.json`）。该目录
相对 HEAD 的计数差异是既有 dev 分支工作造成的，不属于本次任务范围，已用
`git checkout -- web/default/src/i18n/locales/_reports/` 恢复，未纳入本次改动。

## 边界

- 本次只统一"未设置"语义，不改变已显式写入 `"true"` / `"false"` 时的行为。
- 未做浏览器交互验证：未实际在 classic 与 default 之间切换主题确认开关状态与
  行选择列一致。
- 同一 localStorage 键被两个主题共用的结构未变，仍是历史约定；若后续要拆键需单独评估
  （会丢失用户已有偏好）。
- `web/classic` 侧未做任何改动。
