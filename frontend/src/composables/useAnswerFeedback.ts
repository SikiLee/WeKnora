import { computed, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'
import type {
  DislikeReason,
  MessageFeedbackInput,
  MessageFeedbackState,
} from '../api/feedback'

type FeedbackSubmitter = (
  sessionId: string,
  messageId: string,
  input: MessageFeedbackInput,
) => Promise<{ data: MessageFeedbackState | null }>

export interface UseAnswerFeedbackOptions {
  sessionId: MaybeRefOrGetter<string>
  messageId: MaybeRefOrGetter<string>
  completed: MaybeRefOrGetter<boolean>
  initialFeedback: MaybeRefOrGetter<MessageFeedbackState | null | undefined>
  submit?: FeedbackSubmitter
  onUpdated?: (feedback: MessageFeedbackState | null) => void
}

export function useAnswerFeedback(options: UseAnswerFeedbackOptions) {
  const feedback = ref<MessageFeedbackState | null>(toValue(options.initialFeedback) || null)
  const pending = ref(false)
  const error = ref<unknown>(null)
  let requestID = 0

  const currentIdentity = () => ({
    sessionId: toValue(options.sessionId).trim(),
    messageId: toValue(options.messageId).trim(),
  })

  watch(
    () => {
      const identity = currentIdentity()
      return `${identity.sessionId}\u0000${identity.messageId}`
    },
    () => {
      ++requestID
      pending.value = false
      error.value = null
    },
    { flush: 'sync' },
  )

  watch(
    () => toValue(options.initialFeedback),
    (value) => {
      feedback.value = value || null
    },
    { deep: true, flush: 'sync' },
  )

  const canRate = computed(() => Boolean(
    toValue(options.completed) &&
    toValue(options.sessionId).trim() &&
    toValue(options.messageId).trim(),
  ))
  const isLiked = computed(() => feedback.value?.feedback_type === 'like')
  const isDisliked = computed(() => feedback.value?.feedback_type === 'dislike')

  const apply = async (input: MessageFeedbackInput): Promise<boolean> => {
    if (!canRate.value || pending.value) return false
    const target = currentIdentity()
    const activeRequestID = ++requestID
    pending.value = true
    error.value = null
    try {
      const submit = options.submit || (async (sessionId, messageId, payload) => {
        const { setMessageFeedback } = await import('../api/feedback')
        return setMessageFeedback(sessionId, messageId, payload)
      })
      const response = await submit(
        target.sessionId,
        target.messageId,
        input,
      )
      const current = currentIdentity()
      if (
        activeRequestID !== requestID
        || current.sessionId !== target.sessionId
        || current.messageId !== target.messageId
      ) return true
      feedback.value = response.data || null
      options.onUpdated?.(feedback.value)
      return true
    } catch (cause) {
      if (activeRequestID !== requestID) return true
      error.value = cause
      return false
    } finally {
      if (activeRequestID === requestID) pending.value = false
    }
  }

  const toggleLike = () => apply({ feedback_type: isLiked.value ? 'none' : 'like' })
  const clearDislike = () => apply({ feedback_type: 'none' })
  const submitDislike = (reasonCode: DislikeReason, reasonText = '') => apply({
    feedback_type: 'dislike',
    reason_code: reasonCode,
    ...(reasonCode === 'other' && reasonText.trim() ? { reason_text: reasonText.trim() } : {}),
  })

  return {
    feedback,
    pending,
    error,
    canRate,
    isLiked,
    isDisliked,
    toggleLike,
    clearDislike,
    submitDislike,
  }
}
