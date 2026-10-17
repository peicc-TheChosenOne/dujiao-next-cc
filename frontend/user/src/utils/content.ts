import DOMPurify from 'dompurify'
import { getImageUrl } from './image'

// 富文本来自后台编辑与上游站点导入，均不可信：在 DOMPurify 默认白名单基础上
// 保留编辑器生成的链接 target，并去掉可伪造登录框或覆盖全站样式的元素。
const RICH_HTML_SANITIZE_CONFIG = {
    ADD_ATTR: ['target'],
    FORBID_TAGS: ['style', 'form', 'input', 'button', 'textarea', 'select'],
}

/**
 * 将 HTML 内容中的图片路径转换为显示用的绝对路径，并净化为可安全 v-html 的片段
 * /uploads/xxx -> http://domain/uploads/xxx
 */
export function processHtmlForDisplay(html: string): string {
    if (!html) return ''

    // 匹配 src="/uploads/..."，支持单引号和双引号
    // 使用非贪婪匹配 .*?
    const withImages = html.replace(/src=["'](\/uploads\/.*?)["']/g, (_, path) => {
        return `src="${getImageUrl(path)}"`
    })
    // 必须在改写之后净化：对已净化的 HTML 再做正则替换可能把引号写进属性值，重新引入注入
    return DOMPurify.sanitize(withImages, RICH_HTML_SANITIZE_CONFIG)
}

/**
 * 将 HTML 内容中的图片路径转换为存储用的相对路径
 * http://domain/uploads/xxx -> /uploads/xxx
 */
export function processHtmlForStorage(html: string): string {
    if (!html) return ''

    const apiBaseUrl = import.meta.env.VITE_API_BASE_URL || ''
    let apiHost = ''
    try {
        if (apiBaseUrl) {
            // 提取域名 (例如 localhost:8080 或 domain.com)
            apiHost = new URL(apiBaseUrl).host
        } else {
            // 如果未配置 API_BASE_URL，则认为是同源，使用当前域名
            apiHost = window.location.host
        }
    } catch (e) {
        // Fallback
        apiHost = window.location.host
    }

    // 匹配 src="value" 或 src='value'
    return html.replace(/src=["'](.*?)["']/g, (match, src) => {
        try {
            // 如果是完整 URL (http:// 或 https://)
            if (src.startsWith('http://') || src.startsWith('https://')) {
                const url = new URL(src)

                // 检查域名是否匹配 API 域名，且路径以 /uploads/ 开头
                // url.host 会自动处理端口号 (例如 localhost:8080)
                if (url.host === apiHost && url.pathname.startsWith('/uploads/')) {
                    // 返回相对路径，例如 src="/uploads/xxx.png"
                    // 始终标准化为双引号
                    return `src="${url.pathname}"`
                }
            }
        } catch (e) {
            // URL 解析失败，忽略，返回原样
        }
        return match
    })
}
