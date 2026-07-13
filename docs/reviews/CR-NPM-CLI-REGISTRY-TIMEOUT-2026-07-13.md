# npm CLI registry 超时复核

## 问题

隔离开发容器每 10 分钟刷新 5 个 npm CLI 包版本，连续 3 轮全部失败，每个包均记录 `npm_registry_timeout`，单轮耗时约 25 秒。

## 根因

- npm 请求已使用 `application/vnd.npm.install-v1+json` abbreviated metadata，并由 Go HTTP client 自动处理 gzip。
- `@openai/codex` abbreviated metadata 当前解压前传输约 648 KiB，解压后约 2.87 MiB，仍低于 4 MiB 解码上限。
- 宿主机和隔离容器下载耗时约 5.6 至 6.0 秒，超过后端原有 5 秒总超时。
- registry、GitHub 和其他外部地址均可连接，问题不是容器断网。

## 修复

将单包 npm registry 请求总超时从 5 秒调整为 8 秒。4 MiB 元数据上限、abbreviated metadata 请求头、错误分类和缓存降级语义保持不变。前端手动刷新请求超时仍为 10 秒，高于后端超时。

## 验证边界

代码测试需要覆盖 npm CLI version service、controller 和诊断接口。隔离环境重建后，应等待或触发一次后台刷新，并确认至少能成功刷新当前包记录；若 registry 实际耗时再次超过 8 秒，仍应显式记录 `npm_registry_timeout`，不得静默使用过期结果冒充刷新成功。
