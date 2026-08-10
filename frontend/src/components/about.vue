<script setup>
import { MdPreview } from 'md-editor-v3';
import 'md-editor-v3/lib/preview.css';
import {h, computed, nextTick, onBeforeUnmount, onMounted, ref} from 'vue';
import {CheckUpdate, GetConfig, GetVersionInfo,GetUserManual,OpenURL,RestartAsAdmin} from "../../wailsjs/go/main/App";
import {EventsOff, EventsOn,Environment} from "../../wailsjs/runtime";
import {NAvatar, NButton, NTree, useNotification} from "naive-ui";
import {isWebMode} from "../runtime-env";
const updateLog = ref('');
const versionInfo = ref('');
const icon = ref('https://raw.githubusercontent.com/ArvinLovegood/go-stock/master/build/appicon.png');
const notify = useNotification()
const showManual = ref(false)
const manualContent = ref('')
const manualId = 'manual-preview'
const darkTheme = ref(false)
const theme = computed(() => darkTheme.value ? 'dark' : 'light')
const manualScrollRef = ref(null)
const catalogList = ref([])
const webMode = isWebMode

const buildCatalogTree = (headings) => {
  if (!headings.length) return []
  const roots = []
  const stack = []
  for (const h of headings) {
    const node = { key: h.text, label: h.text, level: h.level, children: [] }
    while (stack.length && stack[stack.length - 1].level >= h.level) {
      stack.pop()
    }
    if (stack.length) {
      stack[stack.length - 1].children.push(node)
    } else {
      roots.push(node)
    }
    stack.push(node)
  }
  const clean = (nodes) => {
    for (const n of nodes) {
      if (n.children.length === 0) delete n.children
      else clean(n.children)
    }
  }
  clean(roots)
  return roots
}

const catalogTree = computed(() => buildCatalogTree(catalogList.value))

const onTreeSelect = (keys) => {
  if (keys.length) scrollToHeading(keys[0])
}

const slugifyHeading = (text) => {
  return text
    .trim()
    .replace(/[^\w\u4e00-\u9fff]/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '')
}

const extractCatalog = () => {
  if (!manualScrollRef.value) return
  const headings = manualScrollRef.value.querySelectorAll('h1, h2, h3, h4, h5, h6')
  catalogList.value = Array.from(headings).map(h => ({
    text: h.textContent?.trim() || '',
    level: parseInt(h.tagName.slice(1))
  }))
}

const scrollToHeading = (headingText) => {
  if (!manualScrollRef.value) return
  const container = manualScrollRef.value
  const headings = container.querySelectorAll('h1, h2, h3, h4, h5, h6')
  for (const h of headings) {
    const text = h.textContent?.trim()
    if (text === headingText) {
      const containerRect = container.getBoundingClientRect()
      const headingRect = h.getBoundingClientRect()
      container.scrollTop += headingRect.top - containerRect.top - 10
      return
    }
  }
}

const openManual = () => {
  if (!manualContent.value) {
    GetUserManual().then(res => {
      manualContent.value = res
      showManual.value = true
      nextTick(() => { setTimeout(extractCatalog, 500) })
    })
  } else {
    showManual.value = true
    nextTick(() => { setTimeout(extractCatalog, 300) })
  }
}

onMounted(() => {
  document.title = '关于软件';
  GetConfig().then(res => {
    darkTheme.value = res.darkTheme
  })
  GetVersionInfo().then((res) => {
    updateLog.value = res.content;
    versionInfo.value = res.version;
    icon.value = res.icon;
  });
})
onBeforeUnmount(() => {
  notify.destroyAll()
  EventsOff("updateVersion")
  EventsOff("updateNeedAdmin")
})

EventsOn("updateVersion",async (msg) => {
  const githubTimeStr = msg.published_at;
  const utcDate = new Date(githubTimeStr);
  const date = new Date(utcDate.getTime());
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  const hours = String(date.getHours()).padStart(2, '0');
  const minutes = String(date.getMinutes()).padStart(2, '0');
  const seconds = String(date.getSeconds()).padStart(2, '0');

  const formattedDate = `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;

  notify.info({
    avatar: () =>
        h(NAvatar, {
          size: 'small',
          round: false,
          src: icon.value
        }),
    title: '发现新版本: ' + msg.tag_name,
    content: () => {
      return h('div', {
        style: {
          'text-align': 'left',
          'font-size': '14px',
        }
      }, { default: () => msg.commit?.message })
    },
    duration: 5000,
    meta: "发布时间:"+formattedDate,
    action: () => {
      return h(NButton, {
        type: 'primary',
        size: 'small',
        onClick: () => {
          Environment().then(env => {
            switch (env.platform) {
              case 'windows':
                window.open(msg.html_url)
                break
              default :
                OpenURL(msg.html_url)
                break
            }
          })
        }
      }, { default: () => '查看' })
    }
  })
})

EventsOn("updateNeedAdmin", (msg) => {
  notify.warning({
    avatar: () =>
        h(NAvatar, {
          size: 'small',
          round: false,
          src: icon.value
        }),
    title: '更新需要管理员权限',
    content: () => {
      return h('div', {
        style: {
          'text-align': 'left',
          'font-size': '14px',
        }
      }, { default: () => '新版本 ' + (msg.version || '') + ' 下载完成，但自动替换文件需要管理员权限。请以管理员身份重启程序后再次检查更新。' })
    },
    duration: 15000,
    action: () => {
      return h(NButton, {
        type: 'warning',
        size: 'small',
        onClick: () => {
          RestartAsAdmin()
        }
      }, { default: () => '以管理员身份重启' })
    }
  })
})

</script>

<template>
      <n-space vertical size="large"  style="--wails-draggable:no-drag">
        <!-- 软件描述 -->
        <n-card size="large">
          <n-divider title-placement="center">关于软件</n-divider>
          <n-space vertical >
            <n-image width="100" :src="icon" />
            <h1>
              <n-badge :value="versionInfo" :offset="[80,10]" type="success">
                <n-gradient-text type="info" :size="50" >go-stock</n-gradient-text>
              </n-badge>
            </h1>
            <n-flex justify="center">
              <n-button v-if="!webMode" size="tiny" @click="CheckUpdate(1)" type="info" tertiary>检查更新</n-button>
              <n-button size="tiny" @click="openManual" type="success" tertiary >查看用户手册</n-button>
            </n-flex>
            <div style="justify-self: center;text-align: left" >
              <p>自选股行情实时监控，基于Wails和NaiveUI构建的AI赋能股票分析工具</p>
              <p>目前已支持A股，港股，美股，未来计划加入基金，ETF等支持</p>
              <p>支持DeepSeek，OpenAI， Ollama，LMStudio，AnythingLLM，<a href="https://cloud.siliconflow.cn/i/foufCerk" target="_blank">硅基流动</a>，<a href="https://www.volcengine.com/experience/ark?utm_term=202502dsinvite&ac=DSASUQY5&rc=IJSE43PZ" target="_blank">火山方舟</a>，阿里云百炼等平台或模型</p>
              <p>
                <i style="color: crimson">本软件仅供学习研究目的，AI分析结果仅供参考，本软件不提供任何投资建议或决策，风险自担！</i>
              </p>
              <p v-if="updateLog">更新说明：{{updateLog}}</p>
            </div>
          </n-space>
        </n-card>

        <n-modal
          v-model:show="showManual"
          preset="card"
          title="用户手册"
          style="width: 90vw; max-height: 90vh"
          :bordered="false"
          :segmented="{ content: true, footer: true }"
        >
          <div style="display: flex; max-height: 75vh;">
            <div v-if="catalogList.length" class="manual-catalog" style="width: 240px; min-width: 240px; border-right: 1px solid var(--n-border-color); padding: 8px 4px; overflow-y: auto;">
              <div style="font-weight: bold; margin-bottom: 8px; padding: 0 8px;">目录</div>
              <n-tree
                :data="catalogTree"
                :block-line="true"
                :block-node="true"
                :selectable="true"
                :cancelable="false"
                default-expand-all
                key-field="key"
                label-field="label"
                children-field="children"
                @update:selected-keys="onTreeSelect"
              />
            </div>
            <div ref="manualScrollRef" style="flex: 1; overflow-y: auto; padding: 0 16px;">
              <MdPreview style="text-align: left;" :id="manualId" v-model="manualContent" :theme="theme" :preview-theme="'github'" :md-heading-id="slugifyHeading" @onHtmlChanged="extractCatalog" />
            </div>
          </div>
        </n-modal>
      </n-space>
</template>

<style scoped>
/* 可以在这里添加一些样式 */
h1, h2 {
  margin: 0;
  padding: 6px 0;
}

p {
  margin: 2px 0;
}

ul {
  list-style-type: disc;
  padding-left: 20px;
}

a {
  color: #18a058;
  text-decoration: none;
}

a:hover {
  text-decoration: underline;
}

.manual-catalog > div:hover {
  color: #18a058;
}

.manual-catalog :deep(.n-tree-node-content) {
  text-align: left;
  justify-content: flex-start;
}

.manual-catalog :deep(.n-tree-node) {
  text-align: left;
}
</style>
