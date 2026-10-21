# Code crash resilience review (2026-10-17)

## Scope

本轮只改项目代码和 Compose 配置，不重建容器、不修改生产数据库、不执行 `pg_resetwal`。
目标是降低 Docker 重启、PostgreSQL 短暂恢复和应用优雅退出期间的异常影响。

## Changes

- `model/utils.go`
  - 批量更新器改为可停止的 ticker，并提供 `StopBatchUpdater`。
  - 周期更新与最终 flush 通过互斥锁串行化，避免关闭连接池时仍有批量写库。
  - 数据库写失败或数据库尚未初始化时，增量重新放回内存队列，不再一次失败就静默丢失。
- `main.go`
  - HTTP server 停止后，先停止并等待批量更新器，再执行最终 flush，最后由 defer 关闭数据库。
- `model/main.go`
  - 数据库连接遇到 PostgreSQL recovery、连接拒绝、连接重置、连接数耗尽等瞬时错误时有限重试。
  - PostgreSQL SQLSTATE 与通用错误文本均受控匹配；认证失败、迁移错误和 checkpoint 损坏不会重试。
- `docker-compose.yml`、`.env.example`
  - 暴露启动重试参数，并说明 `BATCH_UPDATE_ENABLED=false` 可用于严格优先额度持久化的部署。

## Verification

以下命令在本轮执行并读取退出码：

- `go test ./model -run 'Test(BatchUpdate|FlushBatchUpdates|IsTransientDatabaseStartupError)' -count=1`：通过。
- `go test -race ./model -run 'Test(BatchUpdate|FlushBatchUpdates|IsTransientDatabaseStartupError)' -count=1`：通过。
- `go test ./model -count=1`：通过。
- `go vet ./model ./controller ./service ./middleware ./relay/common ./relay/helper ./common ./types`：通过。
- `go build ./...`：通过。
- `docker compose config --quiet`：通过。

## Boundaries

- 早先的组合包测试曾通过，但当前工作区的 `go test ./... -count=1` 触发 `controller/TestManageUserOverrideInvalidatesQuotaCache` 失败；该用例单独 `-count=5` 也稳定失败，失败路径与本轮文件无关。
- `go test ./model -shuffle=on -count=3` 和 `go test ./model -count=3` 暴露仓库既有的共享 SQLite 测试夹具泄漏（`no such table`、唯一键冲突）；本轮没有扩大范围修复测试基础设施。
- SIGKILL、宿主机断电和 Docker VM 存储损坏不会触发 Go shutdown hook；应用代码不能替代 PostgreSQL 备份、独立磁盘副本或恢复演练。
- 本轮没有重建或重启正式 `new-api`，因此只证明源码、测试和 Compose 配置层行为，未宣称生产镜像已更新。
