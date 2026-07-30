# AuroraMihomo 技术设计文档（TDD）

## 1. 项目概述

AuroraMihomo 是一个 Mihomo 一体化运行与配置管理平台。

目标：

-   单实例管理 Mihomo
-   内置 Sub-Store 能力
-   内置 Zashboard
-   提供 Web 配置中心
-   实现 OpenClash 类似配置生命周期管理

核心：

    订阅
     |
    Sub-Store
     |
    配置合并引擎
     |
    Mihomo Runtime
     |
    Web UI

------------------------------------------------------------------------

# 2. 总体架构

                     Browser

                        |
                        v

                  Vue3 Frontend

                        |
                        |

                  go-zero API

                        |
         +--------------+--------------+
         |              |              |

     Config Service Mihomo Service Scheduler

         |              |              |

     Merge Engine   Mihomo Core    Update Manager

         |
     SQLite

------------------------------------------------------------------------

# 3. 技术栈

## Backend

-   Go 1.23+
-   go-zero
-   SQLite
-   GORM
-   yaml.v3
-   WebSocket
-   robfig/cron

## Frontend

-   Vue3
-   TypeScript
-   Vite
-   Pinia
-   TailwindCSS
-   shadcn-vue

## Runtime

-   Mihomo Core
-   Sub-Store
-   Zashboard

------------------------------------------------------------------------

# 4. Go项目结构

    AuroraMihomo

    apps/

     api/
     worker/


    internal/

     api/
     config/
     merge/
     mihomo/
     substore/
     scheduler/
     updater/
     storage/


    pkg/

     yaml/
     utils/
     docker/

    web/

    data/

------------------------------------------------------------------------

# 5. go-zero API设计

## 服务

    aurora-api

------------------------------------------------------------------------

## Mihomo接口

    GET /api/mihomo/status

    POST /api/mihomo/start

    POST /api/mihomo/stop

    POST /api/mihomo/reload

    GET /api/mihomo/logs

------------------------------------------------------------------------

## 订阅接口

    GET /api/subscriptions

    POST /api/subscriptions

    PUT /api/subscriptions/:id

    POST /api/subscriptions/:id/update

------------------------------------------------------------------------

## 配置接口

    GET /api/config/base

    PUT /api/config/base

    POST /api/config/merge

    GET /api/config/diff

------------------------------------------------------------------------

## 冲突接口

    GET /api/conflicts

    POST /api/conflicts/:id/resolve

------------------------------------------------------------------------

# 6. 数据库设计

SQLite:

## subscriptions

字段：

    id
    name
    url
    interval
    enabled
    last_update
    status

------------------------------------------------------------------------

## base_configs

    id
    type
    content
    updated_at

------------------------------------------------------------------------

## conflicts

    id
    type
    path
    local_value
    remote_value
    resolution

------------------------------------------------------------------------

## tasks

    id
    name
    cron
    last_run
    status

------------------------------------------------------------------------

# 7. Mihomo生命周期

启动：

    Manager

     |
    生成config.yaml

     |
    启动mihomo

     |
    检查API

     |
    Ready

更新：

    Merge

     |
    validate

     |
    backup

     |
    replace

     |
    reload

------------------------------------------------------------------------

# 8. Sub-Store集成

第一阶段：

    Go

     |
    node runtime

     |
    Sub-Store

第二阶段：

Go Native Converter。

------------------------------------------------------------------------

# 9. 配置生命周期

    用户配置

          |

    base.yaml


    订阅

          |

    remote.yaml


          |

    Merge Engine


          |

    config.yaml


          |

    Mihomo

------------------------------------------------------------------------

# 10. 部署

单容器：

    AuroraMihomo

    - API
    - Scheduler
    - Mihomo
    - SubStore
    - Web

Docker:

    network_mode:host

    volume:/data
