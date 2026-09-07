const listeners = new Map()
const selectedWebFiles = new Map()
const selectedWebFileTokensBySignature = new Map()
let selectedWebFileSequence = 0

function dispatchEvent(name, data = []) {
  const entries = listeners.get(name)
  if (!entries?.length) return

  const remaining = []
  for (const entry of entries) {
    try {
      entry.callback(...data)
    } catch (error) {
      console.error(`[web event] ${name}`, error)
    }
    if (entry.remaining < 0) {
      remaining.push(entry)
    } else if (entry.remaining > 1) {
      remaining.push({...entry, remaining: entry.remaining - 1})
    }
  }
  if (remaining.length) listeners.set(name, remaining)
  else listeners.delete(name)
}

function eventsOnMultiple(name, callback, maxCallbacks) {
  const entries = listeners.get(name) || []
  entries.push({callback, remaining: maxCallbacks})
  listeners.set(name, entries)
  return () => {
    const current = listeners.get(name) || []
    const next = current.filter(entry => entry.callback !== callback)
    if (next.length) listeners.set(name, next)
    else listeners.delete(name)
  }
}

async function callRPC(method, args) {
  const response = await fetch(`/api/rpc/${encodeURIComponent(method)}`, {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({args}),
  })
  const payload = await response.json().catch(() => ({error: `HTTP ${response.status}`}))
  if (!response.ok || payload.error) {
    throw new Error(payload.error || `调用 ${method} 失败`)
  }
  return payload.result
}

function downloadBlob(filename, blob) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
  return filename
}

function decodeBase64(value) {
  const encoded = String(value || '').replace(/^data:[^,]+,/, '').replace(/\s/g, '')
  const binary = atob(encoded)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index)
  }
  return bytes
}

function selectFiles(accept, multiple = false) {
  return new Promise(resolve => {
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = accept
    input.multiple = multiple
    input.onchange = () => resolve(Array.from(input.files || []))
    input.oncancel = () => resolve([])
    input.click()
  })
}

async function selectFile(accept) {
  const files = await selectFiles(accept)
  return files[0] || null
}

function registerWebFile(file) {
  const signature = `${file.name}\u0000${file.size}\u0000${file.lastModified}`
  const existingToken = selectedWebFileTokensBySignature.get(signature)
  if (existingToken && selectedWebFiles.has(existingToken)) return existingToken

  selectedWebFileSequence += 1
  const safeName = String(file.name || 'file').replace(/[\\/]/g, '_')
  const token = `web-upload://${Date.now().toString(36)}-${selectedWebFileSequence}/${safeName}`
  selectedWebFiles.set(token, {file, signature})
  selectedWebFileTokensBySignature.set(signature, token)
  return token
}

function resolveWebFiles(tokens) {
  const normalizedTokens = Array.isArray(tokens) ? tokens : [tokens]
  return normalizedTokens.map(token => {
    const entry = selectedWebFiles.get(token)
    if (!entry) throw new Error('浏览器文件选择已失效，请重新选择文件')
    return {token, ...entry}
  })
}

function consumeWebFiles(entries) {
  for (const entry of entries) {
    selectedWebFiles.delete(entry.token)
    if (selectedWebFileTokensBySignature.get(entry.signature) === entry.token) {
      selectedWebFileTokensBySignature.delete(entry.signature)
    }
  }
}

async function uploadWebForm(path, formData, fallbackMessage) {
  const response = await fetch(path, {method: 'POST', body: formData})
  const payload = await response.json().catch(() => ({error: `HTTP ${response.status}`}))
  if (!response.ok || payload.error) {
    throw new Error(payload.error || fallbackMessage)
  }
  return payload.result
}

function installRuntime() {
  const noop = () => undefined
  const falsePromise = () => Promise.resolve(false)

  window.runtime = {
    LogPrint: console.log,
    LogTrace: console.trace,
    LogDebug: console.debug,
    LogInfo: console.info,
    LogWarning: console.warn,
    LogError: console.error,
    LogFatal: console.error,
    EventsOnMultiple: eventsOnMultiple,
    EventsOff(name, ...additionalNames) {
      for (const eventName of [name, ...additionalNames]) listeners.delete(eventName)
    },
    EventsOffAll() {
      listeners.clear()
    },
    EventsEmit(name, ...data) {
      dispatchEvent(name, data)
    },
    WindowReload: () => window.location.reload(),
    WindowReloadApp: () => window.location.reload(),
    WindowSetAlwaysOnTop: noop,
    WindowSetSystemDefaultTheme: noop,
    WindowSetLightTheme: noop,
    WindowSetDarkTheme: noop,
    WindowCenter: noop,
    WindowSetTitle: title => {
      document.title = title
    },
    WindowFullscreen: () => document.documentElement.requestFullscreen?.(),
    WindowUnfullscreen: () => document.exitFullscreen?.(),
    WindowIsFullscreen: () => Promise.resolve(Boolean(document.fullscreenElement)),
    WindowGetSize: () => Promise.resolve({w: window.innerWidth, h: window.innerHeight}),
    WindowSetSize: noop,
    WindowSetMaxSize: noop,
    WindowSetMinSize: noop,
    WindowSetPosition: noop,
    WindowGetPosition: () => Promise.resolve({x: window.screenX, y: window.screenY}),
    WindowHide: noop,
    WindowShow: noop,
    WindowMaximise: noop,
    WindowToggleMaximise: noop,
    WindowUnmaximise: noop,
    WindowIsMaximised: falsePromise,
    WindowMinimise: noop,
    WindowUnminimise: noop,
    WindowSetBackgroundColour: noop,
    ScreenGetAll: () => Promise.resolve([{
      isCurrent: true,
      isPrimary: true,
      width: window.screen.width,
      height: window.screen.height,
    }]),
    WindowIsMinimised: falsePromise,
    WindowIsNormal: () => Promise.resolve(true),
    BrowserOpenURL: url => window.open(url, '_blank', 'noopener,noreferrer'),
    Environment: () => Promise.resolve({buildType: 'web', platform: 'web', arch: 'web'}),
    Quit: noop,
    Hide: noop,
    Show: noop,
    ClipboardGetText: () => navigator.clipboard?.readText?.() || Promise.resolve(''),
    ClipboardSetText: async text => {
      if (!navigator.clipboard?.writeText) return false
      try {
        await navigator.clipboard.writeText(text)
        return true
      } catch {
        return false
      }
    },
    OnFileDrop: noop,
    OnFileDropOff: noop,
    CanResolveFilePaths: () => false,
    ResolveFilePaths: () => [],
  }
}

function installAppProxy() {
  const specialMethods = {
    OpenURL(url) {
      window.open(url, '_blank', 'noopener,noreferrer')
      return Promise.resolve()
    },
    QuitApp: () => Promise.resolve(),
    RestartAsAdmin: () => Promise.resolve(false),
    SaveImage(name, base64Data) {
      const filename = `${name || 'go-stock'}AI分析.png`
      return Promise.resolve(downloadBlob(filename, new Blob([decodeBase64(base64Data)], {type: 'image/png'})))
    },
    SaveWordFile(filename, base64Data) {
      return Promise.resolve(downloadBlob(filename || 'go-stock.docx', new Blob([decodeBase64(base64Data)], {
        type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
      })))
    },
    async ExportConfig() {
      const config = await callRPC('GetConfig', [])
      downloadBlob('config.json', new Blob([JSON.stringify(config, null, 2)], {type: 'application/json'}))
      return '导出成功: config.json'
    },
    async ImportSkillPackage() {
      const file = await selectFile('.zip,application/zip')
      if (!file) return '未选择文件'
      const formData = new FormData()
      formData.append('file', file, file.name)
      return uploadWebForm('/api/skills/import', formData, '导入技能包失败')
    },
    async ImportTradingRecordsFromExcel() {
      const file = await selectFile('.xls,.xlsx,.txt,.csv,text/plain,text/csv')
      if (!file) return null
      const formData = new FormData()
      formData.append('file', file, file.name)
      return uploadWebForm('/api/trading-records/import', formData, '导入交易记录失败')
    },
    async ExportTradingRecordTemplate() {
      const response = await fetch('/api/trading-records/template')
      if (!response.ok) throw new Error('下载交易记录模板失败')
      const filename = response.headers.get('X-Download-Filename')
      if (!filename) throw new Error('交易记录模板响应缺少文件名')
      return downloadBlob(decodeURIComponent(filename), await response.blob())
    },
    async ExportTableToXLSX(filename, table) {
      const response = await fetch('/api/tables/export', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({filename, table}),
      })
      if (!response.ok) {
        const payload = await response.json().catch(() => ({}))
        throw new Error(payload.error || '导出 Excel 失败')
      }
      const downloadName = response.headers.get('X-Download-Filename')
      if (!downloadName) throw new Error('Excel 响应缺少文件名')
      return downloadBlob(decodeURIComponent(downloadName), await response.blob())
    },
    async PickKBFilePath() {
      const file = await selectFile('.txt,.md,.markdown,text/plain,text/markdown')
      return file ? registerWebFile(file) : ''
    },
    async PickKBFilePaths() {
      const files = await selectFiles('.txt,.md,.markdown,text/plain,text/markdown', true)
      return files.map(registerWebFile)
    },
    async UploadKBFile(kbName, token) {
      const entries = resolveWebFiles(token)
      const formData = new FormData()
      formData.append('kbName', kbName)
      formData.append('file', entries[0].file, entries[0].file.name)
      const result = await uploadWebForm('/api/knowledge-base/file/import', formData, '导入知识库文件失败')
      consumeWebFiles(entries)
      return result
    },
    async UploadKBFiles(kbName, tokens) {
      const entries = resolveWebFiles(tokens)
      if (entries.length === 0) throw new Error('请选择知识库文件')
      const formData = new FormData()
      formData.append('kbName', kbName)
      for (const entry of entries) {
        formData.append('files', entry.file, entry.file.name)
      }
      const result = await uploadWebForm('/api/knowledge-base/files/import', formData, '启动知识库文件导入失败')
      consumeWebFiles(entries)
      return result
    },
    async SaveAsMarkdown(stockCode, stockName) {
      const result = await callRPC('GetAIResponseResult', [stockCode])
      if (!result?.content && !result?.Content) return '分析结果异常,无法保存。'
      const content = result.content || result.Content
      const filename = `${stockName || stockCode || 'go-stock'}AI分析结果.md`
      downloadBlob(filename, new Blob([content], {type: 'text/markdown;charset=utf-8'}))
      return `已下载：${filename}`
    },
  }

  const appProxy = new Proxy({}, {
    get(_target, method) {
      if (typeof method !== 'string') return undefined
      if (specialMethods[method]) return specialMethods[method]
      return (...args) => callRPC(method, args)
    },
  })
  window.go = window.go || {}
  window.go.main = window.go.main || {}
  window.go.main.App = appProxy
}

function connectEventStream() {
  const eventSource = new EventSource('/api/events')
  eventSource.onmessage = event => {
    try {
      const payload = JSON.parse(event.data)
      dispatchEvent(payload.name, Array.isArray(payload.data) ? payload.data : [])
    } catch (error) {
      console.error('[web event] 消息解析失败', error)
    }
  }
  eventSource.onerror = () => {
    // EventSource 会按浏览器退避策略自动重连。
  }
}

function installBrowserNotifications() {
  eventsOnMultiple('browserNotification', payload => {
    if (!('Notification' in window) || Notification.permission !== 'granted') return
    new Notification(payload?.title || 'go-stock', {body: payload?.content || ''})
  }, -1)
}

export function installWebBridge() {
  if (window.go?.main?.App && window.runtime) return false
  installRuntime()
  installAppProxy()
  installBrowserNotifications()
  connectEventStream()
  return true
}
