/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

// 动态翻译键登记（仅供提取器扫描，运行时不导入本模块）
//
// 以下键在运行期由常量对象的值（渠道类型、请求头上游档案、状态映射、日期预设、
// 用户级上下文预设等）经 t(变量.字段) 消费。i18next-cli 只解析字面量参数，
// 无法静态解析对象属性值，故这些键会从词典遗漏。
// 这里以注释中的 t(...) 形式登记，i18next-cli 的注释解析器会将其提取，
// 从而保证词典不遗漏；新增此类动态键时请在此追加。

// 渠道类型标签（CHANNEL_OPTIONS，经 ChannelsTabs/ChannelsColumnDefs 的 t(变量.label) 消费）
// t('360 智脑')
// t('自定义渠道')
// t('知识库：FastGPT')
// t('知识库：AI Proxy')
// t('嵌入模型：MokaAI M3E')
// t('字节火山方舟、豆包通用')
// t('可灵')
// t('即梦')
// t('豆包视频')
// t('百度文心千帆')
// t('百度文心千帆V2')
// t('阿里通义千问')
// t('讯飞星火认知')
// t('智谱 ChatGLM（已经弃用，请使用智谱 GLM-4V）')
// t('智谱 GLM-4V')
// 时区标签（COMMON_TIMEZONES，经 TieredPricingEditor 的 t(tz.label) 消费）
// t('UTC+8 北京 (Asia/Shanghai)')
// t('UTC-5 纽约 (America/New_York)')
// t('UTC-8 洛杉矶 (America/Los_Angeles)')
// t('UTC-6 芝加哥 (America/Chicago)')
// t('UTC+0 伦敦 (Europe/London)')
// t('UTC+1 柏林 (Europe/Berlin)')
// t('UTC+9 东京 (Asia/Tokyo)')
// t('UTC+8 新加坡 (Asia/Singapore)')
// t('UTC+9 首尔 (Asia/Seoul)')
// t('UTC+10 悉尼 (Australia/Sydney)')
// 用户级上下文预设（CONTEXT_KEY_PRESETS，经 SettingsChannelAffinity 的 t(x.label) 消费）
// t('id（用户 ID）')
// 高级参数覆盖的操作模式、条件模式、字段同步、请求头等下拉标签（ParamOverrideEditorModal
// 的 optionList / label map / 字段指引，经 tOptions()/t(变量) 消费）
// t('设置字段')
// t('追加到末尾')
// t('追加到开头')
// t('复制字段')
// t('移动字段')
// t('字符串替换')
// t('裁剪前缀')
// t('裁剪后缀')
// t('确保前缀')
// t('确保后缀')
// t('去掉空白')
// t('返回自定义错误')
// t('清理对象项')
// t('请求头透传')
// t('字段同步')
// t('设置请求头')
// t('完全匹配')
// t('前缀匹配')
// t('后缀匹配')
// t('请求体字段')
// t('请求头字段')
// t('文本')
// t('数字')
// t('布尔值')
// t('空值')
// t('对象')
// t('整条请求头值')
// t('Token 映射')
// t('填入目标路径')
// t('填入来源字段')
// t('填入目标字段')
// t('常用请求字段')
// t('支持多级模型名，例如 openai/gpt-4o-mini')
// t('采样温度')
// t('控制输出随机性')
// t('最大输出 Token')
// t('控制输出长度上限')
// t('最后一条消息内容')
// t('常用于重写用户输入')
// t('上下文字段')
// t('是否重试')
// t('true 表示重试请求')
// t('上次错误码')
// t('配合重试策略使用')
// t('会话 ID')
// t('可用于路由或缓存命中')
// t('请求头映射字段')
// t('标准化 Authorization')
// t('统一小写后可稳定匹配')
// t('标准化 X-Debug-Mode')
// t('适合灰度 / 调试开关判断')

// t('名称已存在')
// t('Headers JSON 不能为空')
// t('Headers JSON 必须是合法 JSON')
// t('Headers JSON 必须是非空对象')
// t('Headers JSON 的值必须全部是字符串')
// t('Anthropic Beta/Version 透传')
// t('Claude Code 真实请求头透传')
// t('Codex CLI 动态请求头透传')
// t('Codex Desktop 动态请求头透传')
// t('Droid CLI 真实请求头透传')
// t('Gemini CLI 真实请求头透传')
// t('Gemini 图片 4K')
// t('Haiku 模型')
// t('OpenAI SDK 元数据透传')
// t('Opus 模型')
// t('Qwen Code 真实请求头透传')
// t('Sonnet 模型')
// t('主模型')
// t('仅 Chat 代理')
// t('今天')
// t('停止中')
// t('准备中')
// t('前缀名称匹配')
// t('包含名称匹配')
// t('可信 New API')
// t('可用率')
// t('同集群 New API')
// t('后缀名称匹配')
// t('复制功能需要 HTTPS 环境，请手动复制')
// t('多模态')
// t('官方 New API')
// t('官方 OpenAI')
// t('已终止')
// t('已部署')
// t('当模型名包含 gemini/image 并以 4k 结尾时写入 imageSize=4K。')
// t('待支付')
// t('无法复制此类型的消息内容')
// t('时间促销')
// t('有异常')
// t('此消息没有可复制的文本内容')
// t('浏览器不支持复制功能，请手动复制')
// t('精确名称匹配')
// t('维护中')
// t('网络连接失败或服务器无响应')
// t('自定义请求体格式错误，请检查JSON格式')
// t('请求条件')
// t('近 30 天')
// t('近 7 天')
// t('透传 Anthropic-Beta、Anthropic-Version 和 X-App 等运行时请求头。')
// t('透传 Claude Code 会话、Anthropic Beta 和 Stainless 动态头。')
// t('透传 Codex Desktop 会话、窗口、turn metadata 和客户端请求 ID；不包含 User-Agent，UA 请使用客户端模板。')
// t('透传 Codex 会话、窗口、turn metadata 和客户端请求 ID；不包含 User-Agent，UA 请使用客户端模板。')
// t('透传 Droid CLI 使用的 Stainless 客户端动态头。')
// t('透传 Gemini CLI 的 x-goog-api-client 动态头。')
// t('透传 OpenAI-Organization、OpenAI-Project 和 X-Stainless-* 客户端元数据。')
// t('透传 Qwen Code 使用的 Stainless 客户端动态头。')
// t('透传 X-Request-Id、X-Trace-Id、X-Correlation-Id、Traceparent。')
// t('通用 OpenAI')
// t('通用代理')
// t('高延迟')
// t('默认行为')

// 计费变量覆盖/短标签（BILLING_VARS 的 label/shortLabel，经 helpers/utils.jsx、
// helpers/render.jsx 的 t(label) 消费）
// t('1h缓存创建')
// t('1h缓存创建价格')
// t('图片输入')
// t('图片输出')
// t('图片输出价格')
// t('音频输入')
// t('音频输出')
// 阶梯计费编辑器的变量与预设标签（TieredPricingEditor 的 t(v.label)/t(p.label) 消费）
// t('p (输入)')
// t('c (输出)')
// t('夜间半价')
// t('周末8折')
// 兑换码状态文案（redemption.constants 的 text，经 RedemptionsColumnDefs 的
// t(statusConfig.text) 消费）
// t('未使用')
// t('已使用')
// 订阅套餐有效期/重置周期的自定义选项（AddEditSubscriptionModal 的 t(o.label) 消费）
// t('自定义(秒)')
// 渠道亲和性校验消息（SettingsChannelAffinity 的 t(validation.message) 消费）
// t('Key 不能为空')
// t('Key 来源不能为空')
// t('Key 来源类型不合法')
// t('Path 不能为空')
// t('规则 JSON 不合法')
// t('规则 JSON 必须是数组')
// t('{{label}} JSON 格式不正确')
// t('{{label}} 必须是 JSON 对象')
// 请求头模板校验消息（headerOverrideUserAgent 的 message，经各弹窗 t(message) 消费）
// t('请求头名称不能为空！')
// t('请求头值类型不受支持: {{key}}')
// 协议转换规则预览与提醒（protocolConversionPolicy 的 reason/warning，经
// ProtocolPolicyRuleBody/Summary 的 t(result.reason)/t(attentionKeys[0]) 消费）
// t('模型正则不匹配。')
// t('渠道范围为空，这条规则不会命中。')
// t('示例请求命中规则，但全局透传会跳过协议转换。')
// t('示例渠道和模型命中这条条件规则；只有需要自定义工具桥接的请求才会转换。')
// t('预览需要填写模型名称。')
// t('已启用全局请求透传，运行时会跳过协议转换。')
// 登录页默认密码提醒（LoginForm 的 Modal.error 标题与内容）
// t('您正在使用默认密码！')
// t('请立刻修改默认密码！')

// 高级参数覆盖的操作模式描述（MODE_DESCRIPTIONS，经 ParamOverrideEditorModal 的
// t(MODE_DESCRIPTIONS[mode]) 消费）
// t('把值写入目标字段')
// t('删除目标字段')
// t('把值追加到数组 / 字符串 / 对象末尾')
// t('把值追加到数组 / 字符串 / 对象开头')
// t('把来源字段复制到目标字段')
// t('把来源字段移动到目标字段')
// t('在目标字段里做字符串替换')
// t('在目标字段里做正则替换')
// t('去掉字符串前缀')
// t('去掉字符串后缀')
// t('确保字符串有指定前缀')
// t('确保字符串有指定后缀')
// t('去掉字符串头尾空白')
// t('把字符串转成小写')
// t('把字符串转成大写')
// t('立即返回自定义错误')
// t('按条件清理对象中的子项')
// t('把客户端原始请求里的指定请求头透传到上游；适合 Codex CLI / Claude Code 等要求真实客户端动态头的渠道')
// t('在一个字段有值、另一个缺失时自动补齐')
// t('设置运行期请求头：可直接覆盖整条值，也可对逗号分隔的 token 做删除、替换、追加或白名单保留')
// t('删除运行期请求头')
// 高级参数覆盖编辑器 JSON 校验提示（ParamOverrideEditorModal 的 t(jsonError) 消费）
// t('JSON 格式不正确')
// 渠道列表标签分组行前缀（ChannelsColumnDefs 的 t('标签：') 消费）
// t('标签：')
// 上游模型冲突字段名（UpstreamConflictModal 的 FIELD_LABELS，经 t(rawLabel) 消费）
// t('命名规则')

export {};
