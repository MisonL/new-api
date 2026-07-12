# GPT-5.6 Responses 正式环境升级审计

## 1. 结论

2026-07-12 已完成正式环境升级。最终正式镜像为 `new-api-local:prod-20260712-stable-9299f29f2`，运行提交为 `9299f29f2147eea27cc2b5d6b47662fde2b35791`，容器健康。

最终运行代码与 `4c3db224b28f01c3ba0c5f1792d6cf379f7399e2` 的业务代码一致，包含以下修复：

- Responses 历史 `namespace` 精确索引移除与受控重试。
- `arguments` 字符串和对象双向兼容重试。
- SSE 前导事件缓冲与 ping 安全重试。
- 流式错误状态映射和多种错误事件解析。
- namespace 工具能力显式路由检查。

渠道 150 的上游后端对 `arguments` 类型要求不一致，不能使用固定 object 或 string 转换。正式环境已禁用渠道 150 的全部 `gpt-5.6*` 能力，保留其其他模型能力；同时保留 `supports_namespace_tools=false`，防止误用 namespace 工具。

## 2. 发布过程

### 2.1 首次升级

首次将正式容器从提交 `15a5dbd6f22d7bc76c0d42437f86c96844e11a46` 升级到 `4c3db224b28f01c3ba0c5f1792d6cf379f7399e2`。升级后容器健康，普通 `gpt-5.6-sol` Responses 请求返回 `200`。

生产监控随后发现两条长会话在前序渠道失败后落到渠道 150，最终仍返回：

```text
Invalid type for 'input[406].arguments': expected an object, but got a string instead.
```

错误发生前已经存在语义输出，不能安全重放，否则可能重复工具调用。

### 2.2 反证与回退

为验证首次请求前规范化，曾提交 `156f63b4a639c2ca225f4ffb2163638619d57187` 并临时将渠道 150 配置为 object。真实请求证明转换已执行，但另一条生产请求返回相反错误：

```text
Invalid type for 'input[38].arguments': expected a string, but got an object instead.
```

这证明渠道 150 后端能力不一致，固定格式不是正确方案。已执行以下回退：

- 删除正式和开发渠道 150 的临时 `arguments_format` 配置。
- 使用提交 `9299f29f2` 回退 `156f63b4a` 的代码改动。
- 禁用正式和开发渠道 150 的全部 `gpt-5.6*` ability。
- 重新构建并部署最终稳定镜像。

## 3. 验证记录

### 3.1 代码验证

以下命令均退出 0：

```bash
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./controller ./model ./relay/common ./relay/channel/openai ./relay ./relay/helper
git diff --check
```

提交 `9299f29f2` 与 `4c3db224b` 在本次涉及的五个业务文件上无差异。

### 3.2 构建与运行验证

正式与开发容器均验证为：

```text
version=v1.1.0
commit=9299f29f2147eea27cc2b5d6b47662fde2b35791
```

正式状态接口和开发状态接口均返回 `success=true`。

### 3.3 真实请求验证

最终稳定镜像部署后，真实 `gpt-5.6-sol` Responses 请求结果为：

```text
HTTP 200
status=completed
output_text=OK
```

连续 2 分钟执行 4 次正式环境采样，每次结果均为：

- 容器状态：`healthy`
- 新增 `arguments` 或 `namespace` 协议错误：0
- 渠道 150 的 `gpt-5.6*` 日志：0

## 4. 备份与回滚

保留以下 PostgreSQL 自定义格式备份：

- `backups/new-api-pre-responses-compat-20260712-104932.dump`
  - SHA-256：`2d4462fc6aca4579ad44dfae7de8fea44798151c5fd27d6c7f3c8ce86c428bfc`
- `backups/new-api-pre-arguments-format-20260712-150638.dump`
  - SHA-256：`b7e646a622ff59d893036e79c224456c583b65a5689766f1d9bd82c0757ab85c`
- `backups/new-api-final-stable-20260712-154551.dump`
  - SHA-256：`502b0dc4eeb53924c2d544b084742c094053c03fc7c8e8ee42634429aa44393b`

保留以下回滚镜像：

- `new-api-local:rollback-20260712-15a5dbd6f`
- `new-api-local:rollback-20260712-responses-compat-4c3db224b`
- `new-api-local:rollback-20260712-arguments-format-156f63b4a`

## 5. 剩余风险

正式环境剩余渠道仍观察到上游 `429`、`502` 和 `524`。这些错误来自上游限额、网关或连接可用性，不属于本次 `namespace` 和 `arguments` 协议修复范围。

渠道 150 在确认后端协议一致前不得重新启用 `gpt-5.6*`。重新启用前必须分别验证长历史、多轮工具调用、compact 后续请求，以及 object 和 string 两种 `arguments` 历史输入。
