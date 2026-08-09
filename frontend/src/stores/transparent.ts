import { defineStore } from 'pinia'
import { useNotifyStore } from './notify'
import api from '../api'
import { apiErrorMessage } from '../utils/apiError'
import router from '../router'

/** 透明代理模式。off 表示关闭 */
export type TransparentMode = 'off' | 'tun' | 'tproxy'

/** 单个模式的可用性结论 */
export interface TransparentModeStatus {
  mode: TransparentMode
  available: boolean
  /** 不可用的原因，面向用户，直接展示 */
  reason: string
  /** 缺失的依赖（命令名或内核特性） */
  missing: string[]
  /** 补齐依赖的具体命令，可复制执行 */
  installHint: string
}

/** 环境检测报告 */
export interface TransparentEnvReport {
  os: string
  arch: string
  kernel: string
  distro: string
  packageManager: string
  root: boolean
  capNetAdmin: boolean
  capNetRaw: boolean
  /**
   * 为 true 而 capNetAdmin 为 false，说明容器 cap_add 给了权限但当前进程
   * 拿不到（非 root 且二进制无 file capability）。这两种情况修复方式不同，
   * 界面上要区分提示。
   */
  capNetAdminBounding: boolean
  inContainer: boolean
  hostNetwork: boolean
  /** /dev/net/tun 的实际路径，不存在时为空 */
  tunDevice: string
  /**
   * iptables 命令的后端类型：nf_tables / legacy / 空。
   * 自定义防火墙规则按 iptables 语法执行，用户需要知道规则落在哪套后端
   * ——legacy 与 nftables 两套规则互不可见，写错地方等于没写。
   */
  iptablesBackend: string
  modes: TransparentModeStatus[]
  /** 不阻止启用但需要用户知道的问题 */
  warnings: string[]
}

/** 环境准备的单步结果 */
export interface TransparentProvisionStep {
  /** 步骤名，面向用户 */
  name: string
  /** 实际执行的命令，用于审计"面板到底动了什么" */
  command: string
  success: boolean
  /** 成功时是简要说明；失败时是命令原始输出，不要截断后再展示 */
  detail: string
  /** 无需执行（已满足条件） */
  skipped: boolean
}

/** 环境准备结果 */
export interface TransparentProvisionResult {
  success: boolean
  message: string
  steps: TransparentProvisionStep[]
  /** 改动会随重启或容器重建丢失，需提示用户 */
  notPersistent: boolean
  /** 等价手动命令。成功与否都会返回：失败时是兜底，成功时便于记进部署脚本 */
  manualCommands: string[]
  /** 执行后重新探测的环境报告 */
  env: TransparentEnvReport
}

export interface TransparentStatus {
  enabled: boolean
  mode: TransparentMode
  /** 刚启用、等待确认网络正常；超时未确认会自动回滚 */
  pendingConfirm: boolean
  /** 距自动回滚的剩余秒数 */
  secondsLeft: number
  tproxyPort: number
  tunStack: string
  /**
   * 基础配置里有 tproxy-port，但接管流量所需的防火墙规则与策略路由不是本面板
   * 下发的（enabled 因此为 false）。
   *
   * TProxy 生效需要两半：端口让内核监听，防火墙规则把流量引过去。后者只有面板
   * 能放上去，且在配置文件里没有任何痕迹——所以"填了端口"不等于"已接管"。
   * 用来提示用户确认自己是不是漏了一步（也可能他本就自己在管规则）。
   */
  portConfiguredOnly: boolean
  /**
   * 宿主上的防火墙规则与当前配置不一致（规则里烧进的端口已不是配置里的值）。
   *
   * 正常情况下合并末尾会自动重新下发规则，所以这个字段为 true 只发生在重下发
   * 失败时。此时内核听在新端口、规则还往旧端口投，流量会进黑洞，
   * 而用户刚看到的是「配置已生效」——必须显式提示。
   */
  rulesOutOfSync: boolean
  env: TransparentEnvReport
}

const emptyEnv = (): TransparentEnvReport => ({
  os: '',
  arch: '',
  kernel: '',
  distro: '',
  packageManager: '',
  root: false,
  capNetAdmin: false,
  capNetRaw: false,
  capNetAdminBounding: false,
  inContainer: false,
  hostNetwork: false,
  tunDevice: '',
  iptablesBackend: '',
  modes: [],
  warnings: [],
})

/**
 * 自定义防火墙规则与内置规则展示数据（GET /transparent/rules）。
 */
export interface TransparentRules {
  /** 用户自定义规则原文（未规范化，保留注释与空行） */
  customRules: string
  /** iptables 命令的后端类型：nf_tables / legacy / 空 */
  iptablesBackend: string
  /** 面板内置 nft 规则文本（按当前配置参数生成） */
  builtinNFTRules: string
  /** 策略路由命令（每行一条） */
  policyRoutes: string[]
  /** 宿主上实际生效的面板 nft 规则；TProxy 未开启时为空 */
  activeRules: string
}

/**
 * 透明代理开关。
 *
 * 两处与其它 store 不同的地方：
 *   - 状态里带环境检测结论，界面据此决定开关是否可操作。环境不支持时
 *     后端会拒绝写入，前端提前禁用只是为了不让用户白点一次。
 *   - 启用 TProxy 后进入待确认状态，需要在时限内确认网络正常，否则后端自动回滚。
 *     这里用本地倒计时驱动显示，真正的回滚由后端计时并持久化，
 *     前端刷新或关掉页面都不影响。TUN 不进入该状态（mihomo 自管规则、
 *     退出即清理），是否待确认一律以后端返回的 pendingConfirm 为准。
 */
/**
 * 状态接口应在数百毫秒内返回；给足冗余但绝不能无限等。
 * 浏览器对同一主机默认只有约 6 条并行连接，一条挂死的
 * /transparent/status 会拖垮文档懒加载、其它 API，表现为
 * 「开关灰掉 + 点文档没反应」。
 *
 * 超时刻意短于常见代理/中间层默认值：超时后必须释放连接并允许重试，
 * 不能无限占着浏览器连接池。
 */
const STATUS_TIMEOUT_MS = 8_000
/** 超时或网络失败时自动再试一次（新 TCP，避开脏 keep-alive） */
const STATUS_MAX_ATTEMPTS = 2

/** 模块级单飞，不放 pinia state：Promise 不宜做响应式字段 */
let fetchInflight: Promise<void> | null = null
/** 可中断进行中的 status 请求（重试时 abort 旧请求） */
let fetchAbort: AbortController | null = null

export const useTransparentStore = defineStore('transparent', {
  state: () => ({
    status: {
      enabled: false,
      mode: 'off' as TransparentMode,
      pendingConfirm: false,
      secondsLeft: 0,
      tproxyPort: 7893,
      tunStack: 'mixed',
      portConfiguredOnly: false,
      rulesOutOfSync: false,
      env: emptyEnv(),
    } as TransparentStatus,
    loading: false,
    saving: false,
    /**
     * 是否至少成功拉取过一次环境报告。
     *
     * 初始 modes 是空数组，anyAvailable 为 false——若在首次成功响应前
     * 就渲染「不具备条件」，会把「还在加载 / 请求挂起」误判成环境不行，
     * 开关被灰掉且用户找不到真正原因（F12 里 status 一直 pending）。
     */
    envReady: false,
    /** 最近一次 fetch 失败文案；成功时清空 */
    fetchError: '' as string,
    /** 正在执行环境准备。装包可能几十秒，按钮需要禁用并给出反馈 */
    provisioning: false,
    /**
     * 最近一次环境准备的结果。保留在 store 里而不是执行完就丢：
     * 失败时用户需要对着命令原始输出排查，切走再回来不该丢失。
     */
    provisionResult: null as TransparentProvisionResult | null,
    /**
     * 自定义防火墙规则与内置规则展示数据。独立于 status 加载：
     * status 每次请求都做实时环境检测（读 /proc、exec 子进程），
     * 规则数据只在需要编辑/查看时拉取。
     */
    rules: null as TransparentRules | null,
    /** 自定义规则是否正在保存（按钮禁用与文案反馈） */
    savingRules: false,
    /** 本地倒计时定时器 id */
    timer: 0 as unknown as ReturnType<typeof setInterval> | 0,
  }),
  getters: {
    /** 有任一模式可用；都不可用时开关禁用（仅 envReady 后有意义） */
    anyAvailable(state): boolean {
      return state.status.env.modes.some((m) => m.available)
    },
    /**
     * 开关是否应禁用。
     * 加载中、未就绪、无可用模式、保存中都不可操作；
     * 与「不具备条件」文案解耦——后者只在 envReady && !anyAvailable 时出现。
     */
    switchDisabled(state): boolean {
      if (state.saving || state.loading || !state.envReady) return true
      return !state.status.env.modes.some((m) => m.available)
    },
    /** 可选模式列表，供界面渲染单选 */
    availableModes(state): TransparentModeStatus[] {
      return state.status.env.modes.filter((m) => m.available)
    },
    /** 取指定模式的结论，用于展示不可用原因 */
    modeStatus(state) {
      return (mode: TransparentMode): TransparentModeStatus | undefined =>
        state.status.env.modes.find((m) => m.mode === mode)
    },
    /**
     * 是否有缺失的软件包可以自动装。
     *
     * 判据是"有模式因为缺工具而不可用"，而不是简单看 modes 里有没有
     * 不可用项：缺 /dev/net/tun 那种装包也解决不了（要改 compose 映射设备），
     * 给按钮反而误导。
     */
    canInstallPackages(state): boolean {
      return state.status.env.modes.some((m) => !m.available && m.installHint !== '')
    },
    /**
     * sysctl 是否有可调项。
     *
     * 后端的告警文案里带 sysctl 键名，据此判断——比在前端复刻一遍
     * "哪些值算不合规"的规则可靠，那种复刻一定会跟后端漂移。
     */
    canApplySysctl(state): boolean {
      return state.status.env.warnings.some((w) => w.includes('ip_forward') || w.includes('rp_filter'))
    },
    /** 只要有一类可做，就显示自动准备入口 */
    canProvision(): boolean {
      return this.canInstallPackages || this.canApplySysctl
    },
  },
  actions: {
    /**
     * 拉取透明代理状态。
     *
     * @param opts.force 为 true 时中断进行中的请求再发新请求（「重试」按钮）。
     *   默认合并并发：Dashboard / Settings / 倒计时归零可能同时触发。
     */
    async fetch(opts?: { force?: boolean }) {
      if (opts?.force && fetchAbort) {
        fetchAbort.abort()
        fetchAbort = null
        fetchInflight = null
      }
      if (fetchInflight) {
        return fetchInflight
      }
      this.loading = true
      this.fetchError = ''
      const ac = new AbortController()
      fetchAbort = ac
      // 先占位再赋值真实 Promise：finally 里用身份比较，避免 force 后旧轮清新轮
      let settled: Promise<void> | null = null
      settled = (async () => {
        try {
          // 关键路径用原生 fetch：与共享 axios 实例的 keep-alive 队列解耦。
          // 外层每尝试独立 timeout；失败再试一次，避开半死不活的复用连接。
          // Authorization 与 api 拦截器保持一致。
          let lastErr: unknown = null
          for (let attempt = 1; attempt <= STATUS_MAX_ATTEMPTS; attempt++) {
            if (ac.signal.aborted && fetchInflight !== settled) {
              return
            }
            // 每轮独立 timeout controller，与 force 的外层 ac 组合
            const attemptAc = new AbortController()
            const onParentAbort = () => attemptAc.abort()
            ac.signal.addEventListener('abort', onParentAbort)
            const timer = window.setTimeout(() => attemptAc.abort(), STATUS_TIMEOUT_MS)
            try {
              const token = localStorage.getItem('aurora_token')
              const headers: Record<string, string> = {
                Accept: 'application/json',
                // 提示中间层/浏览器不要复用可能已脏的连接（fetch 可能忽略，后端也会 close）
                Connection: 'close',
              }
              if (token) headers.Authorization = `Bearer ${token}`
              const res = await fetch('/api/v1/transparent/status', {
                method: 'GET',
                headers,
                signal: attemptAc.signal,
                cache: 'no-store',
              })
              if (!res.ok) {
                // 401 = token 已失效：与 api 拦截器对齐——清 token、静默跳登录。
                // 这里用原生 fetch，不经 axios 拦截器，必须自己处理，
                // 否则用户会看到「加载透明代理状态失败（HTTP 401）」的 toast，
                // 而且 401 重试多少次都一样，直接结束本轮请求。
                if (res.status === 401) {
                  localStorage.removeItem('aurora_token')
                  if (router.currentRoute.value.name !== 'login') {
                    router.push({
                      name: 'login',
                      query: { redirect: router.currentRoute.value.fullPath },
                    })
                  }
                  return
                }
                let detail = ''
                try {
                  const ct = res.headers.get('content-type') || ''
                  if (ct.includes('application/json')) {
                    const body: unknown = await res.json()
                    if (body && typeof body === 'object') {
                      const rec = body as Record<string, unknown>
                      for (const k of ['message', 'msg'] as const) {
                        const v = rec[k]
                        if (typeof v === 'string' && v.trim()) {
                          detail = v.trim()
                          break
                        }
                      }
                    }
                  } else {
                    detail = (await res.text()).trim()
                  }
                } catch {
                  /* ignore body parse */
                }
                throw new Error(detail || `加载透明代理状态失败（HTTP ${res.status}）`)
              }
              const data = (await res.json()) as TransparentStatus
              // 不用名为 apply 的 action：与 Function.prototype.apply 撞名时，
              // 部分打包/代理路径下 this.apply(data) 会变成「把 store 当函数调用」而抛错，
              // 表现为接口 200 但界面一直停在初始空 env（不具备条件）。
              this.applyStatus(data)
              this.envReady = true
              this.fetchError = ''
              return
            } catch (e: unknown) {
              lastErr = e
              // 被 force 整轮取消：直接结束，不报错
              if (ac.signal.aborted && fetchInflight !== settled) {
                return
              }
              // 还有重试机会：短暂让出事件循环再发，给连接池腾槽
              if (attempt < STATUS_MAX_ATTEMPTS) {
                await new Promise((r) => window.setTimeout(r, 200))
                continue
              }
            } finally {
              window.clearTimeout(timer)
              ac.signal.removeEventListener('abort', onParentAbort)
            }
          }
          // 两轮都失败
          if (ac.signal.aborted && fetchInflight !== settled) {
            return
          }
          const timedOut =
            lastErr != null &&
            (lastErr instanceof DOMException ||
              (lastErr instanceof Error && /abort|timeout/i.test(lastErr.message)))
          const msg = timedOut
            ? '请求超时，请稍后重试'
            : apiErrorMessage(lastErr, '加载透明代理状态失败')
          this.fetchError = msg
          useNotifyStore().error(msg)
        } finally {
          // 只收尾本轮：force 后旧 finally 不得清掉新 Promise / 提前 loading=false
          if (fetchInflight === settled) {
            fetchInflight = null
            fetchAbort = null
            this.loading = false
          }
        }
      })()
      fetchInflight = settled
      return settled
    },

    async update(payload: {
      enabled: boolean
      mode: TransparentMode
      tproxyPort?: number
      tunStack?: string
    }) {
      this.saving = true
      try {
        const res = await api.put<TransparentStatus>('/transparent', payload, {
          skipErrorToast: true,
          timeout: STATUS_TIMEOUT_MS,
        })
        this.applyStatus(res.data)
        this.envReady = true
        // 开关动的是 base.yaml 里的 tun.enable / tproxy-port，而「配置中心」
        // 把 base.yaml 缓存在自己的 model 里。不刷新的话，用户切到 TProxy 后
        // 去配置中心会看到 TUN 开关仍是开着的（缓存里的旧值），更糟的是他在
        // 那页点保存就会把这份过期配置整份写回去，把刚做的切换覆盖掉。
        //
        // 用动态 import 而不是顶层 import：config store 已经 import 了本模块
        // （保存后要回头刷新开关状态），顶层互引会形成循环依赖。
        await this.refreshConfigCenter()
        if (payload.enabled) {
          // 只有进入确认窗口时才提"请确认"：TUN 不开窗口，
          // 对它说这句话会让用户去找一个不存在的确认按钮。
          // 以后端返回的 pendingConfirm 为准，而不是按模式猜。
          useNotifyStore().success(
            this.status.pendingConfirm
              ? '透明代理已启用，请确认网络正常'
              : '透明代理已启用',
          )
        } else {
          useNotifyStore().success('透明代理已关闭')
        }
        return true
      } catch (e: unknown) {
        // 后端会把"缺什么、怎么补"放在错误里，原样展示才有可操作性
        useNotifyStore().error(apiErrorMessage(e, '设置透明代理失败'))
        return false
      } finally {
        this.saving = false
      }
    },

    async confirm() {
      try {
        const res = await api.post<TransparentStatus>('/transparent/confirm', null, {
          skipErrorToast: true,
          timeout: STATUS_TIMEOUT_MS,
        })
        this.applyStatus(res.data)
        this.envReady = true
        useNotifyStore().success('已确认，自动回滚取消')
        return true
      } catch (e: unknown) {
        useNotifyStore().error(apiErrorMessage(e, '确认失败'))
        return false
      }
    },

    /**
     * 尝试自动补齐环境（装依赖 / 调 sysctl）。
     *
     * 不改开关状态、不动防火墙，只把环境从"不可用"推向"可用"，
     * 所以不涉及那套 90 秒确认窗口。
     *
     * 后端返回的 env 是执行后重新探测的结果，这里直接写回 status.env：
     * 用户点完按钮应当立刻看到模式是否变为可用，而不用自己再刷一次。
     */
    async provision(opts: { installPackages: boolean; applySysctl: boolean }) {
      this.provisioning = true
      // 先清掉上次结果，避免新一轮执行期间界面上还挂着旧的成功/失败信息
      this.provisionResult = null
      try {
        const res = await api.post<TransparentProvisionResult>('/transparent/provision', opts, {
          skipErrorToast: true,
        })
        this.provisionResult = res.data
        if (res.data?.env) {
          this.status.env = res.data.env
        }
        const notify = useNotifyStore()
        if (res.data?.success) {
          notify.success(res.data.message || '环境准备完成')
        } else {
          // 部分失败也走 error：成功一半仍需要用户处理剩下那半，
          // 用 success 提示会让人以为不用管了
          notify.error(res.data?.message || '环境准备未完全成功，详情见下方步骤')
        }
        return res.data?.success === true
      } catch (e: unknown) {
        // 后端把"为什么不能做"（非 root、非 Linux 等）放在错误里，原样展示
        useNotifyStore().error(apiErrorMessage(e, '环境准备失败'))
        return false
      } finally {
        this.provisioning = false
      }
    },

    /**
     * 拉取自定义防火墙规则与内置规则展示数据（GET /transparent/rules）。
     *
     * 与 status 分开请求：status 每次调用都做实时环境检测（读 /proc、
     * exec 出 ip/iptables 子进程），而规则数据只在编辑/查看时有用，
     * 为一次展示付一次环境检测的代价不成比例。
     */
    async fetchRules() {
      try {
        const res = await api.get<TransparentRules>('/transparent/rules', {
          skipErrorToast: true,
        })
        this.rules = res.data
      } catch (e: unknown) {
        useNotifyStore().error(apiErrorMessage(e, '加载防火墙规则失败'))
      }
    },

    /**
     * 保存自定义防火墙规则（PUT /transparent/rules）。
     *
     * 后端会校验每行格式并在 TProxy 运行中时立即重新应用；
     * 保存成功后重新拉取展示数据（内置规则文本可能随参数变化）。
     */
    async saveRules(text: string): Promise<boolean> {
      this.savingRules = true
      try {
        const res = await api.put<{ message?: string }>(
          '/transparent/rules',
          { customRules: text },
          { skipErrorToast: true },
        )
        // 成功文案以后端为准：重应用失败时后端会返回错误，不会走到这里
        useNotifyStore().success(res.data?.message || '自定义防火墙规则已保存')
        await this.fetchRules()
        // 重应用成功/跳过都可能改变 rulesOutOfSync，顺带刷新状态
        if (this.status.enabled && this.status.mode === 'tproxy') {
          await this.fetch()
        }
        return true
      } catch (e: unknown) {
        useNotifyStore().error(apiErrorMessage(e, '保存防火墙规则失败'))
        return false
      } finally {
        this.savingRules = false
      }
    },

    /**
     * 让「配置中心」重新读一遍 base.yaml。
     *
     * 透明代理开关与配置中心编辑的是同一份文件（tun.enable / tproxy-port），
     * 这里改完必须让那边的缓存失效，否则两个页面会各说各话——而配置中心是整份
     * 写回的，拿着旧缓存保存会直接覆盖掉本次切换。
     *
     * 只在它已经加载过时才刷新：没加载过说明用户还没去过那一页，
     * 届时 onMounted 自会拉取，这里提前请求只是白费一次往返。
     *
     * 失败不向上传播：本次开关切换已经成功了，因为"另一个页面的缓存没刷上"
     * 而报错会让用户以为切换失败。fetchBase 自身已有错误提示与
     * baseLoaded=false 的保护（那个状态会禁止保存，正是我们要的兜底）。
     */
    async refreshConfigCenter() {
      try {
        const { useConfigStore } = await import('./config')
        const cfg = useConfigStore()
        if (cfg.baseLoaded) {
          await cfg.fetchBase()
        }
      } catch {
        // 动态 import 或刷新失败都不影响本次切换的结果，保持静默
      }
    },

    /** 写入状态并按需启动/停止倒计时 */
    applyStatus(s: TransparentStatus | undefined) {
      if (!s) return
      // env.modes 必须是数组：后端约定空切片而非 null，但仍防御一次，
      // 避免 anyAvailable 的 .some 在异常响应上抛错把整页弄挂。
      const env = s.env ? { ...s.env, modes: s.env.modes || [], warnings: s.env.warnings || [] } : emptyEnv()
      this.status = { ...s, env }
      if (this.status.pendingConfirm && this.status.secondsLeft > 0) {
        this.startCountdown()
      } else {
        this.stopCountdown()
      }
    },

    startCountdown() {
      this.stopCountdown()
      this.timer = setInterval(() => {
        if (this.status.secondsLeft > 0) {
          this.status.secondsLeft -= 1
        }
        if (this.status.secondsLeft <= 0) {
          this.stopCountdown()
          // 倒计时归零说明后端应已回滚，拉一次真实状态而不是自行推断：
          // 回滚是否成功、当前是什么模式，只有后端知道
          void this.fetch()
        }
      }, 1000)
    },

    stopCountdown() {
      if (this.timer) {
        clearInterval(this.timer as ReturnType<typeof setInterval>)
        this.timer = 0
      }
    },
  },
})
