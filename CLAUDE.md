# AuroraMihomo — Claude Code 入口

本文件是 Claude Code（含 Desktop Code 会话）的项目记忆入口。
跨前后端规范与目录约定统一维护在 `AGENTS.md`，请以它为准，避免双份漂移。

@AGENTS.md

## Claude Code 在本仓库的配置

| 资源 | 位置 | 说明 |
|---|---|---|
| Skills | `.claude/skills/` → `.agents/skills/` | 目录联接，改一处两边生效；ZCode 读 `.agents/skills/`，Claude 读 `.claude/skills/` |
| MCP | 仓库根 `.mcp.json` | 项目级，团队可共享；首次打开需批准 |
| 本地覆盖 | `.claude/settings.local.json` | 本机权限/审批，已 gitignore，勿提交 |
| 用户级插件 | `~/.claude/settings.json` | 如 superpowers；不随仓库分发 |

### 常用 MCP

- `shadcn-vue`：查询/浏览/安装组件注册表；通过 `mcp -c frontend` 指向 `frontend/components.json`。

### Desktop 使用提示

1. 用 Claude Desktop 打开本仓库目录 `D:\goWork\AuroraMihomo`（或你的检出路径）。
2. 信任工作区后，批准 `.mcp.json` 中的服务器（或看 `.claude/settings.json` 是否已 `enableAllProjectMcpServers`）。
3. 在会话里用 `/mcp` 查看连接状态；用 `/skills` 或自然语言触发 skill。
4. **Cowork / 云端会话**不会读取本机 `~/.claude/skills/`；项目 skill 需在 `.claude/skills/`（本仓库已配置）。账号级 skill 在 Desktop **Customize** 或 claude.ai 里启用。
