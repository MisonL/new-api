# 当前工作区整理与验证收尾 - 2026-09-14

## 范围

本记录收口当前 `dev` 工作区中已经存在的后端、协议适配、路由健康治理、计费统计、双前端、部署脚本和测试改动。整理遵循保留 dirty worktree、只移出有证据的临时产物、不改变正式服务生命周期的边界。

## 清理结果

- `.firecrawl/` 是未被源码、构建或测试引用的研究抓取产物，已移至仓库外的可恢复临时目录，不纳入 Git。
- 未发现可证明孤立的 Go、TypeScript、JavaScript、Shell 或 Python 功能文件；未跟踪源码和测试继续按功能提交。
- `.bun-version`、`deploy/env/dev-legacy.env.example` 及新增测试/部署文件均存在代码、CI 或文档引用，予以保留。
- 历史审计记录只在仓库内保留有独立验收价值的版本；被后续记录替代的过程稿移出工作区但不删除历史 Git 内容。

## 当前改动分组

1. 后端协议、路由、健康、并发、计费和安全边界，以及对应 Go 回归测试。
2. Default/Classic 前端、国际化、日志缓存统计和路由可观测性，以及对应前端测试。
3. Docker、macOS TUN、CI、脚本、桌面端和运维文档。
4. 当前功能审计记录和本收尾索引。

## 验证门禁

本记录只登记本轮实际读取到退出码的命令。隔离容器健康、真实渠道、正式服务和生产数据库仍单独记录，不因本地测试通过而视为已验收。

本轮实际验证结果：

- `go test -p=1 -count=1 -timeout=900s ./...`：退出码 0，所有有测试包通过。
- `go vet ./...`：退出码 0。
- `cd web/default && bun run lint && bun run typecheck && bun run build`：退出码 0；类型检查、生产构建和 Safari 兼容检查通过。
- `cd web/classic && bun run lint && bun run test:node && bun run build`：退出码 0；Prettier、137/137 Node 测试、生产构建和 Safari 兼容检查通过。
- `git diff --check` 和涉及 Go 文件的 `gofmt -l`：无输出、退出码 0。

## 边界

- 本轮不重启、重建或写入正式服务、正式 PostgreSQL、Redis、Caddy 或渠道配置。
- 真实上游渠道、跨进程 Redis 故障、生产发布和回滚验收不由本地门禁替代。
