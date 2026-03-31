import DOMPurify from 'dompurify'

// Markdown 渲染后允许的标签和属性（覆盖 marked 输出 + hljs 语法高亮所需的 class）
const ALLOWED_TAGS = [
  'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'p', 'br', 'hr',
  'ul', 'ol', 'li', 'a', 'strong', 'em', 'b', 'i', 'u', 's', 'del', 'ins',
  'blockquote', 'pre', 'code', 'table', 'thead', 'tbody', 'tr', 'th', 'td',
  'img', 'span', 'div', 'sub', 'sup', 'details', 'summary'
]

const ALLOWED_ATTR = [
  'href', 'target', 'rel', 'class', 'id', 'style',
  'alt', 'title', 'src', 'width', 'height', 'colspan', 'rowspan'
]

/**
 * 消毒 HTML 内容，防止 XSS 攻击
 * 用于所有 v-html 渲染前的内容过滤
 */
export function sanitizeHtml(html: string): string {
  return DOMPurify.sanitize(html, {
    ALLOWED_TAGS,
    ALLOWED_ATTR,
    ALLOW_DATA_ATTR: false
  })
}
