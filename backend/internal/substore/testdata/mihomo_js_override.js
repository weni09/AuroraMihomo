/**
 * JS 脚本覆写版配置（与 YAML 覆写版、Go 模板版产出同一份结果）。
 *
 * 与另外两种模板语言的关键差异：
 * 1. 入参 config 已经是系统生成的基础配置（含 proxies、以及一个占位的
 *    proxy-groups "Proxy" 与 rules "MATCH,Proxy"）。proxies 不需要自己写，
 *    但那两个占位键必须整体替换掉，否则会残留在产物里。
 * 2. 没有 YAML 锚点，也不需要 Go 模板的 list/fields 变通：直接用 JS 数组
 *    与对象承担"定义一次、复用多处"的职责，成员数组要 slice() 拷贝，
 *    避免多个策略组共享同一个数组引用（后续任何一处改动会串到全部组）。
 * 3. 返回值经 JSON 往返后由 Go 序列化成 YAML，顶层键按字母序输出，
 *    不保留这里的书写顺序——语义一致，形态与 YAML 覆写版不同。
 */
function main(config) {
  // 两套可复用的策略组成员，对应 YAML 版的 &pr / &pr1 锚点
  var prMembers = ['👵 大妈节点', '🐂 所有-手动'];
  var pr1Members = [
    'DIRECT', 'REJECT', '👵 大妈节点', '🦁 香港-自动', '🎌 日本-自动',
    '🏝️ 台湾-自动', '🌿 新加坡-自动', '💄 韩国-自动', '🦅 美国-自动',
    '🐳 所有-自动', '🌀 其他-自动', '🐂 所有-手动',
  ];
  var listBase = 'https://raw.githubusercontent.com/weni09/clash_my_conf/refs/heads/main/list';

  // 全局配置
  config['global-ua'] = 'clash';
  config.ipv6 = true;
  config['allow-lan'] = true;
  config['unified-delay'] = true;
  config['tcp-concurrent'] = true;
  config['geodata-mode'] = false;
  config['geodata-loader'] = 'standard';
  config['geo-auto-update'] = true;
  config['geo-update-interval'] = 24;
  config['geox-url'] = {
    geoip: 'https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip.dat',
    geosite: 'https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat',
    mmdb: 'https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/country.mmdb',
    asn: 'https://github.com/xishang0128/geoip/releases/download/latest/GeoLite2-ASN.mmdb',
  };

  // 策略组选择和fakeip缓存
  config.profile = { 'store-selected': true, 'store-fake-ip': true };

  // 引用固定成员的策略组：[名称, 组类型, 成员数组]
  var templated = [
    ['🤖⚡ AI', 'fallback', prMembers],
    ['🔵🧠 Meta AI', 'fallback', prMembers],
    ['🔍🧩 Perplexity', 'fallback', prMembers],
    ['🐙💻 GitHub', 'fallback', prMembers],
    ['🪂 TikTok', 'fallback', prMembers],
    ['🙋 Telegram', 'fallback', prMembers],
    ['🌎 Google', 'fallback', prMembers],
    ['🕊️ Twitter(X)', 'fallback', prMembers],
    ['🗣️ Facebook', 'fallback', prMembers],
    ['💁 WhatApp', 'fallback', prMembers],
    ['🌳 Amazon', 'fallback', prMembers],
    ['🍎 Apple', 'fallback', prMembers],
    ['Ⓜ️ Microsoft', 'fallback', prMembers],
    ['🎮 Steam', 'fallback', prMembers],
    ['🕹️ Game', 'fallback', prMembers],
    ['🎞️ YouTube', 'fallback', prMembers],
    ['📺 Disney', 'fallback', prMembers],
    ['🎥 Netflix', 'fallback', prMembers],
    ['🎬 HBO MAX', 'fallback', prMembers],
    ['🎵 Spotify', 'fallback', prMembers],
    ['👌 虚拟币', 'fallback', prMembers],
    ['⏱ 检测', 'select', pr1Members],
    ['✈️ 国外', 'fallback', prMembers],
    ['🐼 国内', 'select', pr1Members],
  ];

  // 按地区/延迟自动选择的策略组：[名称, 测速间隔, 节点名筛选正则]
  var autoGroups = [
    ['🐳 所有-自动', 180, '^((?!(国内|Ghost|Dmit)).)*$'],
    ['🦁 香港-自动', 180, '(?i)广港|香港|HK|Hong Kong|🇭🇰|HongKong'],
    ['🏝️ 台湾-自动', 180, '(?i)广台|台湾|台灣|TW|Tai Wan|🇹🇼|🇨🇳|TaiWan|Taiwa'],
    ['🎌 日本-自动', 180, '(?i)广日|日本|JP|川日|东京|大阪|泉日|埼玉|沪日|深日|🇯🇵|Japan'],
    ['🌿 新加坡-自动', 180, '(?i)广新|新加坡|SG|坡|狮城|🇸🇬|Singapore'],
    ['💄 韩国-自动', 180, '(?i)广韩|韩国|韓國|KR|首尔|春川|🇰🇷|Korea'],
    ['🦅 美国-自动', 180, '(?i)广美|美|US|纽约|波特兰|达拉斯|俄勒|凤凰城|费利蒙|硅谷|拉斯|洛杉|圣何塞|圣克拉|西雅|芝加|🇺🇸|United States'],
    ['🌀 其他-自动', 180, '(?i)波|柬|尼|也|克|比|尔|立|冰|秘|耳|利|埃|希|孟|芬|愛|澳|英|德|南|意|法|拿|墨|印|越|俄|瑞|智|荷|比|巴|沙|班|泰国|泰國|Thailand|曼谷|Bangkok|芭提雅|清迈|🇹🇭|德|烏|以|Australia|Konghwaguk'],
    ['👵 大妈节点', 120, 'Dmit'],
  ];

  var groups = templated.map(function (item) {
    // slice() 是必须的：直接放 prMembers 会让 21 个组共享同一个数组
    return { name: item[0], type: item[1], proxies: item[2].slice() };
  });
  // 手动组不带 filter，靠 include-all 收全部节点，位置在自动组之前
  groups.push({ name: '🐂 所有-手动', type: 'select', 'include-all': true });
  autoGroups.forEach(function (item) {
    groups.push({
      name: item[0],
      type: 'url-test',
      lazy: false,
      'include-all': true,
      tolerance: 50,
      interval: item[1],
      filter: item[2],
    });
  });
  // 整体替换掉基础配置里的占位组
  config['proxy-groups'] = groups;

  config.rules = [
    'RULE-SET,a1,🤖⚡ AI',
    'RULE-SET,a2,🔵🧠 Meta AI',
    'RULE-SET,a3,🔍🧩 Perplexity',
    'RULE-SET,a4,🐙💻 GitHub',
    'RULE-SET,a5,🪂 TikTok',
    'RULE-SET,a6,🙋 Telegram',
    'RULE-SET,a34,🌎 Google',
    'RULE-SET,a7,🕊️ Twitter(X)',
    'RULE-SET,a8,🗣️ Facebook',
    'RULE-SET,a9,💁 WhatApp',
    'RULE-SET,a10,🌳 Amazon',
    'RULE-SET,a11,🍎 Apple',
    'RULE-SET,a12,Ⓜ️ Microsoft',
    'RULE-SET,a13,🎮 Steam',
    'RULE-SET,a14,🕹️ Game',
    'RULE-SET,a15,🕹️ Game',
    'RULE-SET,a16,🕹️ Game',
    'RULE-SET,a17,🕹️ Game',
    'RULE-SET,a18,🕹️ Game',
    'RULE-SET,a19,🕹️ Game',
    'RULE-SET,a20,🎞️ YouTube',
    'RULE-SET,a21,🎞️ YouTube',
    'RULE-SET,a22,📺 Disney',
    'RULE-SET,a23,🎥 Netflix',
    'RULE-SET,a24,🎬 HBO MAX',
    'RULE-SET,a25,🎵 Spotify',
    'RULE-SET,a26,👌 虚拟币',
    'RULE-SET,a27,🐼 国内',
    'RULE-SET,a28,👌 虚拟币',
    'RULE-SET,a29,⏱ 检测',
    'RULE-SET,a30,✈️ 国外',
    'RULE-SET,a31,✈️ 国外',
    'RULE-SET,a32,✈️ 国外',
    'RULE-SET,a33,🐼 国内',
    'MATCH,🐼 国内',
  ];

  // 34 个规则集共用同一套字段，只有名称与文件名不同
  var providerFiles = [
    ['a1', 'AI.list'], ['a2', 'MetaAi.list'], ['a3', 'Perplexity.list'],
    ['a4', 'GitHub.list'], ['a5', 'TikTok.list'], ['a6', 'Telegram.list'],
    ['a7', 'Twitter.list'], ['a8', 'Facebook.list'], ['a9', 'Whatsapp.list'],
    ['a10', 'Amazon.list'], ['a11', 'Apple.list'], ['a12', 'Microsoft.list'],
    ['a13', 'Steam.list'], ['a14', 'Epic.list'], ['a15', 'EA.list'],
    ['a16', 'Blizzard.list'], ['a17', 'UBI.list'], ['a18', 'Sony.list'],
    ['a19', 'Nintendo.list'], ['a20', 'YouTube1.list'], ['a21', 'YouTube2.list'],
    ['a22', 'Disney.list'], ['a23', 'Netflix.list'], ['a24', 'HBO.list'],
    ['a25', 'Spotify.list'], ['a26', 'OKX.list'], ['a27', 'Direct.list'],
    ['a28', 'xnb.list'], ['a29', 'Check.list'], ['a30', 'Proxy.list'],
    ['a31', 'Global.list'], ['a32', 'MyProxy.list'], ['a33', 'PT.list'],
    ['a34', 'Google.list'],
  ];
  var providers = {};
  providerFiles.forEach(function (item) {
    providers[item[0]] = {
      type: 'http',
      interval: 86400,
      behavior: 'classical',
      format: 'text',
      url: listBase + '/' + item[1],
    };
  });
  config['rule-providers'] = providers;

  return config;
}
