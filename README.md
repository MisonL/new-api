# new-api

基于 `QuantumNous/new-api` 的独立演进版本。

本仓库继续保留 `new-api` 的基础定位，但未来路线、功能取舍、发布节奏和上游吸纳策略由本仓库单独决定，不再以“百分百跟随上游”为目标。

## 相对原版 new-api 的新增/改动

- 企业 SSO 三条链路：`JWT Direct`、`Trusted Header`、`CAS`
- `OpenAI Chat` 与 `OpenAI Responses` 协议转换策略可视化配置
- 阶梯计费表达式与工具定价能力
- 请求/响应内容日志：用户授权开启、弹窗查看、JSON 导出、单条删除、批量删除
- `Responses` 流式首包前恢复等待与相关稳定性增强
- 渠道上游模型更新检测、自动同步与忽略列表
- 用户绑定信息管理面板与用户属性入口增强
- Dashboard 增强：时间范围切换、通道趋势排行
- 使用日志与后台表格增强：筛选联想、横向滚动、详情显示稳定性改进
- Web UI 体验增强：可拖拽侧边栏、表单可访问性修补、亮暗主题细节修正
- Codex / 长连接异常判定与通道恢复策略优化
- 若干稳定性修复：分发缓存反同步、负延迟保护等

以上清单以当前 `main` 已合入能力为准，后续会继续按本仓库路线演进。

## 项目治理

- 本仓库独立开发，方向由维护者决定。
- 上游 `QuantumNous/new-api` 是可选输入，不是唯一产品路线来源。
- 上游新变更按需选择性吸纳，不承诺全量同步。
- 吸纳上游前，先评估对本仓库现有增强功能、配置兼容性、Web UI 和后端行为的影响。
- 吸纳上游后，默认需要在独立环境完成构建、回归和关键链路验证。

## 快速开始

```bash
git clone https://github.com/MisonL/new-api.git
cd new-api
docker compose up -d
```

默认访问地址：

```text
http://localhost:3000
```

如果你需要持久化数据或保留现有配置，请在启动前先检查并挂载数据目录、数据库连接和环境变量，不要直接覆盖生产实例。

## 开发命令

```bash
go test ./controller ./model ./relay/common ./relay/helper ./service
cd web && bun install
cd web && bun run lint
cd web && bun run build
```

## 与上游的关系

- 上游项目：`QuantumNous/new-api`
- 本仓库会继续吸纳对基础能力有价值、且不会破坏现有增强功能的上游变更
- 不符合本仓库路线或会明显影响既有能力可用性的上游改动，可以跳过

## 说明

- 项目名称、模块路径和上游归属信息继续保留
- 生产验证前，请先在独立环境完成配置迁移和联调
