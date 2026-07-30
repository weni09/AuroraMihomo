import * as yaml from 'js-yaml'

/**
 * 全项目统一的 YAML 解析入口。
 *
 * 为什么不直接用 js-yaml 的默认行为：
 * mihomo / Clash 配置广泛使用 YAML 1.1 的锚点合并来复用策略组与规则集模板，
 * 既有 `<<: *anchor` 的隐式写法，也有官方 Sub-Store 导出配置里常见的
 * `!!merge <<: *anchor` 显式标签写法。js-yaml 默认走 YAML 1.2 core schema，
 * 遇到显式 !!merge 会直接抛 "unknown scalar tag !<tag:yaml.org,2002:merge>"，
 * 比后端使用的 gopkg.in/yaml.v3 更严格。
 *
 * 两侧宽严不一致会造成很实际的问题：一份后端能正确解析、能正常渲染出 mihomo
 * 配置的模板，在前端保存前校验时被误判为非法而拦下。统一切到 YAML 1.1 schema
 * 与后端对齐，避免这类"后端明明支持、前端却不让存"的分歧。
 */
export const loadYaml = (text: string): unknown =>
  yaml.load(text, { schema: yaml.YAML11_SCHEMA })

/**
 * 序列化回 YAML。
 *
 * noRefs 是必须的。原始 `<<: *anchor` 的合并确实在 load 阶段就展开了，
 * 但展开产物里多处仍指向**同一个对象/数组实例**（合并键复用的就是同一份
 * 值），js-yaml 的 dump 默认会为这种重复引用自动生成新锚点，输出
 * `proxies: &ref_0` / `proxies: *ref_0`。
 *
 * 结果是「格式化」把 `&pr`/`*pr` 换成了 `&ref_0`/`*ref_0`，锚点数从
 * 64 降到 26 而非归零——用户点了格式化却发现锚点还在。
 * noRefs 让每处都写成完整值，代价是文件变长，但这正是格式化的目的：
 * 展开复用、便于逐条核对。
 */
export const dumpYaml = (value: unknown): string =>
  yaml.dump(value, { lineWidth: -1, noRefs: true })
