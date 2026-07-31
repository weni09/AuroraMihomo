# 前端内置文档

`user-guide.md` 是 `userdocs/user-guide.md` 的副本，由
`make sync-docs`（或 `node scripts/sync-docs.mjs`）同步而来。

为什么要复制而不是跨目录 `import '../../userdocs/...?raw'`：
`userdocs/` 在前端目录之外，dev server 的 fs 白名单默认不含它，
放行等于把仓库上层目录暴露给开发服务器。复制的代价是需要同步一步，
但换来构建与 dev 行为一致、且不放宽文件访问范围。

**不要直接编辑本目录下的 md**，改 `userdocs/` 下的原件再同步。
CI 会校验两者一致。

目录划分：`userdocs/` 是面向最终用户的文档，`docs/` 是开发设计文档
（架构、数据模型、TDD 等），两者不混用。
