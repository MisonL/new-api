# 文档索引

本目录存放 README 之外的说明文档。README 面向首次访问者，这里放长部署说明、专项能力、设计方案和审计证据。

## 从哪里开始

| 你的目的 | 入口 |
| --- | --- |
| 接手这个仓库 | [HANDOFF-NEW-API-2026-09-20.md](HANDOFF-NEW-API-2026-09-20.md) |
| 查某次验证的事实与命令 | [reviews/README.md](reviews/README.md) |
| 查设计意图与约束 | [architecture/README.md](architecture/README.md) |
| 查运维步骤与门禁 | [operations/](operations/) |
| 查渠道与模型配置 | [channel/](channel/) |

## 目录说明

| 目录 | 内容 |
| --- | --- |
| `architecture/` | 架构设计、迁移规划、协议兼容方案。记录设计意图，不记录运行结果。 |
| `operations/` | 运维手册：定价维护、macOS Docker TUN、存储、发布门禁。 |
| `reviews/` | 阶段性审计与验证记录。`archive/` 存放 2026-07 及更早的历史记录。 |
| `channel/` | 渠道高级配置与模型测试运行配置说明。 |
| `installation/` | 面板类安装说明。 |
| `images/` | README 与文档引用的图片。 |
| `openapi/` | OpenAPI 描述文件。 |
| `superpowers/` | 早期开发计划与设计稿，保留供追溯。 |

## 约定

- 新验证证据写入 `docs/reviews/CR-<主题>-<YYYY-MM-DD>.md`，并同步 `reviews/README.md` 索引。
- 新设计方案写入 `docs/architecture/`，并同步该目录索引。
- 运行状态类内容必须写清采样时间、命令、结果和边界；服务 `healthy` 与"运行最新代码"分开表述。
- 文档之间用相对路径互链；跨一层目录加 `../`。
- 不引用不存在的截图、GIF 或阶段报告。
