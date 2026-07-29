import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const component = readFileSync(new URL('./ChunkFeedbackSummary.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../api/knowledge-base/index.ts', import.meta.url), 'utf8')
const enUS = readFileSync(new URL('../i18n/locales/en-US.ts', import.meta.url), 'utf8')
const zhCN = readFileSync(new URL('../i18n/locales/zh-CN.ts', import.meta.url), 'utf8')

test('chunk feedback audit renders its typed trigger source', () => {
  assert.match(component, /feedback\.sources\.\$\{audit\.trigger_source \|\| 'legacy'\}/)
  assert.match(component, /\{\{\s*audit\.action\s*\}\}/)
  assert.match(component, /ref<ChunkFeedbackDetails \| null>/)
  assert.match(api, /trigger_source: ChunkFeedbackTriggerSource/)
})

test('supported fallback locales label every feedback trigger source', () => {
  for (const locale of [enUS, zhCN]) {
    for (const source of ['like', 'dislike', 'cancel', 'admin_reset', 'content_delete', 'legacy']) {
      assert.match(locale, new RegExp(`\\b${source}:`))
    }
  }
})
