# AuroraMihomo API Specification

## 1. API Overview

Framework:

    go-zero

Protocol:

    REST + WebSocket

Base:

    /api/v1

------------------------------------------------------------------------

# 2. System API

## Status

GET

    /system/status

Response:

``` json
{
 "version":"1.0",
 "uptime":12345,
 "mihomo":"running"
}
```

------------------------------------------------------------------------

# 3. Mihomo API

## Status

GET

    /mihomo/status

## Reload

POST

    /mihomo/reload

## Restart

POST

    /mihomo/restart

## Logs

GET

    /mihomo/logs

------------------------------------------------------------------------

# 4. Subscription API

## List

GET

    /subscriptions

## Create

POST

    /subscriptions

Request:

``` json
{
"name":"airport",

"url":"http://xxx"
}
```

## Update

POST

    /subscriptions/{id}/update

流程：

    fetch

    ↓

    convert

    ↓

    merge

    ↓

    reload

------------------------------------------------------------------------

# 5. Config API

## Get Base Config

GET

    /config/base

## Update Base Config

PUT

    /config/base

## Merge

POST

    /config/merge

Response:

``` json
{
"success":true,
"conflicts":3
}
```

------------------------------------------------------------------------

# 6. Conflict API

## List

GET

    /conflicts

## Resolve

POST

    /conflicts/{id}/resolve

Request:

``` json
{
"resolution":"local"
}
```

------------------------------------------------------------------------

# 7. Diff API

GET

    /config/diff

返回：

``` json
{
"added":[],
"removed":[],
"changed":[]
}
```

------------------------------------------------------------------------

# 8. Update API

## Check

GET

    /update/check

## Upgrade Mihomo

POST

    /update/mihomo

## Upgrade Zashboard

POST

    /update/zashboard

------------------------------------------------------------------------

# 9. WebSocket Events

Endpoint:

    /ws

事件：

## mihomo.status

``` json
{
"type":"mihomo.status",
"status":"running"
}
```

------------------------------------------------------------------------

## task.progress

``` json
{
"type":"task.progress",
"name":"subscription_update",
"percent":80
}
```

------------------------------------------------------------------------

# 10. go-zero Definition Example

``` go
service aurora-api {

 get /api/v1/system/status returns Status

 post /api/v1/mihomo/reload returns Result

 get /api/v1/subscriptions returns []Subscription

 post /api/v1/config/merge returns MergeResult

}
```

------------------------------------------------------------------------

# 11. Authentication Future

预留：

    JWT

    Token

    API Key
