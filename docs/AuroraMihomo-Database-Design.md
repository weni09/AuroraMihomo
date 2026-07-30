# AuroraMihomo Database Design

## 1. Database Overview

AuroraMihomo uses SQLite as the default embedded database.

Goals:

-   Zero external database dependency
-   Suitable for NAS / Docker / Home Server
-   Support configuration history
-   Support rollback
-   Support task scheduling
-   Support conflict management

Database:

    data/aurora.db

------------------------------------------------------------------------

# 2. Core Tables

## 2.1 subscriptions

订阅源管理。

``` sql
CREATE TABLE subscriptions (
 id INTEGER PRIMARY KEY,

 name TEXT NOT NULL,

 url TEXT NOT NULL,

 type TEXT DEFAULT 'mihomo',

 enabled INTEGER DEFAULT 1,

 interval INTEGER DEFAULT 3600,

 last_update DATETIME,

 status TEXT,

 error_message TEXT,

 created_at DATETIME,

 updated_at DATETIME
);
```

------------------------------------------------------------------------

## 2.2 configs

配置存储。

保存：

-   base.yaml
-   remote.yaml
-   override.yaml
-   final.yaml

``` sql
CREATE TABLE configs (

 id INTEGER PRIMARY KEY,

 name TEXT,

 type TEXT,

 content TEXT,

 version INTEGER,

 created_at DATETIME

);
```

type:

    base

    remote

    override

    merged

------------------------------------------------------------------------

# 3. Configuration Version

## config_versions

用于回滚。

``` sql
CREATE TABLE config_versions (

 id INTEGER PRIMARY KEY,

 config TEXT,

 hash TEXT,

 file_path TEXT,

 created_at DATETIME

);
```

流程：

    merge

    ↓

    backup

    ↓

    validate

    ↓

    activate

------------------------------------------------------------------------

# 4. Conflict Storage

## conflicts

保存配置冲突。

``` sql
CREATE TABLE conflicts (

 id INTEGER PRIMARY KEY,

 type TEXT,

 path TEXT,

 local_value TEXT,

 remote_value TEXT,

 resolution TEXT,

 resolved INTEGER DEFAULT 0,

 created_at DATETIME

);
```

resolution:

    local

    remote

    merge

    manual

------------------------------------------------------------------------

# 5. Scheduler

## tasks

后台任务。

``` sql
CREATE TABLE tasks (

 id INTEGER PRIMARY KEY,

 name TEXT,

 cron TEXT,

 enabled INTEGER,

 last_run DATETIME,

 next_run DATETIME,

 status TEXT

);
```

任务：

    subscription_update

    config_merge

    mihomo_reload

    version_check

------------------------------------------------------------------------

# 6. Mihomo Runtime

## mihomo_state

``` sql
CREATE TABLE mihomo_state (

 id INTEGER PRIMARY KEY,

 version TEXT,

 pid INTEGER,

 status TEXT,

 started_at DATETIME

);
```

------------------------------------------------------------------------

# 7. User Settings

## settings

保存系统设置。

``` sql
CREATE TABLE settings (

 key TEXT PRIMARY KEY,

 value TEXT

);
```

例如：

    merge.policy=local

    mihomo.port=9090

------------------------------------------------------------------------

# 8. Index Design

推荐：

    subscriptions.url

    configs.type

    conflicts.resolved

    tasks.enabled

建立索引提升查询。

------------------------------------------------------------------------

# 9. Migration

使用：

    golang-migrate

目录：

    migrations/

    001_init.sql

    002_config_version.sql

    003_conflict.sql

------------------------------------------------------------------------

# 10. Future Extension

支持：

-   多用户
-   云同步
-   Git版本
-   配置分享
