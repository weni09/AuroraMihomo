# 说明：本目录当前未被代码使用

`backend/` 中的建表逻辑完全依赖 GORM 的 `AutoMigrate`
（见 `backend/internal/repository/database.go` 的 `NewDatabase` 函数），
本目录下的 `.sql` 文件**从未被任何 Go 代码读取或执行**。

## 现状

- `001_init.sql`、`002_conflict_versions_substore.sql`、`003_substore_deep.sql`
  是项目早期规划 `golang-migrate` 迁移方案时留下的草稿。
- 这些文件里的表结构已经与 `backend/internal/model/models.go` 中的实际
  GORM 模型**严重脱节**（例如 `subscriptions` 表缺少 `content`、
  `operators`、`share_token`、`upload`、`download` 等后续新增字段）。
  **不要**把这里的 SQL 当作当前数据库结构的参考依据。

## 如果要接入 golang-migrate

需要：
1. 重新生成与 `models.go` 完全对齐的迁移脚本（而不是修补这三个旧文件）；
2. 在 `NewDatabase` 中用迁移执行替换或补充 `AutoMigrate` 调用；
3. 补充回滚（down）脚本与迁移版本表。

在完成上述接入前，本目录内容仅作历史参考，不代表任何生效的数据库契约。
