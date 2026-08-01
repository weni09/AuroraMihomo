// 后端返回的状态码与技术标识符统一在此映射为中文，
// 避免各页面把 ok / error / rewrite 之类原样展示给用户。

export const subStatusLabel = (s?: string): string =>
  ({ ok: '正常', error: '失败', pending: '待刷新' }[s || ''] || '未刷新')

export const taskStatusLabel = (s?: string): string =>
  ({ ok: '成功', success: '成功', error: '失败', running: '执行中', idle: '待运行' }[s || ''] || '待运行')

export const wsStatusLabel = (s?: string): string =>
  ({
    connecting: '连接中',
    live: '已连接',
    closed: '已断开',
    error: '连接异常',
    // 服务端主动通知重启（close code 1012）。与"连接异常"区分：
    // 这是预期内的短暂中断，不该让用户以为出了故障
    restarting: '服务端重启中',
  })[s || ''] ||
  s ||
  ''

// 应用日志级别（go-zero logx 的级别）。与内核日志的 stdout/stderr 不同，
// 这些是真正的严重程度分级。
export const appLogLevelLabel = (s?: string): string =>
  ({
    debug: '调试',
    info: '信息',
    error: '错误',
    slow: '慢调用',
    stat: '统计',
    severe: '严重',
  })[s || ''] ||
  s ||
  '日志'

// 内核日志级别（mihomo 自身的 log-level 用词）。与应用日志级别刻意分成两张表：
// mihomo 有 warning 而无 slow/stat/severe，合成一张会让筛选下拉框
// 出现永远筛不出内容的选项。
export const kernelLogLevelLabel = (s?: string): string =>
  ({
    debug: '调试',
    info: '信息',
    warning: '告警',
    error: '错误',
    silent: '静默',
  })[s || ''] ||
  s ||
  '日志'

export const mihomoStatusLabel = (s?: string): string =>
  ({ running: '运行中', stopped: '已停止', unknown: '状态未知' }[s || ''] || '状态未知')

// ruleScopeLabel / templateTypeLabel 已随「全局规则与模板」页面一同移除：
// 全局改写规则不再存在，模板转换并入模板文件（用配置类型标签表达）。

export const fileTypeLabel = (s?: string): string =>
  ({ raw: '纯文本', yaml: 'YAML', json: 'JSON', ini: 'INI', script: 'JS 脚本' }[s || ''] || s || '')

export const templateLangLabel = (s?: string): string =>
  ({ gotemplate: 'Go 模板', yaml: 'YAML 覆写', javascript: 'JS 脚本覆写' }[s || ''] || 'Go 模板')

// configKindLabel / resolutionLabel 已随「冲突处理」「配置差异」的旧条目列表
// 一同移除：冲突处理页已下线，配置差异页改为直接对比三份原始 YAML，
// 不再需要按 kind 打中文标签。
