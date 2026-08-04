<script setup lang="ts">
import { computed } from 'vue'
import { X } from 'lucide-vue-next'
import CodeEditor from './CodeEditor.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'

// JS 脚本算子的示例代码，作为空编辑器的占位提示
const scriptPlaceholder = `// 过滤掉名称含「过期」的节点，并给美国节点加前缀
return proxies
  .filter((p) => !p.name.includes('过期'))
  .map((p) => {
    if (/US|美国/.test(p.name)) p.name = '[US] ' + p.name
    return p
  })`

// 处理管道编辑器：组合与单条订阅共用，
// 对外以 UI 模型（payload 为对象）双向绑定，与后端的 JSON 字符串格式由调用方转换。
const props = withDefaults(defineProps<{
  modelValue: any[]
  title?: string
}>(), {
  title: '配置数据处理管道',
})

const emit = defineEmits<{ (e: 'update:modelValue', v: any[]): void }>()

const ops = computed({
  get: () => props.modelValue,
  set: (v: any[]) => emit('update:modelValue', v),
})

// 为每个算子分配稳定标识：v-for 若用索引作 key，
// 删除中间项后 DOM 会被按位复用，导致输入框的值与算子类型错位。
const OP_LABELS: Record<string, string> = {
  filter: '节点过滤',
  rename: '正则改名',
  flag: '自动加国旗',
  set_property: '覆盖属性',
  region: '按地区筛选',
  useless: '剔除无效节点',
  regex_delete: '正则删除字符',
  sort: '名称排序',
  regex_sort: '关键词排序',
  resolve_domain: '域名解析为 IP',
  script: 'JS 脚本',
  quick_setting: '常用配置',
}
const opLabel = (type: string) => OP_LABELS[type] || type

// 「添加算子」按钮清单。accent 标记的是注入脚本，能力最强也最危险，
// 用蓝色与其余常规算子区分
const addButtons: Array<{ type: string; label: string; accent?: boolean }> = [
  { type: 'quick_setting', label: '➕ 常用配置' },
  { type: 'filter', label: '➕ 节点过滤' },
  { type: 'rename', label: '➕ 正则改名' },
  { type: 'flag', label: '➕ 自动加国旗' },
  { type: 'set_property', label: '➕ 覆盖节点属性' },
  { type: 'region', label: '➕ 按地区筛选' },
  { type: 'useless', label: '➕ 剔除无效节点' },
  { type: 'regex_delete', label: '➕ 正则删除字符' },
  { type: 'sort', label: '➕ 名称排序' },
  { type: 'regex_sort', label: '➕ 关键词排序' },
  { type: 'resolve_domain', label: '➕ 域名解析为 IP' },
  { type: 'script', label: '⚡ 注入 JS 脚本', accent: true },
]

let uidSeq = 0
const uidOf = (op: any): number => {
  if (op.__uid === undefined) op.__uid = ++uidSeq
  return op.__uid
}

const addOperator = (type: string) => {
  const defaults: Record<string, any> = {
    rename: { pattern: '', replace: '' },
    filter: { action: 'keep', pattern: '' },
    flag: {},
    set_property: { key: '', value: '', valType: 'string' },
    sort: { order: 'asc' },
    regex_sort: { patternsText: 'HK\nJP\nSG\nUS' },
    regex_delete: { pattern: '' },
    resolve_domain: { ipv6: false },
    region: { action: 'keep', regionsText: 'HK\nJP\nSG' },
    useless: {},
    script: { script: 'function operator(proxies) {\n  // your code here\n  return proxies;\n}' },
    quick_setting: {
      useless: 'DISABLED',
      udp: 'DEFAULT',
      scert: 'DEFAULT',
      tfo: 'DEFAULT',
      aead: 'DEFAULT',
      reuse: 'DEFAULT',
      block_quic: 'DEFAULT',
      ecn: 'DEFAULT',
      ip_version: 'DEFAULT',
      client_fingerprint: 'DEFAULT',
    },
  }
  if (!(type in defaults)) return
  emit('update:modelValue', [...props.modelValue, { type, enabled: true, payload: defaults[type] }])
}

const removeOperator = (index: number) => {
  const next = [...props.modelValue]
  next.splice(index, 1)
  emit('update:modelValue', next)
}
</script>

<template>
  <div class="border-t pt-5">
    <div class="flex items-center justify-between mb-4">
      <!-- 整个编辑器的标题，不对应任何单个控件，故用 h3 而非 label -->
      <h3 class="block text-sm font-semibold text-fg">{{ title }}</h3>
    </div>
    
    <!-- 十一个算子按钮：原先每个都重复一长串类名，抽成数组后配色只需维护一处 -->
    <div class="flex flex-wrap gap-2 mb-5">
      <Button
        v-for="btn in addButtons"
        :key="btn.type"
        type="button"
        size="sm"
        :variant="btn.accent ? 'default' : 'outline'"
        @click="addOperator(btn.type)"
      >
        {{ btn.label }}
      </Button>
    </div>

    <div class="space-y-3">
      <div v-if="ops.length === 0" class="text-sm text-fg-subtle italic px-2 py-4 bg-elevated rounded-lg text-center border border-dashed border-line">
        暂未添加处理步骤，源节点将直接被合并输出。
      </div>

      <!-- 窄屏纵向堆叠：算子名固定 w-28 加右侧表单，在手机上会把
           输入框压到只剩几十像素 -->
      <div v-for="(op, idx) in ops" :key="uidOf(op)" class="flex flex-col sm:flex-row items-start gap-2 sm:gap-4 bg-elevated p-3 sm:p-4 rounded-xl border border-line relative group shadow-sm transition-all hover:shadow-md">
        <!-- Label -->
        <div class="mt-1.5 text-sm font-mono font-bold sm:w-28 shrink-0 flex items-center gap-2">
          <span class="text-fg-subtle">{{ idx + 1 }}.</span>
          <span :class="op.type === 'script' ? 'text-primary' : 'text-indigo-600 dark:text-indigo-400'">{{ opLabel(op.type) }}</span>
          <!-- 窄屏时移除按钮跟在标题行右端，桌面端仍固定在行尾 -->
          <!-- aria-label 而非仅 title：title 在触屏上无法触发，部分读屏也会忽略 -->
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            class="tap-target ml-auto text-fg-subtle hover:text-rose-500 sm:hidden"
            :aria-label="`移除第 ${idx + 1} 个操作`"
            title="移除此操作"
            @click="removeOperator(idx)"
          >
            <!-- shadcn Button 的 [&_svg]:size-4 特异性高于 h-5 w-5，须加 ! 前缀 -->
            <X class="!h-5 !w-5" aria-hidden="true" />
          </Button>
        </div>

        <!-- Dynamic Forms -->
        <div class="flex-1 w-full min-w-0">
          <!-- Quick Setting Form -->
          <div v-if="op.type === 'quick_setting'" class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3 w-full">
            <div>
              <Label :for="`qs-${uidOf(op)}-useless`" class="text-xs text-fg-muted block mb-1">过滤非法节点</Label>
              <Select v-model="op.payload.useless">
                <SelectTrigger :id="`qs-${uidOf(op)}-useless`" class="text-sm w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="DISABLED">不剔除</SelectItem>
                  <SelectItem value="ENABLED">剔除</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label :for="`qs-${uidOf(op)}-udp`" class="text-xs text-fg-muted block mb-1">UDP 转发（全部协议）</Label>
              <Select v-model="op.payload.udp">
                <SelectTrigger :id="`qs-${uidOf(op)}-udp`" class="text-sm w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="DEFAULT">不改</SelectItem>
                  <SelectItem value="ENABLED">开启</SelectItem>
                  <SelectItem value="DISABLED">关闭</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label :for="`qs-${uidOf(op)}-scert`" class="text-xs text-fg-muted block mb-1">跳过证书验证（全部协议）</Label>
              <Select v-model="op.payload.scert">
                <SelectTrigger :id="`qs-${uidOf(op)}-scert`" class="text-sm w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="DEFAULT">不改</SelectItem>
                  <SelectItem value="ENABLED">开启</SelectItem>
                  <SelectItem value="DISABLED">关闭</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label :for="`qs-${uidOf(op)}-tfo`" class="text-xs text-fg-muted block mb-1">TCP Fast Open（全部协议）</Label>
              <Select v-model="op.payload.tfo">
                <SelectTrigger :id="`qs-${uidOf(op)}-tfo`" class="text-sm w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="DEFAULT">不改</SelectItem>
                  <SelectItem value="ENABLED">开启</SelectItem>
                  <SelectItem value="DISABLED">关闭</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label :for="`qs-${uidOf(op)}-aead`" class="text-xs text-fg-muted block mb-1">VMess AEAD（仅 VMess）</Label>
              <Select v-model="op.payload.aead">
                <SelectTrigger :id="`qs-${uidOf(op)}-aead`" class="text-sm w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="DEFAULT">不改</SelectItem>
                  <SelectItem value="ENABLED">开启</SelectItem>
                  <SelectItem value="DISABLED">关闭</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label :for="`qs-${uidOf(op)}-mptcp`" class="text-xs text-fg-muted block mb-1">连接复用（仅 Snell / AnyTLS / TrustTunnel）</Label>
              <Select v-model="op.payload.reuse">
                <SelectTrigger :id="`qs-${uidOf(op)}-mptcp`" class="text-sm w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="DEFAULT">不改</SelectItem>
                  <SelectItem value="ENABLED">开启</SelectItem>
                  <SelectItem value="DISABLED">关闭</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label :for="`qs-${uidOf(op)}-block_quic`" class="text-xs text-fg-muted block mb-1">阻止 QUIC（全部协议）</Label>
              <Select v-model="op.payload.block_quic">
                <SelectTrigger :id="`qs-${uidOf(op)}-block_quic`" class="text-sm w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="DEFAULT">不改</SelectItem>
                  <SelectItem value="auto">auto</SelectItem>
                  <SelectItem value="on">on</SelectItem>
                  <SelectItem value="off">off</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label :for="`qs-${uidOf(op)}-ecn`" class="text-xs text-fg-muted block mb-1">ECN（仅 TUIC / Hysteria2）</Label>
              <Select v-model="op.payload.ecn">
                <SelectTrigger :id="`qs-${uidOf(op)}-ecn`" class="text-sm w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="DEFAULT">不改</SelectItem>
                  <SelectItem value="ENABLED">开启</SelectItem>
                  <SelectItem value="DISABLED">关闭</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label :for="`qs-${uidOf(op)}-ipv`" class="text-xs text-fg-muted block mb-1">IP 版本（全部协议）</Label>
              <Select v-model="op.payload.ip_version">
                <SelectTrigger :id="`qs-${uidOf(op)}-ipv`" class="text-sm w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="DEFAULT">不改</SelectItem>
                  <SelectItem value="dual">双栈</SelectItem>
                  <SelectItem value="v4-only">仅 IPv4</SelectItem>
                  <SelectItem value="v6-only">仅 IPv6</SelectItem>
                  <SelectItem value="prefer-v4">IPv4 优先</SelectItem>
                  <SelectItem value="prefer-v6">IPv6 优先</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label :for="`qs-${uidOf(op)}-cfp`" class="text-xs text-fg-muted block mb-1">客户端指纹（仅 VLESS / VMess / Trojan / SS / Snell / AnyTLS）</Label>
              <Select v-model="op.payload.client_fingerprint">
                <SelectTrigger :id="`qs-${uidOf(op)}-cfp`" class="text-sm w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="DEFAULT">不改</SelectItem>
                  <SelectItem value="chrome">chrome</SelectItem>
                  <SelectItem value="firefox">firefox</SelectItem>
                  <SelectItem value="safari">safari</SelectItem>
                  <SelectItem value="ios">ios</SelectItem>
                  <SelectItem value="android">android</SelectItem>
                  <SelectItem value="edge">edge</SelectItem>
                  <SelectItem value="random">random</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <p class="text-xs text-fg-subtle sm:col-span-2 lg:col-span-3">
              不适用于当前节点协议的选项会被自动跳过，不影响其它字段。
              选「不改」时，Reality 节点仍会在输出阶段自动补上 chrome 指纹——
              内核要求 Reality 必须有 uTLS 指纹，缺失会导致节点连不上。
            </p>
          </div>

          <!-- Filter Form -->
          <div v-else-if="op.type === 'filter'" class="flex gap-2">
            <Select v-model="op.payload.action">
              <SelectTrigger class="text-sm w-28 shrink-0"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="keep">保留</SelectItem>
                <SelectItem value="drop">剔除</SelectItem>
              </SelectContent>
            </Select>
            <Input v-model="op.payload.pattern" class="text-sm flex-1 font-mono" placeholder="匹配节点的正则表达式 (如: HK|HongKong)" />
          </div>
          
          <!-- Rename Form -->
          <div v-else-if="op.type === 'rename'" class="flex flex-col md:flex-row gap-2">
            <Input v-model="op.payload.pattern" class="text-sm flex-1 font-mono" placeholder="匹配的正则表达式" />
            <span class="hidden md:flex items-center text-fg-subtle">➜</span>
            <Input v-model="op.payload.replace" class="text-sm flex-1 font-mono" placeholder="替换为" />
          </div>
          
          <!-- Set Property Form -->
          <div v-else-if="op.type === 'set_property'" class="flex flex-col md:flex-row gap-2">
            <Input v-model="op.payload.key" class="text-sm flex-1 font-mono" placeholder="属性名 (如: udp, skip-cert-verify, alpn)" />
            <span class="hidden md:flex items-center text-fg-subtle">=</span>
            <Input v-model="op.payload.value" class="text-sm flex-1 font-mono" placeholder="属性值" />
            <Select v-model="op.payload.valType">
              <SelectTrigger class="text-sm w-32 shrink-0"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="string">文本</SelectItem>
                <SelectItem value="boolean">布尔值</SelectItem>
                <SelectItem value="number">数字</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <!-- Region Filter -->
          <div v-else-if="op.type === 'region'" class="flex flex-col md:flex-row gap-2 w-full">
            <Select v-model="op.payload.action">
              <SelectTrigger class="text-sm md:w-32 shrink-0"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="keep">保留</SelectItem>
                <SelectItem value="drop">剔除</SelectItem>
              </SelectContent>
            </Select>
            <div class="flex-1">
              <Textarea v-model="op.payload.regionsText" class="w-full text-sm font-mono" rows="3" placeholder="每行一个地区代码，如 HK / JP / US" />
              <p class="text-xs text-fg-muted mt-1">
                支持：HK TW JP SG US KR UK DE FR CA AU RU IN TR NL（自动识别中英文与旗帜 emoji）
              </p>
            </div>
          </div>

          <!-- Sort -->
          <div v-else-if="op.type === 'sort'" class="flex items-center gap-3">
            <Select v-model="op.payload.order">
              <SelectTrigger class="text-sm w-40 shrink-0"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="asc">升序（A→Z）</SelectItem>
                <SelectItem value="desc">降序（Z→A）</SelectItem>
              </SelectContent>
            </Select>
            <span class="text-sm text-fg-muted">按节点名称排序</span>
          </div>

          <!-- Regex Sort -->
          <div v-else-if="op.type === 'regex_sort'" class="w-full">
            <Textarea v-model="op.payload.patternsText" class="w-full text-sm font-mono" rows="4" placeholder="每行一个关键词，越靠前优先级越高" />
            <p class="text-xs text-fg-muted mt-1">按关键词优先级排序，未命中的按名称排到末尾。</p>
          </div>

          <!-- Regex Delete -->
          <div v-else-if="op.type === 'regex_delete'" class="w-full">
            <Input v-model="op.payload.pattern" class="text-sm w-full font-mono" placeholder="要从名称中删除的正则，如 【[^】]*】" />
            <p class="text-xs text-fg-muted mt-1">仅删除名称中的匹配片段，不会删除节点本身。</p>
          </div>

          <!-- Resolve Domain -->
          <div v-else-if="op.type === 'resolve_domain'" class="flex items-center gap-3 h-9">
            <label class="flex items-center gap-2 text-sm text-fg-muted">
              <Checkbox v-model="op.payload.ipv6" />
              优先解析 IPv6
            </label>
            <span class="text-sm text-fg-muted">将节点域名解析为 IP 地址</span>
          </div>

          <!-- Useless -->
          <div v-else-if="op.type === 'useless'" class="flex items-center h-9">
            <span class="text-sm text-fg-muted">自动剔除「剩余流量 / 到期时间 / 官网」等机场信息类假节点。</span>
          </div>

          <!-- Script Form -->
          <div v-else-if="op.type === 'script'" class="w-full space-y-2">
            <p class="text-xs text-amber-700 dark:text-amber-400/90 bg-amber-50 dark:bg-amber-950/30 border border-amber-100 dark:border-amber-900/40 rounded px-2.5 py-1.5">
              JS 脚本只在登录后的即时预览、刷新与配置合并中执行；外部分享链接拉取时会自动跳过本算子（其它算子仍生效）。
            </p>
            <!-- 不再强制 dark：编辑器跟随全局主题，
                 浅色模式下嵌一块深色代码框与周围表单割裂 -->
            <CodeEditor
              v-model="op.payload.script"
              language="javascript"
              height="200px"
              :placeholder="scriptPlaceholder"
            />
            <p class="text-xs text-fg-muted mt-1.5">传入 <code>proxies</code> 数组，返回修改后的 <code>proxies</code> 数组。</p>
          </div>

          <!-- Flag Form (No inputs needed) -->
          <div v-else-if="op.type === 'flag'" class="flex items-center h-9">
            <span class="text-sm text-fg-muted">将基于常见名字缩写为节点添加国家/地区 Emoji 图标。</span>
          </div>
        </div>

        <!-- Remove Button -->
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          class="tap-target mt-0.5 text-fg-subtle hover:text-rose-500 hidden sm:inline-flex"
          :aria-label="`移除第 ${idx + 1} 个操作`"
          title="移除此操作"
          @click="removeOperator(idx)"
        >
          <X class="!h-5 !w-5" aria-hidden="true" />
        </Button>
      </div>
    </div>
  </div>
</template>
