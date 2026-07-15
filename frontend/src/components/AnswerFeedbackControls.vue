<template>
  <div v-if="canRate" class="answer-feedback" role="group" :aria-label="t('feedback.answer.label')">
    <t-tooltip :content="t(isLiked ? 'feedback.answer.removeLike' : 'feedback.answer.like')" placement="top">
      <t-button
        size="small"
        variant="outline"
        shape="round"
        :class="['answer-feedback__button', { 'is-active': isLiked }]"
        :loading="pending && pendingAction === 'like'"
        :disabled="pending"
        :aria-pressed="isLiked"
        :aria-label="t(isLiked ? 'feedback.answer.removeLike' : 'feedback.answer.like')"
        @click.stop="handleLike"
      >
        <t-icon name="thumb-up" />
      </t-button>
    </t-tooltip>
    <t-tooltip :content="t(isDisliked ? 'feedback.answer.removeDislike' : 'feedback.answer.dislike')" placement="top">
      <t-button
        size="small"
        variant="outline"
        shape="round"
        :class="['answer-feedback__button', 'answer-feedback__button--dislike', { 'is-active': isDisliked }]"
        :loading="pending && pendingAction === 'dislike'"
        :disabled="pending"
        :aria-pressed="isDisliked"
        :aria-label="t(isDisliked ? 'feedback.answer.removeDislike' : 'feedback.answer.dislike')"
        @click.stop="handleDislike"
      >
        <t-icon name="thumb-down" />
      </t-button>
    </t-tooltip>

    <Teleport to="body">
      <t-dialog
        v-model:visible="dialogVisible"
        :header="t('feedback.answer.dislikeTitle')"
        :confirm-btn="{ content: t('common.confirm'), loading: pending, disabled: !reasonCode }"
        :cancel-btn="t('common.cancel')"
        width="min(460px, calc(100vw - 32px))"
        @confirm="confirmDislike"
        @close="closeDialog"
      >
        <div class="answer-feedback__dialog">
          <p class="answer-feedback__prompt">{{ t('feedback.answer.dislikePrompt') }}</p>
          <t-radio-group v-model="reasonCode" class="answer-feedback__reasons">
            <t-radio v-for="reason in reasons" :key="reason" :value="reason">
              {{ t(`feedback.answer.reasons.${reason}`) }}
            </t-radio>
          </t-radio-group>
          <t-textarea
            v-if="reasonCode === 'other'"
            v-model="reasonText"
            :placeholder="t('feedback.answer.otherPlaceholder')"
            :maxlength="500"
            :autosize="{ minRows: 3, maxRows: 6 }"
          />
        </div>
      </t-dialog>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, toRef } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import type { DislikeReason, MessageFeedbackState } from '@/api/feedback'
import { useAnswerFeedback } from '@/composables/useAnswerFeedback'

const props = defineProps<{
  sessionId: string
  messageId: string
  completed: boolean
  feedback?: MessageFeedbackState | null
}>()

const emit = defineEmits<{
  (event: 'update:feedback', value: MessageFeedbackState | null): void
}>()

const { t } = useI18n()
const dialogVisible = ref(false)
const reasonCode = ref<DislikeReason | ''>('')
const reasonText = ref('')
const pendingAction = ref<'like' | 'dislike' | null>(null)
const reasons: DislikeReason[] = ['incorrect', 'outdated', 'irrelevant', 'incomplete', 'unclear', 'other']

const {
  pending,
  canRate,
  isLiked,
  isDisliked,
  toggleLike,
  clearDislike,
  submitDislike,
} = useAnswerFeedback({
  sessionId: toRef(props, 'sessionId'),
  messageId: toRef(props, 'messageId'),
  completed: toRef(props, 'completed'),
  initialFeedback: toRef(props, 'feedback'),
  onUpdated: (value) => emit('update:feedback', value),
})

const notifyFailure = () => MessagePlugin.error(t('feedback.answer.saveFailed'))

const handleLike = async () => {
  pendingAction.value = 'like'
  if (!await toggleLike()) notifyFailure()
  pendingAction.value = null
}

const handleDislike = async () => {
  if (!isDisliked.value) {
    reasonCode.value = ''
    reasonText.value = ''
    dialogVisible.value = true
    return
  }
  pendingAction.value = 'dislike'
  if (!await clearDislike()) notifyFailure()
  pendingAction.value = null
}

const closeDialog = () => {
  if (pending.value) return
  dialogVisible.value = false
}

const confirmDislike = async () => {
  if (!reasonCode.value) return
  pendingAction.value = 'dislike'
  const saved = await submitDislike(reasonCode.value, reasonText.value)
  pendingAction.value = null
  if (!saved) {
    notifyFailure()
    return
  }
  dialogVisible.value = false
}
</script>

<style scoped lang="less">
.answer-feedback {
  display: inline-flex;
  gap: 6px;
  align-items: center;
}

.answer-feedback__button.is-active {
  color: var(--td-brand-color);
  border-color: var(--td-brand-color);
  background: var(--td-brand-color-light);
}

.answer-feedback__button--dislike.is-active {
  color: var(--td-error-color);
  border-color: var(--td-error-color);
  background: var(--td-error-color-1);
}

.answer-feedback__dialog {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.answer-feedback__prompt {
  margin: 0;
  color: var(--td-text-color-secondary);
}

.answer-feedback__reasons {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

@media (max-width: 520px) {
  .answer-feedback__reasons {
    grid-template-columns: 1fr;
  }
}
</style>
