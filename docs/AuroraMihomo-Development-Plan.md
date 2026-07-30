# AuroraMihomo 开发规划

## 总目标

打造一个类似 OpenClash 的 Mihomo 原生管理平台。

------------------------------------------------------------------------

# Phase 0 设计阶段

周期：1-2周

目标：

完成基础设计。

输出：

-   架构文档
-   API设计
-   数据模型
-   Merge Engine设计

------------------------------------------------------------------------

# Phase 1 MVP

周期：4-6周

目标：

替代当前 Python 自动更新方案。

## 功能

### Backend

完成：

-   go-zero API
-   SQLite
-   Mihomo管理
-   配置生成

### Merge Engine

实现：

-   proxies合并
-   proxy-groups合并
-   rules合并
-   providers合并

### Scheduler

实现：

-   定时订阅更新
-   自动reload

完成标准：

    导入订阅

    ↓

    生成config.yaml

    ↓

    启动mihomo

    ↓

    Web查看状态

------------------------------------------------------------------------

# Phase 2 Web管理平台

周期：4周

## 前端

页面：

    Dashboard

    Mihomo

    Subscriptions

    Config

    Rules

    Proxy Groups

    Conflict

    Logs

## 配置表单

支持：

-   DNS
-   TUN
-   Sniffer
-   Ports
-   Rules

------------------------------------------------------------------------

# Phase 3 完整OpenClash体验

周期：6-8周

增加：

## 冲突管理

支持：

-   本地优先
-   远程优先
-   手动选择

## Diff

显示：

-   新增节点
-   删除节点
-   修改规则

## 回滚

支持：

-   历史版本
-   恢复配置

------------------------------------------------------------------------

# Phase 4 产品化

周期：持续

## 自动更新

支持：

-   Mihomo版本检测
-   Zashboard更新
-   SubStore更新

## 插件系统

支持：

    plugins/

    converter

    provider

    merge-rule

------------------------------------------------------------------------

# 推荐开发顺序

优先级：

1.  Merge Engine

2.  Mihomo Manager

3.  Scheduler

4.  API

5.  Vue UI

6.  SubStore深度整合

7.  自动升级

------------------------------------------------------------------------

# 核心技术风险

## 1. Mihomo配置兼容

解决：

yaml AST + Schema模型。

## 2. SubStore兼容

解决：

第一阶段保持原实现。

## 3. 配置冲突

解决：

Conflict Engine。

------------------------------------------------------------------------

# 长期路线

AuroraMihomo最终成为：

    Mihomo Distribution Platform

    =
    Core
    +
    Config Engine
    +
    Subscription Engine
    +
    Web Control Plane
