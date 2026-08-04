# Claude Code skills（本机联接）

Claude Code 只从 `.claude/skills/<name>/SKILL.md` 发现项目 skill。
本仓库的 skill **源目录**是 `.agents/skills/`（与 ZCode 等工具共用）。

## 本机准备

在仓库根执行一次：

```bash
bash scripts/setup-claude-desktop.sh
```

脚本会在本目录下为每个 skill 创建指向 `.agents/skills/<name>` 的联接（Windows: junction；其它平台: 相对 symlink）。

联接是本机产物，**不入库**；不要把 skill 正文复制进这里。
