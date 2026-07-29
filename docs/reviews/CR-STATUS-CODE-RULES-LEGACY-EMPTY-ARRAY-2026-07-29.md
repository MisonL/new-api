# CR-STATUS-CODE-RULES-LEGACY-EMPTY-ARRAY-2026-07-29

## Summary

采样时间：2026-07-29T14:06:00Z。

隔离开发服务启动时反复记录：

```text
failed to update option map: invalid http status code rules: []
```

根因是旧数据库把 `AutomaticDisableStatusCodes` 与 `AutomaticRetryStatusCodes` 的空规则保存为 `[]`，而当前解析器仅接受逗号分隔的文本规则。载入失败会使内存中的规则保留默认值，因此管理员显式配置的“无规则”不会生效。

## Change

- 仅将精确的旧表示 `[]` 解释为无规则；`[401]` 等非空 JSON 仍然是非法输入。
- `UpdateOption` 与启动期 `updateOptionMap` 对两个状态码 option 使用同一规范化路径。旧值会在内存和 API 输出中变为标准空字符串；后续保存会以标准空字符串写入数据库。
- 非空逗号规则继续按既有范围合并逻辑解析，不引入静默回退。

## Verification

L0：

```bash
go test ./setting/operation_setting ./model \
  -run 'Test(ParseHTTPStatusCodeRanges|UpdateOptionNormalizesLegacyEmptyStatusCodeRules)' \
  -count=1 -timeout 60s
go test ./setting/operation_setting ./model -count=1 -timeout 60s
git diff --check
```

结果：均退出码 `0`。

L2：

```bash
scripts/build-docker-local.sh new-api-local:dev
docker compose -f deploy/compose/dev-isolated.yml \
  --env-file /Volumes/Work/code/new-api/deploy/env/dev-isolated.env \
  up -d --no-deps --force-recreate new-api
docker exec new-api-dev-isolated-new-api-1 /new-api --build-info
curl --retry 10 --retry-all-errors --retry-delay 1 \
  --connect-timeout 3 --max-time 5 \
  -fsS http://127.0.0.1:3001/api/status
```

结果：镜像构建成功，build-info 为 `220ed24ec3057c469880d54b5e7e309e6711cca3-dirty`，状态接口返回 `success:true`，容器最终为 `running healthy`。新启动日志未匹配 `invalid http status code rules`、panic、fatal、监听失败或迁移失败。

## Boundary

- 未修改正式服务、正式数据库或正式 Redis。
- 隔离数据库中原有 `[]` 配置保持不写回；修复在载入时按无规则生效。管理员下次保存设置时会自动使用标准空字符串。
