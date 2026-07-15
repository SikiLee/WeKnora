import assert from 'node:assert/strict'
import test from 'node:test'

import { buildHistoricalAgentAnswerEvent } from './historicalAgentEvents'

test('historical agent answer keeps the persisted completion state', () => {
  assert.deepEqual(buildHistoricalAgentAnswerEvent('partial answer', false, false), {
    type: 'answer',
    content: 'partial answer',
    done: false,
  })
  assert.deepEqual(buildHistoricalAgentAnswerEvent('complete answer', true, true), {
    type: 'answer',
    content: 'complete answer',
    done: true,
    is_fallback: true,
  })
})

test('historical agent answer omits blank content', () => {
  assert.equal(buildHistoricalAgentAnswerEvent('   ', false, false), undefined)
})
