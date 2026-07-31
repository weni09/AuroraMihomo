#!/usr/bin/env node
// 把 userdocs/ 下的用户文档同步到前端内置副本。
//
// 目录划分：userdocs/ 放面向最终用户的文档，docs/ 放开发设计文档
// （架构、数据模型、TDD 等）。两类读者与维护节奏都不同，不混在一起。
//
// 为什么要复制而不是直接 import：文档页要在离线环境也能看，所以内容得
// 打进前端产物；而 userdocs/ 位于前端目录之外，直接 import 需要放宽
// dev server 的 fs 白名单（等于把仓库上层目录暴露出去）。复制到
// frontend/src/content/ 下最省心，代价是多一步同步，
// 用 --check 在 CI 里兜住不一致。
//
// 用法：
//   node scripts/sync-docs.mjs           同步
//   node scripts/sync-docs.mjs --check   只校验是否一致，不一致则退出码 1

import { readFileSync, writeFileSync, existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')

/** 源文件 -> 前端副本 */
const PAIRS = [['userdocs/user-guide.md', 'frontend/src/content/user-guide.md']]

const checkOnly = process.argv.includes('--check')
let failed = 0

for (const [src, dst] of PAIRS) {
  const srcPath = join(root, src)
  const dstPath = join(root, dst)

  if (!existsSync(srcPath)) {
    console.error(`源文件不存在: ${src}`)
    failed = 1
    continue
  }

  const content = readFileSync(srcPath, 'utf8')
  const current = existsSync(dstPath) ? readFileSync(dstPath, 'utf8') : null

  if (current === content) {
    console.log(`一致: ${dst}`)
    continue
  }

  if (checkOnly) {
    console.error(`不一致: ${dst} 与 ${src} 不同，请运行 node scripts/sync-docs.mjs`)
    failed = 1
    continue
  }

  writeFileSync(dstPath, content)
  console.log(`已同步: ${src} -> ${dst}`)
}

process.exit(failed)
