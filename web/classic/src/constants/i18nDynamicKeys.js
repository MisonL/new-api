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

export {};
