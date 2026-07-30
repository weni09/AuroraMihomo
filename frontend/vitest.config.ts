import { fileURLToPath } from 'node:url'
import { mergeConfig, defineConfig } from 'vitest/config'
import viteConfig from './vite.config'

/**
 * 测试配置单独成文件，与 vite.config.ts 合并而非内联到其中：
 * 后者会让构建产物也带上 test 段的类型依赖（vitest/config），
 * 而生产构建不需要 vitest。
 */
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      // happy-dom 而非 jsdom：启动快得多，本项目只需要基本 DOM 与
      // document.body/classList，用不到 jsdom 的完整浏览器语义
      environment: 'happy-dom',
      // tests/ 放构建期的静态约束检查（读源码文件做断言，用到 node:fs），
      // 与 src/ 下的组件测试分开：后者归 tsconfig.app.json（DOM 环境、
      // 不带 node 类型），把两者混在一处会让 type-check 失败
      include: ['src/**/*.{test,spec}.ts', 'tests/**/*.{test,spec}.ts'],
      root: fileURLToPath(new URL('./', import.meta.url)),
      // 每个测试后自动卸载组件、清理 body，避免弹窗测试之间
      // 通过 document.body 的 overflow-hidden class 互相污染
      restoreMocks: true,
      clearMocks: true,
    },
  }),
)
