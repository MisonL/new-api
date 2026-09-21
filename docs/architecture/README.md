# 架构与设计方案索引

本目录存放跨版本的架构设计、迁移规划和协议兼容方案。与 `docs/reviews/` 的区别：
这里记录**设计意图与约束**，`docs/reviews/` 记录**某次验证的事实与证据**。

## 方案

| 文档 | 内容 |
| --- | --- |
| [enterprise-auth-three-phase-plan.md](enterprise-auth-three-phase-plan.md) | 企业统一认证三阶段开发规划 |
| [channel-routing-priority-weight-concurrency-plan.md](channel-routing-priority-weight-concurrency-plan.md) | 渠道路由优先级、百分比权重与并发容量设计 |
| [responses-compat-plan.md](responses-compat-plan.md) | Responses 兼容折中方案：reasoning 文本保留与 opaque 状态拒绝边界 |
| [codex-bridge-interactive-cli-plan.md](codex-bridge-interactive-cli-plan.md) | Codex Bridge 交互式 CLI：模型目录注入与提供方配置 |
| [protocol-conversion-webui-design.md](protocol-conversion-webui-design.md) | 协议转换 WebUI 完整化设计方案 |
| [tauri2-desktop-migration-plan.md](tauri2-desktop-migration-plan.md) | Tauri 2 桌面端替换 Electron 迁移规划 |
| [tauri2-desktop-bridge-validation.md](tauri2-desktop-bridge-validation.md) | Tauri 2 桌面桥接期验证矩阵 |

## 图示

- `channel-routing-scope-webui-v2.html`：渠道作用域 WebUI 设计稿。
- `responses-compact-routing-mechanism.html`：Responses 压缩路由机制图。

## 维护约定

- 方案变更时同步更新本文件，避免索引与实际文件脱节。
- 文档内引用其他目录的文件使用相对路径；跨一层目录用 `../`。
- 方案落地后的验证证据写入 `docs/reviews/`，不要在本目录追加运行结果。
