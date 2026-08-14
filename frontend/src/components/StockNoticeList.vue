<script setup lang="ts">
import {computed, onBeforeMount, onBeforeUnmount, onMounted, ref} from 'vue'
import {
  AbortAnnouncementAIAnalysis,
  GetAiConfigs,
  GetAnnouncementAIAnalysis,
  GetStockList,
  StartAnnouncementAIAnalysis,
  StockNotice,
} from "../../wailsjs/go/main/App"
import {BrowserOpenURL, ClipboardSetText, EventsOn} from "../../wailsjs/runtime"
import {RefreshCircleSharp} from "@vicons/ionicons5"
import KLineChart from "./KLineChart.vue"
import MoneyTrend from "./moneyTrend.vue"
import {useMessage} from "naive-ui"
import {MdPreview} from "md-editor-v3"
import {data, models} from "../../wailsjs/go/models"

type NoticeCode = {
  stock_code: string
  short_name: string
  market_code: string
}

type NoticeColumn = {
  column_name: string
}

type NoticeRow = {
  art_code: string
  title: string
  notice_date: string
  display_time: string
  codes: NoticeCode[]
  columns: NoticeColumn[]
}

type AnnouncementEvent = {
  requestId: string
  artCode: string
  phase: 'preparing' | 'preflight' | 'streaming' | 'completed' | 'failed' | 'cancelled'
  message?: string
  delta?: string
  modelName?: string
  responseId?: string
  errorCode?: string
  preflight?: {
    estimatedInputTokens: number
    reservedOutputTokens: number
    estimatedTotalTokens: number
    contextWindow: number
    safeBudget: number
    capacitySource: string
    usedDefaultCapacity: boolean
    allowed: boolean
  }
  result?: models.AnnouncementAIAnalysis
}

const props = defineProps({
  stockCode: {
    type: String,
    default: '',
  },
  darkTheme: {
    type: Boolean,
    default: false,
  },
})

const list = ref<NoticeRow[]>([])
const options = ref<Array<{label: string, value: string}>>([])
const message = useMessage()
const modalVisible = ref(false)
const loadingSaved = ref(false)
const selectedNotice = ref<NoticeRow | null>(null)
const aiConfigs = ref<data.AIConfig[]>([])
const selectedAIConfigID = ref<number | null>(null)
const savedResult = ref<models.AnnouncementAIAnalysis | null>(null)
const streamingContent = ref('')
const activeRequestID = ref('')
const startPending = ref(false)
const analysisPhase = ref<'idle' | AnnouncementEvent['phase']>('idle')
const analysisMessage = ref('')
const analysisError = ref('')
const preflight = ref<AnnouncementEvent['preflight'] | null>(null)
let stopAnnouncementListener: null | (() => void) = null
let pendingStartEvents: AnnouncementEvent[] = []

const isActive = computed(() => ['preparing', 'preflight', 'streaming'].includes(analysisPhase.value))
const modalContent = computed(() => streamingContent.value || savedResult.value?.content || '')
const showSavedAlongsideDraft = computed(() => Boolean(
  savedResult.value?.content
  && streamingContent.value
  && analysisPhase.value !== 'completed',
))
const previewTheme = computed(() => props.darkTheme ? 'dark' : 'light')
const selectedModelName = computed(() => aiConfigs.value.find(item => item.ID === selectedAIConfigID.value)?.name || '')
const actionLabel = computed(() => savedResult.value ? '重新解读' : '开始解读')
const statusType = computed(() => {
  if (analysisPhase.value === 'failed') return 'error'
  if (analysisPhase.value === 'cancelled') return 'warning'
  if (analysisPhase.value === 'completed') return 'success'
  return 'info'
})
const statusLabel = computed(() => ({
  idle: savedResult.value ? '已加载最近一次成功解读' : '等待开始',
  preparing: '准备公告原文',
  preflight: '上下文容量预检',
  streaming: 'AI 正在生成',
  completed: '已完成并保存',
  failed: '解读失败',
  cancelled: '已取消',
}[analysisPhase.value]))

function getNotice(stockCodes: string) {
  StockNotice(stockCodes).then(result => {
    list.value = (result || []) as NoticeRow[]
  }).catch(error => {
    console.error('StockNotice error:', error)
    message.error('获取公司公告失败，请稍后重试')
  })
}

onBeforeMount(() => {
  getNotice(props.stockCode)
})

onMounted(() => {
  stopAnnouncementListener = EventsOn('announcementAIAnalysis', handleAnnouncementEvent)
})

onBeforeUnmount(() => {
  stopAnnouncementListener?.()
  stopAnnouncementListener = null
  if (activeRequestID.value) {
    AbortAnnouncementAIAnalysis(activeRequestID.value).catch(() => undefined)
  }
})

function findStockList(query: string) {
  if (query) {
    GetStockList(query).then(result => {
      options.value = result.map(item => ({
        label: item.name + " - " + item.ts_code,
        value: item.ts_code,
      }))
    })
  } else {
    getNotice('')
  }
}

function handleSearch(value: string) {
  getNotice(value)
}

function announcementURL(code: string) {
  return `https://pdf.dfcfw.com/pdf/H2_${encodeURIComponent(code)}_1.pdf`
}

function openWin(code: string) {
  BrowserOpenURL(announcementURL(code))
}

function getTypeColor(name = '') {
  if (name.includes('质押') || name.includes('冻结') || name.includes('解冻') || name.includes('解押') || name.includes('解禁')) return 'error'
  if (name.includes('异常') || name.includes('减持') || name.includes('增发') || name.includes('重大')) return 'error'
  if (name.includes('季度报告') || name.includes('年度报告') || name.includes('澄清公告') || name.includes('风险')) return 'error'
  if (name.includes('终止') || name.includes('复牌') || name.includes('停牌') || name.includes('退市')) return 'error'
  if (name.includes('破产') || name.includes('清算')) return 'error'
  if (name.includes('回购') || name.includes('重组') || name.includes('诉讼') || name.includes('仲裁') || name.includes('转让') || name.includes('收购')) return 'warning'
  if (name.includes('调研') || name.includes('募集')) return 'warning'
  return 'info'
}

function getmMarketCode(market: string, code: string) {
  if (market === '0') return 'sz' + code
  if (market === '1') return 'sh' + code
  if (market === '2') return 'bj' + code
  if (market === '3') return 'hk' + code
  return code
}

function noticeType(item: NoticeRow) {
  return item.columns?.[0]?.column_name || '公司公告'
}

function noticeStock(item: NoticeRow) {
  return item.codes?.[0] || {stock_code: '', short_name: '', market_code: ''}
}

function formatDate(value: unknown) {
  if (!value) return ''
  const date = new Date(String(value))
  return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString('zh-CN', {hour12: false})
}

function formatNumber(value?: number) {
  return new Intl.NumberFormat('zh-CN').format(value || 0)
}

async function loadAIConfigs() {
  try {
    aiConfigs.value = (await GetAiConfigs()) || []
    const remembered = Number(localStorage.getItem('announcementAIConfigId') || 0)
    if (aiConfigs.value.some(item => item.ID === remembered)) {
      selectedAIConfigID.value = remembered
    } else {
      selectedAIConfigID.value = aiConfigs.value[0]?.ID || null
    }
  } catch (error) {
    console.error('GetAiConfigs error:', error)
    aiConfigs.value = []
    selectedAIConfigID.value = null
  }
}

async function openAIAnalysis(item: NoticeRow) {
  if (activeRequestID.value) {
    await AbortAnnouncementAIAnalysis(activeRequestID.value).catch(() => undefined)
  }
  startPending.value = false
  pendingStartEvents = []
  selectedNotice.value = item
  modalVisible.value = true
  loadingSaved.value = true
  savedResult.value = null
  streamingContent.value = ''
  activeRequestID.value = ''
  startPending.value = false
  pendingStartEvents = []
  analysisPhase.value = 'idle'
  analysisMessage.value = ''
  analysisError.value = ''
  preflight.value = null
  try {
    const [result] = await Promise.all([
      GetAnnouncementAIAnalysis(item.art_code),
      loadAIConfigs(),
    ])
    if (selectedNotice.value?.art_code !== item.art_code) return
    savedResult.value = result?.ID ? result : null
    analysisMessage.value = savedResult.value ? '已加载最近一次成功解读，可直接查看或重新解读。' : '暂无已保存结果，选择模型后开始解读。'
  } catch (error: any) {
    console.error('GetAnnouncementAIAnalysis error:', error)
    analysisError.value = error?.message || '读取已保存的公告解读失败'
  } finally {
    if (selectedNotice.value?.art_code === item.art_code) loadingSaved.value = false
  }
}

function handleAnnouncementEvent(payload: AnnouncementEvent) {
  if (!payload || payload.artCode !== selectedNotice.value?.art_code) return
  if (!activeRequestID.value && startPending.value) {
    pendingStartEvents.push(payload)
    return
  }
  if (payload.requestId !== activeRequestID.value) return
  applyAnnouncementEvent(payload)
}

function applyAnnouncementEvent(payload: AnnouncementEvent) {
  analysisPhase.value = payload.phase
  analysisMessage.value = payload.message || ''
  if (payload.preflight) preflight.value = payload.preflight
  switch (payload.phase) {
    case 'preparing':
    case 'preflight':
      break
    case 'streaming':
      streamingContent.value += payload.delta || ''
      break
    case 'completed':
      if (payload.result) savedResult.value = new models.AnnouncementAIAnalysis(payload.result)
      streamingContent.value = savedResult.value?.content || streamingContent.value
      activeRequestID.value = ''
      startPending.value = false
      analysisError.value = ''
      message.success('公告 AI 解读已完成并保存')
      break
    case 'failed':
      activeRequestID.value = ''
      startPending.value = false
      analysisError.value = payload.message || '公告 AI 解读失败'
      if (payload.errorCode !== 'save_failed') streamingContent.value = ''
      message.error(analysisError.value)
      break
    case 'cancelled':
      activeRequestID.value = ''
      startPending.value = false
      streamingContent.value = ''
      analysisError.value = ''
      message.info('已取消本次公告解读')
      break
  }
}

async function startAnalysis() {
  const item = selectedNotice.value
  if (!item) return
  if (!selectedAIConfigID.value) {
    message.warning('暂无可用 AI 模型，请先在设置中完成模型配置')
    return
  }
  localStorage.setItem('announcementAIConfigId', String(selectedAIConfigID.value))
  analysisError.value = ''
  analysisMessage.value = '正在启动公告解读…'
  analysisPhase.value = 'preparing'
  streamingContent.value = ''
  preflight.value = null
  startPending.value = true
  pendingStartEvents = []
  try {
    const startingArtCode = item.art_code
    const requestID = await StartAnnouncementAIAnalysis(new models.AnnouncementAIAnalysisRequest({
      artCode: item.art_code,
      stockCode: noticeStock(item).stock_code,
      stockName: noticeStock(item).short_name,
      title: item.title,
      noticeType: noticeType(item),
      noticeDate: item.notice_date?.substring(0, 10) || '',
      aiConfigId: selectedAIConfigID.value,
    }))
    if (selectedNotice.value?.art_code !== startingArtCode || !modalVisible.value) {
      startPending.value = false
      pendingStartEvents = []
      await AbortAnnouncementAIAnalysis(requestID).catch(() => undefined)
      return
    }
    activeRequestID.value = requestID
    startPending.value = false
    const queuedEvents = pendingStartEvents.filter(event => event.requestId === requestID)
    pendingStartEvents = []
    queuedEvents.forEach(applyAnnouncementEvent)
    if (!modalVisible.value && activeRequestID.value === requestID) {
      await AbortAnnouncementAIAnalysis(requestID).catch(() => undefined)
    }
  } catch (error: any) {
    activeRequestID.value = ''
    startPending.value = false
    pendingStartEvents = []
    analysisPhase.value = 'failed'
    analysisError.value = error?.message || '无法启动公告 AI 解读'
    message.error(analysisError.value)
  }
}

async function cancelAnalysis() {
  if (!activeRequestID.value) return
  await AbortAnnouncementAIAnalysis(activeRequestID.value).catch(error => console.error('AbortAnnouncementAIAnalysis error:', error))
}

function handleModalVisibility(show: boolean) {
  if (!show && activeRequestID.value) cancelAnalysis()
}

async function copyResult() {
  const content = modalContent.value.trim()
  if (!content) return
  const copied = await ClipboardSetText(content).catch(() => false)
  if (copied) message.success('完整解读已复制')
  else message.warning('复制失败，请手动选择文本')
}
</script>

<template>
  <n-card>
    <n-auto-complete
      :options="options"
      placeholder="请输入A股名称或者代码"
      clearable
      filterable
      :on-select="handleSearch"
      :on-update:value="findStockList"
    />
  </n-card>
  <n-table striped size="small">
    <n-thead>
      <n-tr>
        <n-th>股票代码</n-th>
        <n-th>股票名称</n-th>
        <n-th>公告标题</n-th>
        <n-th>公告类型</n-th>
        <n-th>公告日期</n-th>
        <n-th>操作</n-th>
        <n-th>
          <n-flex>数据更新时间<n-icon @click="getNotice('')" color="#409EFF" :size="20" :component="RefreshCircleSharp"/></n-flex>
        </n-th>
      </n-tr>
    </n-thead>
    <n-tbody>
      <n-tr v-for="item in list" :key="item.art_code">
        <n-td>
          <n-popover trigger="hover" placement="right">
            <template #trigger>
              <n-tag type="info" :bordered="false">{{ noticeStock(item).stock_code }}</n-tag>
            </template>
            <money-trend
              style="width: 800px"
              :code="getmMarketCode(noticeStock(item).market_code, noticeStock(item).stock_code)"
              :name="noticeStock(item).short_name"
              :days="360"
              :dark-theme="true"
              :chart-height="500"
            />
          </n-popover>
        </n-td>
        <n-td>
          <n-popover trigger="hover" placement="right">
            <template #trigger>
              <n-tag type="info" :bordered="false">{{ noticeStock(item).short_name }}</n-tag>
            </template>
            <k-line-chart
              style="width: 800px"
              :code="getmMarketCode(noticeStock(item).market_code, noticeStock(item).stock_code)"
              :chart-height="500"
              :stock-name="noticeStock(item).short_name"
              :k-days="20"
              :dark-theme="true"
            />
          </n-popover>
        </n-td>
        <n-td>
          <n-a type="info" @click="openWin(item.art_code)">
            <n-text :type="getTypeColor(noticeType(item))">{{ item.title }}</n-text>
          </n-a>
        </n-td>
        <n-td><n-text :type="getTypeColor(noticeType(item))">{{ noticeType(item) }}</n-text></n-td>
        <n-td><n-tag type="info">{{ item.notice_date?.substring(0, 10) }}</n-tag></n-td>
        <n-td><n-button size="tiny" type="primary" secondary @click="openAIAnalysis(item)">AI 解读</n-button></n-td>
        <n-td><n-tag type="info">{{ item.display_time?.substring(0, 19) }}</n-tag></n-td>
      </n-tr>
    </n-tbody>
  </n-table>

  <n-modal
    v-model:show="modalVisible"
    @update:show="handleModalVisibility"
    preset="card"
    transform-origin="center"
    :title="selectedNotice ? `AI 解读：${selectedNotice.title}` : '公告 AI 解读'"
    style="width: min(1000px, calc(100vw - 32px));"
  >
    <n-spin :show="loadingSaved">
      <n-flex vertical :size="12">
        <n-flex justify="space-between" align="center">
          <n-flex align="center">
            <n-tag :type="statusType" round :bordered="false">{{ statusLabel }}</n-tag>
            <n-text depth="3">{{ analysisMessage }}</n-text>
          </n-flex>
          <n-button size="small" secondary @click="selectedNotice && openWin(selectedNotice.art_code)">打开原公告</n-button>
        </n-flex>

        <n-alert v-if="analysisError" type="error" :show-icon="true">{{ analysisError }}</n-alert>
        <n-alert v-if="!aiConfigs.length && !loadingSaved" type="warning" :show-icon="true">
          暂无可用 AI 模型配置，请先在“设置 → AI 模型配置”中新增并保存模型。
        </n-alert>

        <n-card v-if="preflight" size="small" title="上下文容量预检">
          <n-flex :size="8" align="center">
            <n-tag :type="preflight.allowed ? 'success' : 'error'">预计 {{ formatNumber(preflight.estimatedTotalTokens) }} Token</n-tag>
            <n-tag type="info">容量 {{ formatNumber(preflight.contextWindow) }} Token</n-tag>
            <n-tag type="info">安全预算 {{ formatNumber(preflight.safeBudget) }} Token</n-tag>
            <n-tag :type="preflight.usedDefaultCapacity ? 'warning' : 'default'">{{ preflight.capacitySource }}</n-tag>
          </n-flex>
        </n-card>

        <n-flex v-if="showSavedAlongsideDraft" vertical :size="12">
          <n-card size="small" title="最近一次已保存解读">
            <div class="announcement-result announcement-result-split">
              <MdPreview :model-value="savedResult?.content || ''" :theme="previewTheme"/>
            </div>
          </n-card>
          <n-card size="small" title="本次生成（尚未保存）">
            <div class="announcement-result announcement-result-split">
              <MdPreview :model-value="streamingContent" :theme="previewTheme"/>
            </div>
          </n-card>
        </n-flex>
        <div v-else class="announcement-result">
          <MdPreview v-if="modalContent" :model-value="modalContent" :theme="previewTheme"/>
          <n-empty v-else description="暂无公告解读结果"/>
        </div>

        <n-flex v-if="savedResult" justify="space-between">
          <n-text depth="3">模型：{{ savedResult.modelName }} · 生成时间：{{ formatDate(savedResult.generatedAt || savedResult.CreatedAt) }}</n-text>
          <n-text depth="3">提示词版本：{{ savedResult.promptVersion }}</n-text>
        </n-flex>
        <n-alert type="warning" :show-icon="true">
          以上解读仅基于本公告原文，由 AI 生成，仅供信息参考，不构成投资建议。请打开原公告核验，投资需谨慎，风险自担。
        </n-alert>
      </n-flex>
    </n-spin>

    <template #action>
      <n-flex justify="space-between" align="center">
        <n-select
          v-model:value="selectedAIConfigID"
          :options="aiConfigs.map(item => ({label: `${item.name} · ${item.modelName}`, value: item.ID}))"
          :disabled="isActive"
          placeholder="选择 AI 模型"
          style="min-width: 260px; max-width: 420px;"
        />
        <n-flex>
          <n-button :disabled="!modalContent" @click="copyResult">复制完整解读</n-button>
          <n-button v-if="isActive" type="warning" @click="cancelAnalysis">取消生成</n-button>
          <n-button v-else type="primary" :disabled="!selectedAIConfigID || loadingSaved" @click="startAnalysis">
            {{ actionLabel }}<template v-if="selectedModelName"> · {{ selectedModelName }}</template>
          </n-button>
        </n-flex>
      </n-flex>
    </template>
  </n-modal>
</template>

<style scoped>
.announcement-result {
  min-height: 280px;
  max-height: 58vh;
  overflow-y: auto;
  border: 1px solid var(--n-border-color, rgba(128, 128, 128, 0.25));
  border-radius: 6px;
  padding: 8px;
}

.announcement-result-split {
  min-height: 160px;
  max-height: 34vh;
}
</style>
