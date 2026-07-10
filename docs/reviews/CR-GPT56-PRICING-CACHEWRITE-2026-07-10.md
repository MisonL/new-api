# GPT-5.6 计费与缓存写入验证

## 范围

- 为 `gpt-5.6`、`gpt-5.6-sol`、`gpt-5.6-terra` 和 `gpt-5.6-luna` 增加默认 token 计费倍率。
- 对 `gpt-5.5` 及更高版本的带小数点 `gpt-5` 模型系列使用可配置的输出倍率。
- 在渠道测试结算和日志快照中记录缓存读取与缓存写入倍率。

## 计费契约

倍率遵循 `docs/operations/pricing-maintenance.md`：

- `ModelRatio = input_usd_per_1M / 2`
- `CompletionRatio = output_usd_per_1M / input_usd_per_1M`
- `CacheRatio = cached_input_usd_per_1M / input_usd_per_1M`
- `CreateCacheRatio = cache_write_usd_per_1M / input_usd_per_1M`

## 验证记录

采样时间：`2026-07-10 13:42 Asia/Shanghai`。

```text
命令：go test ./controller ./relay/common ./relay/channel/openai ./relay ./service ./setting/ratio_setting -count=1
退出码：0
结果：所选包全部通过

命令：git diff --check
退出码：0
结果：未发现空白字符错误
```

隔离 Docker 镜像已基于存在未提交改动的工作区重建并部署到 `3001` 端口。运行时报告如下：

```text
version=v1.1.0
commit=51074ac3736288ee664966e0a2109e9580d86fcf-dirty
date=2026-07-10T05:42:57Z
```

`GET http://127.0.0.1:3001/api/status` 返回 `success=true`。该结果仅验证隔离运行环境；本次未修改正式环境的计费选项或历史日志。
