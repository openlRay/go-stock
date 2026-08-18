<template>
      <!-- 搜索和筛选区域 -->
   <n-space vertical style="margin-bottom: 12px">
      <n-space>
    <n-input
          v-model:value="searchKeyword"
          placeholder="搜索任务名称..."
          style="width: 200px"
          clearable
          @keyup.enter="handleSearch"
        >
          <template #prefix>
            <n-icon :component="SearchOutline" />
          </template>
        </n-input>
        
        <n-select
          v-model:value="filterTaskType"
          :options="taskTypeOptions"
          placeholder="全部任务类型"
          style="width: 140px"
          clearable
        />
        
        <n-select
          v-model:value="filterStatus"
          :options="statusOptions"
          placeholder="全部状态"
          style="width: 120px"
          clearable
        />
        
        <n-button type="primary"  @click="handleSearch">
          搜索
        </n-button>
        
        <n-button type="warning"  @click="handleCreate">
          <template #icon>
            <n-icon :component="AddOutline" />
          </template>
          新建任务
        </n-button>
 </n-space>
 </n-space> 

      <!-- 任务列表表格 -->
      <n-data-table
          remote
          size="small"
          :columns="columns"
          :data="taskList"
          :loading="loading"
          :pagination="pagination"
          :row-key="(rowData)=>rowData.id"
          flex-height
          style="height: calc(100vh - 210px);margin-top: 10px"
      />

    <!-- 创建/编辑任务弹窗 -->
    <n-modal
      v-model:show="showCreateModal"
      :title="editingTask ? '修改任务' : '创建新任务'"
      preset="dialog"
      :style="{ width: '750px' }"
      @close="resetForm"
      :z-index="2000"
      to="body"
    >
      <n-form
        ref="formRef"
        :model="formData"
        :rules="formRules"
        label-placement="left"
        label-width="130px"
        require-mark-placement="right-hanging"
      >
        <n-form-item label="任务名称" path="name">
          <n-input v-model:value="formData.name" placeholder="请输入任务名称" clearable />
        </n-form-item>

        <n-form-item label="任务类型" path="taskType">
          <n-select
            v-model:value="formData.taskType"
            :options="taskTypeOptions"
            placeholder="请选择任务类型"
          />
        </n-form-item>

        <n-form-item label="执行时间" path="cronExpr">
          <n-space :vertical="true" :size="8" style="width: 100%">
            <n-input
              v-model:value="formData.cronExpr"
              placeholder="请设置任务执行时间"
              readonly
            >
              <template #suffix>
                <n-button size="small" @click="showCronBuilder = true">
                  <template #icon>
                    <n-icon :component="SettingsOutline" />
                  </template>
                  设置
                </n-button>
              </template>
            </n-input>
            <n-space :vertical="true" :size="4" style="width: 100%">
              <n-text depth="3" style="font-size: 12px">
                <n-icon :component="InformationCircleOutline" size="14" />
                支持手动设置、AI 辅助填充和专家 Cron | 当前值：{{ formData.cronExpr || '未设置' }}
              </n-text>
              <n-text v-if="calculateNextRunTime" depth="2" style="font-size: 12px; color: #18a058">
                <n-icon :component="TimeOutline" size="14" />
                下次执行时间：{{ calculateNextRunTime }}
              </n-text>
            </n-space>
          </n-space>
        </n-form-item>

<!--        <n-form-item label="目标任务" path="target">-->
<!--          <n-input-->
<!--            v-model:value="formData.target"-->
<!--            placeholder="股票代码或多个代码（逗号分隔），如：600519,000001"-->
<!--            clearable-->
<!--          />-->
<!--        </n-form-item>-->

        <n-form-item :label="'任务参数'" path="params">
          <!-- 股票分析任务的参数配置 UI -->
          <n-card v-if="formData.taskType === 'stock_analysis'" size="small" style="width: 100%">
            <n-space :vertical="true" :size="12">
              <!-- 第一行：提示词模板和 AI 配置 -->
              <n-grid :cols="2" :x-gap="12">
                <n-gi>
                  <n-form-item label-width="90px" label="提示词模板:">
                    <n-select
                      v-model:value="stockAnalysisParamsData.promptId"
                      :options="promptTemplateOptions"
                      placeholder="请选择提示词模板"
                      filterable
                      style="width: 100%"
                    />
                  </n-form-item>
                </n-gi>
                <n-gi>
                  <n-form-item label-width="90px" label="AI 配置:">
                    <n-select
                      v-model:value="stockAnalysisParamsData.aiConfigId"
                      :options="aiConfigOptions"
                      placeholder="请选择 AI 配置"
                      filterable
                      style="width: 100%"
                    />
                  </n-form-item>
                </n-gi>
              </n-grid>
              
              <!-- 第二行：系统提示词和启用思考 -->
              <n-grid :cols="2" :x-gap="12">
                <n-gi>
                  <n-form-item label-width="90px" label="系统提示词:">
                    <n-select
                      v-model:value="stockAnalysisParamsData.sysPromptId"
                      :options="sysPromptOptions"
                      placeholder="请选择系统提示词（可选）"
                      filterable
                      clearable
                      style="width: 100%"
                    />
                  </n-form-item>
                </n-gi>
                <n-gi>
                  <n-form-item label-width="90px" label="启用思考:">
                    <n-switch v-model:value="stockAnalysisParamsData.thinking" size="large">
                      <template #checked>
                        开启
                      </template>
                      <template #unchecked>
                        关闭
                      </template>
                    </n-switch>
                  </n-form-item>
                </n-gi>
              </n-grid>

              <!-- 第三行：Agent模式和股票代码 -->
              <n-grid :cols="2" :x-gap="12">
                <n-gi>
                  <n-form-item label-width="90px" label="Agent模式:">
                    <n-select
                      v-model:value="stockAnalysisParamsData.agentMode"
                      :options="agentModeOptions"
                      placeholder="请选择Agent模式"
                      style="width: 100%"
                    />
                  </n-form-item>
                </n-gi>
                <n-gi>
                  <n-form-item label-width="90px" label="股票代码:">
                    <n-input
                      v-model:value="stockAnalysisParamsData.stockCode"
                      placeholder="请输入股票代码，如：600519"
                      clearable
                      style="width: 100%"
                    />
                  </n-form-item>
                </n-gi>
              </n-grid>

              <!-- 第四行：股票名称 -->
              <n-grid :cols="2" :x-gap="12">
                <n-gi>
                  <n-form-item label-width="90px" label="股票名称:">
                    <n-input
                      v-model:value="stockAnalysisParamsData.stockName"
                      placeholder="请输入股票名称，如：贵州茅台"
                      clearable
                      style="width: 100%"
                    />
                  </n-form-item>
                </n-gi>
              </n-grid>
            </n-space>
          </n-card>
          
          <!-- 市场分析任务的参数配置 UI -->
          <n-card v-else-if="formData.taskType === 'market_analysis'" size="small" style="width: 100%">
            <n-space :vertical="true" :size="12">
              <!-- 第一行：提示词模板和 AI 配置 -->
              <n-grid :cols="2" :x-gap="12">
                <n-gi>
                  <n-form-item label-width="90px" label="提示词模板:">
                    <n-select
                      v-model:value="marketAnalysisParamsData.promptId"
                      :options="promptTemplateOptions"
                      placeholder="请选择提示词模板"
                      filterable
                      style="width: 100%"
                    />
                  </n-form-item>
                </n-gi>
                <n-gi>
                  <n-form-item label-width="90px" label="AI 配置:">
                    <n-select
                      v-model:value="marketAnalysisParamsData.aiConfigId"
                      :options="aiConfigOptions"
                      placeholder="请选择 AI 配置"
                      filterable
                      style="width: 100%"
                    />
                  </n-form-item>
                </n-gi>
              </n-grid>
              
              <!-- 第二行：系统提示词和启用思考 -->
              <n-grid :cols="2" :x-gap="12">
                <n-gi>
                  <n-form-item label-width="90px" label="系统提示词:">
                    <n-select
                      v-model:value="marketAnalysisParamsData.sysPromptId"
                      :options="sysPromptOptions"
                      placeholder="请选择系统提示词（可选）"
                      filterable
                      clearable
                      style="width: 100%"
                    />
                  </n-form-item>
                </n-gi>
                <n-gi>
                  <n-form-item label-width="90px" label="启用思考:">
                    <n-switch v-model:value="marketAnalysisParamsData.thinking" size="large">
                      <template #checked>
                        开启
                      </template>
                      <template #unchecked>
                        关闭
                      </template>
                    </n-switch>
                  </n-form-item>
                </n-gi>
              </n-grid>

              <!-- 第三行：Agent模式 -->
              <n-grid :cols="2" :x-gap="12">
                <n-gi>
                  <n-form-item label-width="90px" label="Agent模式:">
                    <n-select
                      v-model:value="marketAnalysisParamsData.agentMode"
                      :options="agentModeOptions"
                      placeholder="请选择Agent模式"
                      style="width: 100%"
                    />
                  </n-form-item>
                </n-gi>
              </n-grid>
            </n-space>
          </n-card>

          <n-card v-else-if="formData.taskType === 'strategy_screening'" size="small" style="width: 100%">
            <n-space :vertical="true" :size="12">
              <n-alert v-if="!customStrategiesLoading && customStrategyOptions.length === 0" type="warning" :bordered="false">
                暂无“我的策略”，请先到指标选股页面保存至少一条策略。
              </n-alert>
              <n-form-item label-width="90px" label="我的策略:">
                <n-select
                  v-model:value="strategyScreeningParamsData.strategyId"
                  :options="customStrategyOptions"
                  :loading="customStrategiesLoading"
                  :disabled="!customStrategiesLoading && customStrategyOptions.length === 0"
                  placeholder="请选择要定时执行的策略"
                  filterable
                  style="width: 100%"
                />
              </n-form-item>
              <n-form-item label-width="90px" label="推送数量:">
                <n-select
                  v-model:value="strategyScreeningParamsData.pushLimit"
                  :options="strategyPushLimitOptions"
                  style="width: 100%"
                />
              </n-form-item>
              <n-text depth="3" style="font-size: 12px">
                “全部”会尽量列出本次返回的全部股票；超过通知安全长度时会标明实际展示数量。
              </n-text>
            </n-space>
          </n-card>
          
          <!-- 其他任务类型仍使用文本输入框 -->
          <n-alert v-else-if="formData.taskType === 'motto_push'" type="info" :bordered="false">
            每次执行会从“我的 → 格言”随机选择最多三条并合并推送，无需额外参数。
          </n-alert>
          <n-input
            v-else
            v-model:value="formData.params"
            type="textarea"
            :rows="5"
            placeholder='JSON 格式，如：{"stock_codes":["600519"],"ai_config_id":1}'
            show-count
          />
        </n-form-item>

        <n-form-item label="任务描述" path="description">
          <n-input
            v-model:value="formData.description"
            type="textarea"
            :rows="3"
            placeholder="请输入任务描述（可选）"
            show-count
            maxlength="500"
          />
        </n-form-item>

        <n-form-item label="执行完成后推送" path="notifyOnCompletion">
          <div class="notification-setting-field">
            <n-switch
              v-model:value="formData.notifyOnCompletion"
              size="large"
              :disabled="['motto_push', 'strategy_screening'].includes(formData.taskType)"
            >
              <template #checked>开启</template>
              <template #unchecked>关闭</template>
            </n-switch>
            <n-text v-if="formData.taskType === 'motto_push'" depth="3" style="font-size: 12px">
              “推送格言”任务必须推送执行结果，因此该开关已自动开启。
            </n-text>
            <n-text v-else-if="formData.taskType === 'strategy_screening'" depth="3" style="font-size: 12px">
              “策略选股推送”任务必须推送执行结果，因此该开关已自动开启。
            </n-text>
          </div>
        </n-form-item>

        <n-form-item label="启用状态" path="enable">
          <n-switch v-model:value="formData.enable" size="large">
            <template #checked>
              <n-icon :component="PlayCircleOutline" />
              启用
            </template>
            <template #unchecked>
              <n-icon :component="StopCircleOutline" />
              禁用
            </template>
          </n-switch>
        </n-form-item>
      </n-form>

      <template #action>
        <n-button @click="showCreateModal = false">取消</n-button>
        <n-button type="primary" @click="handleSubmit" :loading="submitting">
          <template #icon>
            <n-icon :component="CheckmarkCircleOutline" />
          </template>
          {{ editingTask ? '修改任务' : '创建新任务' }}
        </n-button>
      </template>
    </n-modal>

    <CronScheduleEditor
      v-model:show="showCronBuilder"
      :cron-expr="formData.cronExpr"
      :ai-config-options="aiConfigOptions"
      @confirm="handleCronScheduleConfirm"
    />
</template>

<script setup>
import { ref, reactive, onMounted, onBeforeUnmount, computed, h, watch } from 'vue'
import { NButton, NIcon, NTag, NSpace, NPopconfirm, useMessage, NText, NCard, NSelect, NSwitch } from 'naive-ui'
import {
  SearchOutline,
  AddOutline,
  PlayOutline,
  PauseOutline,
  TrashOutline,
  CreateOutline,
  SettingsOutline,
  InformationCircleOutline,
  PlayCircleOutline,
  StopCircleOutline,
  CheckmarkCircleOutline,
  FlashOutline,
  TimeOutline
} from '@vicons/ionicons5'
import {
  CreateCronTask,
  UpdateCronTask,
  DeleteCronTask,
  GetCronTaskByID,
  GetCronTaskList,
  EnableCronTask,
  ExecuteCronTaskNow,
  GetCronTaskTypes,
  ValidateCronExpr,
  SearchCronTasks,
  GetAiConfigs,
  CalculateNextRunTimes,
  GetPromptTemplates,
  GetAllCustomStrategies
} from '../../wailsjs/go/main/App'
import {EventsOn} from '../../wailsjs/runtime'
import CronScheduleEditor from './cron-schedule-editor.vue'

const message = useMessage()

// 表单引用
const formRef = ref(null)

// 响应式数据
const loading = ref(false)
const submitting = ref(false)
const showCreateModal = ref(false)
const editingTask = ref(false)
const searchKeyword = ref('')
const filterTaskType = ref(null)
const filterStatus = ref(null)
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)
const runningCount = ref(0)
const pausedCount = ref(0)

// 表单数据
const formData = reactive({
  id: null,
  name: '',
  cronExpr: '',
  taskType: 'market_analysis',
  target: '',
  params: '',
  enable: true,
  notifyOnCompletion: false,
  status: 'active',
  description: ''
})

// 表单验证规则
const formRules = {
  name: { required: true, message: '请输入任务名称', trigger: ['input', 'blur'] },
  cronExpr: { required: true, message: '请输入 Cron 表达式', trigger: ['input', 'blur'] },
  taskType: { required: true, message: '请选择任务类型', trigger: [ 'input', 'blur'] }
}

// 选项数据
const taskTypeOptions = ref([])
const statusOptions = [
  { label: '活跃', value: 'active' },
  { label: '暂停', value: 'paused' },
  { label: '错误', value: 'error' }
]
const strategyPushLimitOptions = [
  { label: '10 只', value: 10 },
  { label: '20 只', value: 20 },
  { label: '50 只', value: 50 },
  { label: '全部', value: 0 }
]

// 生成参数 JSON 预览
const generatedParamsJson = computed(() => {

  if(formData.taskType==='stock_analysis'){
    return JSON.stringify({
      promptId: stockAnalysisParamsData.promptId ,
      aiConfigId: stockAnalysisParamsData.aiConfigId,
      sysPromptId: stockAnalysisParamsData.sysPromptId ,
      thinking: stockAnalysisParamsData.thinking ,
      stockCode: stockAnalysisParamsData.stockCode,
      stockName: stockAnalysisParamsData.stockName,
      agentMode: stockAnalysisParamsData.agentMode
    }, null, 2)
  }
  if(formData.taskType==='market_analysis'){
    return JSON.stringify({
      promptId: marketAnalysisParamsData.promptId ,
      aiConfigId: marketAnalysisParamsData.aiConfigId,
      sysPromptId: marketAnalysisParamsData.sysPromptId ,
      thinking: marketAnalysisParamsData.thinking,
      agentMode: marketAnalysisParamsData.agentMode
    }, null, 2)
  }
  if(formData.taskType==='strategy_screening'){
    return JSON.stringify({
      strategyId: strategyScreeningParamsData.strategyId,
      pushLimit: strategyScreeningParamsData.pushLimit
    }, null, 2)
  }
  return formData.params || ''
})

// 股票分析参数
const showCronBuilder = ref(false)
const calculateNextRunTime = ref('')

const handleCronScheduleConfirm = async (cronExpr) => {
  formData.cronExpr = cronExpr
  try {
    const times = await CalculateNextRunTimes(cronExpr, 1)
    calculateNextRunTime.value = Array.isArray(times) ? (times[0] || '') : ''
  } catch (_) {
    calculateNextRunTime.value = ''
  }
}

//任务参数
const agentModeOptions = [
  { label: '🤖 自动选择', value: '' },
  { label: '⚡ 快速模式', value: 'react' },
  { label: '🧠 规划模式', value: 'plan_execute' },
  { label: '🔬 DeepAgents', value: 'deepagents' }
]

const stockAnalysisParamsData = reactive({
  promptId: 0,
  aiConfigId: 0,
  sysPromptId: 0,
  thinking: true,
  stockCode: '',
  stockName: '',
  agentMode: ''
})
const marketAnalysisParamsData= reactive({
  promptId: 0,
  aiConfigId: 0,
  sysPromptId: 0,
  thinking: true,
  agentMode: ''
})
const strategyScreeningParamsData = reactive({
  strategyId: null,
  pushLimit: 20
})
const customStrategyOptions = ref([])
const customStrategiesLoading = ref(false)
// 获取任务类型显示名称
const getTaskTypeLabel = (value) => {
  const option = taskTypeOptions.value.find(opt => opt.value === value)
  return option ? option.label : value
}

// 表格列定义
const columns = [
  {
    title: 'ID',
    key: 'id',
    width: 60,
    ellipsis: { tooltip: true }
  },
  {
    title: '任务名称',
    key: 'name',
    width: 180,
    ellipsis: { tooltip: true },
    render(row) {
      return h('div', { style: 'display: flex; align-items: center; gap: 8px;' }, [
        h(NTag, { type: 'info', size: 'small', bordered: false }, {
          default: () => getTaskTypeLabel(row.taskType)
        }),
        h('span', {}, { default: () => row.name })
      ])
    }
  },
  // {
  //   title: '任务类型',
  //   key: 'taskType',
  //   width: 120,
  //   render(row) {
  //     return h(NTag, { type: 'info' }, { default: () => row.taskType })
  //   }
  // },
  {
    title: 'Cron 表达式',
    key: 'cronExpr',
    width: 150,
    ellipsis: { tooltip: true },
    render(row) {
      return h(NText, { code: true, depth: 2 }, { default: () => row.cronExpr })
    }
  },
  {
    title: '目标',
    key: 'target',
    width: 150,
    ellipsis: { tooltip: true }
  },
  {
    title: '启用',
    key: 'enable',
    width: 70,
    render(row) {
      return h(NTag, { type: row.enable ? 'success' : 'error' }, {
        default: () => (row.enable ? '是' : '否')
      })
    }
  },
  {
    title: '结果推送',
    key: 'notifyOnCompletion',
    width: 90,
    render(row) {
      return h(NTag, { type: row.notifyOnCompletion ? 'success' : 'default' }, {
        default: () => (row.notifyOnCompletion ? '开启' : '关闭')
      })
    }
  },
  {
    title: '状态',
    key: 'status',
    width: 80,
    render(row) {
      const typeMap = {
        active: 'success',
        paused: 'warning',
        error: 'error'
      }
      return h(NTag, { type: typeMap[row.status] || 'default' }, {
        default: () => row.status
      })
    }
  },
  {
    title: '运行次数',
    key: 'runCount',
    width: 90,
    render(row) {
      return h(NSpace, { align: 'center' }, {
        default: () => [
          h(NIcon, { component: FlashOutline, size: 16, color: '#f0a020' }),
          h(NText, {}, { default: () => row.runCount || 0 })
        ]
      })
    }
  },
  {
    title: '最近执行',
    key: 'lastRunAt',
    width: 200,
    render(row) {
      if (!row.lastRunAt) return h(NText, { depth: 3 }, { default: () => '未运行' })
      const date = new Date(row.lastRunAt)
      const resultType = row.lastRunResult && row.lastRunResult.startsWith('成功') ? 'success' : 'error'
      return h(NSpace, { vertical: true, size: 2 }, {
        default: () => [
          h(NText, {}, { default: () => date.toLocaleString('zh-CN') }),
          row.lastRunResult ? h(NTag, { type: resultType, size: 'small' }, { default: () => row.lastRunResult }) : null
        ]
      })
    }
  },
  // {
  //   title: '下次运行',
  //   key: 'nextRunAt',
  //   width: 160,
  //   render(row) {
  //     if (!row.nextRunAt) return h(NText, { depth: 3 }, { default: () => '-' })
  //     const date = new Date(row.nextRunAt)
  //     return h(NText, {}, {
  //       default: () => date.toLocaleString('zh-CN')
  //     })
  //   }
  // },
  {
    title: '操作',
    key: 'actions',
    width: 280,
    fixed: 'right',
    render(row) {
      return h(NSpace, {}, {
        default: () => [
          h(
            NButton,
            {
              size: 'tiny',
              type: 'success',
              onClick: () => handleExecute(row)
            },
            {
              icon: () => h(NIcon, { component: PlayOutline }),
              default: () => '执行'
            }
          ),
          h(
            NButton,
            {
              size: 'tiny',
              type: row.enable ? 'warning' : 'info',
              onClick: () => handleToggleEnable(row)
            },
            {
              icon: () => h(NIcon, { component: row.enable ? PauseOutline : PlayOutline }),
              default: () => (row.enable ? '暂停' : '启用')
            }
          ),
          h(
            NButton,
            {
              size: 'tiny',
              type: 'primary',
              onClick: () => handleEdit(row)
            },
            {
              icon: () => h(NIcon, { component: CreateOutline }),
              default: () => '编辑'
            }
          ),
          h(
            NPopconfirm,
            {
              onPositiveClick: () => handleDelete(row.id)
            },
            {
              trigger: () =>
                h(
                  NButton,
                  {
                    size: 'tiny',
                    type: 'error'
                  },
                  {
                    icon: () => h(NIcon, { component: TrashOutline }),
                    default: () => '删除'
                  }
                ),
              default: () => `确定要删除任务 "${row.name}" 吗？`
            }
          )
        ]
      })
    }
  }
]

// 分页配置
const pagination = computed(() => ({
  page: currentPage.value,
  pageSize: pageSize.value,
  showSizePicker: true,
  pageSizes: [10, 20, 50, 100],
  itemCount: total.value,
  onChange: handlePageChange,
  onUpdatePageSize: handlePageSizeChange
}))

// 加载任务类型
const loadTaskTypes = async () => {
  try {
    const types = await GetCronTaskTypes()
    taskTypeOptions.value = types.map(t => ({
      label: t.B,
      value: t.A
    }))
  } catch (error) {
    console.error('加载任务类型失败:', error)
  }
}

const loadCustomStrategies = async () => {
  customStrategiesLoading.value = true
  customStrategyOptions.value = []
  try {
    const strategies = await GetAllCustomStrategies()
    customStrategyOptions.value = (strategies || []).map(strategy => ({
      label: strategy.name,
      value: Number(strategy.id)
    }))
  } catch (error) {
    customStrategyOptions.value = []
    console.error('加载我的策略失败:', error)
    message.error('加载我的策略失败')
  } finally {
    customStrategiesLoading.value = false
  }
}

// 加载 AI 配置
const aiConfigOptions=ref([])
let stopAIConfigsChangedListener = () => {}
let stopCronTaskExecutedListener = () => {}
const cronTaskExecutedEventName = 'cronTaskExecuted'
const loadAiConfigs = async () => {
  try {
    const configs = await GetAiConfigs()
    aiConfigOptions.value = configs.map(c => ({
      label: c.name+"["+c.modelName+"]",
      value: c.ID
    }))
    const values = new Set(aiConfigOptions.value.map(option => Number(option.value)))
    const fallback = aiConfigOptions.value[0]?.value ?? null
    if (!values.has(Number(stockAnalysisParamsData.aiConfigId))) stockAnalysisParamsData.aiConfigId = fallback
    if (!values.has(Number(marketAnalysisParamsData.aiConfigId))) marketAnalysisParamsData.aiConfigId = fallback
  } catch (error) {
    console.error('加载 AI 配置失败:', error)
  }
}
const promptTemplateOptions=ref([])
const sysPromptOptions=ref([])
// 加载提示词模板
const loadPromptTemplates = async () => {
  try {
    // 加载用户提示词模板
    const userTemplates = await GetPromptTemplates('', '模型用户Prompt')
    promptTemplateOptions.value = userTemplates.map(t => ({
      label: t.name,
      value: t.ID
    }))
    
    // 加载系统提示词模板（假设类型为 system）
    const sysTemplates = await GetPromptTemplates('', '模型系统Prompt')
    sysPromptOptions.value = sysTemplates.map(t => ({
      label: t.name,
      value: t.ID
    }))
  } catch (error) {
    console.error('加载提示词模板失败:', error)
  }
}

// 加载任务列表
const loadTaskList = async () => {
  loading.value = true
  try {
    const query = {
      page: currentPage.value,
      pageSize: pageSize.value,
      name: searchKeyword.value,
      taskType: filterTaskType.value ?? '',
      status: filterStatus.value ?? ''
    }
    
    const result = await GetCronTaskList(query)
    if (result) {
      taskList.value = result.data || []
      total.value = result.total || 0
    }
  } catch (error) {
    console.error('加载任务列表失败:', error)
    message.error('加载任务列表失败')
  } finally {
    loading.value = false
  }
}

// 搜索任务
const handleSearch = async () => {
  currentPage.value = 1
  await loadTaskList()
}

// 分页变化
const handlePageChange = (page) => {
  currentPage.value = page
  loadTaskList()
}

const handlePageSizeChange = (size) => {
  pageSize.value = size
  currentPage.value = 1
  loadTaskList()
}

// 执行任务
const handleExecute = async (row) => {
  try {
    const result = await ExecuteCronTaskNow(row.id)
    message.success(result)
  } catch (error) {
    message.error('执行任务失败：' + error.message)
  }
}

// 立即执行只返回“已启动”；列表必须等后端完成持久化事件后再刷新。
const handleCronTaskExecuted = (event) => {
  if (!event?.taskId) return
  loadTaskList()
}

// 切换启用状态
const handleToggleEnable = async (row) => {
  try {
    const newEnable = !row.enable
    const result = await EnableCronTask(row.id, newEnable)
    if (result === '操作成功') {
      message.success(newEnable ? '任务已启用' : '任务已禁用')
      await loadTaskList()
    } else {
      message.error(result)
    }
  } catch (error) {
    message.error('操作失败：' + error.message)
  }
}

// 创建任务
const handleCreate = async () => {
  editingTask.value = false
  resetForm()
  showCreateModal.value = true
  await loadCustomStrategies()
}

// 编辑任务
const handleEdit = async (row) => {
  editingTask.value = true
  try {
    await loadCustomStrategies()
    const task = await GetCronTaskByID(row.id)
    if (task) {
      // 先重置表单和 Cron 配置器
      resetForm()
      
      // 然后填充表单数据
      formData.id = task.id
      formData.name = task.name
      // 兼容后端返回 cronExpr 或 CronExpr
      formData.cronExpr = (task.cronExpr ?? task.CronExpr ?? '').trim()
      formData.taskType = task.taskType
      formData.target = task.target
      formData.params = task.params
      formData.enable = task.enable
      formData.notifyOnCompletion = task.notifyOnCompletion === true
      formData.status = task.status
      formData.description = task.description
      
      // 如果是股票分析任务，解析参数到表单
      if (task.taskType === 'stock_analysis' && task.params) {
        try {
          const parsed = JSON.parse(task.params)
          stockAnalysisParamsData.promptId = parsed.promptId ?? null
          stockAnalysisParamsData.aiConfigId = parsed.aiConfigId ?? null
          stockAnalysisParamsData.sysPromptId = parsed.sysPromptId ?? null
          stockAnalysisParamsData.thinking = parsed.thinking || false
          stockAnalysisParamsData.stockCode = parsed.stockCode || ''
          stockAnalysisParamsData.stockName = parsed.stockName || ''
          stockAnalysisParamsData.agentMode = parsed.agentMode || ''
        } catch (e) {
          console.error('解析参数失败:', e)
        }
      }
      
      // 如果是市场分析任务，解析参数到表单
      if (task.taskType === 'market_analysis' && task.params) {
        try {
          const parsed = JSON.parse(task.params)
          marketAnalysisParamsData.promptId = parsed.promptId ?? null
          marketAnalysisParamsData.aiConfigId = parsed.aiConfigId ?? null
          marketAnalysisParamsData.sysPromptId = parsed.sysPromptId ?? null
          marketAnalysisParamsData.thinking = parsed.thinking || false
          marketAnalysisParamsData.agentMode = parsed.agentMode || ''
        } catch (e) {
          console.error('解析参数失败:', e)
        }
      }

      if (task.taskType === 'strategy_screening' && task.params) {
        try {
          const parsed = JSON.parse(task.params)
          strategyScreeningParamsData.strategyId = Number(parsed.strategyId) || null
          // 兼容早期任务参数：只有字段缺失时才回退 20，显式 0 保留为“全部”。
          const parsedPushLimit = Object.prototype.hasOwnProperty.call(parsed, 'pushLimit')
            ? Number(parsed.pushLimit)
            : 20
          strategyScreeningParamsData.pushLimit = [0, 10, 20, 50].includes(parsedPushLimit)
            ? parsedPushLimit
            : 20
        } catch (e) {
          console.error('解析策略选股参数失败:', e)
        }
      }
      
      showCreateModal.value = true
    }
  } catch (error) {
    message.error('获取任务详情失败：' + error.message)
  }
}

// 删除任务
const handleDelete = async (id) => {
  try {
    const result = await DeleteCronTask(id)
    if (result === '删除成功') {
      message.success('任务已删除')
      await loadTaskList()
    } else {
      message.error(result)
    }
  } catch (error) {
    message.error('删除失败：' + error.message)
  }
}

// 检查两次执行间隔是否至少 60 秒，返回 { ok: boolean, minIntervalSeconds?: number }
const checkCronInterval = async (cronExpr) => {
  if (!cronExpr) return { ok: true }
  try {
    const times = await CalculateNextRunTimes(cronExpr, 10)
    if (!Array.isArray(times) || times.length < 2) return { ok: true }
    let minSeconds = Infinity
    for (let i = 1; i < times.length; i++) {
      const prev = new Date(times[i - 1]).getTime()
      const curr = new Date(times[i]).getTime()
      if (!Number.isNaN(prev) && !Number.isNaN(curr)) {
        const sec = (curr - prev) / 1000
        if (sec < minSeconds) minSeconds = sec
      }
    }
    if (minSeconds !== Infinity && minSeconds < 60) {
      return { ok: false, minIntervalSeconds: Math.round(minSeconds) }
    }
    return { ok: true }
  } catch (_) {
    return { ok: true }
  }
}

// 提交表单
const handleSubmit = async () => {
  try {
    // 先整体校验表单（会同步更新所有校验状态）
    if (formRef.value) {
      try {
        await formRef.value.validate()
      } catch {
        // 表单校验未通过，直接返回
        return
      }
    }

    // 简单验证 Cron 表达式
    if (!await validateCronExpression()) {
      return
    }

    // 检查执行间隔不小于 60 秒
    const intervalCheck = await checkCronInterval(formData.cronExpr)
    if (!intervalCheck.ok) {
      message.warning(
        `两次执行间隔过短（约 ${intervalCheck.minIntervalSeconds} 秒），请将间隔设置为至少 60 秒后再保存。`
      )
      return
    }

    if (formData.taskType === 'strategy_screening') {
      if (customStrategiesLoading.value) {
        message.warning('策略列表正在加载，请稍后再试')
        return
      }
      if (customStrategyOptions.value.length === 0) {
        message.warning('暂无可用的“我的策略”，请先保存策略')
        return
      }
      if (!strategyScreeningParamsData.strategyId) {
        message.warning('请选择要定时执行的策略')
        return
      }
      const strategyExists = customStrategyOptions.value.some(
        option => Number(option.value) === Number(strategyScreeningParamsData.strategyId)
      )
      if (!strategyExists) {
        message.warning('所选策略不存在或已删除，请重新选择')
        return
      }
      if (![0, 10, 20, 50].includes(Number(strategyScreeningParamsData.pushLimit))) {
        message.warning('请选择有效的推送数量')
        return
      }
      formData.notifyOnCompletion = true
    }

    formData.params = generatedParamsJson.value

    submitting.value = true
    const submitData = { ...formData }
    
    let result
    if (formData.id) {
      result = await UpdateCronTask(submitData)
    } else {
      result = await CreateCronTask(submitData)
    }

    if (result.includes('成功')) {
      message.success(result)
      showCreateModal.value = false
      await loadTaskList()
    } else {
      message.error(result)
    }
  } catch (error) {
    message.error('操作失败：' + error.message)
  } finally {
    submitting.value = false
  }
}

// 验证 Cron 表达式
const validateCronExpression = async () => {
  if (!formData.cronExpr) return false
  
  try {
    const result = await ValidateCronExpr(formData.cronExpr)
    if (result === '有效表达式') {
      //message.success('Cron 表达式有效')
      return true
    } else {
      message.error(result)
      return false
    }
  } catch (error) {
    message.error('Cron 表达式无效：' + error.message)
    return false
  }
}

// 重置表单
const resetForm = () => {
  Object.assign(formData, {
    id: null,
    name: '',
    cronExpr: '',
    taskType: '',
    target: '',
    params: '',
    enable: true,
    notifyOnCompletion: false,
    status: 'active',
    description: ''
  })
  Object.assign(stockAnalysisParamsData, {
    promptId: null,
    aiConfigId: null,
    sysPromptId: null,
    thinking: true,
    stockCode: '',
    stockName: '',
    agentMode: ''
  })
  Object.assign(marketAnalysisParamsData, {
    promptId: null,
    aiConfigId: null,
    sysPromptId: null,
    thinking: true,
    agentMode: ''
  })
  Object.assign(strategyScreeningParamsData, {
    strategyId: null,
    pushLimit: 20
  })
  calculateNextRunTime.value = ''
  // 重置表单校验状态
  if (formRef.value) {
    formRef.value.restoreValidation()
  }
}

// 任务列表数据
const taskList = ref([])

// 监听任务类型变化，重置参数
watch(() => formData.taskType, (newType) => {
  if (newType === 'motto_push' || newType === 'strategy_screening') {
    formData.notifyOnCompletion = true
  }
  if (newType === 'stock_analysis') {
    // 如果是股票分析任务，尝试解析现有参数
    if (formData.params) {
      try {
        const parsed = JSON.parse(formData.params)
        stockAnalysisParamsData.promptId = parsed.promptId ?? null
        stockAnalysisParamsData.aiConfigId = parsed.aiConfigId ?? null
        stockAnalysisParamsData.sysPromptId = parsed.sysPromptId ?? null
        stockAnalysisParamsData.thinking = parsed.thinking || false
        stockAnalysisParamsData.stockCode = parsed.stockCode || ''
        stockAnalysisParamsData.stockName = parsed.stockName || ''
        stockAnalysisParamsData.agentMode = parsed.agentMode || ''
      } catch (e) {
        console.error('解析参数失败:', e)
      }
    }
  }
})

// 初始化
onMounted(async () => {
  stopAIConfigsChangedListener = EventsOn('aiConfigsChanged', loadAiConfigs)
  stopCronTaskExecutedListener = EventsOn(cronTaskExecutedEventName, handleCronTaskExecuted)
  await loadTaskTypes()
  await loadAiConfigs()
  await loadPromptTemplates()
  await loadCustomStrategies()
  await loadTaskList()
})

onBeforeUnmount(() => {
  stopAIConfigsChangedListener()
  stopCronTaskExecutedListener()
})
</script>

<style scoped>
.notification-setting-field {
  display: flex;
  width: 100%;
  min-width: 0;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
  text-align: left;
}
</style>
