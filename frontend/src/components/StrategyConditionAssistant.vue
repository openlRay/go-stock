<template>
  <div class="strategy-assistant">
    <n-button
      v-if="!expanded"
      type="primary"
      secondary
      class="strategy-assistant__toggle"
      @click="expanded = true"
    >
      <span class="strategy-assistant__plus" aria-hidden="true">＋</span>
      AI 帮我生成
    </n-button>

    <section v-else class="strategy-assistant__panel" aria-label="AI 选股助手">
      <header class="strategy-assistant__header">
        <div>
          <div class="strategy-assistant__title">
            <span class="strategy-assistant__plus" aria-hidden="true">＋</span>
            AI 选股助手
          </div>
          <p>AI 建议独立预览，应用前不会修改当前条件。</p>
        </div>
        <n-button text :disabled="loading" @click="expanded = false">收起</n-button>
      </header>

      <div class="strategy-assistant__field strategy-assistant__model-field">
        <label>对话模型</label>
        <n-select
          v-model:value="selectedAIConfigID"
          :options="aiConfigOptions"
          :disabled="loading || !aiConfigOptions.length"
          placeholder="请选择对话模型"
          filterable
        />
      </div>

      <n-alert v-if="configLoadError" type="error" :bordered="false" class="strategy-assistant__alert">
        {{ configLoadError }}
      </n-alert>
      <n-alert v-else-if="!aiConfigOptions.length" type="warning" :bordered="false" class="strategy-assistant__alert">
        尚未配置可用的对话模型，请先前往“AI 模型服务配置”添加模型。手工编辑和保存策略仍可正常使用。
      </n-alert>

      <div class="strategy-assistant__field">
        <label>{{ hasSuggestion ? '告诉 AI 如何继续调整' : '告诉 AI 你想怎么选' }}</label>
        <div class="strategy-assistant__input-row">
          <n-input
            v-model:value="instruction"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 5 }"
            maxlength="2000"
            show-count
            :disabled="loading"
            :placeholder="instructionPlaceholder"
          />
          <n-button
            v-if="loading"
            class="strategy-assistant__generate"
            @click="cancelGeneration"
          >
            取消生成
          </n-button>
          <n-button
            v-else
            type="primary"
            class="strategy-assistant__generate"
            :disabled="!canGenerate"
            @click="generateSuggestion"
          >
            {{ hasSuggestion ? '继续调整' : '生成建议' }}
          </n-button>
        </div>
      </div>

      <div class="strategy-assistant__examples" aria-label="常用示例">
        <button
          v-for="example in examples"
          :key="example.label"
          type="button"
          :disabled="loading"
          @click="instruction = example.instruction"
        >
          {{ example.label }}
        </button>
      </div>

      <n-alert v-if="generationError" type="error" :bordered="false" class="strategy-assistant__alert">
        {{ generationError }}
      </n-alert>

      <div v-if="hasSuggestion" class="strategy-assistant__draft">
        <div class="strategy-assistant__draft-header">
          <strong>AI 建议稿</strong>
          <n-tag :type="isApplied ? 'success' : 'warning'" size="small" :bordered="false">
            {{ isApplied ? '已应用，未保存' : '未应用' }}
          </n-tag>
        </div>
        <n-input
          v-model:value="suggestion"
          type="textarea"
          :autosize="{ minRows: 3, maxRows: 8 }"
          maxlength="2000"
          show-count
          :disabled="loading"
          placeholder="AI 建议会显示在这里，你也可以直接编辑"
        />
        <p v-if="summary" class="strategy-assistant__summary">✓ {{ summary }}</p>
        <ul v-if="warnings.length" class="strategy-assistant__warnings">
          <li v-for="warning in warnings" :key="warning">{{ warning }}</li>
        </ul>
        <p class="strategy-assistant__execution-tip">△ 请在执行选股后核对上游实际识别条件</p>

        <div class="strategy-assistant__actions">
          <n-button :disabled="loading" @click="discardSuggestion">
            {{ isApplied ? '清除建议记录' : '放弃建议' }}
          </n-button>
          <n-button
            type="primary"
            :disabled="loading || isApplied || !suggestion.trim()"
            @click="applySuggestion"
          >
            {{ isApplied ? '已应用到选股条件' : '应用到选股条件' }}
          </n-button>
        </div>
      </div>
      <div v-else class="strategy-assistant__empty">
        生成结果会先作为独立建议稿显示在这里，不会覆盖上方当前条件。
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useMessage } from 'naive-ui'
import { EventsOn } from '../../wailsjs/runtime'
import {
  AbortStrategyConditionGeneration,
  GenerateStrategyCondition,
  GetAiConfigs
} from '../../wailsjs/go/main/App'

type AIConfigOptionSource = {
  ID: number
  name?: string
  modelName?: string
  modelType?: string
  isDefault?: boolean
}

const props = withDefaults(defineProps<{
  currentQuery?: string
  active?: boolean
  sessionKey?: string | number
}>(), {
  currentQuery: '',
  active: false,
  sessionKey: 0
})

const emit = defineEmits<{
  apply: [query: string]
}>()

const message = useMessage()
const expanded = ref(false)
const instruction = ref('')
const suggestion = ref('')
const summary = ref('')
const warnings = ref<string[]>([])
const selectedAIConfigID = ref<number | null>(null)
const aiConfigOptions = ref<Array<{ label: string, value: number }>>([])
const configLoadError = ref('')
const generationError = ref('')
const loading = ref(false)
const activeRequestID = ref('')
const lastAppliedSuggestion = ref('')
let latestRequestSequence = 0
let fallbackRequestSequence = 0
let stopAIConfigsChangedListener: () => void = () => undefined

const examples = [
  { label: '稳健低估值', instruction: '生成一组稳健低估值条件，兼顾盈利质量，并排除 ST 股和退市股' },
  { label: '短线放量突破', instruction: '生成短线放量突破条件，包含成交量和价格趋势要求，不推荐具体股票' },
  { label: '排除高风险板块', instruction: '在当前条件基础上，排除 ST 股、退市股、科创板和创业板' }
]

const hasSuggestion = computed(() => Boolean(suggestion.value.trim()))
const canGenerate = computed(() => Boolean(
  instruction.value.trim()
  && selectedAIConfigID.value
  && aiConfigOptions.value.length
))
const instructionPlaceholder = computed(() => hasSuggestion.value
  ? '例如：再增加量比大于 1.2，并排除创业板'
  : '例如：寻找近 20 日走强、当日放量且排除高风险板块的股票')
const isApplied = computed(() => Boolean(
  suggestion.value.trim()
  && suggestion.value.trim() === lastAppliedSuggestion.value
  && props.currentQuery.trim() === lastAppliedSuggestion.value
))

function readableError(error: any) {
  return error?.message || String(error || '未知错误')
}

function createRequestID() {
  const randomUUID = globalThis.crypto?.randomUUID?.bind(globalThis.crypto)
  if (randomUUID) return `strategy-condition-${randomUUID()}`
  fallbackRequestSequence += 1
  return `strategy-condition-${Date.now()}-${fallbackRequestSequence}`
}

async function loadAIConfigs() {
  try {
    const configs = ((await GetAiConfigs()) || []).filter((config: AIConfigOptionSource) => {
      const modelType = String(config.modelType || '').toLowerCase()
      return !modelType || modelType === 'chat'
    }) as AIConfigOptionSource[]
    aiConfigOptions.value = configs.map(config => ({
      label: `${config.name || '未命名配置'}[${config.modelName || '未指定模型'}]${config.isDefault ? ' · 默认' : ''}`,
      value: Number(config.ID)
    }))
    const validIDs = new Set(aiConfigOptions.value.map(option => option.value))
    if (!validIDs.has(Number(selectedAIConfigID.value))) {
      selectedAIConfigID.value = Number((configs.find(config => config.isDefault) || configs[0])?.ID) || null
    }
    configLoadError.value = ''
  } catch (error) {
    aiConfigOptions.value = []
    selectedAIConfigID.value = null
    configLoadError.value = '加载 AI 模型配置失败，请稍后重试；手工编辑和保存不受影响。'
  }
}

async function generateSuggestion() {
  if (!instruction.value.trim()) {
    message.warning('请先告诉 AI 你想怎么选或如何调整')
    return
  }
  if (!selectedAIConfigID.value) {
    message.warning('暂无可用的对话模型，请先完成 AI 模型服务配置')
    return
  }

  if (activeRequestID.value) {
    await AbortStrategyConditionGeneration(activeRequestID.value).catch(() => undefined)
  }
  const requestID = createRequestID()
  const requestSequence = ++latestRequestSequence
  const currentQuery = suggestion.value.trim() || props.currentQuery.trim()
  activeRequestID.value = requestID
  loading.value = true
  generationError.value = ''

  try {
    const result = await GenerateStrategyCondition({
      requestId: requestID,
      instruction: instruction.value.trim(),
      currentQuery,
      aiConfigId: selectedAIConfigID.value
    })
    // 关闭、切换策略或发起更新请求后，旧响应不能覆盖新的建议稿。
    if (requestSequence !== latestRequestSequence || activeRequestID.value !== requestID || !props.active) return
    if (!result?.query?.trim()) throw new Error('AI 未返回可用的选股条件')
    suggestion.value = result.query.trim()
    summary.value = result.summary?.trim() || ''
    warnings.value = Array.isArray(result.warnings) ? result.warnings.filter(Boolean) : []
    instruction.value = ''
    message.success('AI 建议已生成，确认应用后才会修改当前条件')
  } catch (error) {
    if (requestSequence !== latestRequestSequence || activeRequestID.value !== requestID || !props.active) return
    generationError.value = readableError(error)
  } finally {
    if (requestSequence === latestRequestSequence && activeRequestID.value === requestID) {
      activeRequestID.value = ''
      loading.value = false
    }
  }
}

async function cancelGeneration() {
  const requestID = activeRequestID.value
  if (!requestID) return
  // 先让本地 gate 失效，再调用取消 RPC，避免 reject 回调显示成普通失败。
  latestRequestSequence += 1
  activeRequestID.value = ''
  loading.value = false
  await AbortStrategyConditionGeneration(requestID).catch(() => undefined)
  message.info('已取消本次生成，已有建议和当前条件均未改变')
}

function discardSuggestion() {
  suggestion.value = ''
  summary.value = ''
  warnings.value = []
  instruction.value = ''
  lastAppliedSuggestion.value = ''
  generationError.value = ''
}

function applySuggestion() {
  const query = suggestion.value.trim()
  if (!query) return
  lastAppliedSuggestion.value = query
  emit('apply', query)
}

function resetSession() {
  const requestID = activeRequestID.value
  latestRequestSequence += 1
  activeRequestID.value = ''
  loading.value = false
  expanded.value = false
  discardSuggestion()
  if (requestID) AbortStrategyConditionGeneration(requestID).catch(() => undefined)
}

watch(() => props.active, active => {
  if (!active) resetSession()
})

watch(() => props.sessionKey, () => resetSession())

onMounted(async () => {
  stopAIConfigsChangedListener = EventsOn('aiConfigsChanged', loadAIConfigs)
  await loadAIConfigs()
})

onBeforeUnmount(() => {
  resetSession()
  stopAIConfigsChangedListener()
})
</script>

<style scoped>
.strategy-assistant { width: 100%; }

.strategy-assistant__toggle { margin-top: 2px; }

.strategy-assistant__plus {
  width: 22px;
  height: 22px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  margin-right: 7px;
  color: #fff;
  background: #18a058;
  border-radius: 50%;
  font-size: 18px;
  line-height: 1;
}

.strategy-assistant__panel {
  padding: 14px 16px;
  background: #f1faf5;
  border: 1px solid #67ca94;
  border-radius: 13px;
}

.strategy-assistant__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 12px;
}

.strategy-assistant__title {
  display: flex;
  align-items: center;
  color: #137a47;
  font-size: 16px;
  font-weight: 700;
}

.strategy-assistant__header p {
  margin: 3px 0 0 29px;
  color: #697381;
  font-size: 12px;
}

.strategy-assistant__field { margin-top: 12px; }

.strategy-assistant__field label {
  display: block;
  margin-bottom: 6px;
  color: #596473;
  font-size: 13px;
}

.strategy-assistant__model-field { max-width: 360px; }

.strategy-assistant__input-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 148px;
  gap: 10px;
  align-items: stretch;
}

.strategy-assistant__generate { height: 100%; min-height: 58px; }

.strategy-assistant__examples {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 10px;
}

.strategy-assistant__examples button {
  padding: 5px 14px;
  color: #52685d;
  background: #fff;
  border: 1px solid #b8dcca;
  border-radius: 999px;
  cursor: pointer;
  font: inherit;
  font-size: 12px;
}

.strategy-assistant__examples button:hover:not(:disabled) {
  color: #137a47;
  border-color: #18a058;
}

.strategy-assistant__examples button:disabled { cursor: not-allowed; opacity: .55; }

.strategy-assistant__alert { margin-top: 10px; }

.strategy-assistant__draft {
  margin-top: 14px;
  padding: 12px;
  background: #fff;
  border: 1px solid #d7e2dc;
  border-radius: 10px;
}

.strategy-assistant__draft-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}

.strategy-assistant__summary {
  margin: 8px 0 0;
  color: #3c7d5c;
  font-size: 12px;
}

.strategy-assistant__warnings {
  margin: 8px 0 0;
  padding-left: 18px;
  color: #c56a00;
  font-size: 12px;
}

.strategy-assistant__execution-tip {
  margin: 8px 0 0;
  color: #d97706;
  font-size: 12px;
  text-align: right;
}

.strategy-assistant__actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 12px;
}

.strategy-assistant__empty {
  margin-top: 12px;
  padding: 12px;
  color: #77818d;
  background: rgb(255 255 255 / 66%);
  border: 1px dashed #c8ded2;
  border-radius: 9px;
  font-size: 12px;
  text-align: center;
}

@media (max-width: 640px) {
  .strategy-assistant__panel { padding: 12px; }
  .strategy-assistant__model-field { max-width: none; }
  .strategy-assistant__input-row { grid-template-columns: 1fr; }
  .strategy-assistant__generate { min-height: 38px; }
  .strategy-assistant__actions { align-items: stretch; flex-direction: column-reverse; }
  .strategy-assistant__actions :deep(.n-button) { width: 100%; }
}
</style>
