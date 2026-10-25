import header from 'eslint-plugin-header';

// eslint-plugin-header@3.1.1 是最新版，但它同时踩了两个已被 ESLint 10 移除的
// 旧接口，直接引用会让 classic 前端的 lint 门禁整体失败：
//
// 1. 规则没有声明 meta.schema。flat config 对未声明 schema 的规则按“不接受
//    任何选项”校验，header/header 传参直接报 "should NOT have more than 0 items"。
// 2. 规则实现里用 context.getSourceCode()，该接口在 ESLint 10 已被删除，改为
//    context.sourceCode，运行时报 "context.getSourceCode is not a function"。
//
// 这里只在本地补上 schema，并把规则 create() 收到的 context 包一层 Proxy，让
// 旧调用落到现代属性上。规则判断与自动修复逻辑保持原样，没有重写。
// 上游发布可用版本后可删除本文件并直接引用 eslint-plugin-header。
const headerRule = header.rules.header;

// 用普通浅拷贝而不是 Proxy：ESLint 10 的 context 上有只读不可配置属性
// （report 等），Proxy 返回包装函数会违反不变量直接抛 TypeError。
function withLegacyContext(context) {
  return {
    ...context,
    getSourceCode() {
      return context.sourceCode;
    },
  };
}

export default {
  ...header,
  rules: {
    header: {
      ...headerRule,
      meta: {
        ...headerRule.meta,
        schema: [
          { type: 'string' },
          {
            type: 'array',
            items: { type: 'string' },
          },
          {
            anyOf: [
              { type: 'integer', minimum: 0 },
              {
                type: 'object',
                properties: {
                  lineEndings: { enum: ['unix', 'windows'] },
                  numNewlines: { type: 'integer', minimum: 0 },
                },
                additionalProperties: false,
              },
            ],
          },
        ],
      },
      create(context) {
        return headerRule.create(withLegacyContext(context));
      },
    },
  },
};
