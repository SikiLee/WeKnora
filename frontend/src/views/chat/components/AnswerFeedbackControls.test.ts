import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const component = readFileSync(new URL('./AnswerFeedbackControls.vue', import.meta.url), 'utf8')
const botMessage = readFileSync(new URL('./botmsg.vue', import.meta.url), 'utf8')
const streamHandler = readFileSync(
  new URL('../../../composables/useChatStreamHandler.ts', import.meta.url),
  'utf8',
)

test('answer feedback is server-eligible and excludes client-side attribution guesses', () => {
  assert.match(botMessage, /props\.session\?\.feedback_eligible === true/)
  assert.doesNotMatch(botMessage, /const refs = props\.session\?\.knowledge_references/)
  assert.match(streamHandler, /message\.feedback_eligible = dataPayload\?\.feedback_eligible === true/)
})

test('only the latest operation may commit or roll back state', () => {
  assert.match(component, /const operation = \+\+operationSequence/)
  assert.match(component, /if \(operation !== operationSequence\) return/g)
  assert.match(component, /current\.value = previous/)
  assert.match(component, /props\.message\.my_feedback = previous/)
  assert.match(component, /operationSequence \+= 1/)
})

test('feedback controls expose pressed state, responsive layout, and focus restoration', () => {
  assert.equal((component.match(/:aria-pressed=/g) || []).length, 2)
  assert.match(component, /role="group"/)
  assert.match(component, /role="menu"/)
  assert.match(component, /@media \(max-width: 390px\)/)
  assert.match(component, /button\?\.focus\?\.\(\)/)
})
