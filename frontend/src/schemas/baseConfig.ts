export type FieldType = 'switch' | 'text' | 'number' | 'select' | 'textarea' | 'string-array' | 'yaml-object' | 'hosts-map'

/** 下拉选项：value 是写入配置的真实值，label 仅用于界面展示 */
export interface FieldOption {
  value: string
  label: string
}

export interface FormField {
  key: string
  title: string
  type: FieldType
  /** 字符串写法等价于 { value: x, label: x } */
  options?: (string | FieldOption)[]
  help?: string
  /**
   * 输入框占位提示，用于展示官方默认值或填写示例。
   * 只是提示，不会被写入配置：留空即代表「不设置该参数」，
   * 由 mihomo 内核自己回落到默认值。
   */
  placeholder?: string
  /**
   * 标记该字段为源码编辑区，值为语法高亮所用的语言。
   * 设置后视图会用 CodeMirror 编辑器替代普通 textarea。
   */
  code?: 'yaml' | 'javascript'
  /** 源码编辑器高度，仅在 code 字段上生效 */
  codeHeight?: string
}

/** 归一化下拉选项，供视图统一渲染 */
export function normalizeOptions(options?: (string | FieldOption)[]): FieldOption[] {
  return (options || []).map(o => (typeof o === 'string' ? { value: o, label: o } : o))
}

export interface FormSection {
  id: string
  title: string
  fields: FormField[]
}

export const baseConfigSchema: FormSection[] = [
  {
    id: 'general',
    title: '通用设置',
    fields: [
      {
        key: 'mode',
        title: '运行模式',
        type: 'select',
        options: [
          { value: '', label: '未设置（使用内核默认）' },
          { value: 'rule', label: '规则模式' },
          { value: 'global', label: '全局代理' },
          { value: 'direct', label: '全局直连' },
        ],
        help: '规则模式按 rules 分流；全局代理让所有流量走同一节点；全局直连不使用代理。官方默认 rule。',
      },
      {
        key: 'log-level',
        title: '日志级别',
        type: 'select',
        options: [
          { value: '', label: '未设置（使用内核默认）' },
          { value: 'silent', label: '静默' },
          { value: 'error', label: '仅错误' },
          { value: 'warning', label: '警告' },
          { value: 'info', label: '信息' },
          { value: 'debug', label: '调试' },
        ],
        help: '日志详细程度。排查问题时用「调试」，日常建议「信息」或「警告」。官方默认 info。',
      },
      { key: 'allow-lan', title: '允许局域网连接 (Allow LAN)', type: 'switch', help: '开启后局域网内其他设备可通过本机代理端口上网。官方默认关闭；开启前请确认网络环境可信。' },
      { key: 'bind-address', title: '绑定地址', type: 'text', placeholder: '*', help: 'allow-lan 开启时限制监听地址。* 表示所有网卡，也可填具体 IPv4/IPv6 地址。官方默认 *。' },
      { key: 'ipv6', title: '启用 IPv6', type: 'switch', help: '关闭后内核不处理 IPv6 流量，AAAA 查询将返回空。官方默认开启。' },
      {
        key: 'find-process-mode',
        title: '进程匹配模式 (Find Process Mode)',
        type: 'select',
        options: [
          { value: '', label: '未设置（使用内核默认）' },
          { value: 'always', label: 'always（强制匹配所有进程）' },
          { value: 'strict', label: 'strict（默认，由内核判断）' },
          { value: 'off', label: 'off（不匹配进程，路由器推荐）' },
        ],
        help: '路由器等无进程信息的环境建议选择 off；strict 为官方默认。',
      },
      { key: 'global-client-fingerprint', title: '全局客户端指纹 (Global Client Fingerprint，已废弃)', type: 'text', placeholder: '建议留空', help: '该配置已被 Mihomo 移除，填了不生效，内核启动时只会打一条错误日志。TLS 指纹改为按节点设置 client-fingerprint：Reality 节点缺指纹时本平台会自动补 chrome，也可在订阅的「常用配置」算子里显式指定。' },
      { key: 'tcp-concurrent', title: 'TCP 并发连接 (TCP Concurrent)', type: 'switch', help: '并发连接所有解析出的 IP，使用最快握手成功的连接。官方默认关闭。' },
      { key: 'unified-delay', title: '统一延迟计算 (Unified Delay)', type: 'switch', help: '统一延迟计算方式，抵消不同协议握手差异，让节点延迟更有可比性。官方默认关闭。' },
      { key: 'interface-name', title: '出口网卡 (Interface Name)', type: 'text', placeholder: 'eth0', help: '指定出口网卡名称（如 en0、eth0）。留空由系统自动选择。' },
      { key: 'routing-mark', title: '路由标记 (Routing Mark，仅 Linux)', type: 'number', placeholder: '留空表示不设置', help: '出站流量附加的路由标记，仅 Linux 生效。留空表示不设置。' },
      { key: 'disable-keep-alive', title: '禁用 TCP Keep-Alive', type: 'switch', help: '禁用 TCP keep-alive。部分环境下可减少空连接开销，Android 上强制为开。官方默认关闭。' },
      { key: 'keep-alive-idle', title: 'Keep-Alive 空闲时间 (秒)', type: 'number', placeholder: '15', help: 'TCP keep-alive 最大空闲时间（秒），官方默认 15。' },
      { key: 'keep-alive-interval', title: 'Keep-Alive 探测间隔 (秒)', type: 'number', placeholder: '15', help: 'TCP keep-alive 探测包发送间隔（秒），官方默认 15。' },
      {
        key: 'authentication',
        title: 'HTTP/SOCKS 入口认证 (username:password，每行一条)',
        type: 'textarea',
        placeholder: 'user1:pass1\nuser2:pass2',
        help: '代理端口的登录凭据，每行一条，格式为 用户名:密码。留空则不校验。',
      },
      {
        key: 'skip-auth-prefixes',
        title: '跳过认证的 IP 段 (每行一条)',
        type: 'textarea',
        placeholder: '127.0.0.1/32\n192.168.1.0/24',
        help: '免认证的来源 IP 段（CIDR），每行一条。通常填本机或可信内网段。',
      },
      { key: 'lan-allowed-ips', title: '允许连接的 IP 段 (Lan Allowed IPs)', type: 'string-array', placeholder: '0.0.0.0/0, ::/0', help: '允许连接的来源 IP 段（CIDR），仅在 allow-lan 开启时生效。官方默认放通全部。' },
      { key: 'lan-disallowed-ips', title: '禁止连接的 IP 段 (Lan Disallowed IPs)', type: 'string-array', placeholder: '192.168.0.3/32', help: '禁止连接的来源 IP 段（CIDR）。黑名单优先级高于白名单。' },
    ],
  },
  {
    id: 'ports',
    title: '端口设置',
    fields: [
      { key: 'port', title: 'HTTP 代理端口', type: 'number', placeholder: '7890', help: 'HTTP 代理监听端口。留空表示不启用。' },
      { key: 'socks-port', title: 'SOCKS 代理端口', type: 'number', placeholder: '7891', help: 'SOCKS5 代理监听端口。留空表示不启用。' },
      { key: 'mixed-port', title: '混合代理端口', type: 'number', placeholder: '7890', help: 'HTTP 与 SOCKS5 共用的混合端口，推荐只开这一个。' },
      { key: 'redir-port', title: 'Redir 端口 (仅 Linux/macOS)', type: 'number', placeholder: '7892', help: 'mihomo 原生 REDIRECT 端口，仅支持 TCP（UDP 无法接管）。与「透明代理」页的一键开关无关：面板不会为它下发 iptables nat 规则，需要你自己写并自己清理。想一键接管请改用「透明代理」页的 TUN / TProxy。' },
      { key: 'tproxy-port', title: 'TProxy 端口 (仅 Linux)', type: 'number', placeholder: '7893', help: 'mihomo TPROXY 端口，支持 TCP/UDP，仅 Linux。只填这里不足以让流量被接管：还需要防火墙规则与策略路由把流量引到该端口，那部分不在配置文件里。想一键接管请用「透明代理」页的开关（它会下发规则，并把端口写在此处）；若你自行维护规则，在这里填端口是可以的，面板不会改动它。' },
    ],
  },
  {
    id: 'controller',
    title: '外部控制 (External Controller)',
    fields: [
      { key: 'external-controller', title: 'RESTful API 监听地址', type: 'text', placeholder: '127.0.0.1:9090', help: 'RESTful API 监听地址，管理面板与热重载依赖它。官方默认 127.0.0.1:9090。' },
      { key: 'external-controller-tls', title: 'RESTful API HTTPS 监听地址', type: 'text', placeholder: '0.0.0.0:9443', help: 'API 的 HTTPS 监听地址，需同时配置 TLS 证书。' },
      { key: 'external-controller-unix', title: 'Unix Socket 监听地址', type: 'text', placeholder: 'mihomo.sock', help: '通过 Unix socket 访问 API 的路径。注意：经此路径访问不校验密钥。' },
      { key: 'external-controller-pipe', title: 'Windows Named Pipe 监听地址', type: 'text', placeholder: '\\\\.\\pipe\\mihomo', help: 'Windows 命名管道路径。注意：经此路径访问不校验密钥。' },
      { key: 'secret', title: 'API 密钥 (Secret)', type: 'text', placeholder: '建议填写足够随机的长字符串', help: '访问 API 的密钥。对外暴露 external-controller 时务必设置。' },
      { key: 'external-ui', title: '外部控制面板目录', type: 'text', placeholder: 'ui', help: '面板静态文件目录，可用绝对路径或相对工作目录的路径。' },
      { key: 'external-ui-name', title: '外部控制面板名称', type: 'text', placeholder: 'zashboard', help: 'external-ui 目录下要使用的子文件夹名。' },
      { key: 'external-ui-url', title: '外部控制面板下载地址', type: 'text', placeholder: 'https://github.com/Zephyruso/zashboard/archive/refs/heads/gh-pages.zip', help: '面板资源的下载地址，内核可据此自动下载更新面板。' },
      { key: 'external-doh-server', title: 'DoH 服务路径 (External DoH Server)', type: 'text', placeholder: '/dns-query', help: '内置 DoH 服务的路径，如 /dns-query。注意：该地址不校验密钥。' },
    ],
  },
  {
    id: 'geodata',
    title: 'GeoData 规则库',
    fields: [
      { key: 'geodata-mode', title: '启用 GeoData 模式', type: 'switch', help: '开启后使用 .dat 格式 GeoIP 数据，关闭则使用 .mmdb 格式。官方默认关闭。' },
      {
        key: 'geodata-loader',
        title: 'GeoData 加载方式',
        type: 'select',
        options: [
          { value: '', label: '未设置（使用内核默认）' },
          { value: 'memconservative', label: 'memconservative（默认，省内存）' },
          { value: 'standard', label: 'standard' },
        ],
        help: 'GeoData 的加载模式。memconservative 占用内存更少，为官方默认。',
      },
      {
        key: 'geosite-matcher',
        title: 'GeoSite 匹配实现',
        type: 'select',
        options: [
          { value: '', label: '未设置（使用内核默认）' },
          { value: 'succinct', label: 'succinct（默认）' },
          { value: 'mph', label: 'mph' },
        ],
        help: 'GeoSite 规则的匹配算法实现，一般无需修改。succinct 为官方默认。',
      },
      { key: 'geo-auto-update', title: '自动更新 GeoData', type: 'switch', help: '开启后内核按下方间隔自动更新 GeoIP/GeoSite 数据库。官方默认关闭。' },
      { key: 'geo-update-interval', title: '更新间隔 (小时)', type: 'number', placeholder: '24', help: 'Geo 数据库自动更新间隔（小时），官方默认 24。' },
      { key: 'geox-url.geoip', title: 'GeoIP 数据下载地址', type: 'text', placeholder: 'https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat', help: 'GeoIP 数据库下载地址，可改为国内镜像加速。' },
      { key: 'geox-url.geosite', title: 'GeoSite 数据下载地址', type: 'text', placeholder: 'https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat', help: 'GeoSite 数据库下载地址，可改为国内镜像加速。' },
      { key: 'geox-url.mmdb', title: 'MMDB 数据下载地址', type: 'text', placeholder: 'https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/country.mmdb', help: 'MMDB 格式 IP 数据库下载地址。' },
    ],
  },
  {
    id: 'profile',
    title: '运行状态持久化 (Profile)',
    fields: [
      { key: 'profile.store-selected', title: '记住 select 组的选择结果', type: 'switch', help: '记住你在面板中手动选择的节点，重启后自动恢复。建议开启。' },
      { key: 'profile.store-fake-ip', title: '持久化 fake-ip 映射', type: 'switch', help: '持久化 fake-ip 映射表，避免重启后域名与 IP 对应关系变化。' },
    ],
  },
  {
    id: 'dns',
    title: '域名解析 (DNS)',
    fields: [
      { key: 'dns.enable', title: '开启域名解析', type: 'switch', help: '关闭后内核使用系统 DNS，下方 DNS 设置全部失效。使用 TUN 时必须开启。' },
      { key: 'dns.listen', title: 'DNS 监听地址', type: 'text', placeholder: '0.0.0.0:1053', help: 'mihomo 自身 DNS 服务的监听地址。「透明代理」页选 TProxy 时，面板会把 53 端口的查询重定向到这里——留空则内核不监听独立 DNS 端口，TProxy 模式下域名解析无法被接管。建议 0.0.0.0:1053。不推荐填 53：它是特权端口，常被 systemd-resolved / dnsmasq 占用，mihomo 绑定失败时所有 DNS 查询都会被导向一个没人监听的端口而中断（面板会在开启透明代理时探测该端口，无监听则拒绝下发规则并说明原因）。' },
      { key: 'dns.ipv6', title: '启用 IPv6', type: 'switch', help: '关闭后 AAAA 查询返回空结果，可避免部分 IPv6 连通性问题。' },
      {
        key: 'dns.enhanced-mode',
        title: '增强模式',
        type: 'select',
        options: [
          { value: '', label: '未设置（使用内核默认）' },
          { value: 'fake-ip', label: 'fake-ip（推荐）' },
          { value: 'redir-host', label: 'redir-host' },
        ],
        help: 'fake-ip 用虚拟 IP 响应查询，兼容性与速度更好，为推荐值；redir-host 返回真实 IP。',
      },
      { key: 'dns.fake-ip-range', title: '伪装 IP 范围 (Fake IP)', type: 'text', placeholder: '198.18.0.1/16', help: 'fake-ip 模式使用的虚拟 IP 网段，需避开真实内网网段。官方默认 198.18.0.1/16。' },
      {
        key: 'dns.fake-ip-filter',
        title: 'Fake IP 过滤名单 (每行一条)',
        type: 'textarea',
        placeholder: '+.lan\n+.local\n*.msftconnecttest.com\ntime.windows.com',
        help: '这些域名不使用 fake-ip 而返回真实 IP，每行一条，支持 + 通配前缀。',
      },
      { key: 'dns.nameserver', title: '名称服务器 (Nameserver)', type: 'string-array', placeholder: 'https://223.5.5.5/dns-query, https://1.12.12.12/dns-query', help: '默认 DNS 服务器，多个用逗号分隔。支持 udp/tcp/tls/https/quic 等格式。' },
      {
        // 按域名/规则集指定上游，优先于默认 nameserver。值可以是单个服务器字符串，
        // 也可以是服务器列表；键支持 geosite:、规则集名、+.domain 等官方写法。
        key: 'dns.nameserver-policy',
        title: '域名解析策略 (Nameserver Policy)',
        type: 'yaml-object',
        code: 'yaml',
        codeHeight: '220px',
        placeholder:
          '"geosite:cn,private":\n  - https://doh.pub/dns-query\n  - https://dns.alidns.com/dns-query\n"+.google.com":\n  - https://cloudflare-dns.com/dns-query\n  - https://dns.google/dns-query\n"www.baidu.com": https://doh.pub/dns-query',
        help:
          '按域名指定使用哪组 DNS 上游，匹配到的规则优先于上方「名称服务器」。' +
          '键可以是具体域名、+.example.com 通配、geosite:cn / rule-set:xxx 等；' +
          '值可以是单个服务器字符串，或 YAML 列表（多个上游）。' +
          '常用于国内域名走国内 DoH、Google 等走境外 DoH，避免污染。' +
          '留空表示不使用策略，全部走默认 nameserver。',
      },
      { key: 'dns.fallback', title: '备用服务器 (Fallback)', type: 'string-array', placeholder: 'https://1.1.1.1/dns-query, https://8.8.8.8/dns-query', help: '备用 DNS，用于 nameserver 结果被判定为污染时回退查询。建议用 DoH（https://1.1.1.1/dns-query）：透明代理环境下，直连的 UDP 上游（裸 IP:53）可能不可达或被劫持，而 DoH 走 443 经代理可达。' },
      {
        key: 'dns.fallback-filter',
        title: '污染检测过滤器 (Fallback Filter)',
        type: 'yaml-object',
        placeholder: 'geoip: true\ngeoip-code: CN\nipcidr:\n  - 240.0.0.0/4\n  - 127.0.0.0/8\n  - 2001:db8::/32\n  - ::1/128',
        help: '判定 nameserver 返回的结果是否被污染，被判定为污染的结果会用 fallback 重查。geoip: true + geoip-code: CN 表示「返回 IP 属于 CN 才可信」，境外 IP（对国内域名反常）触发重查；ipcidr 列出命中即视为污染的网段，IPv4 与 IPv6 都支持（如 240.0.0.0/4 保留段、127.0.0.0/8 回环、::1 回环、2001:db8::/32 文档段）。留空使用内核默认。',
      },
      { key: 'dns.default-nameserver', title: '默认名称服务器 (Default Nameserver)', type: 'string-array', placeholder: '223.5.5.5, 119.29.29.29', help: '用于解析 nameserver/fallback 中域名形式服务器地址的纯 IP DNS。只能填 IP。' },
      { key: 'dns.proxy-server-nameserver', title: '代理节点域名解析服务器 (Proxy Server Nameserver)', type: 'string-array', placeholder: 'https://223.5.5.5/dns-query', help: '专门用于解析代理节点域名，不受规则影响。开启 respect-rules 时必须配置。' },
      { key: 'dns.direct-nameserver', title: '直连域名解析服务器 (Direct Nameserver)', type: 'string-array', placeholder: 'system://', help: '仅用于直连出口的域名解析，多个用逗号分隔。' },
      { key: 'dns.use-hosts', title: '使用 hosts 映射 (Use Hosts)', type: 'switch', help: '是否使用下方「自定义 hosts 映射」里配置的域名映射，官方默认开启。关掉则那些映射不生效。' },
      { key: 'dns.use-system-hosts', title: '使用系统 hosts (Use System Hosts)', type: 'switch', help: '是否查询操作系统的 hosts 文件（Windows 为 C:\\Windows\\System32\\drivers\\etc\\hosts，Linux/macOS 为 /etc/hosts），官方默认开启。' },
      {
        // hosts 是顶层键而非 dns 的子键（见 backend/internal/domain/config.go），
        // 但它只在 use-hosts 开启时才起作用，放在开关旁边才找得到
        key: 'hosts',
        title: '自定义 hosts 映射',
        type: 'hosts-map',
        help: '域名到地址的映射，需上方「使用 hosts 映射」开启才生效。域名支持 +.example.com / *.example.com 通配；指向可填 IP、多个 IP（逗号分隔）或另一个域名作别名。',
      },
      { key: 'dns.respect-rules', title: '解析遵循路由规则 (Respect Rules)', type: 'switch', help: '开启后 DNS 请求也走 rules 匹配，需同时配置 proxy-server-nameserver。' },
      { key: 'dns.prefer-h3', title: '优先使用 DoH 的 HTTP/3 (Prefer H3)', type: 'switch', help: 'DoH 优先使用 HTTP/3，网络不稳定时建议关闭。官方默认关闭。' },
      { key: 'dns.ipv6-timeout', title: 'IPv6 解析超时 (毫秒)', type: 'number', placeholder: '100', help: '等待 AAAA 记录的超时时间（毫秒），超时则只用 IPv4 结果。' },
      {
        key: 'dns.cache-algorithm',
        title: '缓存算法 (Cache Algorithm)',
        type: 'select',
        options: [
          { value: '', label: '默认' },
          { value: 'lru', label: 'lru' },
          { value: 'arc', label: 'arc' },
        ],
        help: 'DNS 缓存淘汰算法，lru 为官方默认。',
      },
    ],
  },
  {
    id: 'tun',
    title: '虚拟网卡 (TUN)',
    fields: [
      { key: 'tun.enable', title: '开启虚拟网卡', type: 'switch', help: '开启虚拟网卡接管全局流量，需要管理员/root 权限。容器部署需额外授予 NET_ADMIN。' },
      {
        key: 'tun.stack',
        title: '协议栈',
        type: 'select',
        options: [
          { value: '', label: '未设置（使用内核默认）' },
          { value: 'system', label: 'system（系统栈）' },
          { value: 'gvisor', label: 'gvisor' },
          { value: 'mixed', label: 'mixed（混合）' },
        ],
        help: 'system 兼容性最好；gvisor 为官方默认，用户态实现更安全；mixed 为混合模式。',
      },
      { key: 'tun.auto-route', title: '自动路由 (Auto Route)', type: 'switch', help: '自动接管本机路由表，使本机发出的流量进入 TUN。单机代理的核心开关；关闭则需手动配路由。' },
      { key: 'tun.auto-detect-interface', title: '自动检测接口', type: 'switch', help: '自动探测真实出口网卡，网络切换（如 WiFi 转有线）时更稳。' },
      {
        key: 'tun.auto-redirect',
        title: '自动防火墙重定向 (Auto Redirect)',
        type: 'switch',
        help:
          '让 mihomo 内核自行写入并在退出时清理防火墙重定向规则（仅 Linux 生效，macOS 会忽略）。' +
          '规则由 mihomo 维护，不是本面板的 aurora_tproxy；在本面板默认环境下通常表现为 iptables nat 链' +
          '（mihomo-prerouting / REDIRECT），而不是“系统里没有 iptables”。' +
          '与「自动路由」不同：auto-route 改本机路由表，管本机进程出网；auto-redirect 改防火墙，' +
          '把经本机转发的流量（旁路由/网关下的局域网设备）也拐进 TUN，zashboard 中连接类型多为 Redir。' +
          '旁路由/网关建议开启；单机自用开 auto-route + dns-hijack 通常已够。' +
          '若开启后拉不起 Meta 虚拟网卡，请先看内核日志是否出现 netlink “file exists”：' +
          '这是部分 Alpine 环境上 mihomo 默认 nft 后端的问题。本面板从 v0.3.1 起在 Linux 启动 mihomo 时' +
          '默认注入 DISABLE_NFTABLES=1，强制走 iptables 后端。确认进程带有该环境变量后冷启动内核即可，' +
          '一般不必再靠关闭本项来“救命”。',
      },
      { key: 'tun.dns-hijack', title: 'DNS 劫持 (DNS Hijack)', type: 'string-array', placeholder: 'any:53, tcp://any:53', help: '劫持这些地址的 DNS 请求，多个用逗号分隔。不写协议默认为 udp://。' },
      { key: 'tun.device', title: '网卡名称 (Device)', type: 'text', placeholder: 'utun0', help: '留空由内核自动命名，如 utun0。' },
      { key: 'tun.mtu', title: 'MTU', type: 'number', placeholder: '9000', help: '最大传输单元，影响吞吐上限。不确定时留空使用默认值。' },
      { key: 'tun.strict-route', title: '严格路由 (Strict Route)', type: 'switch', help: '阻止绕过 TUN 的流量，可防止 DNS 泄漏。部分网络环境下可能导致连不通。' },
      { key: 'tun.inet4-address', title: 'IPv4 地址段 (Inet4 Address)', type: 'string-array', placeholder: '198.18.0.1/30', help: 'TUN 网卡的 IPv4 地址段（CIDR）。' },
      { key: 'tun.inet6-address', title: 'IPv6 地址段 (Inet6 Address)', type: 'string-array', placeholder: 'fdfe:dcba:9876::1/126', help: 'TUN 网卡的 IPv6 地址段（CIDR）。' },
      { key: 'tun.route-address', title: '路由包含地址 (Route Address)', type: 'string-array', placeholder: '0.0.0.0/1, 128.0.0.0/1', help: '自定义需要走 TUN 的网段，填写后将替代默认全局路由。' },
      { key: 'tun.route-exclude-address', title: '路由排除地址 (Route Exclude Address)', type: 'string-array', placeholder: '192.168.0.0/16, 10.0.0.0/8', help: '排除在 TUN 之外的网段，这些流量绕过代理直连。' },
      { key: 'tun.endpoint-independent-nat', title: '独立端点 NAT (Endpoint Independent NAT)', type: 'switch', help: '开启更宽松的 NAT 以改善 P2P 连通性，性能会略有下降。' },
    ],
  },
  {
    id: 'sniffer',
    title: '域名嗅探 (Sniffer)',
    fields: [
      { key: 'sniffer.enable', title: '开启域名嗅探', type: 'switch', help: '开启域名嗅探，可从流量中还原域名，让 IP 直连的连接也能匹配域名规则。' },
      { key: 'sniffer.force-dns-mapping', title: '强制 DNS 映射', type: 'switch', help: '对 fake-ip 已映射的连接也强制重新嗅探域名。' },
      { key: 'sniffer.parse-pure-ip', title: '解析纯 IP', type: 'switch', help: '对没有域名信息的纯 IP 连接尝试嗅探。' },
      { key: 'sniffer.override-destination', title: '用嗅探结果覆盖连接目标', type: 'switch', help: '用嗅探到的域名覆盖原始目标地址，影响后续规则匹配。' },
      {
        key: 'sniffer.sniff',
        title: '按协议配置嗅探 (YAML)',
        type: 'yaml-object',
        code: 'yaml',
        codeHeight: '200px',
        placeholder: 'HTTP:\n  ports: [80, 8080-8880]\n  override-destination: true\nTLS:\n  ports: [443, 8443]\nQUIC:\n  ports: [443, 8443]',
        help: '决定对哪些协议执行嗅探。只开上面的总开关而不配这里，嗅探不会生效。',
      },
      {
        key: 'sniffer.force-domain',
        title: '强制嗅探的域名 (每行一条)',
        type: 'textarea',
        placeholder: '+.v2ex.com\ngoogle.com',
        help: '强制对这些域名执行嗅探，每行一条。',
      },
      {
        key: 'sniffer.skip-domain',
        title: '跳过嗅探的域名 (每行一条)',
        type: 'textarea',
        placeholder: '+.apple.com\nMicrosoft.com',
        help: '如 +.apple.com，避免误伤依赖 SNI 的服务。',
      },
      {
        key: 'sniffer.skip-src-address',
        title: '跳过的来源地址 (每行一条)',
        type: 'textarea',
        placeholder: '192.168.1.100/32',
        help: '跳过嗅探的来源地址段（CIDR），每行一条。',
      },
      {
        key: 'sniffer.skip-dst-address',
        title: '跳过的目标地址 (每行一条)',
        type: 'textarea',
        placeholder: '10.0.0.0/8',
        help: '跳过嗅探的目标地址段（CIDR），每行一条。',
      },
    ],
  },
  {
    id: 'proxy-groups',
    title: '策略组 (Proxy Groups)',
    fields: [
      {
        key: 'proxy-groups-raw',
        title: '策略组定义 (YAML)',
        type: 'textarea',
        code: 'yaml',
        codeHeight: '340px',
        placeholder:
          '- name: 节点选择\n' +
          '  type: select\n' +
          '  proxies:\n' +
          '    - 自动选择\n' +
          '    - DIRECT\n' +
          '\n' +
          '- name: 自动选择\n' +
          '  type: url-test\n' +
          '  url: https://www.gstatic.com/generate_204\n' +
          '  interval: 300\n' +
          '  tolerance: 50\n' +
          '  proxies:\n' +
          '    - DIRECT\n',
        help: '以 YAML 数组格式定义本地策略组，将与订阅中的同名策略组合并（本地保留 type/url/interval/strategy）。',
      },
    ],
  },
  {
    id: 'rules',
    title: '基础路由规则',
    fields: [
      {
        key: 'rules',
        title: '路由规则 (每行一条)',
        type: 'textarea',
        code: 'yaml',
        codeHeight: '340px',
        placeholder:
          'DOMAIN-SUFFIX,local,DIRECT\n' +
          'IP-CIDR,127.0.0.0/8,DIRECT,no-resolve\n' +
          'IP-CIDR,192.168.0.0/16,DIRECT,no-resolve\n' +
          'GEOIP,CN,DIRECT\n' +
          'MATCH,节点选择',
        help: '这些底层规则将被置顶插入到最终生成的配置文件中。每行一条，顺序即优先级。',
      },
    ],
  },
  {
    id: 'advanced',
    title: '高级参数 (YAML 兜底)',
    fields: [
      {
        key: 'advanced-raw',
        title: '其它官方参数 (YAML)',
        type: 'textarea',
        code: 'yaml',
        codeHeight: '420px',
        placeholder:
          '# 本页未覆盖的任意官方参数都可写在这里，例如：\n' +
          'proxies:\n' +
          '  - name: 示例节点\n' +
          '    type: ss\n' +
          '    server: example.com\n' +
          '    port: 8388\n' +
          '    cipher: aes-256-gcm\n' +
          '    password: your-password\n' +
          '\n' +
          'rule-providers:\n' +
          '  reject:\n' +
          '    type: http\n' +
          '    behavior: domain\n' +
          '    url: https://example.com/reject.yaml\n' +
          '    path: ./ruleset/reject.yaml\n' +
          '    interval: 86400\n' +
          '\n' +
          'experimental:\n' +
          '  quic-go-disable-gso: true\n',
        help:
          '以 YAML 对象格式填写本页未覆盖的任意官方 Mihomo 参数（如 listeners、proxy-providers、' +
          'sub-rules、tls、experimental、tunnels、ntp 等），保存后会原样合并进最终配置。' +
          'hosts 已有专属表单（见「域名解析」分组），在这里写会被忽略。' +
          '与订阅合并时同名顶层键遵循本地优先，本地未声明的键由远程补齐。',
      },
    ],
  },
]

/**
 * 已被上方各分组显式建模的顶层字段（不含 dns/tun/sniffer/proxy-groups/rules，
 * 这些字段各自有专门的分组与序列化逻辑），用于 advanced-raw 与 model 之间的双向同步：
 * 写入 advanced-raw 时必须排除这些键，否则会覆盖专属表单已经写入的值；
 * 展示 advanced-raw 时同理需要排除，否则会造成同一字段在两处重复编辑。
 *
 * 注意：`proxies` / `rule-providers` 等字段故意不在此列——它们没有专属表单
 * 分组，需要经由「高级参数」文本域可见可编辑，排除它们会导致本地 base 配置
 * 里的这些字段在界面上既没有专属入口、也无法通过兜底框访问。
 *
 * `hosts` 原属上述情况，自「域名解析」分组新增 hosts-map 表单后转为受管：
 * 不排除它的话，两处都能改同一个键，且 advanced-raw 的「先删非受管键再写入」
 * 会把 hosts 表单刚写的内容删掉。
 */
export const advancedExcludedKeys = new Set([
  'mode',
  'log-level',
  'allow-lan',
  'bind-address',
  'ipv6',
  'find-process-mode',
  'global-client-fingerprint',
  'tcp-concurrent',
  'unified-delay',
  'interface-name',
  'routing-mark',
  'disable-keep-alive',
  'keep-alive-idle',
  'keep-alive-interval',
  'authentication',
  'skip-auth-prefixes',
  'lan-allowed-ips',
  'lan-disallowed-ips',
  'port',
  'socks-port',
  'mixed-port',
  'redir-port',
  'tproxy-port',
  'external-controller',
  'external-controller-tls',
  'external-controller-unix',
  'external-controller-pipe',
  'secret',
  'external-ui',
  'external-ui-name',
  'external-ui-url',
  'external-doh-server',
  'geodata-mode',
  'geodata-loader',
  'geosite-matcher',
  'geo-auto-update',
  'geo-update-interval',
  'geox-url',
  'profile',
  'hosts',
  'dns',
  'tun',
  'sniffer',
  'proxy-groups',
  'rules',
])

export function getByPath(obj: any, path: string) {
  return path.split('.').reduce((acc, key) => (acc == null ? undefined : acc[key]), obj)
}

/**
 * 删除指定路径上的键，并逐层清理因此变空的父对象。
 *
 * 用于「默认/未设置」语义的下拉项：若直接写入空字符串，yaml.dump 会产出
 * 形如 `cache-algorithm: ''` 的空值，而 mihomo 对这类枚举字段会因非法值
 * 拒绝加载整份配置。必须把键移除而不是留空。
 */
export function deleteByPath(obj: any, path: string) {
  const keys = path.split('.')
  // 记录每一层的容器，便于自底向上清理空对象
  const chain: any[] = [obj]
  let cur = obj
  for (let i = 0; i < keys.length - 1; i++) {
    const k = keys[i] as string
    if (cur[k] == null || typeof cur[k] !== 'object') return
    cur = cur[k]
    chain.push(cur)
  }
  delete cur[keys[keys.length - 1] as string]

  for (let i = chain.length - 1; i > 0; i--) {
    const container = chain[i]
    if (container && typeof container === 'object' && !Array.isArray(container) && Object.keys(container).length === 0) {
      delete chain[i - 1][keys[i - 1] as string]
    } else {
      break
    }
  }
}

export function setByPath(obj: any, path: string, value: any) {
  const keys = path.split('.')
  let cur = obj
  for (let i = 0; i < keys.length - 1; i++) {
    const k = keys[i] as string
    if (cur[k] == null || typeof cur[k] !== 'object') cur[k] = {}
    cur = cur[k]
  }
  const lastKey = keys[keys.length - 1] as string
  cur[lastKey] = value
}
