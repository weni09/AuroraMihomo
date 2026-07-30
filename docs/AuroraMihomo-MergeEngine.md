# AuroraMihomo-MergeEngine.md

# AuroraMihomo Merge Engine 技术设计文档

## 1. 概述

AuroraMihomo Merge Engine 是 AuroraMihomo 的核心配置引擎。

目标：

-   将本地可视化配置（Base Config）
-   Sub-Store生成的远程配置（Remote Config）
-   用户自定义覆盖规则（Override Config）

合成为最终 Mihomo 运行配置。

核心流程：

    Base Config
         |
         |
    Remote Config
         |
         |
    Override Policy
         |
         v

    Merge Engine

         |
         v

    config.yaml

------------------------------------------------------------------------

# 2. 设计目标

## 2.1 功能目标

支持：

-   Mihomo完整配置解析
-   配置语义合并
-   冲突检测
-   冲突解决
-   配置Diff
-   配置回滚
-   自动生成最终配置

## 2.2 非目标

不做：

-   替代Mihomo核心
-   替代Sub-Store全部功能（初期）
-   修改用户原始订阅

------------------------------------------------------------------------

# 3. 配置来源模型

AuroraMihomo存在三个配置层。

## Layer 1: Base Config

来源：

    用户本地配置

特点：

-   稳定
-   长期维护
-   表单生成

例如：

``` yaml
dns:
  enable: true

tun:
  enable: true
```

------------------------------------------------------------------------

## Layer 2: Remote Config

来源：

    Sub-Store

特点：

-   动态
-   节点变化
-   策略组变化

例如：

``` yaml
proxies:
 - name: HK01
 - name: JP01
```

------------------------------------------------------------------------

## Layer 3: Override Config

来源：

    用户冲突处理结果

例如：

``` yaml
rules:

 DOMAIN-SUFFIX,google.com:
   priority: local
```

------------------------------------------------------------------------

# 4. 配置模型

不要直接使用 map\[string\]interface{}。

采用强类型模型。

## Config

``` go
type Config struct {

 Proxies []Proxy

 ProxyGroups []ProxyGroup

 Rules []Rule

 RuleProviders map[string]RuleProvider

 DNS DNSConfig

 TUN TunConfig

 Sniffer SnifferConfig

 General map[string]interface{}

}
```

------------------------------------------------------------------------

# 5. Merge Pipeline

完整流程：

    Load YAML

        |

    Parse AST

        |

    Normalize

        |

    Detect Conflict

        |

    Apply Policy

        |

    Generate YAML

        |

    Validate

------------------------------------------------------------------------

# 6. Normalize 标准化

目的：

避免格式差异导致错误。

例如：

输入：

    HK01
    hk01
     HK01

统一：

    HK01

处理：

-   trim
-   unicode normalize
-   case规则

------------------------------------------------------------------------

# 7. Proxies 合并策略

节点唯一标识：

    proxy.name

示例：

Base:

``` yaml
proxies:

- name: DIRECT
```

Remote:

``` yaml
proxies:

- name: HK01
```

结果：

``` yaml
proxies:

- name: DIRECT

- name: HK01
```

冲突：

Base:

``` yaml
HK01
server:a.com
```

Remote:

``` yaml
HK01
server:b.com
```

生成：

``` json
{
"type":"proxy",
"name":"HK01",
"local":"server:a.com",
"remote":"server:b.com"
}
```

------------------------------------------------------------------------

# 8. Proxy Group 合并策略

唯一键：

    proxy-group.name

合并字段：

## 保留本地：

-   type
-   url
-   interval
-   strategy

## 合并：

-   proxies
-   use

例如：

Base:

``` yaml
proxy-groups:

- name: Proxy
  type: select
  proxies:
   - DIRECT
```

Remote:

``` yaml
proxy-groups:

- name: Proxy
  proxies:
   - HK01
```

结果：

``` yaml
- name: Proxy
  type: select
  proxies:

   - DIRECT
   - HK01
```

------------------------------------------------------------------------

# 9. Rules 合并策略

Rule特点：

有顺序。

因此不能简单排序。

规则：

    Local Rules

    优先插入顶部


    Remote Rules

    追加


    Duplicate Remove

例如：

Base:

    DOMAIN-SUFFIX,local,DIRECT

Remote:

    DOMAIN-SUFFIX,local,Proxy

产生Conflict。

------------------------------------------------------------------------

# 10. Rule Provider

key作为唯一ID。

例如：

``` yaml
rule-providers:

 apple:
   url:
```

合并：

    Base优先

原因：

用户通常维护自己的provider。

------------------------------------------------------------------------

# 11. DNS/TUN等系统配置

默认：

Local First。

原因：

这些属于运行环境。

字段：

    dns

    tun

    sniffer

    external-controller

    secret

    mixed-port

禁止远程覆盖。

补充（实现约定）：

「禁止远程覆盖」是**默认**行为，不是硬约束。用户在设置页显式把 dns / tun
策略改为「远程优先」时才会采用远程值，且必须满足：远程确实声明了该段
（非零值）。否则空的远程段会把本地设置抹掉。

------------------------------------------------------------------------

# 11.1 官方参数的完整性保障

需求：本地支持官方**所有**参数的表单式配置，且任何官方参数都不得在
「解析 → 合并 → 生成」的往返中丢失。

实现分三层兜底：

1.  顶层参数：`domain.Config` 显式建模常用字段，其余官方顶层字段
    （listeners / proxy-providers / sub-rules / tls / experimental / ntp /
    tunnels 等）由 `General map[string]interface{}`（`yaml:",inline"`）承载。

2.  段内子字段：`dns` / `tun` / `sniffer` 是强类型结构体，只建模了常用子集，
    因此各自带一个内联的 `Extra map[string]interface{}`，承载
    nameserver-policy / respect-rules / device / mtu / strict-route /
    skip-domain / override-destination 等未建模的官方子字段。
    缺少 Extra 时这些字段会被静默丢弃。

3.  枚举字段的空值：`enhanced-mode` / `stack` 这类枚举型字符串必须带
    `omitempty`。写出 `enhanced-mode: ""` 会让 mihomo 因非法枚举值
    拒绝加载**整份**配置。前端下拉框选择「默认」时同样必须删除该键，
    而不是写入空字符串。

对应回归测试：`engine.TestUnmodeledSubFieldsSurviveRoundTrip`、
`engine.TestEmptyEnumFieldsAreOmitted`、`engine.TestExtraFieldsFollowLocalFirst`。

------------------------------------------------------------------------

# 11.2 远程配置为空时的行为

需求：远程配置为空时，直接使用本地配置。

判定与流程：

    buildRemoteConfig
         |
         |  无启用订阅 -> 写入 name="remote-merged" 且 content=""
         |  有订阅但全部失败 -> 返回错误，不覆盖既有可用配置
         v
    读取聚合行（按 name 定位，不能按 type）
         |
         |  content 为空白 -> remoteCfg = nil
         v
    MergeDetailed(base, nil, ...) -> 仅本地层，零冲突

关键约束：远程聚合结果必须按 `name = "remote-merged"` 读取。
`type = "remote"` 下还存着每条订阅的原始快照（`remote-<id>`），
且这些快照的写入时间通常**晚于**聚合结果；按 type 取「最近一条」
会退化成「只用某一条订阅」，导致其余订阅的节点全部消失。

对应回归测试：`service.TestGetRemoteMergedConfigIgnoresPerSubscriptionRows`、
`service.TestMergeWithoutSubscriptionsKeepsLocalConfigIntact`、
`service.TestMergeWithEmptyRemoteRowUsesLocalOnly`。

------------------------------------------------------------------------

# 12. Conflict Engine

冲突对象：

``` go
type Conflict struct {

 ID string

 Type string

 Path string

 Local interface{}

 Remote interface{}

 Resolution string

}
```

类型：

    proxy

    proxy-group

    rule

    dns

    tun

    provider

------------------------------------------------------------------------

# 13. Conflict Resolution

支持：

## Local

本地优先。

## Remote

远程优先。

## Merge

自动合并。

## Manual

用户选择。

------------------------------------------------------------------------

# 14. Diff Engine

提供：

    Before

    After

展示：

新增：

    + HK01
    + JP01

删除：

    - OLD01

修改：

    ~ Rule Proxy -> DIRECT

------------------------------------------------------------------------

# 15. Override Storage

保存：

SQLite。

表：

    conflicts

    id

    type

    path

    resolution

    value

    created_at

生成：

    override.yaml

------------------------------------------------------------------------

# 16. Merge Policy

用户配置：

``` yaml
policy:

proxy:
 local

rules:
 manual

dns:
 local

tun:
 local
```

------------------------------------------------------------------------

# 17. Validation

生成后必须：

    mihomo -t

流程：

    merge

     |

    validate

     |

    replace

     |

    reload

失败：

自动恢复旧配置。

------------------------------------------------------------------------

# 18. 回滚机制

目录：

    data/backups/

    config.yaml.20260725

    config.yaml.20260726

保留：

默认10份。

------------------------------------------------------------------------

# 19. Go Package设计

    internal/configmerge/


    model/

    parser/

    normalizer/

    merge/

    conflict/

    diff/

    validator/

    generator/

------------------------------------------------------------------------

# 20. API设计

## Merge

    POST /api/config/merge

## Conflict List

    GET /api/conflicts

## Resolve

    POST /api/conflicts/{id}/resolve

## Diff

    GET /api/config/diff

------------------------------------------------------------------------

# 21. 未来扩展

支持：

-   多机场合并
-   多配置环境
-   Git版本控制
-   配置云同步
-   插件化Merge Rule

------------------------------------------------------------------------

# 总结

AuroraMihomo Merge Engine 是整个项目区别于普通 Mihomo 面板的核心。

它负责解决：

    订阅动态变化
    +
    本地长期维护
    +
    用户个性规则

    如何共存

目标：

实现 Docker/Linux 环境下接近 OpenClash 的配置生命周期管理能力。
