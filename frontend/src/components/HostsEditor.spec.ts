import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import HostsEditor from './HostsEditor.vue'

type HostsMap = Record<string, unknown> | undefined

/** 挂载并收集所有回传值，便于断言「最后一次写回是什么」 */
function mountEditor(modelValue?: Record<string, unknown>) {
  const emitted: HostsMap[] = []
  const wrapper = mount(HostsEditor, {
    props: {
      modelValue,
      // 受控组件：把回传值写回 props，模拟父组件（ConfigView）的行为。
      // 少了这一步就测不到「回传后外部值变化会不会打断输入」这条路径
      'onUpdate:modelValue': async (v: HostsMap) => {
        emitted.push(v)
        await wrapper.setProps({ modelValue: v ?? {} })
      },
    },
  })
  return { wrapper, emitted }
}

const domainInputs = (wrapper: ReturnType<typeof mount>) =>
  wrapper.findAll('input').filter((i) => i.attributes('aria-label')?.includes('的域名'))
const targetInputs = (wrapper: ReturnType<typeof mount>) =>
  wrapper.findAll('input').filter((i) => i.attributes('aria-label')?.includes('指向的地址'))
const addButton = (wrapper: ReturnType<typeof mount>) =>
  wrapper.findAll('button').find((b) => b.text().includes('添加映射'))!

describe('HostsEditor 值形态的读写', () => {
  it('单值与数组分别铺成一行，数组以逗号分隔回显', () => {
    const { wrapper } = mountEditor({
      'router.local': '192.168.1.1',
      'multi.local': ['10.0.0.1', '10.0.0.2'],
    })

    const targets = targetInputs(wrapper)
    expect(domainInputs(wrapper).map((i) => i.element.value)).toEqual(['router.local', 'multi.local'])
    expect(targets.map((i) => i.element.value)).toEqual(['192.168.1.1', '10.0.0.1, 10.0.0.2'])
  })

  it('单个目标写回标量，多个目标写回数组', async () => {
    const { wrapper, emitted } = mountEditor({ 'a.local': '1.1.1.1' })

    await targetInputs(wrapper)[0]!.setValue('2.2.2.2, 3.3.3.3')
    expect(emitted.at(-1)).toEqual({ 'a.local': ['2.2.2.2', '3.3.3.3'] })

    await targetInputs(wrapper)[0]!.setValue('4.4.4.4')
    expect(emitted.at(-1)).toEqual({ 'a.local': '4.4.4.4' })
  })

  /**
   * 回归：域名是 map 的键，逐字符编辑不能丢值。
   * 直接把 map 绑到输入框时，`a.local` → `b.local` 的中间态会不断删旧键建新键，
   * 值在某一步就被丢掉（或撞上同名键把别人覆盖了）。
   */
  it('逐字符改域名时值始终跟随，不丢失', async () => {
    const { wrapper, emitted } = mountEditor({ 'a.local': '1.1.1.1' })
    const input = domainInputs(wrapper)[0]!

    for (const text of ['a.loca', 'a.loc', 'a.lo', 'b.lo', 'b.loc', 'b.local']) {
      await input.setValue(text)
    }

    expect(emitted.at(-1)).toEqual({ 'b.local': '1.1.1.1' })
    // 中途每一次回传都必须带着值，不能出现空值
    for (const e of emitted) {
      expect(Object.values(e!)).toEqual(['1.1.1.1'])
    }
  })

  it('域名清空的中间态不产生键，也不影响其他行', async () => {
    const { wrapper, emitted } = mountEditor({ 'a.local': '1.1.1.1', 'b.local': '2.2.2.2' })

    await domainInputs(wrapper)[0]!.setValue('')

    expect(emitted.at(-1)).toEqual({ 'b.local': '2.2.2.2' })
  })

  it('新增的空行不写入 map，填了域名才生效', async () => {
    const { wrapper, emitted } = mountEditor({ 'a.local': '1.1.1.1' })

    await addButton(wrapper).trigger('click')
    // 点「添加映射」本身不该产生回传：空行没有对应的键
    expect(emitted).toHaveLength(0)

    await targetInputs(wrapper)[1]!.setValue('9.9.9.9')
    expect(emitted.at(-1)).toEqual({ 'a.local': '1.1.1.1' })

    await domainInputs(wrapper)[1]!.setValue('new.local')
    expect(emitted.at(-1)).toEqual({ 'a.local': '1.1.1.1', 'new.local': '9.9.9.9' })
  })

  it('删除最后一条时回传 undefined，让调用方删键而不是写空映射', async () => {
    const { wrapper, emitted } = mountEditor({ 'a.local': '1.1.1.1' })

    const removeBtn = wrapper.findAll('button').find((b) => b.attributes('aria-label')?.includes('删除第 1 条'))
    await removeBtn!.trigger('click')

    expect(emitted.at(-1)).toBeUndefined()
  })

  it('重复域名给出提示但不阻止输入', async () => {
    const { wrapper } = mountEditor({ 'a.local': '1.1.1.1', 'b.local': '2.2.2.2' })

    await domainInputs(wrapper)[1]!.setValue('a.local')

    expect(wrapper.text()).toContain('域名重复，只有最后一条会生效')
  })

  /**
   * 形态特殊的条目（嵌套结构等）必须原样保留：
   * 强行塞进两个输入框就得改形，等于悄悄替用户改了配置。
   */
  it('无法用输入框表达的条目只读，且保存时原样保留', async () => {
    const weird = { nested: { key: 'value' } }
    const { wrapper, emitted } = mountEditor({ 'weird.local': weird, 'a.local': '1.1.1.1' })

    expect(wrapper.text()).toContain('该条目结构特殊')
    // 只读行不渲染指向输入框，但域名行仍在（便于识别是哪一条）
    expect(targetInputs(wrapper)).toHaveLength(1)

    await targetInputs(wrapper)[0]!.setValue('5.5.5.5')
    expect(emitted.at(-1)).toEqual({ 'weird.local': weird, 'a.local': '5.5.5.5' })
  })

  it('外部值整体替换时重新铺开（对应「放弃修改，重新加载」）', async () => {
    const { wrapper } = mountEditor({ 'a.local': '1.1.1.1' })

    await wrapper.setProps({ modelValue: { 'x.local': '8.8.8.8', 'y.local': '9.9.9.9' } })

    expect(domainInputs(wrapper).map((i) => i.element.value)).toEqual(['x.local', 'y.local'])
  })

  it('未配置 hosts 时显示空态并可新增', async () => {
    const { wrapper } = mountEditor(undefined)

    expect(wrapper.text()).toContain('尚未配置任何 hosts 映射')
    await addButton(wrapper).trigger('click')
    expect(domainInputs(wrapper)).toHaveLength(1)
  })
})
