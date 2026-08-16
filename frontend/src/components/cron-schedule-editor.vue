<template>
  <!-- 父级任务弹框固定在 2000；子弹框必须更高，避免遮罩和内容被父层覆盖。 -->
  <AppModalShell
    :show="show"
    title="执行时间设置"
    aria-label="执行时间设置"
    :z-index="2100"
    @update:show="handleShowChange"
  >
    <section class="ai-panel">
          <div class="ai-panel__title">✨ AI 帮你填写</div>
          <p class="ai-panel__description">可选能力：描述执行时间，AI 只填充下方表单，不会直接保存。</p>
          <div class="ai-panel__input-row">
            <n-input
              v-model:value="aiText"
              class="ai-panel__input"
              placeholder="例如：每天12点整、工作日下午3点、每5分钟"
              clearable
              @keyup.enter="handleAIParse"
            />
            <n-button
              class="ai-panel__submit"
              type="primary"
              :loading="aiParsing"
              :disabled="!hasAIConfig"
              @click="handleAIParse"
            >
              识别并填充
            </n-button>
          </div>
          <div class="ai-panel__examples">
            <button
              v-for="example in aiExamples"
              :key="example"
              type="button"
              @click="aiText = example"
            >
              {{ example }}
            </button>
          </div>
          <p v-if="!hasAIConfig" class="ai-panel__warning">
            请先在“AI 模型服务配置”中添加并设置默认模型；手动设置和专家 Cron 仍可使用。
          </p>
    </section>

    <section class="manual-panel">
          <h3>手动设置</h3>
          <p>直接修改任何字段，始终以这里的内容为准。</p>

          <div class="mode-segment" role="tablist" aria-label="执行方式">
            <button
              v-for="mode in modeOptions"
              :key="mode.value"
              type="button"
              role="tab"
              :aria-selected="schedule.mode === mode.value"
              :class="{ 'is-active': schedule.mode === mode.value }"
              @click="selectMode(mode.value)"
            >
              {{ mode.label }}
            </button>
          </div>

          <div class="schedule-form">
            <div v-if="schedule.mode === 'interval'" class="schedule-form__row">
              <div class="schedule-form__label">执行间隔</div>
              <div class="schedule-form__content schedule-form__inline">
                <span>每隔</span>
                <n-input-number
                  v-model:value="schedule.interval.value"
                  :min="1"
                  :max="intervalMax"
                  :show-button="false"
                  class="field-number"
                />
                <n-select
                  v-model:value="schedule.interval.unit"
                  :options="intervalUnitOptions"
                  class="field-unit"
                  @update:value="clampIntervalValue"
                />
                <span>执行一次</span>
              </div>
            </div>

            <div v-else-if="schedule.mode === 'daily'" class="schedule-form__row">
              <div class="schedule-form__label">执行时间</div>
              <div class="schedule-form__content schedule-form__inline">
                <span>每天</span>
                <n-time-picker
                  v-model:formatted-value="schedule.daily.time"
                  format="HH:mm"
                  value-format="HH:mm"
                  :clearable="false"
                  class="field-time"
                />
                <span>执行一次</span>
              </div>
            </div>

            <template v-else-if="schedule.mode === 'weekly'">
              <div class="schedule-form__row schedule-form__row--top">
                <div class="schedule-form__label">执行日期</div>
                <div class="schedule-form__content">
                  <n-checkbox-group v-model:value="schedule.weekly.days">
                    <n-space :size="8" wrap>
                      <n-checkbox
                        v-for="day in weekdayOptions"
                        :key="day.value"
                        :value="day.value"
                        :label="day.label"
                      />
                    </n-space>
                  </n-checkbox-group>
                </div>
              </div>
              <div class="schedule-form__row">
                <div class="schedule-form__label">执行时间</div>
                <div class="schedule-form__content schedule-form__inline">
                  <span>所选日期</span>
                  <n-time-picker
                    v-model:formatted-value="schedule.weekly.time"
                    format="HH:mm"
                    value-format="HH:mm"
                    :clearable="false"
                    class="field-time"
                  />
                  <span>执行一次</span>
                </div>
              </div>
            </template>

            <template v-else>
              <div class="schedule-form__row">
                <div class="schedule-form__label">执行日期</div>
                <div class="schedule-form__content schedule-form__inline">
                  <span>每月第</span>
                  <n-input-number
                    v-model:value="schedule.monthly.day"
                    :min="1"
                    :max="31"
                    :show-button="false"
                    class="field-number"
                  />
                  <span>日</span>
                </div>
              </div>
              <div class="schedule-form__row">
                <div class="schedule-form__label">执行时间</div>
                <div class="schedule-form__content schedule-form__inline">
                  <span>当天</span>
                  <n-time-picker
                    v-model:formatted-value="schedule.monthly.time"
                    format="HH:mm"
                    value-format="HH:mm"
                    :clearable="false"
                    class="field-time"
                  />
                  <span>执行一次</span>
                </div>
              </div>
            </template>

            <div class="schedule-form__row">
              <div class="schedule-form__label">时区</div>
              <div class="schedule-form__content">
                <n-select
                  :value="timezone"
                  :options="timezoneOptions"
                  disabled
                  class="field-timezone"
                />
              </div>
            </div>
          </div>

          <section class="schedule-preview" :class="{ 'schedule-preview--error': cronValidationError }">
            <div class="schedule-preview__summary">
              {{ customCron ? '自定义 Cron' : scheduleSummary }}（{{ timezone }}）
            </div>
            <div v-if="cronValidationError" class="schedule-preview__error">{{ cronValidationError }}</div>
            <div v-else class="schedule-preview__times">
              <strong>接下来：</strong>
              <span v-if="!nextRunTimes.length">正在计算…</span>
              <span v-for="(time, index) in nextRunTimes.slice(0, 3)" :key="`${time}-${index}`">
                {{ formatRunTime(time) }}
              </span>
            </div>
          </section>
    </section>

    <details class="expert-panel">
          <summary>专家设置：查看或直接编辑 Cron 表达式</summary>
          <div class="expert-panel__content">
            <div class="expert-panel__input-row">
              <n-input
                v-model:value="expertCron"
                placeholder="秒 分 时 日 月 周，例如：0 */5 * * * *"
                @keyup.enter="applyExpertCron"
              />
              <n-button type="primary" ghost @click="applyExpertCron">应用表达式</n-button>
            </div>
            <p :class="expertValidationError ? 'expert-panel__error' : 'expert-panel__valid'">
              {{ expertValidationError || '有效表达式' }}
            </p>
            <div class="expert-panel__help">
              <div><strong>六段格式：</strong><code>秒 分 时 日 月 周</code></div>
              <div class="expert-panel__symbols">
                <span><code>*</code> 任意值</span>
                <span><code>,</code> 多个指定值</span>
                <span><code>-</code> 连续范围</span>
                <span><code>/</code> 间隔步长</span>
              </div>
              <div class="expert-panel__examples">
                <span><code>0 */5 * * * *</code> 每 5 分钟</span>
                <span><code>0 0 12 * * *</code> 每天 12:00</span>
                <span><code>0 30 9 * * 1-5</code> 工作日 09:30</span>
              </div>
              <p>普通设置固定“秒”为 0。合法但无法转换为普通表单的表达式会作为自定义 Cron 原样保留。</p>
            </div>
          </div>
    </details>

    <template #footer>
        <n-button class="footer-button footer-button--cancel" @click="handleShowChange(false)">取消</n-button>
        <n-button
          class="footer-button footer-button--primary"
          type="primary"
          :disabled="!!cronValidationError || !workingCron"
          @click="confirmSchedule"
        >
          使用此设置
        </n-button>
    </template>
  </AppModalShell>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useMessage } from 'naive-ui'
import AppModalShell from './common/AppModalShell.vue'
import {
  CalculateNextRunTimes,
  ParseCronScheduleText,
  ValidateCronExpr
} from '../../wailsjs/go/main/App'

const props = defineProps({
  show: { type: Boolean, default: false },
  cronExpr: { type: String, default: '' },
  aiConfigOptions: { type: Array, default: () => [] }
})

const emit = defineEmits(['update:show', 'confirm'])
const message = useMessage()
const timezone = 'Asia/Shanghai'
const timezoneOptions = [{ label: timezone, value: timezone }]
const modeOptions = [
  { label: '间隔执行', value: 'interval' },
  { label: '每天', value: 'daily' },
  { label: '每周', value: 'weekly' },
  { label: '每月', value: 'monthly' }
]
const intervalUnitMeta = {
  minute: { label: '分钟', max: 59 },
  hour: { label: '小时', max: 23 },
  day: { label: '天', max: 31 }
}
const intervalUnitOptions = Object.entries(intervalUnitMeta).map(([value, meta]) => ({
  label: meta.label,
  value
}))
const weekdayOptions = [
  { label: '一', value: 1 },
  { label: '二', value: 2 },
  { label: '三', value: 3 },
  { label: '四', value: 4 },
  { label: '五', value: 5 },
  { label: '六', value: 6 },
  { label: '日', value: 0 }
]
const weekdayNames = { 0: '周日', 1: '周一', 2: '周二', 3: '周三', 4: '周四', 5: '周五', 6: '周六' }
const aiExamples = ['每5分钟', '每天上午9点半', '工作日下午3点', '每周一上午10点']

const schedule = reactive({
  mode: 'daily',
  interval: { value: 5, unit: 'minute' },
  daily: { time: '12:00' },
  weekly: { days: [1, 2, 3, 4, 5], time: '15:00' },
  monthly: { day: 1, time: '12:00' }
})
const expertCron = ref('')
const customCron = ref(false)
const customCronValue = ref('')
const cronValidationError = ref('')
const expertValidationError = ref('')
const nextRunTimes = ref([])
const aiText = ref('每天12点整')
const aiParsing = ref(false)
let previewRequestID = 0
let expertValidationRequestID = 0

const hasAIConfig = computed(() => props.aiConfigOptions.length > 0)
const intervalMax = computed(() => intervalUnitMeta[schedule.interval.unit]?.max || intervalUnitMeta.minute.max)
const intervalValue = computed(() => {
  return Math.min(intervalMax.value, Math.max(1, Number(schedule.interval.value) || 1))
})
const monthDay = computed(() => Math.min(31, Math.max(1, Number(schedule.monthly.day) || 1)))

const commonCron = computed(() => {
  if (schedule.mode === 'interval') {
    // “每 N 天”按月内日期步进，在跨月后重新计算；它不是严格的 N×24 小时间隔。
    if (schedule.interval.unit === 'day') return `0 0 0 */${intervalValue.value} * *`
    if (schedule.interval.unit === 'hour') return `0 0 */${intervalValue.value} * * *`
    return `0 */${intervalValue.value} * * * *`
  }
  if (schedule.mode === 'daily') {
    const [hour, minute] = splitTime(schedule.daily.time)
    return `0 ${minute} ${hour} * * *`
  }
  if (schedule.mode === 'weekly') {
    const [hour, minute] = splitTime(schedule.weekly.time)
    return `0 ${minute} ${hour} * * ${normalizedWeekdays(schedule.weekly.days).join(',')}`
  }
  const [hour, minute] = splitTime(schedule.monthly.time)
  return `0 ${minute} ${hour} ${monthDay.value} * *`
})

const workingCron = computed(() => customCron.value ? customCronValue.value.trim() : commonCron.value)
const scheduleSummary = computed(() => {
  if (schedule.mode === 'interval') {
    return `每 ${intervalValue.value} ${intervalUnitMeta[schedule.interval.unit]?.label || '分钟'}执行一次`
  }
  if (schedule.mode === 'daily') return `每天 ${normalizeTime(schedule.daily.time)} 执行一次`
  if (schedule.mode === 'weekly') {
    const labels = normalizedWeekdays(schedule.weekly.days).map(day => weekdayNames[day])
    return `每${labels.join('、')} ${normalizeTime(schedule.weekly.time)} 执行一次`
  }
  return `每月 ${monthDay.value} 日 ${normalizeTime(schedule.monthly.time)} 执行一次`
})

watch(() => props.show, show => {
  if (!show) return
  initializeFromCron(props.cronExpr || '0 0 12 * * *')
}, { immediate: true })

watch(workingCron, cron => {
  expertCron.value = cron
  validateAndPreview(cron)
}, { immediate: true })

watch(expertCron, cron => validateExpertDraft(cron), { immediate: true })

watch(() => schedule.weekly.days, days => {
  if (!days.length) schedule.weekly.days = [1]
}, { deep: true })

function handleShowChange(value) {
  emit('update:show', value)
}

function selectMode(mode) {
  schedule.mode = mode
  customCron.value = false
  customCronValue.value = ''
}

function clampIntervalValue() {
  schedule.interval.value = intervalValue.value
}

function normalizeTime(value) {
  const [hour, minute] = splitTime(value)
  return `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
}

function splitTime(value) {
  const [hour = '0', minute = '0'] = String(value || '00:00').split(':')
  return [Number(hour), Number(minute)]
}

function normalizedWeekdays(days) {
  return [...new Set((days || []).map(Number))].sort((a, b) => a - b)
}

function normalizeCron(raw) {
  const value = String(raw || '').trim().replace(/\s+/g, ' ')
  if (!value) return ''
  const parts = value.split(' ')
  if (parts.length === 5) return `0 ${parts.join(' ')}`
  return parts.join(' ')
}

function initializeFromCron(raw) {
  const cron = normalizeCron(raw)
  expertCron.value = cron
  if (!applyCommonCron(cron)) {
    customCron.value = true
    customCronValue.value = cron
  }
}

function applyCommonCron(raw) {
  const parts = normalizeCron(raw).split(' ')
  if (parts.length !== 6) return false
  const [second, minute, hour, day, month, week] = parts
  if (second !== '0' || month !== '*') return false

  let match = minute.match(/^\*\/(\d+)$/)
  if (match && hour === '*' && day === '*' && week === '*') {
    const value = Number(match[1])
    if (value < 1 || value > 59) return false
    schedule.mode = 'interval'
    schedule.interval.value = value
    schedule.interval.unit = 'minute'
    customCron.value = false
    return true
  }
  match = hour.match(/^\*\/(\d+)$/)
  if (minute === '0' && match && day === '*' && week === '*') {
    const value = Number(match[1])
    if (value < 1 || value > 23) return false
    schedule.mode = 'interval'
    schedule.interval.value = value
    schedule.interval.unit = 'hour'
    customCron.value = false
    return true
  }
  match = day.match(/^\*\/(\d+)$/)
  // 只把当前编辑器能够无损表达的 00:00 月内日期步进规则回填为“每 N 天”。
  if (minute === '0' && hour === '0' && match && week === '*') {
    const value = Number(match[1])
    if (value < 1 || value > 31) return false
    schedule.mode = 'interval'
    schedule.interval.value = value
    schedule.interval.unit = 'day'
    customCron.value = false
    return true
  }
  if (/^\d+$/.test(minute) && /^\d+$/.test(hour)) {
    const time = `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
    if (day === '*' && week === '*') {
      schedule.mode = 'daily'
      schedule.daily.time = time
      customCron.value = false
      return true
    }
    const weekdays = parseCommonWeekdays(week)
    if (day === '*' && weekdays) {
      schedule.mode = 'weekly'
      schedule.weekly.days = weekdays
      schedule.weekly.time = time
      customCron.value = false
      return true
    }
    if (/^\d+$/.test(day) && week === '*') {
      const value = Number(day)
      if (value < 1 || value > 31) return false
      schedule.mode = 'monthly'
      schedule.monthly.day = value
      schedule.monthly.time = time
      customCron.value = false
      return true
    }
  }
  return false
}

function parseCommonWeekdays(value) {
  if (/^[0-6](?:,[0-6])*$/.test(value)) return normalizedWeekdays(value.split(','))
  const range = value.match(/^([0-6])-([0-6])$/)
  if (!range) return null
  const start = Number(range[1])
  const end = Number(range[2])
  if (start > end) return null
  return Array.from({ length: end - start + 1 }, (_, index) => start + index)
}

async function validateExpertDraft(raw) {
  const requestID = ++expertValidationRequestID
  const cron = normalizeCron(raw)
  if (!cron) {
    expertValidationError.value = '请输入 Cron 表达式'
    return
  }
  try {
    const validation = await ValidateCronExpr(cron)
    if (requestID !== expertValidationRequestID) return
    expertValidationError.value = validation === '有效表达式' ? '' : validation
  } catch (error) {
    if (requestID !== expertValidationRequestID) return
    expertValidationError.value = `表达式校验失败：${error?.message || error}`
  }
}

async function validateAndPreview(cron) {
  const requestID = ++previewRequestID
  if (!cron) {
    cronValidationError.value = '请输入 Cron 表达式'
    nextRunTimes.value = []
    return
  }
  try {
    const validation = await ValidateCronExpr(cron)
    if (requestID !== previewRequestID) return
    if (validation !== '有效表达式') {
      cronValidationError.value = validation
      nextRunTimes.value = []
      return
    }
    const times = await CalculateNextRunTimes(cron, 5)
    if (requestID !== previewRequestID) return
    cronValidationError.value = ''
    nextRunTimes.value = Array.isArray(times) ? times : []
  } catch (error) {
    if (requestID !== previewRequestID) return
    cronValidationError.value = `表达式校验失败：${error?.message || error}`
    nextRunTimes.value = []
  }
}

async function applyExpertCron() {
  const cron = normalizeCron(expertCron.value)
  const validation = await ValidateCronExpr(cron)
  if (validation !== '有效表达式') {
    message.error(validation)
    return
  }
  if (applyCommonCron(cron)) {
    customCronValue.value = ''
    message.success('已转换并填入手动设置')
  } else {
    customCron.value = true
    customCronValue.value = cron
    message.success('已作为自定义 Cron 应用')
  }
  await validateAndPreview(cron)
}

async function handleAIParse() {
  if (!aiText.value.trim()) {
    message.warning('请先描述希望任务何时执行')
    return
  }
  if (!hasAIConfig.value) {
    message.warning('请先在 AI 模型服务配置中添加并设置默认模型')
    return
  }
  aiParsing.value = true
  try {
    const result = await ParseCronScheduleText({ text: aiText.value.trim(), aiConfigId: 0 })
    applyAIResult(result)
    message.success('AI 已填充手动设置，请核对后再使用')
  } catch (error) {
    message.error(error?.message || String(error))
  } finally {
    aiParsing.value = false
  }
}

function applyAIResult(result) {
  const mode = result?.mode
  if (!modeOptions.some(option => option.value === mode)) throw new Error('AI 未返回可用的执行时间')

  if (mode === 'interval') {
    const unit = result.intervalUnit
    const value = Number(result.intervalValue)
    const unitMeta = intervalUnitMeta[unit]
    if (!unitMeta || value < 1 || value > unitMeta.max) throw new Error('AI 返回的执行间隔无效')
  } else if (mode === 'daily') {
    if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(result.time || '')) throw new Error('AI 返回的执行时间无效')
  } else if (mode === 'weekly') {
    const days = normalizedWeekdays(result.weekdays)
    if (!days.length || days.some(day => day < 0 || day > 6) || !/^([01]\d|2[0-3]):[0-5]\d$/.test(result.time || '')) {
      throw new Error('AI 返回的每周执行时间无效')
    }
  } else if (mode === 'monthly') {
    const day = Number(result.monthDay)
    if (day < 1 || day > 31 || !/^([01]\d|2[0-3]):[0-5]\d$/.test(result.time || '')) {
      throw new Error('AI 返回的每月执行时间无效')
    }
  }

  customCron.value = false
  customCronValue.value = ''
  schedule.mode = mode
  if (mode === 'interval') {
    schedule.interval.value = Number(result.intervalValue)
    schedule.interval.unit = result.intervalUnit
  } else if (mode === 'daily') {
    schedule.daily.time = result.time
  } else if (mode === 'weekly') {
    schedule.weekly.days = normalizedWeekdays(result.weekdays)
    schedule.weekly.time = result.time
  } else {
    schedule.monthly.day = Number(result.monthDay)
    schedule.monthly.time = result.time
  }
}

function formatRunTime(value) {
  const match = String(value || '').match(/^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}:\d{2})/)
  if (!match) return String(value || '').replace('T', ' ')
  const [, year, month, day, time] = match
  const weekday = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'][
    new Date(Date.UTC(Number(year), Number(month) - 1, Number(day))).getUTCDay()
  ]
  return `${Number(month)}/${Number(day)}${weekday} ${time}`
}

function confirmSchedule() {
  if (cronValidationError.value || !workingCron.value) return
  emit('confirm', workingCron.value)
  emit('update:show', false)
}
</script>

<style scoped>
.ai-panel {
  padding: 14px 16px;
  background: #f1faf6;
  border: 1px solid #8fd1b4;
  border-radius: 12px;
}

.ai-panel__title {
  font-size: 16px;
  font-weight: 700;
}

.ai-panel__description,
.manual-panel > p {
  margin: 7px 0 10px;
  color: #707a88;
  line-height: 1.55;
}

.ai-panel__input-row,
.expert-panel__input-row {
  display: flex;
  gap: 10px;
}

.ai-panel__input { flex: 1; }
.ai-panel__submit { width: 112px; flex: 0 0 112px; }

.ai-panel__examples {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
  margin-top: 10px;
}

.ai-panel__examples button {
  padding: 5px 9px;
  color: #16a05d;
  background: #eaf8f0;
  border: 0;
  border-radius: 7px;
  font: inherit;
  cursor: pointer;
}

.ai-panel__examples button:hover { background: #dcf3e6; }
.ai-panel__warning { margin: 9px 0 0; color: #b26a00; font-size: 12px; }

.manual-panel { padding-top: 14px; }
.manual-panel h3 { margin: 0; font-size: 17px; font-weight: 700; }

.mode-segment {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  overflow: hidden;
  border: 1px solid #dce2ea;
  border-radius: 9px;
}

.mode-segment button {
  height: 44px;
  padding: 0 12px;
  color: #242b35;
  background: #fff;
  border: 0;
  border-right: 1px solid #dce2ea;
  font: 600 14px/1 inherit;
  cursor: pointer;
}

.mode-segment button:last-child { border-right: 0; }
.mode-segment button:hover { background: #f7faf8; }
.mode-segment button.is-active { color: #169b5c; background: #eaf7f0; }

.schedule-form { padding: 10px 0 2px; }
.schedule-form__row { min-height: 48px; display: flex; align-items: center; }
.schedule-form__row--top { align-items: flex-start; padding-top: 8px; }
.schedule-form__label { width: 134px; flex: 0 0 134px; font-weight: 650; }
.schedule-form__content { flex: 1; min-width: 0; }
.schedule-form__inline { display: flex; align-items: center; gap: 9px; font-weight: 550; }
.field-number { width: 88px; }
.field-unit { width: 100px; }
.field-time { width: 104px; }
.field-timezone { width: 100%; }

.schedule-preview {
  padding: 15px;
  background: #edf6ff;
  border: 1px solid #8dbdf4;
  border-radius: 10px;
}

.schedule-preview--error { background: #fff2f0; border-color: #f2a49a; }
.schedule-preview__summary { font-weight: 700; }
.schedule-preview__times { display: flex; flex-wrap: wrap; gap: 0 18px; margin-top: 9px; color: #707b8a; }
.schedule-preview__times strong { color: #707b8a; }
.schedule-preview__error { margin-top: 8px; color: #d03050; }

.expert-panel {
  margin-top: 12px;
  border-top: 1px solid #e1e6ec;
}

.expert-panel summary {
  padding: 13px 0 15px;
  color: #687382;
  font-weight: 650;
  cursor: pointer;
  list-style-position: inside;
}

.expert-panel__content { padding: 0 0 18px; }
.expert-panel__input-row > :first-child { flex: 1; }
.expert-panel__error, .expert-panel__valid { margin: 7px 0; font-size: 12px; }
.expert-panel__error { color: #d03050; }
.expert-panel__valid { color: #18a058; }
.expert-panel__help { padding: 12px; color: #687382; background: #f7f8fa; border-radius: 9px; font-size: 12px; }
.expert-panel__help code { color: #263241; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.expert-panel__symbols, .expert-panel__examples { display: flex; flex-wrap: wrap; gap: 8px 18px; margin-top: 8px; }
.expert-panel__help p { margin: 9px 0 0; }

.footer-button { min-width: 76px; }
.footer-button--primary { min-width: 110px; }

:deep(.n-input),
:deep(.n-base-selection),
:deep(.n-input-number) {
  --n-border-radius: 8px !important;
}

:deep(.n-input__input-el) { text-align: left; }

:deep(.n-button) { --n-border-radius: 8px !important; }
:deep(.ai-panel__submit.n-button),
:deep(.footer-button--primary.n-button) { font-weight: 650; }

@media (max-width: 640px) {
  .ai-panel__input-row { flex-direction: column; }
  .ai-panel__submit { width: 100%; flex-basis: auto; }
  .mode-segment button { padding: 0 4px; font-size: 13px; }
  .schedule-form__row { align-items: flex-start; flex-direction: column; gap: 7px; padding: 8px 0; }
  .schedule-form__label { width: auto; flex-basis: auto; }
  .schedule-form__content { width: 100%; }
  .schedule-form__inline { flex-wrap: wrap; }
  .schedule-preview__times { flex-direction: column; gap: 4px; }
}
</style>
