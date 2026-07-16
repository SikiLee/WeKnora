export type ModalRequestTarget = {
  visible: boolean
  mode: string
  kbId: string
}

export type ModalRequestToken = ModalRequestTarget & {
  generation: number
}

export function createModalRequestScope(getTarget: () => ModalRequestTarget) {
  let generation = 0
  let resetTimer: ReturnType<typeof setTimeout> | undefined

  const clearResetTimer = () => {
    if (resetTimer === undefined) return
    clearTimeout(resetTimer)
    resetTimer = undefined
  }

  const capture = (): ModalRequestToken => ({
    generation,
    ...getTarget(),
  })

  const isCurrent = (token: ModalRequestToken) => {
    const current = getTarget()
    return token.generation === generation
      && token.visible === current.visible
      && token.mode === current.mode
      && token.kbId === current.kbId
  }

  const commit = (token: ModalRequestToken, update: () => void) => {
    if (!isCurrent(token)) return false
    update()
    return true
  }

  const invalidate = () => {
    ++generation
    clearResetTimer()
    return capture()
  }

  const scheduleReset = (update: () => void, delay = 300) => {
    clearResetTimer()
    const token = capture()
    resetTimer = setTimeout(() => {
      resetTimer = undefined
      if (!token.visible && isCurrent(token)) update()
    }, delay)
  }

  const dispose = () => {
    ++generation
    clearResetTimer()
  }

  return {
    capture,
    isCurrent,
    commit,
    invalidate,
    scheduleReset,
    dispose,
  }
}
