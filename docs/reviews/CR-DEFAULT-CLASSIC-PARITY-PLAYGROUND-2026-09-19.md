# Classic 与 Default 前端功能 parity：Playground

采样时间：2026-09-19（Asia/Shanghai）

## 结论

本轮复核了 Classic 与 Default 的 Playground、调试面板、消息操作、图片输入和配置同步路径。发现并补齐 4 项真实行为差异；未发现新的控制台业务入口缺失。Default 与 Classic 的业务范围保持一致，视觉实现继续使用各自前端的组件体系。

## 已补齐

- Default 调试请求/响应查看器补齐 JSON 高亮、复制、URL 链接识别、超大内容截断与展开，以及请求/响应空态。
- Default 调试面板同步显示预览更新时间和最后请求时间；SSE 事件查看器保留逐事件展开、复制和全部展开/折叠能力。
- Default 输入框支持图片剪贴板粘贴；未启用图片输入时阻止附件进入请求并提示到设置中启用，行为与 Classic 对齐。
- Default 消息操作允许对用户、助手和 System 消息重试，并从最近的用户消息重建请求上下文；增加用户消息和助手/System 消息回归测试。
- 新增 3 个调试文案并同步到 6 个 Default locale，源码扫描无缺失键。

## 验证

| 命令 | 结果 |
| --- | --- |
| `cd web/default && bun test` | 通过，220 pass，0 fail，866 expect() calls |
| `cd web/default && bun run typecheck` | 通过，退出码 0 |
| `cd web/default && bun run lint` | 通过，退出码 0 |
| `cd web/default && ./node_modules/.bin/eslint <本轮目标文件>` | 通过，退出码 0 |
| `cd web/default && ./node_modules/.bin/prettier --check <本轮目标文件>` | 通过 |
| `cd web/default && bun run i18n:sync` | 通过，源码扫描 0 missing |
| `cd web/default && bun run build` | 通过，Rsbuild、Safari compatibility rewrite/check 和浏览器兼容检查均通过 |
| `cd web/classic && bun run test:node` | 通过，137 pass，0 fail |
| `git diff --check` | 通过 |

## 边界

- 工作区包含大量前序 dirty 修改，本轮未回退、覆盖或清理无关文件。
- 正式服务 `127.0.0.1:13000` 未重启、未重建、未写入数据库或 Redis；本报告不等同于正式发布验收。
- 本轮构建验证了本地产物和兼容性，未重建隔离容器，也未让正式服务加载本轮前端产物。
