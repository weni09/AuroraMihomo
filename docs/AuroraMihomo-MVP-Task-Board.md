# AuroraMihomo MVP Task Board

## Milestone 0 - Project Bootstrap
- [x] Initialize Go module
- [x] Initialize go-zero API
- [x] Create Vue3 project
- [x] Docker development environment
- [x] CI workflow

## Milestone 1 - Core Runtime
- [x] Mihomo process manager
- [x] Status detection
- [x] Reload API
- [x] Log streaming
- [x] Version detection
- [x] Startup bootstrap download (mihomo/zashboard)
- [x] Multi-CDN auto update settings

## Milestone 2 - Config Engine
- [x] YAML parser
- [x] Config model
- [x] Base/Remote loading
- [x] Merge Engine v1
- [x] Validation + backup/rollback
- [x] Conflict detection
- [x] Diff report
- [x] Config versions restore

## Milestone 3 - Subscription System
- [x] Subscription database
- [x] Fetch scheduler
- [x] Go-native SubStore core (multi-protocol)
- [x] Single convert API
- [x] Collection(组合订阅) + share token
- [x] Regex rewrite rules
- [x] Automatic update
- [x] Error handling

## Milestone 4 - Web API
- [x] Dashboard/system status
- [x] Mihomo control APIs
- [x] Subscription CRUD + update-now
- [x] Config base/merge/diff/versions
- [x] Conflict list/resolve
- [x] Convert/collections/sub-rules/share
- [x] Update settings APIs
- [x] WebSocket

## Milestone 5 - Frontend
- [x] Dashboard
- [x] Subscriptions
- [x] Settings (auto-update/cron/CDN)
- [x] Conflicts
- [x] Diff
- [x] Versions
- [x] Collections
- [x] Full config schema forms
- [x] Log viewer

## Milestone 6 - Production
- [~] Docker image skeleton
- [ ] Multi-arch build
- [x] Upgrade manager (mihomo/zashboard + CDN)
- [~] Backup restore (versions API done; full DR pending)
- [x] Documentation

## Notes
- Go SubStore parsers: clash-yaml / share-links / v2ray-json / sip008 / surge / quantumultx
- Node substore wrapper retained only as legacy fallback path
