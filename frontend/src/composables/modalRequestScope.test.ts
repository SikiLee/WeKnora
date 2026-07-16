import assert from 'node:assert/strict'
import test from 'node:test'

import { createModalRequestScope, type ModalRequestTarget } from './modalRequestScope'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((next) => { resolve = next })
  return { promise, resolve }
}

test('an older modal response cannot overwrite the current knowledge base state', async () => {
  const target: ModalRequestTarget = { visible: true, mode: 'edit', kbId: 'kb-a' }
  const scope = createModalRequestScope(() => ({ ...target }))
  const state = {
    formName: '',
    creatorId: '',
    vectorStoreSource: '',
    permission: '',
    accessResolved: false,
    hasFiles: false,
    loading: false,
  }
  const responseA = deferred<typeof state>()
  const tokenA = scope.invalidate()
  state.loading = true
  const loadA = responseA.promise.then((response) => {
    scope.commit(tokenA, () => Object.assign(state, response))
  }).finally(() => {
    scope.commit(tokenA, () => { state.loading = false })
  })

  target.kbId = 'kb-b'
  const tokenB = scope.invalidate()
  const responseB = {
    formName: 'Knowledge B',
    creatorId: 'creator-b',
    vectorStoreSource: 'shared',
    permission: 'editor',
    accessResolved: true,
    hasFiles: true,
    loading: false,
  }
  assert.equal(scope.commit(tokenB, () => Object.assign(state, responseB)), true)

  responseA.resolve({
    formName: 'Knowledge A',
    creatorId: 'creator-a',
    vectorStoreSource: 'user',
    permission: 'owner',
    accessResolved: true,
    hasFiles: false,
    loading: false,
  })
  await loadA

  assert.deepEqual(state, responseB)
})

test('reopening a modal clears the old close timer before it can reset new state', (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const target: ModalRequestTarget = { visible: true, mode: 'edit', kbId: 'kb-a' }
  const scope = createModalRequestScope(() => ({ ...target }))
  let formName = 'Knowledge A'
  scope.invalidate()

  target.visible = false
  scope.invalidate()
  scope.scheduleReset(() => { formName = '' })
  t.mock.timers.tick(100)

  target.visible = true
  target.kbId = 'kb-b'
  scope.invalidate()
  formName = 'Knowledge B'
  t.mock.timers.tick(300)

  assert.equal(formName, 'Knowledge B')
})

test('mode changes and disposal invalidate captured requests', () => {
  const target: ModalRequestTarget = { visible: true, mode: 'edit', kbId: 'kb-a' }
  const scope = createModalRequestScope(() => ({ ...target }))
  const editToken = scope.invalidate()
  assert.equal(scope.isCurrent(editToken), true)

  target.mode = 'create'
  scope.invalidate()
  assert.equal(scope.isCurrent(editToken), false)

  const createToken = scope.capture()
  scope.dispose()
  assert.equal(scope.isCurrent(createToken), false)
})
