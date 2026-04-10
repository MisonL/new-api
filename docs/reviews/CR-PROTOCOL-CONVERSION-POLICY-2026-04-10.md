# Protocol Conversion Policy Review

日期：2026-04-10
分支：`main`
基线：`upstream/main`
关键提交：

- `88422af4` `merge: sync feat/protocol-conversion-policy-ui with main`
- `879896bf` `fix(web): harden protocol conversion policy editor`
- `f828c968` `fix(web): preserve protocol policy editor state sync`

## 范围

本次复核覆盖以下内容：

- `feat/protocol-conversion-policy-ui` 合入 `main`
- Chat Completions 与 Responses 协议转换策略后端能力
- 协议转换策略管理台可视化编辑器
- 规则新增、展开、删除、无效作用域告警
- 主分支重新构建与独立测试环境验证

## 代码事实

- 协议转换后端：
  - `service/openaicompat/policy.go`
  - `setting/model_setting/global.go`
  - `relay/responses_via_chat.go`
  - `relay/responses_handler.go`
- 管理台页面：
  - `web/src/pages/Setting/Model/SettingGlobalModel.jsx`
- 协议转换可视化编辑器：
  - `web/src/components/settings/ProtocolConversionPolicyEditor.jsx`
  - `web/src/components/settings/protocolConversionPolicy/utils.js`
  - `web/src/components/settings/protocolConversionPolicy/ProtocolPolicyHeader.jsx`
  - `web/src/components/settings/protocolConversionPolicy/ProtocolPolicyRuleBody.jsx`
  - `web/src/components/settings/protocolConversionPolicy/ProtocolPolicyRuleSummary.jsx`

## 自动化验证

### 1. Go 全量测试

命令：

```bash
GOCACHE=/tmp/go-build-new-api-main go test ./... -count=1
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

- 通过
- 最新入口资源为 `/assets/index-DRJ8e-vz.js`

### 3. 主程序重建

命令：

```bash
GOCACHE=/tmp/go-build-new-api-main go build -o /tmp/new-api-main-test/new-api .
```

结果：

- 二进制构建成功
- 构建时出现 Go module stat cache 权限告警，但不影响产物生成

## 独立测试环境

环境：

- 二进制：`/tmp/new-api-main-test/new-api`
- 数据库：`/tmp/new-api-main-test/data/main.db`
- 日志目录：`/tmp/new-api-main-test/logs`
- 端口：`13030`
- 服务地址：`http://127.0.0.1:13030`

启动命令：

```bash
SESSION_SECRET=test-session-secret-20260410 \
CRYPTO_SECRET=test-crypto-secret-20260410 \
SQLITE_PATH=/tmp/new-api-main-test/data/main.db \
PORT=13030 \
/tmp/new-api-main-test/new-api --log-dir /tmp/new-api-main-test/logs
```

说明：

- 该环境使用独立 SQLite 文件，不连接正式环境

## 运行时验证

### 1. 状态接口

命令：

```bash
curl -s http://127.0.0.1:13030/api/status
curl -s http://127.0.0.1:13030/api/setup
```

结果：

- `/api/status` 返回 `success: true`
- `server_address` 为 `http://127.0.0.1:13030`
- `setup` 为 `true`
- `/api/setup` 返回 `status: true`

### 2. 登录态接口

命令：

```bash
curl -s \
  -b /tmp/new-api-main-test/cookies.txt \
  -H 'New-Api-User: 1' \
  http://127.0.0.1:13030/api/user/self

curl -s \
  -b /tmp/new-api-main-test/cookies.txt \
  -H 'New-Api-User: 1' \
  http://127.0.0.1:13030/api/option/
```

结果：

- `rootadmin` 登录态可用
- `/api/option/` 中可见：
  - `global.chat_completions_to_responses_policy`
  - `channel_affinity_setting.rules`

## 浏览器实测

浏览器目标地址：

- `http://127.0.0.1:13030/console`
- `http://127.0.0.1:13030/console/setting?tab=models`

验证结论：

- 管理台可正常登录并进入系统设置
- “模型相关设置”中已显示“协议转换兼容配置”可视化编辑器
- 默认 legacy 规则可正确展示为规则卡片
- 点击“新增规则”后，会新增 `chat-to-responses` 模板规则并自动展开
- 规则展开后可编辑名称、协议方向、渠道范围、模型正则
- 当移除渠道类型且未勾选“作用于全部渠道”时，会立即出现“不会命中”警告
- 删除前一条规则后，剩余规则的展开态保持正常，不再被错误收起

## 本轮补充修复

问题：

- 规则编辑过程中，父组件每次回写 JSON 都会触发子组件再次反序列化，导致规则的临时 client key 丢失
- 在“删除一条规则后继续编辑剩余规则”的场景下，展开态会被重置

修复：

- 在 `web/src/components/settings/ProtocolConversionPolicyEditor.jsx` 中增加序列化结果对比
- 当外部 `value` 反序列化后的结构与当前 `rules` 序列化结果一致时，不再重复 `setRules`

## 残余观察项

- `/api/setup` 当前仍返回 `root_init: false`
- 但系统初始化、管理员登录、控制台访问、设置页加载均已正常
- 该项更像状态口径问题，不阻塞本次功能合入与独立测试验证结论

## 结论

`feat/protocol-conversion-policy-ui` 已合入 `main`，并完成额外前端收口。

当前确认通过的内容：

- 主线构建通过
- 独立测试环境启动通过
- 管理台协议转换策略可视化编辑器可用
- 关键交互链路已验证，包括新增、展开、删除、无效作用域提示
- 删除规则后的展开态回归问题已修复并复测通过
