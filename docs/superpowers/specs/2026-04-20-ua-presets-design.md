# UA 预置模板设计

日期：2026-04-20

## 1. 目标

为后台 `请求头覆盖` 编辑区增加可直接插入的 `User-Agent` 预置模板，覆盖以下两处入口：

- 渠道编辑弹窗
- 标签编辑弹窗

目标是降低用户手工填写 `User-Agent` 的出错率，并保证两处交互、数据结构和写入语义完全一致。

## 2. 范围与边界

本轮只修改前端表单层，不修改：

- 后端 `header_override` 解析逻辑
- 数据库存储结构
- 现有请求头覆盖 JSON 语义
- 现有“填入模板”“填入透传模版”“格式化”功能

不引入：

- 后端配置中心
- 全局 UA 管理页面
- 用户自定义 UA 持久化

## 3. 控制目标

### Primary Setpoint

用户可以在渠道编辑与标签编辑中，通过统一的预置入口，快速插入常见浏览器和主流 AI Coding CLI 的 `User-Agent`，且不会误删其它 header。

### Acceptance

- 渠道编辑与标签编辑都能看到一致的 UA 预置入口
- 预置列表按分组展示
- 点击预置后，只改写 `User-Agent` 字段
- 若原 JSON 为空，自动生成最小合法 JSON
- 若原 JSON 合法，保留其它 header，只更新 `User-Agent`
- 若原 JSON 非法，不静默覆盖，直接提示用户先修正

### Guardrails

- 不改变 `header_override` 最终提交值格式
- 不改变现有模版按钮行为
- 不引入与当前系统风格不一致的新交互形态

## 4. 用户场景

### 场景 A：空白配置

用户首次配置请求头覆盖，点击某个 UA 预置后，文本框直接写入：

```json
{
  "User-Agent": "..."
}
```

### 场景 B：已有其它 header

用户已配置：

```json
{
  "Authorization": "Bearer {api_key}",
  "X-Trace": "demo"
}
```

点击某个 UA 预置后，结果应变为：

```json
{
  "Authorization": "Bearer {api_key}",
  "X-Trace": "demo",
  "User-Agent": "..."
}
```

### 场景 C：已有 User-Agent

用户已有 `User-Agent` 时，点击新预置后应替换旧值，不重复插入第二个键。

### 场景 D：非法 JSON

若当前编辑框内容不是合法 JSON，点击预置时不应直接覆盖，而应提示用户先修正或格式化。

## 5. 方案选型

采用共享注册表 + 共享插入逻辑方案。

### 数据层

新增一份前端静态注册表，集中维护 UA 预置：

- `id`
- `group`
- `name`
- `ua`

### 交互层

在 `请求头覆盖` 区块下新增一个轻量级预置入口，按分组渲染可点击项。

分组：

- 浏览器
- AI Coding CLI
- API SDK / 调试工具

### 写入层

统一走一个插入函数：

1. 读当前字段值
2. 尝试解析 JSON
3. 为空则创建新对象
4. 合法则只更新 `User-Agent`
5. 非法则提示，不写入
6. 最终输出格式化 JSON 字符串

## 6. 首批预置清单

### 浏览器

- Chrome Windows
- Chrome macOS
- Safari macOS
- Edge Windows
- Firefox Windows
- Mobile Safari iPhone
- Chrome Android

### AI Coding CLI

- Codex CLI
- Claude Code
- Gemini CLI
- Qwen Code
- OpenCode
- Droid
- AMP

### API SDK / 调试工具

- OpenAI Python
- OpenAI Node
- Anthropic Python
- Anthropic TypeScript
- PostmanRuntime
- curl

说明：

- 首批清单采用前端内置静态值
- 不承诺与各工具未来真实版本号自动同步
- 命名以“用户识别与快速填写”为主，版本字符串可使用当前稳定模板

## 7. 组件与复用

建议抽出两个共享对象：

- `UA_PRESET_REGISTRY`
  - 单一事实源，避免渠道编辑和标签编辑分叉维护
- `applyUserAgentPresetToHeaderOverride`
  - 共享写入函数，保证两处语义一致

如果 UI 代码量合适，可继续抽一个小型展示组件：

- `HeaderOverrideUserAgentPresets`

该组件只负责展示与点击，不负责最终业务提交。

## 8. 错误处理

- 当前值为空：直接插入
- 当前值为合法 JSON 但不是对象：提示格式不合法
- 当前值为非法 JSON：提示先修正
- 预置项缺失：不渲染

禁止：

- 静默覆盖整段文本
- 非法 JSON 时强行重置用户内容

## 9. 验证策略

### L0

- 前端构建或最小静态检查
- 共享工具函数的行为自测

### L1

- 手工检查两处弹窗是否都能展示预置
- 校验空白、已有 header、已有 UA、非法 JSON 四类场景

### L2

- 仅在需要时进入真实页面联调
- 本轮不涉及后端 gate 变更

## 10. 残余风险

- 个别 AI client 的 UA 模板未来可能变化，需要后续维护
- 若两个弹窗现有局部状态管理差异较大，抽共享组件时可能需要轻微适配
- 标签编辑和渠道编辑若有不同的默认值序列化策略，最终格式化风格可能存在细小差异，需要统一
