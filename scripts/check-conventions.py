#!/usr/bin/env python3
"""开发规范的可机检部分。

只检查能可靠自动判定的规则，主观项（注释是否讲清了"为什么"、
命名是否贴切）仍需人工评审，不在此列。

规则来自 AGENTS.md、backend/AGENTS.md 与 frontend/AGENTS.md：
  BE1  SQL 只能出现在 internal/repository 与 internal/model
  BE2  goctl 生成物（含 DO NOT EDIT 头）不得手改 —— 校验头部完好
  BE3  logic 层用 l.Error/l.Info，不直接调用全局 logx.Error/logx.Info
  FE1  禁止硬编码中性色（slate/gray/zinc/neutral/stone），须用主题 token
  FE2  前端不得引入 shadcn 语义 token —— 当前未定义，写了不生效
  SK1  skill 文档不得含外部机器的绝对路径

用法：
  python scripts/check-conventions.py            # 全量
  python scripts/check-conventions.py --only BE  # 只跑后端规则
退出码非 0 表示存在违规。
"""

from __future__ import annotations

import argparse
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


class Finding:
    def __init__(self, rule: str, path: str, line: int, text: str, hint: str):
        self.rule, self.path, self.line, self.text, self.hint = rule, path, line, text, hint


def walk(rel_dir: str, exts: tuple[str, ...], skip_tests: bool = False):
    base = os.path.join(ROOT, rel_dir)
    for dirpath, dirnames, filenames in os.walk(base):
        dirnames[:] = [d for d in dirnames if d not in
                       ("node_modules", "dist", "__pycache__", ".git", "coverage")]
        for fn in filenames:
            if not fn.endswith(exts):
                continue
            if skip_tests and fn.endswith("_test.go"):
                continue
            full = os.path.join(dirpath, fn)
            rel = os.path.relpath(full, ROOT).replace(os.sep, "/")
            try:
                yield rel, open(full, encoding="utf-8", errors="replace").read()
            except OSError:
                continue


def strip_go_noise(line: str) -> str:
    """去掉行注释与字符串字面量，避免把注释/日志文案当成代码匹配。"""
    line = re.sub(r"//.*$", "", line)
    line = re.sub(r'"(?:[^"\\]|\\.)*"', '""', line)
    line = re.sub(r"`[^`]*`", "``", line)
    return line


# ---------- 后端 ----------

# 只认真正的 SQL 调用与语句，且要求 SELECT 后面跟得像字段列表，
# 以免匹配到 Go 的 select {} 语句（此前误报的根源）。
SQL_RX = re.compile(
    r"""(?:
          \.Raw\s*\(
        | \.Exec\s*\(\s*["`]           # Exec 后紧跟字符串才算 SQL，排除进程 Exec
        | \bINSERT\s+INTO\b
        | \bDELETE\s+FROM\b
        | \bUPDATE\s+\w+\s+SET\b
        | \bSELECT\s+(?:\*|\w+(?:\s*,\s*\w+)*)\s+FROM\b
    )""",
    re.I | re.X,
)


def check_be1_sql_layer() -> list[Finding]:
    out = []
    for rel, text in walk("backend", (".go",), skip_tests=True):
        if "/internal/repository/" in rel or "/internal/model/" in rel:
            continue
        for i, raw in enumerate(text.split("\n"), 1):
            line = strip_go_noise(raw)
            if SQL_RX.search(line):
                out.append(Finding(
                    "BE1", rel, i, raw.strip(),
                    "SQL 只能写在 internal/repository 或 internal/model",
                ))
    return out


def check_be2_generated() -> list[Finding]:
    """生成物必须保留 DO NOT EDIT 头。头没了通常意味着被手改过。"""
    targets = [
        "backend/api/internal/handler/routes.go",
        "backend/api/internal/types/types.go",
    ]
    out = []
    for rel in targets:
        full = os.path.join(ROOT, rel)
        if not os.path.isfile(full):
            out.append(Finding("BE2", rel, 0, "(文件缺失)",
                               "goctl 生成物缺失，请重新生成"))
            continue
        head = open(full, encoding="utf-8", errors="replace").read(200)
        if "DO NOT EDIT" not in head:
            out.append(Finding("BE2", rel, 1, head.split("\n")[0],
                               "生成物的 DO NOT EDIT 头丢失，疑似被手改"))
    return out


LOGX_CALL_RX = re.compile(r"\blogx\.(Error|Info|Debug|Slow|Severe)\w*\s*\(")


def check_be3_logger() -> list[Finding]:
    """logic 层应使用内嵌的 l.Error/l.Info，而非全局 logx。

    logx.WithContext / logx.Field 等构造用法不算违规。
    """
    out = []
    for rel, text in walk("backend/api/internal/logic", (".go",), skip_tests=True):
        for i, raw in enumerate(text.split("\n"), 1):
            line = strip_go_noise(raw)
            if LOGX_CALL_RX.search(line):
                out.append(Finding(
                    "BE3", rel, i, raw.strip(),
                    "logic 层请用 l.Error(...) / l.Info(...) 而非全局 logx",
                ))
    return out


# ---------- 前端 ----------

NEUTRAL_RX = re.compile(
    r"\b(?:bg|text|border|ring|from|to|via|divide|placeholder|shadow|outline|accent|caret|decoration)"
    r"-(?:slate|gray|zinc|neutral|stone)-\d{2,3}\b"
)

# shadcn 语义 token 的工具类。
#
# 这条规则最初是"一律禁止"，因为项目当时没有定义这组 token，写了不生效。
# 项目接入 shadcn-vue 后，main.css 已把这些变量接到既有 --c-* 上、
# tailwind.config.js 也做了映射，它们现在是生效的。
# 因此规则改为"必须有定义支撑"：token 在 tailwind 配置里注册过就放行，
# 没注册的仍然拦住——那才是真正写了不生效的情况。
#
# 显式列举 shadcn 的语义 token 名。
#
# 曾尝试改成"匹配所有 bg-*/text-* 再查是否注册"，结果误报 314 处：
# base.css 里的 CSS 变量名（--vt-c-text-light-1）、border-hover 这类
# 非颜色工具类都会被当成 token。语义 token 是一个有限且明确的集合，
# 列举它才能既准又稳。新增 token（sidebar / chart 等）需同步这份名单，
# 下面的自检测试会在 tailwind 注册了却没列进来时提醒。
SHADCN_TOKENS = (
    "background", "foreground", "card", "popover", "primary", "secondary",
    "muted", "accent", "destructive", "border", "input", "ring",
    "sidebar", "chart",
)

SHADCN_TOKEN_RX = re.compile(
    r"\b(?:bg|text|border|ring|divide|outline)"
    r"-(" + "|".join(SHADCN_TOKENS) + r")(-foreground)?\b"
)

# tailwind.config.js 里注册过的颜色名视为已定义。
# 读配置而非维护一份硬编码清单：清单会与配置脱节，
# 而脱节的方向恰好是"配置里删了 token、检查却仍放行"，属于静默失效。
TAILWIND_COLOR_RX = re.compile(r"^\s*'?([a-zA-Z][\w-]*)'?\s*:\s*", re.M)


def defined_tailwind_colors() -> set[str]:
    """取 tailwind.config.js 中 colors 段注册的颜色名（含 foreground 子键）。"""
    path = os.path.join(ROOT, "frontend", "tailwind.config.js")
    try:
        with open(path, encoding="utf-8") as f:
            text = f.read()
    except OSError:
        return set()
    # 只看 colors: { ... } 内部，避免把 fontFamily 等其它段的键也算进来
    start = text.find("colors:")
    if start < 0:
        return set()
    depth, end = 0, len(text)
    for i in range(text.find("{", start), len(text)):
        if text[i] == "{":
            depth += 1
        elif text[i] == "}":
            depth -= 1
            if depth == 0:
                end = i
                break
    section = text[start:end]
    names = set(TAILWIND_COLOR_RX.findall(section))
    # DEFAULT/foreground 是子键名，不是可用的 token 名
    return {n for n in names if n not in {"DEFAULT", "colors", "extend"}}


def check_fe1_neutral_colors() -> list[Finding]:
    out = []
    for rel, text in walk("frontend/src", (".vue", ".ts", ".css")):
        for i, raw in enumerate(text.split("\n"), 1):
            for m in NEUTRAL_RX.finditer(raw):
                out.append(Finding(
                    "FE1", rel, i, m.group(0),
                    "改用主题 token：canvas/surface/elevated/fg/fg-muted/line",
                ))
    return out


def check_fe2_shadcn_tokens() -> list[Finding]:
    """拦住未在 tailwind 配置里注册的语义 token（写了不生效）。"""
    defined = defined_tailwind_colors()
    out = []
    for rel, text in walk("frontend/src", (".vue", ".ts", ".css")):
        # main.css 与 tailwind 配置本身就是定义这些 token 的地方
        if rel.endswith("assets/main.css"):
            continue
        for i, raw in enumerate(text.split("\n"), 1):
            for m in SHADCN_TOKEN_RX.finditer(raw):
                base = m.group(1)
                if base in defined:
                    continue
                out.append(Finding(
                    "FE2", rel, i, m.group(0),
                    f"'{base}' 未在 tailwind.config.js 的 colors 中注册，写了不生效",
                ))
    return out


# ---------- skill ----------

FOREIGN_PATH_RX = re.compile(r"/root/\.openclaw|\.claude/skills|/home/\w+/\.(?:openclaw|claude)")


def check_sk1_foreign_paths() -> list[Finding]:
    out = []
    skills = os.path.join(ROOT, ".agents", "skills")
    if not os.path.isdir(skills):
        return out
    for rel, text in walk(".agents/skills", (".md",)):
        for i, raw in enumerate(text.split("\n"), 1):
            if raw.lstrip().startswith(">"):
                continue  # 引用块是我们标注"此依赖不可用"的说明，允许提及
            if FOREIGN_PATH_RX.search(raw):
                out.append(Finding(
                    "SK1", rel, i, raw.strip()[:100],
                    "skill 脚本路径应为 .agents/skills/...（本仓库布局）",
                ))
    return out


CHECKS = [
    ("BE1", "SQL 分层", check_be1_sql_layer),
    ("BE2", "goctl 生成物完好", check_be2_generated),
    ("BE3", "logic 层日志用法", check_be3_logger),
    ("FE1", "禁用硬编码中性色", check_fe1_neutral_colors),
    ("FE2", "禁用未定义的 shadcn token", check_fe2_shadcn_tokens),
    ("SK1", "skill 无外部绝对路径", check_sk1_foreign_paths),
]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--only", default="", help="只跑指定前缀的规则，如 BE / FE / SK")
    ap.add_argument("--baseline", default="", metavar="FILE",
                    help="基线文件；不超过其中记录的数量不计入失败（存量债务）")
    ap.add_argument("--update-baseline", action="store_true",
                    help="把当前违规写入基线文件（仅在明确接受现状时使用）")
    args = ap.parse_args()

    # 基线按 (规则, 文件) 记数而非仅记文件名：
    # 只记文件名会放过同一文件里新增的违规，等于给存量文件开了永久后门。
    baseline: dict[str, int] = {}
    if args.baseline and os.path.isfile(args.baseline):
        for ln in open(args.baseline, encoding="utf-8"):
            ln = ln.strip()
            if not ln or ln.startswith("#"):
                continue
            parts = ln.rsplit(" ", 1)
            if len(parts) == 2 and parts[1].isdigit():
                baseline[parts[0]] = int(parts[1])

    all_findings: list[Finding] = []
    total_new = 0
    total_baselined = 0

    for code, title, fn in CHECKS:
        if args.only and not code.startswith(args.only):
            continue
        findings = fn()
        all_findings.extend(findings)

        # 按 (规则, 文件) 分组，与基线的允许数量比对
        grouped: dict[str, list[Finding]] = {}
        for f in findings:
            grouped.setdefault(f"{f.rule} {f.path}", []).append(f)

        new: list[Finding] = []
        rule_baselined = 0
        over_baselined_files = set()
        for key, items in grouped.items():
            allowed = baseline.get(key, 0)
            if len(items) > allowed:
                new.extend(items[allowed:])
                if allowed:
                    # 该文件本就有基线额度，脚本无法判定具体哪一处是新增的，
                    # 报出的行号仅供定位文件，需人工对比基线数量。
                    over_baselined_files.add(items[0].path)
            rule_baselined += min(len(items), allowed)
        total_baselined += rule_baselined

        if not new:
            suffix = f"（基线内 {rule_baselined} 处待清理）" if rule_baselined else ""
            print(f"  \033[32mPASS\033[0m {code} {title}{suffix}")
            continue
        print(f"  \033[31mFAIL\033[0m {code} {title} — {len(new)} 处新增违规")
        for f in new[:15]:
            note = "  (该文件已有基线额度，行号仅供定位文件)" \
                if f.path in over_baselined_files else ""
            print(f"        {f.path}:{f.line}  {f.text}{note}")
            print(f"          → {f.hint}")
        if len(new) > 15:
            print(f"        ...另有 {len(new) - 15} 处")
        total_new += len(new)

    if args.update_baseline:
        if not args.baseline:
            print("--update-baseline 需要同时指定 --baseline FILE", file=sys.stderr)
            return 2
        counts: dict[str, int] = {}
        for f in all_findings:
            key = f"{f.rule} {f.path}"
            counts[key] = counts.get(key, 0) + 1
        with open(args.baseline, "w", encoding="utf-8", newline="\n") as fh:
            fh.write("# 规范检查基线：记录接受现状的存量违规，格式为「规则 文件 数量」。\n")
            fh.write("# 由 scripts/check-conventions.py --update-baseline 生成。\n")
            fh.write("# 只减不增：修好存量后请同步下调数字，新代码不得写进这里。\n")
            for key in sorted(counts):
                fh.write(f"{key} {counts[key]}\n")
        print(f"\n基线已写入 {args.baseline}（{len(counts)} 条，{sum(counts.values())} 处）。")
        return 0

    print()
    if total_baselined:
        print(f"基线内已知违规 {total_baselined} 处（不计入失败，请逐步清理）。")
    if total_new:
        print(f"\033[31m规范检查未通过：{total_new} 处新增违规。\033[0m")
        return 1
    print("\033[32m规范检查通过。\033[0m")
    return 0


if __name__ == "__main__":
    sys.exit(main())
