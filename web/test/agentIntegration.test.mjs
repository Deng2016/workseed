import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'

const source = await readFile(new URL('../src/agentIntegration.ts', import.meta.url), 'utf8')
const skill = await readFile(new URL('../src/skills/workseed-auto-work/SKILL.md', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
})
const integration = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)

test('使用当前端口生成本机 MCP 地址', () => {
  assert.equal(integration.buildWorkseedMcpEndpoint('8868'), 'http://127.0.0.1:8868/mcp')
})

test('生成可复制的 Workseed MCP 配置', () => {
  assert.equal(
    integration.formatWorkseedMcpConfig('8868'),
    `{
  "mcpServers": {
    "workseed": {
      "url": "http://127.0.0.1:8868/mcp"
    }
  }
}`,
  )
})

test('随前端提供完整的 workseed-auto-work Skill', () => {
  assert.match(skill, /^---\nname: workseed-auto-work\n/)
  assert.match(skill, /事种ID:/)
  assert.match(skill, /complete_seed/)
  assert.match(skill, /"inputTokens": INPUT_TOKENS/)
  assert.match(skill, /"commitId": "COMMIT_ID"/)
  assert.match(skill, /"implementationApproach": "IMPLEMENTATION_APPROACH"/)
  assert.match(skill, /## 退出概览/)
})
