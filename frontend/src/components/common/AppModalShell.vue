<template>
  <n-modal
    :show="show"
    :mask-closable="maskClosable"
    :z-index="zIndex"
    transform-origin="center"
    @update:show="handleShowChange"
  >
    <!--
      n-modal 的直属子节点必须是实际 DOM 元素。这里不要再包一层组件或 Fragment，
      否则 Naive UI/vueuc 的 focus trap 可能找不到可聚焦根节点，导致输入框无法编辑。
    -->
    <div
      class="app-modal-shell"
      role="dialog"
      aria-modal="true"
      :aria-label="ariaLabel || title"
      :style="{ '--app-modal-width': width }"
    >
      <header class="app-modal-shell__header">
        <h2><slot name="title">{{ title }}</slot></h2>
        <button class="app-modal-shell__close" type="button" aria-label="关闭" @click="handleShowChange(false)">×</button>
      </header>

      <main class="app-modal-shell__body">
        <slot />
      </main>

      <footer v-if="$slots.footer" class="app-modal-shell__footer">
        <slot name="footer" />
      </footer>
    </div>
  </n-modal>
</template>

<script setup>
/**
 * 通用弹框只管理稳定的窗口骨架和显隐事件。
 * 校验、提交、loading 和业务面板必须由调用方持有，避免公共组件反向依赖业务领域。
 */
defineProps({
  show: { type: Boolean, default: false },
  title: { type: String, required: true },
  ariaLabel: { type: String, default: '' },
  width: { type: String, default: '760px' },
  maskClosable: { type: Boolean, default: false },
  zIndex: { type: Number, default: undefined }
})

const emit = defineEmits(['update:show'])

function handleShowChange(value) {
  emit('update:show', value)
}
</script>

<style scoped>
.app-modal-shell {
  width: min(var(--app-modal-width), calc(100vw - 32px));
  max-height: min(920px, calc(100vh - 32px));
  display: flex;
  flex-direction: column;
  overflow: hidden;
  color: #202733;
  background: #fff;
  border: 1px solid #dde4ec;
  border-radius: 18px;
  box-shadow: 0 24px 70px rgb(15 23 42 / 22%);
  font-size: 14px;
  text-align: left;
}

.app-modal-shell__header {
  min-height: 68px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  border-bottom: 1px solid #e2e7ee;
}

.app-modal-shell__header h2 {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
  letter-spacing: .01em;
}

.app-modal-shell__close {
  width: 34px;
  height: 34px;
  padding: 0;
  color: #697381;
  background: transparent;
  border: 0;
  border-radius: 9px;
  font: 300 32px/30px Arial, sans-serif;
  cursor: pointer;
}

.app-modal-shell__close:hover { background: #f3f5f7; }

.app-modal-shell__body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 16px 24px 0;
}

.app-modal-shell__footer {
  min-height: 72px;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  padding: 0 24px;
  background: #fff;
  border-top: 1px solid #e2e7ee;
}

@media (max-width: 640px) {
  .app-modal-shell {
    width: calc(100vw - 20px);
    max-height: calc(100vh - 20px);
    border-radius: 14px;
  }

  .app-modal-shell__header,
  .app-modal-shell__footer {
    padding-left: 16px;
    padding-right: 16px;
  }

  .app-modal-shell__body { padding: 16px 16px 0; }
}
</style>
