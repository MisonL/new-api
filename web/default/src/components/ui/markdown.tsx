import ReactMarkdown from 'react-markdown'
import rehypeRaw from 'rehype-raw'
import rehypeSanitize, { defaultSchema } from 'rehype-sanitize'
import remarkGfm from 'remark-gfm'
import { cn } from '@/lib/utils'

interface MarkdownProps {
  children: string
  className?: string
}

// rehypeRaw 会把内容里的原始 HTML 解构进 React 树；不叠加净化时，
// <img onerror> 之类的属性事件向量会随管理员配置的富文本（首页、公告、
// 关于、法律条款）进入页面。白名单在 GitHub 风格默认 schema 上补齐
// style/img 数据属性，其余脚本类内容一律剥除。
const sanitizeSchema = {
  ...defaultSchema,
  tagNames: [...(defaultSchema.tagNames ?? []), 'style'],
  attributes: {
    ...defaultSchema.attributes,
    '*': [...(defaultSchema.attributes?.['*'] ?? []), 'style', 'class'],
    img: [...(defaultSchema.attributes?.img ?? []), 'src', 'srcSet', 'sizes'],
  },
  protocols: {
    ...defaultSchema.protocols,
    href: ['http', 'https', 'mailto', 'tel'],
    src: ['http', 'https', 'data'],
  },
}

export function Markdown({ children, className }: MarkdownProps) {
  return (
    <div
      className={cn(
        'prose prose-sm dark:prose-invert max-w-none',
        'prose-headings:font-semibold prose-headings:tracking-tight',
        'prose-h1:text-2xl prose-h2:text-xl prose-h3:text-lg',
        'prose-p:leading-relaxed prose-p:my-2',
        'prose-a:text-primary prose-a:no-underline hover:prose-a:underline',
        'prose-code:bg-muted prose-code:px-1 prose-code:py-0.5 prose-code:rounded prose-code:before:content-none prose-code:after:content-none',
        'prose-pre:bg-muted prose-pre:border',
        'prose-blockquote:border-l-primary prose-blockquote:bg-muted/50 prose-blockquote:py-1',
        'prose-ul:my-2 prose-ol:my-2 prose-li:my-1',
        'prose-table:border prose-thead:bg-muted',
        'prose-td:border prose-th:border prose-td:px-3 prose-th:px-3',
        'prose-img:rounded-lg prose-img:shadow-sm',
        '[&>*:first-child]:mt-0 [&>*:last-child]:mb-0',
        '[overflow-wrap:anywhere] break-words',
        className
      )}
    >
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeRaw, [rehypeSanitize, sanitizeSchema]]}
        components={{
          // 自定义组件渲染（可选）
          a: ({ node, ...props }) => (
            <a {...props} target='_blank' rel='noopener noreferrer' />
          ),
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  )
}
