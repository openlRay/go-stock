const assert = require('node:assert/strict')
const {readFileSync} = require('node:fs')
const {join} = require('node:path')
const {test} = require('node:test')
const {runInNewContext} = require('node:vm')

function createBridge(fetch) {
  const downloads = []
  const revoked = []
  const timers = []
  const blobs = []
  const context = {
    window: {}, console, fetch,
    EventSource: class {},
    document: {
      createElement() {
        const link = {click() { downloads.push({filename: link.download, href: link.href}) }}
        return link
      },
    },
    URL: {
      createObjectURL(blob) { blobs.push(blob); return 'blob:test' },
      revokeObjectURL(url) { revoked.push(url) },
    },
    setTimeout(callback) { timers.push(callback) },
  }
  const source = readFileSync(join(__dirname, 'web-bridge.js'), 'utf8')
    .replace('export function installWebBridge', 'function installWebBridge')
  runInNewContext(source + '\ninstallWebBridge()', context)
  return {app: context.window.go.main.App, downloads, revoked, timers, blobs}
}

test('表格导出通过同源接口下载真实响应并释放 Blob', async () => {
  let request
  const workbook = new Blob(['PK workbook'])
  const bridge = createBridge(async (url, options) => {
    request = {url, options}
    return {ok: true, headers: new Headers({'X-Download-Filename': encodeURIComponent('选股.xlsx')}), blob: async () => workbook}
  })
  const table = {columns: [{key: 'code', title: '代码'}], rows: [{code: '000001'}]}
  assert.equal(await bridge.app.ExportTableToXLSX('选股.xlsx', table), '选股.xlsx')
  assert.equal(request.url, '/api/tables/export')
  assert.equal(request.options.method, 'POST')
  assert.deepEqual(JSON.parse(request.options.body), {filename: '选股.xlsx', table})
  assert.equal(bridge.blobs[0], workbook)
  assert.deepEqual(bridge.downloads, [{filename: '选股.xlsx', href: 'blob:test'}])
  bridge.timers.forEach(callback => callback())
  assert.deepEqual(bridge.revoked, ['blob:test'])
})

test('Excel 导出错误不会伪装成成功下载', async () => {
  const bridge = createBridge(async () => ({ok: false, json: async () => ({error: '数据量超限'})}))
  await assert.rejects(bridge.app.ExportTableToXLSX('选股.xlsx', {}), /数据量超限/)
  assert.equal(bridge.downloads.length, 0)
})

test('交易模板保持无参数调用并使用后端 XLSX 文件名', async () => {
  const bridge = createBridge(async url => {
    assert.equal(url, '/api/trading-records/template')
    return {ok: true, headers: new Headers({'X-Download-Filename': encodeURIComponent('交易记录导入模板.xlsx')}), blob: async () => new Blob(['PK'])}
  })
  assert.equal(await bridge.app.ExportTradingRecordTemplate(), '交易记录导入模板.xlsx')
  assert.equal(bridge.downloads.length, 1)
})
