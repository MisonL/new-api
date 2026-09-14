# GPT-5.6 Responses 正式环境升级审计

## 1. 结论

受控发布记录确认升级完成，候选镜像、部署 revision 和容器生命周期值保存在私有部署审计中，不纳入公开仓库。

最终运行代码包含以下已验证修复：

- Responses 历史 `namespace` 精确索引移除与受控重试。
- `arguments` 字符串和对象双向兼容重试。
- SSE 前导事件缓冲与 ping 安全重试。
- 流式错误状态映射和多种错误事件解析。
- namespace 工具能力显式路由检查。

一个上游渠道对 `arguments` 类型要求不一致，不能使用固定 object 或 string 转换。部署配置已对该渠道禁用受影响模型能力，保留其其他模型能力；同时保留 `supports_namespace_tools=false`，防止误用 namespace 工具。渠道标识保存在私有运行记录中。

## 2. 发布过程

### 2.1 首次升级

首次升级将经过门禁的候选 revision 部署到应用容器。升级后容器健康，普通 Responses 请求返回 `200`。具体 revision 仅保存在私有部署审计中。

生产监控随后发现长会话在前序渠道失败后落到受影响渠道，最终仍返回：

```text
Invalid type for 'input[406].arguments': expected an object, but got a string instead.
```

错误发生前已经存在语义输出，不能安全重放，否则可能重复工具调用。

### 2.2 反证与回退

为验证首次请求前规范化，曾部署一个受控候选并临时把受影响渠道配置为 object。真实请求证明转换已执行，但另一条请求返回相反错误：

```text
Invalid type for 'input[38].arguments': expected a string, but got an object instead.
```

这证明受影响渠道的后端能力不一致，固定格式不是正确方案。已执行以下回退：

- 删除正式和开发渠道中的临时 `arguments_format` 配置。
- 回退不正确的规范化改动。
- 禁用受影响渠道的相关模型 ability。
- 重新构建并部署经过验证的稳定镜像。

## 3. 验证记录

### 3.1 代码验证

以下命令均退出 0：

```bash
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./controller ./model ./relay/common ./relay/channel/openai ./relay ./relay/helper
git diff --check
```

最终部署 revision 与已验证候选在本次涉及的业务文件上无差异，完整 revision 对比保存在私有审计记录。

### 3.2 构建与运行验证

正式与开发容器均验证为候选 build-info：

```text
version=<candidate-version>
commit=<validated-revision>
```

正式状态接口和开发状态接口均返回 `success=true`。

### 3.3 真实请求验证

最终稳定镜像部署后，真实 Responses 请求结果为：

```text
HTTP 200
status=completed
output_text=OK
```

连续采样期间，每次结果均为：

- 容器状态：`healthy`
- 新增 `arguments` 或 `namespace` 协议错误：0
- 受影响渠道的相关模型日志：0

## 4. 备份与回滚

发布前、变更前和最终稳定态均保留了 PostgreSQL 自定义格式备份。备份位置、哈希和恢复演练记录保存在私有运维审计中。

保留了可验证的回滚镜像。镜像引用和 digest 保存在私有运维审计中。

## 5. 剩余风险

正式环境剩余渠道仍观察到上游 `429`、`502` 和 `524`。这些错误来自上游限额、网关或连接可用性，不属于本次 `namespace` 和 `arguments` 协议修复范围。

受影响渠道在确认后端协议一致前不得重新启用相关模型能力。重新启用前必须分别验证长历史、多轮工具调用、compact 后续请求，以及 object 和 string 两种 `arguments` 历史输入。
