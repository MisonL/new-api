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
