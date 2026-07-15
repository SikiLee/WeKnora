import assert from 'node:assert/strict'
import test from 'node:test'
import { ref } from 'vue'

import type { MessageFeedbackInput, MessageFeedbackState } from '../api/feedback'
import { useAnswerFeedback } from './useAnswerFeedback'

const state = (feedbackType: 'like' | 'dislike', reasonCode?: 'incorrect'): MessageFeedbackState => ({
  feedback_type: feedbackType,
  ...(reasonCode ? { reason_code: reasonCode } : {}),
  feedback_at: '2026-07-15T08:30:00Z',
})

test('feedback controls stay hidden until a completed persisted answer exists', async () => {
  const completed = ref(false)
  let calls = 0
  const model = useAnswerFeedback({
    sessionId: ref('session-1'),
    messageId: ref(''),
    completed,
    initialFeedback: ref(null),
    submit: async () => {
      calls += 1
      return { data: state('like') }
    },
  })

  assert.equal(model.canRate.value, false)
  assert.equal(await model.toggleLike(), false)
  completed.value = true
  assert.equal(model.canRate.value, false)
  assert.equal(calls, 0)
})

test('like is selected, canceled, and restored from server responses', async () => {
  const inputs: MessageFeedbackInput[] = []
  const updates: Array<MessageFeedbackState | null> = []
  const model = useAnswerFeedback({
    sessionId: 'session-1',
    messageId: 'message-1',
    completed: true,
    initialFeedback: state('like'),
    submit: async (_sessionId, _messageId, input) => {
      inputs.push(input)
      return { data: input.feedback_type === 'none' ? null : state('like') }
    },
    onUpdated: (value) => updates.push(value),
  })

  assert.equal(model.isLiked.value, true)
  assert.equal(await model.toggleLike(), true)
  assert.equal(model.feedback.value, null)
  assert.deepEqual(inputs[0], { feedback_type: 'none' })

  assert.equal(await model.toggleLike(), true)
  assert.equal(model.isLiked.value, true)
  assert.deepEqual(inputs[1], { feedback_type: 'like' })
  assert.deepEqual(updates, [null, state('like')])
})

test('dislike replaces like and sends only valid other-reason text', async () => {
  let received: MessageFeedbackInput | null = null
  const model = useAnswerFeedback({
    sessionId: 'session-1',
    messageId: 'message-1',
    completed: true,
    initialFeedback: state('like'),
    submit: async (_sessionId, _messageId, input) => {
      received = input
      return {
        data: {
          feedback_type: 'dislike',
          reason_code: input.reason_code,
          reason_text: input.reason_text,
          feedback_at: '2026-07-15T08:31:00Z',
        },
      }
    },
  })

  assert.equal(await model.submitDislike('other', '  Missing a source  '), true)
  assert.deepEqual(received, {
    feedback_type: 'dislike',
    reason_code: 'other',
    reason_text: 'Missing a source',
  })
  assert.equal(model.isLiked.value, false)
  assert.equal(model.isDisliked.value, true)
})

test('failed and concurrent requests never overwrite the prior state', async () => {
  let release: ((value: { data: MessageFeedbackState | null }) => void) | undefined
  let calls = 0
  const model = useAnswerFeedback({
    sessionId: 'session-1',
    messageId: 'message-1',
    completed: true,
    initialFeedback: state('dislike', 'incorrect'),
    submit: async () => {
      calls += 1
      return new Promise((resolve) => {
        release = resolve
      })
    },
  })

  const first = model.toggleLike()
  assert.equal(model.pending.value, true)
  assert.equal(await model.clearDislike(), false)
  assert.equal(calls, 1)
  assert.equal(model.isDisliked.value, true)
  release?.({ data: state('like') })
  assert.equal(await first, true)
  assert.equal(model.isLiked.value, true)

  const failing = useAnswerFeedback({
    sessionId: 'session-1',
    messageId: 'message-1',
    completed: true,
    initialFeedback: state('like'),
    submit: async () => { throw new Error('network unavailable') },
  })
  assert.equal(await failing.toggleLike(), false)
  assert.equal(failing.isLiked.value, true)
  assert.match(String(failing.error.value), /network unavailable/)
})
