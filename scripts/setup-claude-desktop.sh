#!/usr/bin/env bash
# 为本仓库创建 Claude Code Desktop 所需的本地 skill 联接。
#
# 源目录仍是 .agents/skills/（ZCode / 多工具共享）。
# Claude Code 只扫描 .claude/skills/，因此在 Windows 上用目录联接（junction）
# 映射过去，避免把 skill 内容复制进两份。
#
# 联接是本机产物，不入库（见 .gitignore）；团队成员 clone 后跑一次本脚本即可。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/.agents/skills"
DST="$ROOT/.claude/skills"

# Git Bash / MSYS 的 /d/... 路径 PowerShell 认不了，转成 Windows 路径。
to_win_path() {
  local p="$1"
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -w "$p"
    return
  fi
  # 兜底：/d/foo → D:\foo
  if [[ "$p" =~ ^/([a-zA-Z])/(.*)$ ]]; then
    echo "${BASH_REMATCH[1]^}:\\${BASH_REMATCH[2]//\//\\}"
    return
  fi
  echo "$p"
}

if [[ ! -d "$SRC" ]]; then
  echo "error: missing $SRC" >&2
  exit 1
fi

mkdir -p "$DST"

is_windows=0
case "$(uname -s 2>/dev/null || echo unknown)" in
  MINGW*|MSYS*|CYGWIN*|Windows_NT) is_windows=1 ;;
esac

link_one() {
  local name="$1"
  local target="$SRC/$name"
  local link="$DST/$name"

  if [[ ! -d "$target" ]]; then
    echo "skip: $name (source missing)"
    return 0
  fi

  if [[ -e "$link" || -L "$link" ]]; then
    if [[ "$is_windows" -eq 1 ]]; then
      local link_win
      link_win="$(to_win_path "$link")"
      powershell.exe -NoProfile -Command \
        "if (Test-Path -LiteralPath '$link_win') { Remove-Item -LiteralPath '$link_win' -Force -Recurse }" \
        >/dev/null
    else
      rm -rf "$link"
    fi
  fi

  if [[ "$is_windows" -eq 1 ]]; then
    local link_win target_win
    link_win="$(to_win_path "$link")"
    target_win="$(to_win_path "$target")"
    powershell.exe -NoProfile -Command \
      "New-Item -ItemType Junction -Path '$link_win' -Target '$target_win' | Out-Null"
  else
    ln -s "../../.agents/skills/$name" "$link"
  fi
  echo "linked $name"
}

shopt -s nullglob
for dir in "$SRC"/*/; do
  name="$(basename "$dir")"
  [[ "$name" == .* ]] && continue
  link_one "$name"
done

echo
echo "Claude skills ready under $DST"
echo "Next: open this repo in Claude Desktop / Code, trust workspace, approve .mcp.json if prompted."
echo "Verify in session: /mcp   and project skills under .claude/skills/"
