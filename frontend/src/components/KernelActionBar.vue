<script setup lang="ts">
import { computed, ref } from 'vue'
import { Button } from '@/components/ui/button'
import { DialogDescription } from '@/components/ui/dialog'
import ModalDialog from './ModalDialog.vue'
import { useMihomoStore } from '../stores/mihomo'
import { useNotifyStore } from '../stores/notify'
import api from '../api'

/**
 * 内核操作条：启动 / 停止 / 重启 / 重载配置，可选更新内核版本。
 *
 * 停止与重启会中断当前代理连接，必须先走 ModalDialog 二次确认，
 * 禁止 window.confirm。确认前不得调用 store 的 stop/restart。
 *
 * 控制台可用 compact 缩小按钮；内核管理页可 showUpdate 露出更新按钮。
 */
withDefaults(
  defineProps<{
    showUpdate?: boolean
    compact?: boolean
  }>(),
  {
    showUpdate: false,
    compact: false,
  },
)

const store = useMihomoStore()
const notify = useNotifyStore()

/** 等待确认的危险操作；null 表示对话框关闭 */
const pendingAction = ref<'stop' | 'restart' | null>(null)
const updating = ref(false)

const confirmOpen = computed(() => pendingAction.value !== null)

const confirmTitle = computed(() =>
  pendingAction.value === 'restart' ? '确认重启' : '确认停止',
)

const confirmMessage = computed(() =>
  pendingAction.value === 'restart'
    ? '重启内核会短暂中断所有代理连接，确定重启？'
    : '停止内核将中断所有正在进行的代理连接，确定停止？',
)

/** compact 时控制台用小按钮 */
function sizeFor(compact: boolean): 'sm' | 'default' {
  return compact ? 'sm' : 'default'
}

function requestStop() {
  pendingAction.value = 'stop'
}

function requestRestart() {
  pendingAction.value = 'restart'
}

function closeConfirm() {
  pendingAction.value = null
}

async function confirmOk() {
  const action = pendingAction.value
  pendingAction.value = null
  if (action === 'stop') await store.stop()
  else if (action === 'restart') await store.restart()
}

/** 与旧 MihomoView.updateCore 同路径：先 info toast，再 success/error */
async function updateCore() {
  updating.value = true
  notify.push('info', '正在下载最新内核…')
  try {
    const res = await api.post('/update/mihomo')
    const text =
      (res.data as { message?: string; success?: boolean } | undefined)?.message || '更新完成'
    const success = (res.data as { success?: boolean } | undefined)?.success
    if (success === false) notify.error(text)
    else notify.success(text)
    await store.fetchStatus()
  } catch (e: unknown) {
    const err = e as { response?: { data?: { message?: string } } }
    notify.error(err?.response?.data?.message || '更新失败')
  } finally {
    updating.value = false
  }
}
</script>

<template>
  <div class="flex flex-wrap gap-2">
    <Button
      data-testid="kernel-start"
      :size="sizeFor(compact)"
      @click="store.start()"
    >
      启动
    </Button>
    <Button
      data-testid="kernel-stop"
      variant="destructive"
      :size="sizeFor(compact)"
      @click="requestStop"
    >
      停止
    </Button>
    <Button
      data-testid="kernel-restart"
      variant="outline"
      :size="sizeFor(compact)"
      @click="requestRestart"
    >
      重启
    </Button>
    <Button
      data-testid="kernel-reload"
      variant="outline"
      :size="sizeFor(compact)"
      @click="store.reload()"
    >
      重载配置
    </Button>
    <Button
      v-if="showUpdate"
      data-testid="kernel-update"
      variant="outline"
      :size="sizeFor(compact)"
      :disabled="updating"
      @click="updateCore"
    >
      {{ updating ? '更新中…' : '更新内核版本' }}
    </Button>
  </div>

  <!-- 危险操作确认：正文 + 取消/确定都在默认 slot（与项目 ModalDialog 约定一致） -->
  <ModalDialog
    :open="confirmOpen"
    :title="confirmTitle"
    max-width="max-w-md"
    @close="closeConfirm"
  >
    <DialogDescription class="text-sm text-fg mb-5">
      {{ confirmMessage }}
    </DialogDescription>
    <div class="flex justify-end gap-2">
      <Button
        data-testid="kernel-confirm-cancel"
        variant="outline"
        @click="closeConfirm"
      >
        取消
      </Button>
      <Button
        data-testid="kernel-confirm-ok"
        :variant="pendingAction === 'stop' ? 'destructive' : 'default'"
        @click="confirmOk"
      >
        确定
      </Button>
    </div>
  </ModalDialog>
</template>
