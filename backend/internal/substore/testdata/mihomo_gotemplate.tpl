{{- /*
  Go 模板版配置（与 YAML 覆写版产出同一份结果，但由模板自己写全整份结构）。

  为什么写法和 YAML 覆写版差别这么大：
  1. Go 模板不基于系统自动生成的基础配置，proxies 必须由模板自己输出。
     节点的 ws-opts / reality-opts / grpc-opts 等是嵌套结构，直接
     {{ $v }} 打印会写成 Go 的 map[k:v] 语法而非 YAML，内核会拒绝加载，
     所以统一用 proxiesYaml 序列化。
  2. YAML 锚点（&pr / *pr）是 YAML 语法，Go 模板阶段还没有 YAML 解析器，
     用不了。这里改用模板变量 $pr / $pr1 / $class 承担同样的"定义一次、
     复用多处"职责，效果等价。

  可用辅助函数：proxiesYaml / proxyYaml / toYaml / indent / names / quote / list / fields
*/ -}}
{{- /* 以下三个变量只存"字段内容"，不含外层花括号，由使用处补 { } */ -}}
{{- /* 策略组模板：用 slice 存成员，配合 range 输出块状 YAML。
     不再手写 "{a, b}" 流式花括号——官方 Sub-Store 产物是块状展开，
     手写花括号会让两边 diff 时整段都对不上。 */ -}}
{{- $prMembers := list "👵 大妈节点" "🐂 所有-手动" -}}
{{- $pr1Members := list "DIRECT" "REJECT" "👵 大妈节点" "🦁 香港-自动" "🎌 日本-自动" "🏝️ 台湾-自动" "🌿 新加坡-自动" "💄 韩国-自动" "🦅 美国-自动" "🐳 所有-自动" "🌀 其他-自动" "🐂 所有-手动" -}}
{{- $base := "https://raw.githubusercontent.com/weni09/clash_my_conf/refs/heads/main/list" -}}
global-ua: clash

# 全局配置
ipv6: true
allow-lan: true
unified-delay: true
tcp-concurrent: true
geodata-mode: false
geodata-loader: standard
geo-auto-update: true
geo-update-interval: 24
geox-url:
  geoip: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip.dat"
  geosite: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat"
  mmdb: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/country.mmdb"
  asn: "https://github.com/xishang0128/geoip/releases/download/latest/GeoLite2-ASN.mmdb"

# 策略组选择和fakeip缓存
profile:
  store-selected: true
  store-fake-ip: true

# 节点：Go 模板不继承自动生成的 proxies，需自己输出
proxies:
{{ proxiesYaml .Nodes | indent 2 }}

# 策略组
proxy-groups:
{{- /* 引用同一组成员的策略组：range 出块状结构，与官方产物一致 */}}
  - name: 🤖⚡ AI
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🔵🧠 Meta AI
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🔍🧩 Perplexity
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🐙💻 GitHub
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🪂 TikTok
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🙋 Telegram
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🌎 Google
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🕊️ Twitter(X)
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🗣️ Facebook
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 💁 WhatApp
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🌳 Amazon
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🍎 Apple
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: Ⓜ️ Microsoft
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🎮 Steam
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🕹️ Game
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🎞️ YouTube
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 📺 Disney
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🎥 Netflix
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🎬 HBO MAX
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🎵 Spotify
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 👌 虚拟币
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: ⏱ 检测
    type: select
    proxies:
{{- range $pr1Members }}
      - {{ . }}
{{- end }}
  - name: ✈️ 国外
    type: fallback
    proxies:
{{- range $prMembers }}
      - {{ . }}
{{- end }}
  - name: 🐼 国内
    type: select
    proxies:
{{- range $pr1Members }}
      - {{ . }}
{{- end }}
  - name: 🐂 所有-手动
    type: select
    include-all: true
  - name: 🐳 所有-自动
    type: url-test
    lazy: false
    include-all: true
    tolerance: 50
    interval: 180
    filter: "^((?!(国内|Ghost|Dmit)).)*$"
  - name: 🦁 香港-自动
    type: url-test
    lazy: false
    include-all: true
    tolerance: 50
    interval: 180
    filter: "(?i)广港|香港|HK|Hong Kong|🇭🇰|HongKong"
  - name: 🏝️ 台湾-自动
    type: url-test
    lazy: false
    include-all: true
    tolerance: 50
    interval: 180
    filter: "(?i)广台|台湾|台灣|TW|Tai Wan|🇹🇼|🇨🇳|TaiWan|Taiwa"
  - name: 🎌 日本-自动
    type: url-test
    lazy: false
    include-all: true
    tolerance: 50
    interval: 180
    filter: "(?i)广日|日本|JP|川日|东京|大阪|泉日|埼玉|沪日|深日|🇯🇵|Japan"
  - name: 🌿 新加坡-自动
    type: url-test
    lazy: false
    include-all: true
    tolerance: 50
    interval: 180
    filter: "(?!)广新|新加坡|SG|坡|狮城|🇸🇬|Singapore"
  - name: 💄 韩国-自动
    type: url-test
    lazy: false
    include-all: true
    tolerance: 50
    interval: 180
    filter: "(?!)广韩|韩国|韓國|KR|首尔|春川|🇰🇷|Korea"
  - name: 🦅 美国-自动
    type: url-test
    lazy: false
    include-all: true
    tolerance: 50
    interval: 180
    filter: "(?!)广美|美|US|纽约|波特兰|达拉斯|俄勒|凤凰城|费利蒙|硅谷|拉斯|洛杉|圣何塞|圣克拉|西雅|芝加|🇺🇸|United States"
  - name: 🌀 其他-自动
    type: url-test
    lazy: false
    include-all: true
    tolerance: 50
    interval: 180
    filter: "(?!)波|柬|尼|也|克|比|尔|立|冰|秘|耳|利|埃|希|孟|芬|愛|澳|英|德|南|意|法|拿|墨|印|越|俄|瑞|智|荷|比|巴|沙|班|泰|德|烏|以|Australia|Konghwaguk"
  - name: 👵 大妈节点
    type: url-test
    lazy: false
    include-all: true
    tolerance: 50
    interval: 120
    filter: "Dmit"

rules:
  - RULE-SET,a1,🤖⚡ AI
  - RULE-SET,a2,🔵🧠 Meta AI
  - RULE-SET,a3,🔍🧩 Perplexity
  - RULE-SET,a4,🐙💻 GitHub
  - RULE-SET,a5,🪂 TikTok
  - RULE-SET,a6,🙋 Telegram
  - RULE-SET,a34,🌎 Google
  - RULE-SET,a7,🕊️ Twitter(X)
  - RULE-SET,a8,🗣️ Facebook
  - RULE-SET,a9,💁 WhatApp
  - RULE-SET,a10,🌳 Amazon
  - RULE-SET,a11,🍎 Apple
  - RULE-SET,a12,Ⓜ️ Microsoft
  - RULE-SET,a13,🎮 Steam
  - RULE-SET,a14,🕹️ Game
  - RULE-SET,a15,🕹️ Game
  - RULE-SET,a16,🕹️ Game
  - RULE-SET,a17,🕹️ Game
  - RULE-SET,a18,🕹️ Game
  - RULE-SET,a19,🕹️ Game
  - RULE-SET,a20,🎞️ YouTube
  - RULE-SET,a21,🎞️ YouTube
  - RULE-SET,a22,📺 Disney
  - RULE-SET,a23,🎥 Netflix
  - RULE-SET,a24,🎬 HBO MAX
  - RULE-SET,a25,🎵 Spotify
  - RULE-SET,a26,👌 虚拟币
  - RULE-SET,a27,🐼 国内
  - RULE-SET,a28,👌 虚拟币
  - RULE-SET,a29,⏱ 检测
  - RULE-SET,a30,✈️ 国外
  - RULE-SET,a31,✈️ 国外
  - RULE-SET,a32,✈️ 国外
  - RULE-SET,a33,🐼 国内
  - MATCH,🐼 国内

rule-providers:
{{- /* 34 个 provider 共用同一套字段，用 range 遍历 "名称 文件名" 对，
     逐条块状输出。此前是 aN: { ... } 流式写法，与官方产物形态不一致。 */}}
{{- range $pair := list "a1 AI.list" "a2 MetaAi.list" "a3 Perplexity.list" "a4 GitHub.list" "a5 TikTok.list" "a6 Telegram.list" "a7 Twitter.list" "a8 Facebook.list" "a9 Whatsapp.list" "a10 Amazon.list" "a11 Apple.list" "a12 Microsoft.list" "a13 Steam.list" "a14 Epic.list" "a15 EA.list" "a16 Blizzard.list" "a17 UBI.list" "a18 Sony.list" "a19 Nintendo.list" "a20 YouTube1.list" "a21 YouTube2.list" "a22 Disney.list" "a23 Netflix.list" "a24 HBO.list" "a25 Spotify.list" "a26 OKX.list" "a27 Direct.list" "a28 xnb.list" "a29 Check.list" "a30 Proxy.list" "a31 Global.list" "a32 MyProxy.list" "a33 PT.list" "a34 Google.list" }}
{{- $parts := fields $pair }}
  {{ index $parts 0 }}:
    type: http
    interval: 86400
    behavior: classical
    format: text
    url: {{ $base }}/{{ index $parts 1 }}
{{- end }}
