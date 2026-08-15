<script setup>
import {computed, h, onBeforeUnmount, onMounted, ref, watch} from "vue";
import {useRouter} from "vue-router";
import {
  CopyAIConfig,
  CreateAIConfig,
  DeleteAIConfig,
  FetchAiModelInfo,
  FetchAiModels,
  GetAIModelCapabilities,
  GetAiConfigs,
  SetDefaultAIConfig,
  UpdateAIConfig
} from "../../wailsjs/go/main/App";
import {NButton, NIcon, NSpace, NTag, NText, NTooltip, useDialog, useMessage} from "naive-ui";
import {data} from "../../wailsjs/go/models";
import {ChevronLeftIcon, HelpCircleFilledIcon} from "tdesign-icons-vue-next";

const message = useMessage()
const dialog = useDialog()
const router = useRouter()

const aiConfigs = ref([])
const listLoading = ref(false)
const submitting = ref(false)
const actionLoadingId = ref(0)
const searchKeyword = ref('')
const drawerVisible = ref(false)
const drawerMode = ref('add')
const editingConfig = ref(null)
const capabilities = ref(null)
const capabilitiesLoading = ref(false)
const stopSequencesText = ref('')
const headerPairs = ref([])
let capabilityRequestSequence = 0
let baseUrlCapabilityTimer = null

function syncHeaderPairsFromConfig() {
  const raw = editingConfig.value?.extraHeaders
  if (!raw) {
    headerPairs.value = []
    return
  }
  try {
    const headers = JSON.parse(raw)
    headerPairs.value = Object.entries(headers).map(([key, value]) => ({key, value}))
  } catch {
    headerPairs.value = []
  }
}

function syncHeaderPairsToConfig() {
  if (!editingConfig.value) return
  const headers = {}
  for (const pair of headerPairs.value) {
    const key = pair.key?.trim()
    if (key) headers[key] = pair.value || ''
  }
  editingConfig.value.extraHeaders = Object.keys(headers).length ? JSON.stringify(headers) : ''
}

function addHeaderPair() {
  headerPairs.value.push({key: '', value: ''})
}

function removeHeaderPair(index) {
  headerPairs.value.splice(index, 1)
}

function fillCodeBuddyHeaders() {
  headerPairs.value = [
    {key: 'x-team-id', value: ''},
    {key: 'x-agent-id', value: ''},
    {key: 'x-task-id', value: '{{uuid}}'},
    {key: 'x-conversation-id', value: '{{sessionId}}'},
  ]
}

const drawerTitle = computed(() => drawerMode.value === 'edit' ? '编辑 AI 配置' : '添加 AI 配置')
const filteredConfigs = computed(() => {
  const keyword = searchKeyword.value.trim().toLowerCase()
  if (!keyword) return aiConfigs.value
  return aiConfigs.value.filter(config => [
    config.name,
    config.modelName,
    config.baseUrl,
    getPlatformName(config.baseUrl)
  ].some(value => (value || '').toLowerCase().includes(keyword)))
})
const tableData = computed(() => filteredConfigs.value.map(config => ({...config, _key: config.ID})))
const pagination = ref({page: 1, pageSize: 10, showSizePicker: true, pageSizes: [5, 10, 20, 50]})

const aiPlatformOptions = [
  {label: 'DeepSeek (https://api.deepseek.com)', value: 'https://api.deepseek.com'},
  {label: '硅基流动 (https://api.siliconflow.cn/v1)', value: 'https://api.siliconflow.cn/v1'},
  {label: '智谱AI(GLM) (https://open.bigmodel.cn/api/paas/v4)', value: 'https://open.bigmodel.cn/api/paas/v4'},
  {label: '智谱GLM Coding Plan (https://open.bigmodel.cn/api/coding/paas/v4)', value: 'https://open.bigmodel.cn/api/coding/paas/v4'},
  {label: '字节豆包(火山引擎) (https://ark.cn-beijing.volces.com/api/v3)', value: 'https://ark.cn-beijing.volces.com/api/v3'},
  {label: '火山引擎 Ark Plan (https://ark.cn-beijing.volces.com/api/plan/v3)', value: 'https://ark.cn-beijing.volces.com/api/plan/v3'},
  {label: '火山引擎 Ark Coding (https://ark.cn-beijing.volces.com/api/coding/v3)', value: 'https://ark.cn-beijing.volces.com/api/coding/v3'},
  {label: '阿里云百炼 (https://dashscope.aliyuncs.com/compatible-mode/v1)', value: 'https://dashscope.aliyuncs.com/compatible-mode/v1'},
  {label: '阿里云百炼 Token Plan 团队版 (https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1)', value: 'https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1'},
  {label: '阿里云百炼 Coding Plan (https://coding.dashscope.aliyuncs.com/v1)', value: 'https://coding.dashscope.aliyuncs.com/v1'},
  {label: 'Moonshot（月之暗面）(https://api.moonshot.cn/v1)', value: 'https://api.moonshot.cn/v1'},
  {label: '腾讯混元 (https://api.hunyuan.cloud.tencent.com/v1)', value: 'https://api.hunyuan.cloud.tencent.com/v1'},
  {label: '讯飞星火 (https://spark-api-open.xf-yun.com/v1)', value: 'https://spark-api-open.xf-yun.com/v1'},
  {label: '零一万物 (https://api.lingyiwanwu.com/v1)', value: 'https://api.lingyiwanwu.com/v1'},
  {label: 'MiniMax (https://api.minimax.chat/v1)', value: 'https://api.minimax.chat/v1'},
  {label: '小米 MiMo TokenPlan (https://token-plan-cn.xiaomimimo.com/v1)', value: 'https://token-plan-cn.xiaomimimo.com/v1'},
  {label: '小米 MiMo (https://api.xiaomimimo.com/v1)', value: 'https://api.xiaomimimo.com/v1'},
  {label: '腾讯云 TokenHub (https://tokenhub.tencentmaas.com/v1)', value: 'https://tokenhub.tencentmaas.com/v1'},
  {label: '腾讯云 Token Plan 个人版 (https://api.lkeap.cloud.tencent.com/plan/v3)', value: 'https://api.lkeap.cloud.tencent.com/plan/v3'},
  {label: '腾讯云 Coding Plan (https://api.lkeap.cloud.tencent.com/coding/v3)', value: 'https://api.lkeap.cloud.tencent.com/coding/v3'},
  {label: 'OpenAI (https://api.openai.com/v1)', value: 'https://api.openai.com/v1'},
  {label: 'Azure OpenAI (https://YOUR_RESOURCE.openai.azure.com)', value: 'https://YOUR_RESOURCE.openai.azure.com'},
  {label: 'OpenRouter (https://openrouter.ai/api/v1)', value: 'https://openrouter.ai/api/v1'},
  {label: 'Ollama (http://localhost:11434/v1)', value: 'http://localhost:11434/v1'},
]

const defaultConfig = () => new data.AIConfig({
  ID: 0,
  name: '',
  baseUrl: 'https://api.deepseek.com',
  apiKey: '',
  modelName: 'deepseek-reasoner',
  modelType: 'chat',
  maxTokens: 8192,
  contextWindow: 0,
  maxCompletionTokens: null,
  temperature: 0.1,
  temperatureConfigured: true,
  topP: null,
  topK: null,
  presencePenalty: null,
  frequencyPenalty: null,
  seed: null,
  stopSequences: [],
  responseFormat: 'text',
  reasoningMode: 'on',
  reasoningEffort: '',
  reasoningBudget: null,
  timeOut: 300,
  httpProxy: '',
  httpProxyEnabled: false,
  sessionId: '',
  thinking: true,
  extraHeaders: '',
  embeddingModel: '',
  isDefault: false,
})

const capability = key => capabilities.value?.[key] || {supported: false, options: []}
const incompatibleFields = computed(() => {
  const config = editingConfig.value
  if (!config || !capabilities.value) return []
	if (config.modelType === 'embedding') return []
  const fields = []
  const add = (key, label) => {
    if (!fields.some(field => field.key === key)) fields.push({key, label})
  }
  const checkNumber = (key, value, label) => {
    if (value === null || value === undefined) return
    const descriptor = capability(key)
    if (!descriptor.supported
      || (descriptor.min != null && value < descriptor.min)
      || (descriptor.max != null && value > descriptor.max)) add(key, label)
  }
  const checkOption = (key, value, empty, label) => {
    if (value === empty || value === null || value === undefined) return
    const descriptor = capability(key)
    if (!descriptor.supported || !(descriptor.options || []).some(option => option.value === value)) add(key, label)
  }
  if (config.temperatureConfigured) checkNumber('temperature', config.temperature, 'Temperature')
  checkNumber('maxCompletionTokens', config.maxCompletionTokens, '最大完成 Token')
  checkNumber('topP', config.topP, 'Top P')
  checkNumber('topK', config.topK, 'Top K')
  checkNumber('presencePenalty', config.presencePenalty, '存在惩罚')
  checkNumber('frequencyPenalty', config.frequencyPenalty, '频率惩罚')
  if (config.seed != null && !capability('seed').supported) add('seed', '随机种子')
  if (!capability('stopSequences').supported && (config.stopSequences || []).length) add('stopSequences', '停止序列')
  checkOption('responseFormat', config.responseFormat, 'text', '输出格式')
  checkOption('reasoningMode', config.reasoningMode, 'off', '推理模式')
  checkOption('reasoningEffort', config.reasoningEffort, '', '推理强度')
  checkNumber('reasoningBudget', config.reasoningBudget, '推理预算')
  return fields
})

function HelpLabel(props) {
  return h(NSpace, {align: 'center', size: 4}, () => [
    h('span', props.text),
    h(NTooltip, {placement: 'top'}, {
      trigger: () => h(NIcon, {size: 16, color: '#2080f0'}, () => h(HelpCircleFilledIcon)),
      default: () => h('div', {style: 'max-width: 360px; white-space: normal;'}, props.help)
    })
  ])
}

function getPlatformName(baseUrl) {
  const option = aiPlatformOptions.find(item => item.value === baseUrl)
  if (!option) return ''
  const index = option.label.indexOf(' (')
  return index > 0 ? option.label.slice(0, index) : option.label
}

function goBackToSettings() {
  router.push({name: 'settings'})
}

function openAddDrawer() {
  drawerMode.value = 'add'
  editingConfig.value = defaultConfig()
  stopSequencesText.value = ''
  syncHeaderPairsFromConfig()
  drawerVisible.value = true
  loadCapabilities()
}

function openEditDrawer(row) {
  drawerMode.value = 'edit'
  editingConfig.value = JSON.parse(JSON.stringify(row))
  editingConfig.value.modelType ||= 'chat'
  editingConfig.value.contextWindow ||= 0
  editingConfig.value.extraHeaders ||= ''
  editingConfig.value.embeddingModel ||= ''
  stopSequencesText.value = (editingConfig.value.stopSequences || []).join('\n')
  syncHeaderPairsFromConfig()
  drawerVisible.value = true
  loadCapabilities()
}

function updateNameFromBaseUrl(value) {
  const platform = getPlatformName(value)
  if (platform && !editingConfig.value.name) editingConfig.value.name = platform
}

function onBaseUrlInput(value) {
  updateNameFromBaseUrl(value)
  if (baseUrlCapabilityTimer) clearTimeout(baseUrlCapabilityTimer)
  baseUrlCapabilityTimer = setTimeout(() => {
    baseUrlCapabilityTimer = null
    loadCapabilities()
  }, 350)
}

function onBaseUrlSelect(value) {
  updateNameFromBaseUrl(value)
  if (baseUrlCapabilityTimer) {
    clearTimeout(baseUrlCapabilityTimer)
    baseUrlCapabilityTimer = null
  }
  loadCapabilities()
}

function onModelNameChange(value) {
  if (!value) return
  const platform = getPlatformName(editingConfig.value.baseUrl) || 'AI'
  if (!editingConfig.value.name || editingConfig.value.name === platform) {
    editingConfig.value.name = `${platform}-${value}`
  }
  loadCapabilities()
  fetchModelInfo(value)
}

async function loadCapabilities() {
  if (!editingConfig.value) return
  const request = ++capabilityRequestSequence
  capabilitiesLoading.value = true
  try {
    const result = await GetAIModelCapabilities(editingConfig.value.baseUrl || '', editingConfig.value.modelName || '')
    if (request === capabilityRequestSequence) capabilities.value = result
  } catch (error) {
    if (request === capabilityRequestSequence) message.error(`读取模型能力失败：${error}`)
  } finally {
    if (request === capabilityRequestSequence) capabilitiesLoading.value = false
  }
}

async function fetchAiModels() {
  const config = editingConfig.value
  if (!config.baseUrl || !config.apiKey) {
    message.warning('请先填写接口地址和 API Key')
    return
  }
  syncHeaderPairsToConfig()
  config._loadingModels = true
  try {
    const models = await FetchAiModels(config.baseUrl, config.apiKey, config.extraHeaders || '')
    config._modelOptions = (models || []).map(value => ({label: value, value}))
    if (!config._modelOptions.length) message.warning('未获取到模型，请检查地址和 API Key，或手动输入模型名称')
  } catch (error) {
    message.error(`获取模型列表失败：${error}`)
  } finally {
    config._loadingModels = false
  }
}

async function fetchModelInfo(modelName) {
  if (!modelName || !editingConfig.value?.baseUrl) return
  try {
    syncHeaderPairsToConfig()
    const info = await FetchAiModelInfo(
      editingConfig.value.baseUrl,
      editingConfig.value.apiKey || '',
      modelName,
      editingConfig.value.extraHeaders || ''
    )
    const updated = []
    if (info?.contextWindow > 0) {
      editingConfig.value.contextWindow = info.contextWindow
      updated.push(`上下文窗口=${info.contextWindow}`)
    }
    if (info?.maxTokens > 0) {
      editingConfig.value.maxTokens = info.maxTokens
      updated.push(`输出上限=${info.maxTokens}`)
    }
    if (updated.length) message.success(`已读取 ${modelName}：${updated.join('，')}`)
  } catch (error) {
    console.debug('FetchAiModelInfo failed', error)
  }
}

function clearIncompatibleFields() {
  const config = editingConfig.value
  for (const field of incompatibleFields.value) {
    if (field.key === 'stopSequences') {
      config.stopSequences = []
      stopSequencesText.value = ''
    } else if (field.key === 'temperature') {
      config.temperatureConfigured = false
      config.temperature = 0
    } else if (field.key === 'responseFormat') config.responseFormat = 'text'
    else if (field.key === 'reasoningMode') {
      config.reasoningMode = 'off'
      config.reasoningEffort = ''
      config.reasoningBudget = null
    } else if (field.key === 'reasoningEffort') config.reasoningEffort = ''
    else config[field.key] = null
  }
  message.info('已清除不兼容参数，请检查后再保存')
}

function buildSavePayload() {
  syncHeaderPairsToConfig()
  const payload = JSON.parse(JSON.stringify(editingConfig.value))
  delete payload._loadingModels
  delete payload._modelOptions
  delete payload._key
  payload.stopSequences = stopSequencesText.value.split('\n').map(value => value.trim()).filter(Boolean)
  payload.modelType = payload.modelType || 'chat'
  payload.thinking = payload.reasoningMode !== 'off'
  if (!payload.temperatureConfigured) payload.temperature = 0
  return payload
}

async function saveConfig() {
  if (submitting.value) return
  const config = editingConfig.value
  if (!config.name || !config.baseUrl || !config.apiKey || !config.modelName) {
    message.warning('名称、接口地址、API Key 和模型名称均为必填项')
    return
  }
  if (incompatibleFields.value.length) {
    message.warning('请先处理当前模型不支持的旧参数')
    return
  }
  submitting.value = true
  try {
    const payload = buildSavePayload()
    if (drawerMode.value === 'edit') await UpdateAIConfig(payload)
    else await CreateAIConfig(payload)
    await loadAiConfigs()
    drawerVisible.value = false
    message.success('保存成功，配置已生效')
  } catch (error) {
    message.error(`保存失败：${error}`)
  } finally {
    submitting.value = false
  }
}

function confirmCopy(row) {
  dialog.warning({
    title: '复制 AI 配置',
    content: `确认复制“${row.name}”吗？确认后将立即创建一个独立副本。`,
    positiveText: '确认复制',
    negativeText: '取消',
    onPositiveClick: async () => {
      actionLoadingId.value = row.ID
      try {
        const copied = await CopyAIConfig(row.ID)
        await loadAiConfigs()
        message.success(`已创建“${copied.name}”`)
      } catch (error) {
        message.error(`复制失败：${error}`)
      } finally {
        actionLoadingId.value = 0
      }
    }
  })
}

function showDeleteReferences(result) {
  dialog.warning({
    title: '无法删除：配置仍被引用',
    content: () => h(NSpace, {vertical: true}, () => [
      h(NText, null, () => result.message),
      ...(result.references || []).map(reference => h(NTag, {type: 'warning', bordered: false}, () => `${reference.sourceName}：${reference.detail}`))
    ]),
    positiveText: '知道了'
  })
}

function confirmDelete(row) {
  dialog.warning({
    title: '删除 AI 配置',
    content: `确认删除“${row.name}”吗？此操作不可撤销。`,
    positiveText: '确认删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      actionLoadingId.value = row.ID
      try {
        const result = await DeleteAIConfig(row.ID)
        if (!result.success) {
          showDeleteReferences(result)
          return
        }
        await loadAiConfigs()
        message.success('删除成功，配置已生效')
      } catch (error) {
        message.error(`删除失败：${error}`)
      } finally {
        actionLoadingId.value = 0
      }
    }
  })
}

async function setDefaultConfig(row) {
  if ((row.modelType || 'chat') === 'embedding') {
    message.warning('向量模型不能设为默认对话模型')
    return
  }
  if (row.isDefault || actionLoadingId.value) return
  actionLoadingId.value = row.ID
  try {
    await SetDefaultAIConfig(row.ID)
    await loadAiConfigs()
    message.success(`已将“${row.name}”设为全局默认模型`)
  } catch (error) {
    message.error(`设置默认模型失败：${error}`)
  } finally {
    actionLoadingId.value = 0
  }
}

const columns = [
  {title: '配置名称', key: 'name', minWidth: 190, resizable: true, render: row => h(NSpace, {size: 6, align: 'center'}, () => [
    h(NText, null, () => row.name),
    row.isDefault ? h(NTag, {type: 'success', size: 'small', bordered: false}, () => '默认') : null
  ])},
  {title: '接口平台', key: 'baseUrl', minWidth: 180, resizable: true, render: row => getPlatformName(row.baseUrl) || row.baseUrl},
  {title: '类型', key: 'modelType', width: 90, render: row => h(NTag, {size: 'small', bordered: false}, () => (row.modelType || 'chat') === 'embedding' ? '向量' : '对话')},
  {title: '模型', key: 'modelName', minWidth: 170, resizable: true},
  {title: '推理模式', key: 'reasoningMode', width: 100, render: row => {
    const enabled = (row.reasoningMode || (row.thinking ? 'on' : 'off')) !== 'off'
    return h(NTag, {type: enabled ? 'success' : 'default', size: 'small', bordered: false}, () => enabled ? '开启' : '关闭')
  }},
  {title: '最大 Token', key: 'maxTokens', width: 110},
  {title: '上下文窗口', key: 'contextWindow', width: 120, render: row => row.contextWindow > 0 ? row.contextWindow : '自动'},
  {title: '操作', key: 'actions', width: 290, fixed: 'right', render: row => h(NSpace, {size: 4}, () => [
    h(NButton, {
      size: 'small',
      type: 'success',
      ghost: true,
      disabled: (row.modelType || 'chat') === 'embedding' || row.isDefault || !!actionLoadingId.value,
      loading: actionLoadingId.value === row.ID,
      onClick: () => setDefaultConfig(row)
    }, () => (row.modelType || 'chat') === 'embedding' ? '仅用于向量' : (row.isDefault ? '当前默认' : '设为默认')),
    h(NButton, {size: 'small', type: 'primary', ghost: true, disabled: actionLoadingId.value === row.ID, onClick: () => openEditDrawer(row)}, () => '编辑'),
    h(NButton, {size: 'small', type: 'info', ghost: true, loading: actionLoadingId.value === row.ID, onClick: () => confirmCopy(row)}, () => '复制'),
    h(NButton, {size: 'small', type: 'error', ghost: true, loading: actionLoadingId.value === row.ID, onClick: () => confirmDelete(row)}, () => '删除')
  ])}
]

async function loadAiConfigs() {
  listLoading.value = true
  try {
    const configs = await GetAiConfigs() || []
    aiConfigs.value = configs.map(config => ({
      ...config,
      modelType: config.modelType || 'chat',
      contextWindow: config.contextWindow || 0,
      extraHeaders: config.extraHeaders || '',
      embeddingModel: config.embeddingModel || '',
    }))
  } catch (error) {
    message.error(`加载 AI 配置失败：${error}`)
  } finally {
    listLoading.value = false
  }
}

watch(() => editingConfig.value?.reasoningMode, mode => {
  if (!editingConfig.value) return
  if (mode === 'off') {
    editingConfig.value.reasoningEffort = ''
    editingConfig.value.reasoningBudget = null
  }
})

onMounted(loadAiConfigs)
onBeforeUnmount(() => {
  if (baseUrlCapabilityTimer) clearTimeout(baseUrlCapabilityTimer)
})
</script>

<template>
  <n-flex style="text-align: left; --wails-draggable:no-drag">
    <n-card size="small" style="width: 100%">
      <template #header>
        <n-space align="center" size="small">
          <n-button quaternary circle size="tiny" title="返回基础设置" @click="goBackToSettings">
            <template #icon><n-icon><ChevronLeftIcon/></n-icon></template>
          </n-button>
          <n-tag type="primary" :bordered="false">AI 模型服务配置</n-tag>
        </n-space>
      </template>
      <template #header-extra>
        <n-button type="primary" dashed @click="openAddDrawer">+ 添加 AI 配置</n-button>
      </template>
      <n-space vertical size="medium">
        <n-text depth="3" style="font-size: 12px">新增、编辑、复制和删除均会在确认后立即生效。高级参数会按接口与模型能力动态展示。</n-text>
        <n-input v-model:value="searchKeyword" placeholder="搜索配置名称 / 平台 / 模型 / 接口地址" clearable style="max-width: 480px" @update:value="pagination.page = 1"/>
        <n-data-table :columns="columns" :data="tableData" :loading="listLoading" :row-key="row => row._key" :pagination="pagination" :bordered="false" :single-line="false" size="small" style="height: calc(100vh - 260px)"/>
      </n-space>
    </n-card>

    <n-drawer v-model:show="drawerVisible" :width="720" placement="right">
      <n-drawer-content :title="drawerTitle" closable>
        <n-spin :show="capabilitiesLoading">
          <n-form v-if="editingConfig" label-placement="left" :label-width="150">
            <n-divider title-placement="left">基础连接</n-divider>
            <n-form-item label="配置名称" required><n-input v-model:value="editingConfig.name" placeholder="例如：本地 GPT-5.6"/></n-form-item>
            <n-form-item label="接口地址" required>
              <n-auto-complete
                v-model:value="editingConfig.baseUrl"
                :options="aiPlatformOptions"
                :input-props="{ autocomplete: 'off', spellcheck: false }"
                clearable
                placeholder="选择预设或直接输入接口地址"
                @update:value="onBaseUrlInput"
                @select="onBaseUrlSelect"
              />
            </n-form-item>
            <n-form-item label="API Key" required><n-input v-model:value="editingConfig.apiKey" type="password" show-password-on="click" placeholder="API Key"/></n-form-item>
            <n-form-item label="模型类型" required>
              <n-radio-group v-model:value="editingConfig.modelType">
                <n-radio-button value="chat">文本对话</n-radio-button>
                <n-radio-button value="embedding">向量模型</n-radio-button>
              </n-radio-group>
              <n-tooltip placement="top">
                <template #trigger><n-icon size="18" color="#2080f0" style="margin-left: 6px; cursor: help"><HelpCircleFilledIcon/></n-icon></template>
                对话模型用于 Agent；向量模型用于知识库和长期记忆的 embeddings 接口。
              </n-tooltip>
            </n-form-item>
            <n-form-item :label="editingConfig.modelType === 'embedding' ? '向量模型名称' : '模型名称'" required>
              <n-space style="width: 100%" :wrap="false">
                <n-select v-model:value="editingConfig.modelName" :options="editingConfig._modelOptions || []" filterable tag :loading="editingConfig._loadingModels" :placeholder="editingConfig.modelType === 'embedding' ? '输入向量模型名，如 text-embedding-3-small' : '输入模型名，如 gpt-5.6-sol'" style="flex: 1" @update:value="onModelNameChange"/>
                <n-button :loading="editingConfig._loadingModels" @click="fetchAiModels">获取模型</n-button>
              </n-space>
            </n-form-item>
            <n-alert v-if="capabilities" type="info" :show-icon="false" style="margin-bottom: 12px">当前能力档案：{{ capabilities.providerName }}</n-alert>

            <template v-if="editingConfig.modelType !== 'embedding'">
            <n-divider title-placement="left">生成参数</n-divider>
            <n-form-item v-if="capability('temperature').supported">
              <template #label><HelpLabel text="Temperature" :help="capability('temperature').description"/></template>
              <n-input-number :value="editingConfig.temperatureConfigured ? editingConfig.temperature : null" clearable :min="capability('temperature').min" :max="capability('temperature').max" :step="capability('temperature').step || 0.1" style="width: 100%" @update:value="value => { editingConfig.temperatureConfigured = value !== null; editingConfig.temperature = value ?? 0 }"/>
            </n-form-item>
            <n-form-item>
              <template #label><HelpLabel text="最大输出 Token" :help="capability('maxTokens').description"/></template>
              <n-input-number v-model:value="editingConfig.maxTokens" :min="1" :step="1" style="width: 100%"/>
            </n-form-item>
            <n-form-item>
              <template #label><HelpLabel text="上下文窗口" help="模型输入与输出合计的 Token 容量。为 0 时按内置模型表或安全默认值推导。"/></template>
              <n-input-number v-model:value="editingConfig.contextWindow" :min="0" :step="1" style="width: 100%"/>
            </n-form-item>
            <n-form-item v-if="capability('maxCompletionTokens').supported">
              <template #label><HelpLabel text="最大完成 Token" :help="capability('maxCompletionTokens').description"/></template>
              <n-input-number v-model:value="editingConfig.maxCompletionTokens" clearable :min="capability('maxCompletionTokens').min" :max="capability('maxCompletionTokens').max" :step="1" style="width: 100%"/>
            </n-form-item>
            <n-form-item v-if="capability('topP').supported">
              <template #label><HelpLabel text="Top P" :help="capability('topP').description"/></template>
              <n-input-number v-model:value="editingConfig.topP" clearable :min="capability('topP').min" :max="capability('topP').max" :step="capability('topP').step" style="width: 100%"/>
            </n-form-item>
            <n-form-item v-if="capability('topK').supported">
              <template #label><HelpLabel text="Top K" :help="capability('topK').description"/></template>
              <n-input-number v-model:value="editingConfig.topK" clearable :min="capability('topK').min" :max="capability('topK').max" style="width: 100%"/>
            </n-form-item>
            <n-form-item v-if="capability('presencePenalty').supported">
              <template #label><HelpLabel text="存在惩罚" :help="capability('presencePenalty').description"/></template>
              <n-input-number v-model:value="editingConfig.presencePenalty" clearable :min="-2" :max="2" :step="0.1" style="width: 100%"/>
            </n-form-item>
            <n-form-item v-if="capability('frequencyPenalty').supported">
              <template #label><HelpLabel text="频率惩罚" :help="capability('frequencyPenalty').description"/></template>
              <n-input-number v-model:value="editingConfig.frequencyPenalty" clearable :min="-2" :max="2" :step="0.1" style="width: 100%"/>
            </n-form-item>
            <n-form-item v-if="capability('seed').supported">
              <template #label><HelpLabel text="随机种子" :help="capability('seed').description"/></template>
              <n-input-number v-model:value="editingConfig.seed" clearable :step="1" style="width: 100%"/>
            </n-form-item>
            <n-form-item v-if="capability('stopSequences').supported">
              <template #label><HelpLabel text="停止序列" :help="capability('stopSequences').description"/></template>
              <n-input v-model:value="stopSequencesText" type="textarea" :rows="3" placeholder="每行一个停止序列"/>
            </n-form-item>
            <n-form-item v-if="capability('responseFormat').supported">
              <template #label><HelpLabel text="输出格式" :help="capability('responseFormat').description"/></template>
              <n-select v-model:value="editingConfig.responseFormat" :options="capability('responseFormat').options"/>
            </n-form-item>

            <n-divider v-if="capability('reasoningMode').supported" title-placement="left">推理参数</n-divider>
            <n-form-item v-if="capability('reasoningMode').supported">
              <template #label><HelpLabel text="推理模式" :help="capability('reasoningMode').description"/></template>
              <n-select v-model:value="editingConfig.reasoningMode" :options="capability('reasoningMode').options"/>
            </n-form-item>
            <n-form-item v-if="capability('reasoningEffort').supported && editingConfig.reasoningMode !== 'off'">
              <template #label><HelpLabel text="推理强度" :help="capability('reasoningEffort').description"/></template>
              <n-select v-model:value="editingConfig.reasoningEffort" clearable :options="capability('reasoningEffort').options" placeholder="使用供应商默认强度"/>
            </n-form-item>
            <n-form-item v-if="capability('reasoningBudget').supported && editingConfig.reasoningMode !== 'off'">
              <template #label><HelpLabel text="推理预算" :help="capability('reasoningBudget').description"/></template>
              <n-input-number v-model:value="editingConfig.reasoningBudget" clearable :min="capability('reasoningBudget').min" :max="capability('reasoningBudget').max" :step="1" style="width: 100%"/>
            </n-form-item>
            <n-alert v-if="capabilities?.profile === 'gemini' && editingConfig.reasoningEffort && editingConfig.reasoningBudget" type="warning">Gemini 推理强度与推理预算不能同时设置，请清除其中一项。</n-alert>
			</template>

            <n-divider title-placement="left">网络设置</n-divider>
            <n-form-item>
              <template #label><HelpLabel text="Timeout（秒）" help="单次模型请求的最大等待时间。推理强度较高时通常需要更长超时。"/></template>
              <n-input-number v-model:value="editingConfig.timeOut" :min="1" :step="1" style="width: 100%"/>
            </n-form-item>
            <n-form-item>
              <template #label><HelpLabel text="HTTP 代理" help="仅该 AI 配置使用的 HTTP(S) 代理。启用后必须填写有效代理地址。"/></template>
              <n-switch v-model:value="editingConfig.httpProxyEnabled"/>
            </n-form-item>
            <n-form-item v-if="editingConfig.httpProxyEnabled" label="代理地址"><n-input v-model:value="editingConfig.httpProxy" placeholder="http://127.0.0.1:7890"/></n-form-item>
            <n-form-item label="自定义 Header">
              <n-space vertical style="width: 100%" :size="8">
                <n-space :size="6">
                  <n-button size="small" dashed @click="addHeaderPair">+ 添加 Header</n-button>
                  <n-button size="small" dashed type="primary" @click="fillCodeBuddyHeaders">填充 CodeBuddy 模板</n-button>
                </n-space>
                <template v-if="!headerPairs.length">
                  <n-text v-pre depth="3" style="font-size: 12px">用于需要额外 Header 的 OpenAI 兼容网关；值支持 {{sessionId}} 与 {{uuid}}。</n-text>
                </template>
                <div v-for="(pair, index) in headerPairs" :key="index" style="display: flex; gap: 8px; align-items: center">
                  <n-input v-model:value="pair.key" size="small" placeholder="Header 名称" style="width: 190px"/>
                  <n-input v-model:value="pair.value" size="small" placeholder="Header 值" style="flex: 1"/>
                  <n-button size="small" quaternary type="error" @click="removeHeaderPair(index)">删除</n-button>
                </div>
              </n-space>
            </n-form-item>

            <n-alert v-if="incompatibleFields.length" type="warning" style="margin-top: 12px">
              已保存参数中有当前模型不支持的项目：{{ incompatibleFields.map(item => item.label).join('、') }}。这些值不会被静默删除，也不会发送给模型；请确认后手动清除。
              <template #action><n-button size="small" type="warning" @click="clearIncompatibleFields">清除不兼容参数</n-button></template>
            </n-alert>
            <n-alert v-for="warning in capabilities?.warnings || []" :key="warning" type="default" :show-icon="false" style="margin-top: 8px">{{ warning }}</n-alert>
          </n-form>
        </n-spin>
        <template #footer>
          <n-space>
            <n-button :disabled="submitting" @click="drawerVisible = false">取消</n-button>
            <n-button type="primary" :loading="submitting" @click="saveConfig">保存</n-button>
          </n-space>
        </template>
      </n-drawer-content>
    </n-drawer>
  </n-flex>
</template>

<style scoped>
:deep(.n-form-item-label) { align-items: center; }
</style>
