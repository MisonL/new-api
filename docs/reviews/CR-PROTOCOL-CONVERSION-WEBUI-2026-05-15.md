# CR-PROTOCOL-CONVERSION-WEBUI-2026-05-15

## 范围

本次审查覆盖协议转换策略 WebUI 完整化改动：

- `web/default` 全局模型配置页的规则列表、规则编辑抽屉、渠道选择、命中预览、JSON 导入和恢复已保存能力。
- `web/classic` 协议转换编辑器对自定义工具桥接和未知字段保留的同步修正。
- `setting/model_setting` 对策略 JSON 的严格校验、未知字段保留和软告警能力。

采样时间：2026-05-15。

## 关键结论

- 规则编辑器已能在可视化模式维护 `rules[].options.enable_custom_tool_bridge`。
- 前端 round-trip 测试覆盖顶层、规则、`options` 未知字段保留，legacy 升级，以及错误方向移除自定义工具桥接。
- 后端保存链路已验证：通过浏览器登录态写入含未知字段的策略后，读取结果保留顶层、规则和 `options` 扩展字段，并规范化规则名称和模型正则。
- 2026-05-19 review-fix 已补充并验证：无 `name` 规则的双向未知字段保留、`options` 非对象显式报错、`options: null` 显式报错、endpoint alias 未知字段保留，以及 Default 前端 JSON 导入严格校验，避免前端静默过滤非法规则。
- 2026-05-19 review-fix 已实现命中预览对目标渠道 `pass_through_body_enabled` 的代码支持；浏览器人工复验渠道透传路径已完成，覆盖全局透传、渠道透传、双重透传和无透传四条路径。
- `3001` 隔离开发容器已使用当前工作区源码重建，运行镜像为 `new-api-local:dev`。

## 验证记录

### 后端

```bash
go test ./setting/model_setting
```

结果：通过。

```bash
go test ./controller ./model ./relay/common ./relay/helper ./service ./setting/model_setting
```

结果：通过。

摘要：

- `controller`: ok, 8.041s
- `model`: ok, 4.491s
- `relay/common`: ok, 0.107s
- `relay/helper`: ok, 14.126s
- `service`: ok, 0.505s
- `setting/model_setting`: ok, cached

### web/default

```bash
bun test tests/protocol-conversion-policy-utils.test.ts
```

结果：通过，3 pass, 0 fail。

```bash
bun run i18n:sync
```

结果：通过。`en`、`fr`、`ja`、`ru`、`vi`、`zh` 的 `missingCount`、`extrasCount`、`untranslatedCount` 均为 0。

```bash
bun run lint
bun run typecheck
bun run build
bun run format:check
```

结果：全部通过。

### 2026-05-19 review-fix 增量验证

```bash
go test ./setting/model_setting ./service/openaicompat
```

结果：通过。覆盖非对象 options 显式报错、`options: null` 显式报错、无 name 规则的双向未知字段保留和 endpoint alias 未知字段保留。

```bash
cd web/default && bun test tests/protocol-conversion-policy-utils.test.ts
```

结果：通过，15 pass, 0 fail。覆盖 Default 前端 JSON 导入严格校验、显式 `rules: []` 不回退 legacy 字段、规则字段错误路径展示、缺失 endpoint 的 required 提示。

```bash
cd web/default && bun run lint
cd web/default && bun run typecheck
cd web/default && bun run build
```

结果：通过。代码实施已通过 typecheck 与 build；渠道透传路径已于 2026-05-19 完成浏览器复验。

```bash
cd web/classic && bun test tests/protocolConversionPolicyUtils.test.js
cd web/classic && bun run build
```

结果：通过。

浏览器复验：渠道透传路径已覆盖以下四个场景：

- 全局透传：开启 `global.pass_through_request_enabled`，创建 `responses` 到 `chat_completions` 规则，限定渠道 `117`，模型正则 `^gpt-5.*$`；预览输入 `channel_id=117`、`channel_type=1`、`model=gpt-5.1`，显示 1 条透传跳过转换提示。
- 渠道透传：关闭全局透传，开启渠道 `117` 的 `pass_through_body_enabled`，使用同一规则和预览输入，显示 1 条透传跳过转换提示。
- 双重透传：同时开启全局透传和渠道透传，显示 1 条跳过转换提示，未重复提示。
- 无透传：关闭全局透传和渠道透传，使用同一规则和预览输入，显示正常命中，不出现跳过转换提示。

复验后已恢复 `global.chat_completions_to_responses_policy`、`global.pass_through_request_enabled`、渠道 `117` 的 `setting`，并恢复临时测试管理员密码哈希。

### web/classic

```bash
bun test tests/protocolConversionPolicyUtils.test.js
```

结果：通过，2 pass, 0 fail。

```bash
bun run lint
bun run eslint
bun run build
```

结果：全部通过。

## 3001 隔离开发环境

构建命令：

```bash
scripts/build-docker-local.sh new-api-local:dev
```

结果：通过。

提交前工作区构建曾显示 `-dirty`，提交后已重新构建隔离开发镜像。最终构建通过 `docker exec new-api-dev-isolated-new-api-1 /new-api --build-info` 复核，`commit` 与当前 Git 提交一致，且不带 `-dirty` 后缀。

最终镜像：`new-api-local:dev`。

重建命令：

```bash
docker compose -f deploy/compose/dev-isolated.yml --env-file deploy/env/dev-isolated.env up -d --no-deps --force-recreate new-api
```

结果：`new-api-dev-isolated-new-api-1` 已重建并启动。

运行态确认：

```bash
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
```

结果：

```text
version=v1.1.0
commit=<当前提交哈希>
date=<最终构建时间>
source=https://github.com/MisonL/new-api
```

```bash
curl -fsS http://127.0.0.1:3001/api/status
```

结果：返回 `success: true`。

```bash
docker inspect new-api-dev-isolated-new-api-1 --format '{{json .State.Status}} {{json .State.Health.Status}}'
```

结果：

```text
"running" "healthy"
```

## 浏览器复验

目标：

- `http://127.0.0.1:5177/system-settings/models/global`
- 后端代理目标：`http://127.0.0.1:3001`
- 登录用户：本地 smoke 管理员用户，用户 ID `24`

验证内容：

- desktop/light：全局模型配置页可渲染，规则列表可见，编辑抽屉可打开。
- mobile/dark：页面和编辑抽屉可渲染。
- 命中预览：输入 `channel_id=117`、`channel_type=1`、`model=gpt-5.1` 后，结果从未命中切换为已命中。
- 保存链路：通过 `/api/option/` 写入含 `future_policy`、`future_rule`、`future_option` 的策略后读取，未知字段均保留；随后已恢复原始配置。
- 控制台：仅有 reduced-motion 提示，无业务错误。

## 边界

- 本次真实保存链路覆盖了 `3001` 隔离开发环境和当前登录态，不代表生产环境配置已变更。
- 本次未变更生产 `3000` 栈。
