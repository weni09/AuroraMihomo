# AuroraMihomo Code Architecture

## 1. Overview

This document defines the internal Go code architecture of AuroraMihomo.

Design goals:

-   Clear module boundaries
-   Easy testing
-   Support future Mihomo embedding
-   Support Sub-Store replacement
-   Suitable for long-running service

------------------------------------------------------------------------

# 2. Repository Structure

    AuroraMihomo/

    ├── cmd/
    │
    │   ├── server/
    │   │   └── main.go
    │   │
    │   └── worker/
    │       └── main.go
    │

    ├── apps/

    │   └── api/

    │       ├── aurora.api
    │       └── internal/

    │

    ├── internal/

    │   ├── mihomo/
    │   ├── config/
    │   ├── merge/
    │   ├── substore/
    │   ├── scheduler/
    │   ├── updater/
    │   ├── storage/
    │   └── runtime/

    │

    ├── pkg/

    │   ├── yaml/
    │   ├── logger/
    │   └── utils/

    └── web/

------------------------------------------------------------------------

# 3. Module Responsibilities

## mihomo

Responsible:

-   process lifecycle
-   status detection
-   reload
-   logs
-   version management

Interface:

``` go
type Manager interface {

 Start() error

 Stop() error

 Restart() error

 Reload() error

 Status() Status

}
```

------------------------------------------------------------------------

## config

Responsible:

-   yaml loading
-   model conversion
-   config generation

Structure:

    config/

    model.go

    parser.go

    generator.go

    validator.go

------------------------------------------------------------------------

## merge

Core module.

Responsibilities:

-   semantic merge
-   conflict detection
-   policy execution
-   diff generation

Interface:

``` go
type Engine interface {

 Merge(
 base Config,
 remote Config,
 policy Policy,
 ) Result

}
```

------------------------------------------------------------------------

## substore

Responsible:

-   subscription fetching
-   conversion
-   update

Initial:

    Go
     |
    Node Runtime
     |
    SubStore

------------------------------------------------------------------------

## scheduler

Background tasks:

-   subscription update
-   merge
-   reload
-   update check

------------------------------------------------------------------------

## updater

Responsible:

-   mihomo upgrade
-   zashboard update
-   runtime update

------------------------------------------------------------------------

# 4. Dependency Direction

Allowed:

    api

     |

    service

     |

    domain

     |

    repository

Avoid:

    merge -> api

    mihomo -> web

------------------------------------------------------------------------

# 5. Service Layer

Example:

    SubscriptionService

          |

    SubscriptionRepository

          |

    SQLite

------------------------------------------------------------------------

# 6. Runtime Manager

Unified control:

    RuntimeManager

     |
     +-- Mihomo
     |
     +-- SubStore
     |
     +-- Scheduler

------------------------------------------------------------------------

# 7. Dependency Injection

Use:

-   go-zero built-in config
-   manual constructor injection

Example:

``` go
func NewService(
 repo Repository,
 merge Engine,
 mihomo Manager,
) Service
```

------------------------------------------------------------------------

# 8. Logging

Unified:

    zap/logx

Categories:

    api

    mihomo

    merge

    scheduler

    update

------------------------------------------------------------------------

# 9. Testing Strategy

Unit:

-   Merge Engine
-   Parser
-   Conflict

Integration:

-   Mihomo lifecycle
-   API

------------------------------------------------------------------------

# 10. Future Extension

Support:

-   plugin interface
-   multi-core runtime
-   cluster mode
