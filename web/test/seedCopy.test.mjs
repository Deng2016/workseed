import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'

const source = await readFile(new URL('../src/seedCopy.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
})
const { formatSeedCopyText } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)

test('复制文本使用带标签的 ID、标题和描述格式', () => {
  assert.equal(
    formatSeedCopyText({ id: 203, title: '优化复制按钮', content: '补充 ID 字段' }),
    '事种ID: 203\n标题：优化复制按钮\n描述：补充 ID 字段',
  )
})

test('详细内容为空时仍保留描述标签', () => {
  assert.equal(
    formatSeedCopyText({ id: 204, title: '没有详细内容', content: '' }),
    '事种ID: 204\n标题：没有详细内容\n描述：',
  )
})
