# CR-HEADER-PROFILE-STRATEGY-2026-04-21

## 范围

- 分支：`feature/header-profile-strategy`
- 目标：将渠道编辑页中的旧 `header_override` 零散入口替换为统一的 `Header Profile` 策略与资源库交互

## 已执行验证

### 1. 后端定向测试

命令：

```bash
go test ./controller -run 'HeaderProfile'
```

结果：

- 退出码：`0`
- 结论：`controller` 中与 `Header Profile` 相关的 CRUD、渠道策略校验、只读保护、序列化回归均通过

### 2. 前端 helper 行为测试

命令：

```bash
cd web
node --test src/components/table/channels/modals/headerProfile.helpers.test.js
```

结果：

- 退出码：`0`
- 结论：以下行为通过
  - builtin 与 user profile 归一化
  - fixed / round_robin / random 选择逻辑
  - `settings.header_profile_strategy` 读写
  - 轮询排序
  - 旧 `header_override` 导入包装
  - 名称重复校验
  - Header JSON 非法 / 空对象 / 非字符串值校验

### 3. 前端生产构建

命令：

```bash
cd web
bun run build
```

结果：

- 退出码：`0`
- 结论：Vite 生产构建通过

### 4. 隔离环境真实 Web UI 验证

环境：

- 后端：`go run . --port 3301`
- 前端：`bun run dev --host 127.0.0.1 --port 4173`
- 页面：`http://127.0.0.1:4173/console/channel`

结果：

- 已复现并修复真实前端崩溃
  - 现象：点击“新建 Profile”会落入 ErrorBoundary
  - 根因：`HeaderProfileEditorModal.jsx` 使用了不存在的 `Input.TextArea`
  - 修复：改为显式引入并使用 `TextArea`
- 已实际验证“新建 Header Profile”弹窗可正常打开，不再崩溃
- 已实际验证非法 JSON 会阻止保存
  - 示例：`{"User-Agent":123}`
  - 提示：`Headers JSON 的值必须全部是字符串`
- 已实际验证合法 JSON 可创建 Profile，并立即出现在“我的 Profile”区域
- 已实际验证固定模式下选择单个 Profile 后，已选区会同步显示选中状态
- 已实际验证自定义 Profile 可编辑并成功保存，名称变更会同步更新到已选区与资源库
- 已实际验证删除自定义 Profile 后，已选区与资源库同步清空，并出现“已启用 Header Profile，但还没有选择任何 Profile”提示
- 已实际验证轮询模式支持多选
  - 选择了 `Chrome macOS` 与 `Codex CLI`
  - 已选区显示 `已选择 2 个`
  - 文案正确显示为：`轮询模式支持多选并按顺序依次使用`
  - 已选区按顺序显示：`顺序 1`、`顺序 2`
- 已实际验证随机模式会保留已选 Profile，不会切换策略时清空选择
  - 同一组已选项仍保留为 2 个
  - 文案正确显示为：`随机模式支持多选并随机挑选使用`
  - 已选区展示从顺序语义切换为：`随机候选`

## 代码级复核结论

### 交互与行为

- 固定模式替换逻辑已由 `toggleSelectedProfile()` 保证，选择新 Profile 会直接替换旧值
- 轮询模式拖拽排序已由 `reorderSelectedProfileIds()` 与 `HeaderProfileStrategySection.jsx` 的拖拽处理接通
- 随机模式与轮询模式都要求至少 1 个已选 Profile，渠道保存前会做前端校验，后端也会再次校验
- builtin Profile 已标记为 `scope=builtin` 且 `readonly=true`，前端不提供编辑/删除入口，后端也拒绝更新和删除
- 旧 `header_override` 不做静默迁移，仅提供“导入为 Profile”入口
- 对旧 `header_override` 执行导入后，会自动创建 Profile 并应用到当前渠道的 `fixed` 策略，避免“导入后仍未生效”的半闭环状态

### UI 呈现

- 已选区仅展示名称、分类、顺序/模式信息，不直接铺开完整请求头值
- 完整 Header 内容仅在 hover 预览中显示
- 资源库按 builtin / user 分区
- 自定义 Profile 的非法 JSON、空名称、重复名称都会在弹窗内阻止保存

## 未执行项

- 未在浏览器会话中完成亮色 / 暗色 / 窄宽度的人工视觉验收

以上未执行项需要在后续视觉联调时继续补证，当前文档不将其记为“已验证通过”。
