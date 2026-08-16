<template>
  <div class="my-page">
    <n-card :bordered="false" class="my-card">
      <n-tabs type="line" animated default-value="mottos">
        <n-tab-pane name="mottos" tab="格言">
          <div class="motto-toolbar">
            <div>
              <h2>我的格言</h2>
              <n-text depth="3">维护自己的格言库，并可使用已配置的 AI 模型润色草稿。</n-text>
            </div>
            <n-button type="primary" @click="openCreate">新增格言</n-button>
          </div>

          <n-data-table
            v-if="loading || mottos.length"
            :columns="columns"
            :data="mottos"
            :loading="loading"
            :row-key="row => row.id"
            :pagination="{ pageSize: 10 }"
            :bordered="false"
          />
          <n-empty v-else description="还没有格言，添加第一条属于你的格言吧">
            <template #extra>
              <n-button type="primary" size="small" @click="openCreate">新增格言</n-button>
            </template>
          </n-empty>
        </n-tab-pane>
      </n-tabs>
    </n-card>

    <AppModalShell
      v-model:show="showEditor"
      :title="editingId ? '编辑格言' : '新增格言'"
      width="720px"
    >
      <n-form ref="formRef" :model="draft" :rules="rules" label-placement="top">
        <n-form-item label="格言正文" path="content">
          <n-input
            v-model:value="draft.content"
            type="textarea"
            :autosize="{ minRows: 6, maxRows: 14 }"
            maxlength="2000"
            show-count
            placeholder="写下一句值得反复提醒自己的话"
          />
        </n-form-item>
        <n-form-item label="AI 润色模型">
          <n-space vertical style="width: 100%">
            <n-select
              v-model:value="draft.aiConfigId"
              :options="aiConfigOptions"
              :disabled="!aiConfigOptions.length"
              placeholder="请选择 AI 配置"
              filterable
            />
            <n-alert v-if="!aiConfigOptions.length" type="warning" :bordered="false">
              尚未配置可用的对话模型，请先前往“设置 → AI 模型服务”添加配置。
            </n-alert>
            <n-text depth="3" class="draft-tip">AI 润色只会更新当前草稿，点击保存后才会写入格言库。</n-text>
          </n-space>
        </n-form-item>
      </n-form>

      <template #footer>
        <n-button @click="showEditor = false">取消</n-button>
        <n-button
          :loading="polishing"
          :disabled="!draft.content.trim() || !draft.aiConfigId || saving"
          @click="polishDraft"
        >
          AI 润色
        </n-button>
        <n-button type="primary" :loading="saving" :disabled="polishing" @click="saveMotto">
          保存
        </n-button>
      </template>
    </AppModalShell>
  </div>
</template>

<script setup>
import { h, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { NButton, NPopconfirm, NSpace, NText, useMessage } from 'naive-ui'
import { EventsOn } from '../../wailsjs/runtime'
import {
  CreateMotto,
  DeleteMotto,
  GetAiConfigs,
  GetMottos,
  PolishMotto,
  UpdateMotto
} from '../../wailsjs/go/main/App'
import AppModalShell from './common/AppModalShell.vue'

const message = useMessage()
const formRef = ref(null)
const loading = ref(false)
const saving = ref(false)
const polishing = ref(false)
const showEditor = ref(false)
const editingId = ref(0)
const mottos = ref([])
const aiConfigOptions = ref([])
const defaultAIConfigId = ref(null)
let stopAIConfigsChangedListener = () => {}
let polishRequestId = 0

const draft = reactive({ content: '', aiConfigId: null })
const rules = {
  content: {
    trigger: ['input', 'blur'],
    validator: (_rule, value) => value?.trim() ? true : new Error('请输入格言正文')
  }
}

function formatDate(value) {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '-' : date.toLocaleString('zh-CN', { hour12: false })
}

const columns = [
  {
    title: '格言',
    key: 'content',
    minWidth: 360,
    ellipsis: { tooltip: true },
    render: row => h(NText, { style: 'white-space: pre-wrap' }, { default: () => row.content })
  },
  { title: '创建时间', key: 'createdAt', width: 180, render: row => formatDate(row.createdAt) },
  { title: '更新时间', key: 'updatedAt', width: 180, render: row => formatDate(row.updatedAt) },
  {
    title: '操作',
    key: 'actions',
    width: 150,
    fixed: 'right',
    render(row) {
      return h(NSpace, { size: 8 }, {
        default: () => [
          h(NButton, { size: 'small', type: 'primary', onClick: () => openEdit(row) }, { default: () => '编辑' }),
          h(NPopconfirm, { onPositiveClick: () => removeMotto(row.id) }, {
            trigger: () => h(NButton, { size: 'small', type: 'error' }, { default: () => '删除' }),
            default: () => '确定删除这条格言吗？'
          })
        ]
      })
    }
  }
]

function readableError(error) {
  return error?.message || String(error || '未知错误')
}

async function loadMottos() {
  loading.value = true
  try {
    mottos.value = await GetMottos() || []
  } catch (error) {
    message.error('加载格言失败：' + readableError(error))
  } finally {
    loading.value = false
  }
}

async function loadAIConfigs() {
  try {
    const configs = (await GetAiConfigs() || []).filter(config => !config.modelType || config.modelType === 'chat')
    aiConfigOptions.value = configs.map(config => ({
      label: `${config.name}[${config.modelName}]${config.isDefault ? ' · 默认' : ''}`,
      value: config.ID
    }))
    defaultAIConfigId.value = (configs.find(config => config.isDefault) || configs[0])?.ID ?? null
    const values = new Set(aiConfigOptions.value.map(option => Number(option.value)))
    if (!values.has(Number(draft.aiConfigId))) {
      draft.aiConfigId = defaultAIConfigId.value
    }
  } catch (error) {
    aiConfigOptions.value = []
    defaultAIConfigId.value = null
    message.error('加载 AI 配置失败：' + readableError(error))
  }
}

function resetEditor() {
  editingId.value = 0
  draft.content = ''
  draft.aiConfigId = defaultAIConfigId.value
  formRef.value?.restoreValidation()
}

function openCreate() {
  resetEditor()
  showEditor.value = true
}

function openEdit(row) {
  resetEditor()
  editingId.value = row.id
  draft.content = row.content || ''
  showEditor.value = true
}

async function polishDraft() {
  if (!draft.content.trim()) {
    message.warning('请先输入需要润色的格言')
    return
  }
  if (!draft.aiConfigId) {
    message.warning('请选择可用的 AI 配置')
    return
  }
  const requestId = ++polishRequestId
  polishing.value = true
  try {
    const result = await PolishMotto({ content: draft.content, aiConfigId: draft.aiConfigId })
    // 编辑器关闭或重新打开后，旧请求不得覆盖新的格言草稿。
    if (requestId !== polishRequestId || !showEditor.value) return
    if (!result?.content) throw new Error('AI 未返回可用内容')
    draft.content = result.content
    message.success('润色结果已写入草稿，请确认后保存')
  } catch (error) {
    if (requestId !== polishRequestId || !showEditor.value) return
    message.error(readableError(error))
  } finally {
    if (requestId === polishRequestId) polishing.value = false
  }
}

async function saveMotto() {
  try {
    await formRef.value?.validate()
  } catch {
    return
  }
  saving.value = true
  try {
    if (editingId.value) {
      await UpdateMotto(editingId.value, draft.content)
      message.success('格言已更新')
    } else {
      await CreateMotto(draft.content)
      message.success('格言已添加')
    }
    showEditor.value = false
    await loadMottos()
  } catch (error) {
    message.error(readableError(error))
  } finally {
    saving.value = false
  }
}

async function removeMotto(id) {
  try {
    await DeleteMotto(id)
    message.success('格言已删除')
    await loadMottos()
  } catch (error) {
    message.error(readableError(error))
  }
}

onMounted(async () => {
  stopAIConfigsChangedListener = EventsOn('aiConfigsChanged', loadAIConfigs)
  await Promise.all([loadMottos(), loadAIConfigs()])
})

watch(showEditor, shown => {
  if (shown) return
  // 关闭弹框即让进行中的润色结果失效；模型调用可能继续完成，但不能再写回 UI 草稿。
  polishRequestId += 1
  polishing.value = false
})

onBeforeUnmount(() => {
  polishRequestId += 1
  stopAIConfigsChangedListener()
})
</script>

<style scoped>
.my-page {
  height: 100%;
  padding: 16px;
  overflow: auto;
  text-align: left;
}

.my-card {
  min-height: calc(100vh - 110px);
}

.motto-toolbar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 18px;
}

.motto-toolbar h2 {
  margin: 0 0 4px;
  font-size: 20px;
}

.draft-tip {
  font-size: 12px;
}

@media (max-width: 640px) {
  .my-page { padding: 10px; }
  .motto-toolbar { align-items: stretch; flex-direction: column; }
}
</style>
