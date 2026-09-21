# CR: i18n 源码扫描漏掉模板字面量内的 t() 调用

- 日期：2026-09-20
- 范围：`web/default/scripts/sync-i18n.mjs`、`web/default/src/i18n/locales/*.json`
- 状态：已修复并验证

## 问题

`sync-i18n.mjs` 的 `collectStaticTranslationKeys` 在遇到字符串字面量时整体跳过
（`index = literal.end`），因此反引号模板字面量内部的 `t()` 调用完全不被扫描。

实测：

```
模板字符串内  => 提取到: []        # `${t('Cache creation ratio')} ...`
直接调用      => 提取到: ["Cache ratio"]
```

后果：三个只在模板字面量中出现的 key 未被收录，而新增的源码扫描门禁对真实缺口
报告 "0 missing"，即门禁本身失效。

受影响 key（源码 9 处引用，集中在 usage-logs）：

- `Cache creation ratio`（`common-logs-columns.tsx:564`、`details-dialog.tsx:315,518`）
- `Cache creation ratio (5m)`（`common-logs-columns.tsx:568`、`details-dialog.tsx:319,522`）
- `Cache creation ratio (1h)`（`common-logs-columns.tsx:572`、`details-dialog.tsx:323,526`）

`Cache ratio` 之所以未受影响，仅因它另有一处非模板位置的使用
（`<FormLabel>{t('Cache ratio')}</FormLabel>`）。

## 修复

在 `sync-i18n.mjs` 中新增 `findSubstitutionEnd`、`skipTemplateLiteral`、
`scanTemplateLiteral` 三个函数，使扫描器进入 `${...}` 占位内部继续扫描，而不是
跳过整个模板字面量。`collectStaticTranslationKeys` 改为可接收外部 `keys` 集合以便递归。

同时为 6 个语言包补齐三个 key。key 一律使用英文原文，value 使用各语言既有术语：

| locale | value |
|---|---|
| en | Cache creation ratio / (5m) / (1h) |
| zh | 创建缓存倍率 / (5m) / (1h) |
| fr | Ratio de création de cache / (5m) / (1h) |
| ja | キャッシュ作成倍率 / (5m) / (1h) |
| ru | Коэффициент создания кэша / (5m) / (1h) |
| vi | Tỷ lệ tạo bộ nhớ đệm / (5m) / (1h) |

## 验证

| 命令 | 结果 |
|---|---|
| `node scripts/sync-i18n.mjs` | 修复前 `4658 keys, 3 missing`；补齐后 `4658 keys, 0 missing`，exit 0 |
| `bun run typecheck` | exit 0 |
| `bun run lint` | exit 0 |
| `bun test` | 220 pass / 0 fail / 866 expect |
| `bun run build` | exit 0，Safari 兼容检查通过 |

扫描器改动前后对比：修复前源码引用 4632 key，修复后 4658 key（多发现 26 个模板内 key）。

## 附：本轮操作事故与恢复记录

在处理本问题时，为撤销一次错误的 key 插入，执行了 `git checkout -- src/i18n/locales/`。
该命令作用范围是整个目录，把 6 个语言文件全部回滚到 HEAD，而当时只备份了 `en.json`。
被回滚的工作区版本每个语言有 5170 个 key，HEAD 只有 4633（fr/ja/ru/vi 为 4623）。

恢复依据：

1. 决定性判据：`reorderLikeBase(base, target, ...)` 只遍历 `Object.keys(base)` 输出，
   target 多出的 key 写入 `_extras/`，不会回写 base。因此 sync **不可能**给 `en.json`
   增加 key，5170 必是 sync 之前就存在的工作区状态。
2. 备份一致性：`/tmp/en.json.bak`（checkout 前）与从 `dist` 提取的 en 对象 key 数、
   key 集合、逐 key 取值全部一致（0 差异）。
3. 恢复来源：`web/default/dist`（构建时间 01:41，早于 checkout）的
   `JSON.parse('{"translation":{...}}')` 内嵌了全部 6 个语言的完整副本，各 5170 key。

恢复结果：6 个语言全部回到 5170 key，相对备份/提取物 0 丢失，随后补齐 3 个新 key
达到 5173。en.json 与 checkout 前备份逐字节一致。

保留的恢复物：`/tmp/i18n-recovery/bundle-{en,zh,fr,ja,ru,vi}.json`（从 dist 提取的
checkout 前各语言快照，用于后续核对）。

教训：`git checkout -- <目录>` 在未对整目录备份前不得使用；应改为只对单个文件操作，
或先用 `git stash`/整目录 `cp -R` 留回滚点。
