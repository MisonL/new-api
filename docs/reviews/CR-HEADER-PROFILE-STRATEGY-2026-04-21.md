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
- 未在隔离开发环境中执行真实 Web UI 点击流验证

以上两项需要在后续隔离环境联调时继续补证，当前文档不将其记为“已验证通过”。
