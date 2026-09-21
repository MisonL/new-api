# Codex 本地 Responses 压缩入口审查

日期：2026-07-27

## 范围

本次改动只覆盖 Codex 本地压缩请求的入口处理。精确匹配条件为：

- `POST /v1/responses`
- `X-Codex-Turn-Metadata.request_kind=compaction`
- `X-Codex-Turn-Metadata.compaction.implementation=responses`

不匹配的普通 Responses、`responses_compaction_v2` 远程压缩和
`/v1/responses/compact` 不进入该入口。

## 行为与不变量

- 全局解压中间件对精确匹配请求不读取正文，因此无效令牌在认证前不会解压或占用请求体内存。
- `TokenAuth` 成功后，入口支持 identity、gzip、br、zstd，并将解压后的正文直接写入私有磁盘文件。
- 正文上限为 768 MiB，磁盘至少保留 128 MiB 空闲空间；超出正文上限返回 413，容量、保留空间或写入失败返回 507。
- 磁盘缓存目录已有残留文件时会先同步现有用量，再判断总容量；并发写入通过预留计数避免超过配置上限。
- 入口不裁剪 `encrypted_content`、工具、历史或其他请求字段，也不将本地压缩改写为 Chat 请求。

## 验证

以下命令在工作树
`/Volumes/Work/code/new-api/.worktrees/responses-opaque-replay-integration`
执行，退出码均为 0：

```text
go test ./common ./middleware ./router -count=1 -timeout 180s
go vet ./common ./middleware ./router
go test -race ./common ./middleware ./router -count=1 -timeout 240s
go test ./common ./middleware ./router -run 'CodexLocalResponsesCompaction|DecompressRequestMiddleware.*Codex|IsCodexLocalResponsesCompaction|RequiredDiskBodyStorage' -count=10 -timeout 240s
git diff --check
git diff --cached --check
```

覆盖项包括：四种支持编码的字节摘要保真、损坏和未知编码、单请求和并发容量耗尽、磁盘目录写入失败、启动后残留缓存统计、精确协议识别，以及实际路由中未授权 gzip 请求的零次正文读取。

## 未覆盖边界

- 本提交尚未消除后续 Responses DTO 反序列化、模型提取、计费和上游请求构造中的整包读取；这些是下一原子任务。
- 尚未执行隔离 Docker 3001 重建或真实上游渠道验证，因此本记录不能作为生产升级依据。
- 正式服务、正式数据库、渠道配置和凭证均未修改。
