/**
 * Web / Desktop 运行时环境标记。
 * 由 main.js 在 installWebBridge 后同步设置，组件可直接导入使用。
 */
export let isWebMode = false

export function setWebMode(enabled) {
  isWebMode = !!enabled
  if (typeof window !== 'undefined') {
    window.__GO_STOCK_IS_WEB__ = isWebMode
  }
}
