# Dev Service And UI Hardening Review

日期：2026-04-14
分支：`main`

## 范围

本次收口覆盖以下内容：

- 开发镜像基础层与构建链升级
- `prefill_groups` PostgreSQL 迁移兼容
- 使用日志中 `client_gone` 的状态归类
- 头部主题/语言/用户菜单浮层样式统一
- 后台表单 `autocomplete` 与字段可访问性补丁
- 系统设置页顶部功能切换区压缩为轻量单行导航
- 独立开发环境重建与回归验证

## 代码事实

- 运行镜像与构建链：
  - `Dockerfile`
  - `go.mod`
  - `go.sum`
- PostgreSQL 迁移兼容：
  - `model/main.go`
  - `model/prefill_group_migration.go`
  - `model/prefill_group_migration_test.go`
- 使用日志流状态：
  - `service/log_info_generate.go`
  - `service/log_info_generate_test.go`
- WebUI 顶栏与导航：
  - `web/src/components/layout/headerbar/HeaderPopupMenu.jsx`
  - `web/src/components/layout/headerbar/LanguageSelector.jsx`
  - `web/src/components/layout/SiderBar.jsx`
  - `web/src/components/common/ui/SelectableButtonGroup.jsx`
  - `web/src/pages/Setting/index.jsx`
  - `web/src/index.css`
- 表单语义与后台弹窗：
  - `web/src/hooks/common/useFormFieldA11yPatch.js`
  - `web/src/components/settings/personal/modals/EmailBindModal.jsx`
  - `web/src/components/table/models/modals/EditVendorModal.jsx`
  - `web/src/components/table/models/modals/EditPrefillGroupModal.jsx`

## 关键结论

- 开发镜像已切到 `alpine:3.22.2` 运行层，builder 使用 `golang:1.26.2-alpine`。
- `prefill_groups` 迁移不再依赖旧约束名存在，PostgreSQL 分支已改为幂等执行。
- 使用日志中的 `client_gone` 不再误显示为异常，而是区分为已取消。
- 系统设置顶部导航已改为单行横向滚动的轻量切换条，不再占用大块首屏高度。
- 独立开发环境 `http://127.0.0.1:3002` 已使用最新代码重建并验证。

## 自动化验证

### 1. Go 测试

命令：

```bash
go test ./...
```

结果：

- 通过

### 2. 前端生产构建

命令：

```bash
cd web
bun run build
```

结果：

- 构建通过

### 3. 差异格式检查

命令：

```bash
git diff --check
```

结果：

- 通过

## 独立开发环境验证

环境：

- 容器：`new-api-dev`
- 端口：`3002`
- Compose 文件：`/tmp/new-api-dev-compose.yml`

验证：

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:3002/api/status
docker ps --filter name='^/new-api-dev$'
```

结果：

- `api/status` 返回 `200`
- 容器状态为 `healthy`

## 浏览器实测

目标页面：

- `http://127.0.0.1:3002/console/setting?tab=operation`
- `http://127.0.0.1:3002/console/personal`
- `http://127.0.0.1:3002/console/models`

验证结论：

- 系统设置页顶部功能导航已压缩为单行横向滚动条
- 主题菜单、语言菜单、用户菜单层级和背景正常
- 后台关键弹窗无新增控制台 `error / warn / issue`
- 表单字段相关 `autocomplete` 提示已清理

## 风险边界

- `pgx/v5` 上游漏洞仍无官方 fixed version，本次仅能升级到当前最新依赖并继续维持隔离部署策略。
- 开发环境验证基于隔离数据库和隔离运行目录，不涉及正式数据写入。
