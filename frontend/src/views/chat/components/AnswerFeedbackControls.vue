<template>
  <div class="answer-feedback" role="group" :aria-label="t('feedback.answerLabel')">
    <t-button
      size="small"
      variant="outline"
      shape="round"
      :theme="current?.type === 'like' ? 'primary' : 'default'"
      :loading="pending === 'like'"
      :title="t('feedback.like')"
      :aria-pressed="current?.type === 'like'"
      @click.stop="submit(current?.type === 'like' ? 'none' : 'like')"
    >
      <t-icon name="thumb-up" />
    </t-button>
    <t-popup v-model="reasonOpen" trigger="click" placement="top-left">
      <t-button
        size="small"
        variant="outline"
        shape="round"
        :theme="current?.type === 'dislike' ? 'danger' : 'default'"
        :loading="pending === 'dislike'"
        :title="t('feedback.dislike')"
        :aria-pressed="current?.type === 'dislike'"
        @click.stop="handleDislikeClick"
      >
        <t-icon name="thumb-down" />
      </t-button>
      <template #content>
        <div class="answer-feedback__reasons" role="menu" :aria-label="t('feedback.reasonLabel')">
          <strong>{{ t('feedback.reasonLabel') }}</strong>
          <t-button
            v-for="reason in reasons"
            :key="reason"
            variant="text"
            size="small"
            role="menuitem"
            @click.stop="chooseReason(reason)"
          >
            {{ t(`feedback.reasons.${reason}`) }}
          </t-button>
        </div>
      </template>
    </t-popup>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import { useI18n } from 'vue-i18n';
import {
  putMessageFeedback,
  type MessageFeedbackReason,
  type MessageFeedbackType,
} from '@/api/chat';
import { createLatestOperationGuard } from '@/utils/latestOperationGuard';

type FeedbackState = { type: Exclude<MessageFeedbackType, 'none'>; reason_code?: MessageFeedbackReason } | null;

const props = defineProps<{
  sessionId: string;
  message: Record<string, any>;
}>();

const { t } = useI18n();
const reasons: MessageFeedbackReason[] = ['inaccurate', 'irrelevant', 'incomplete', 'outdated', 'other'];
const current = ref<FeedbackState>(props.message?.my_feedback ?? null);
const pending = ref<MessageFeedbackType | null>(null);
const reasonOpen = ref(false);
const operations = createLatestOperationGuard();

watch(
  () => [props.message?.id, props.message?.my_feedback],
  () => {
    current.value = props.message?.my_feedback ?? null;
    operations.invalidate();
    pending.value = null;
  },
  { deep: true },
);

const handleDislikeClick = () => {
  if (current.value?.type === 'dislike') {
    reasonOpen.value = false;
    void submit('none');
  }
};

const chooseReason = (reason: MessageFeedbackReason) => {
  reasonOpen.value = false;
  void submit('dislike', reason);
};

const submit = async (type: MessageFeedbackType, reason?: MessageFeedbackReason) => {
  const op = operations.begin();
  const previous = current.value;
  const optimistic: FeedbackState = type === 'none' ? null : { type, ...(reason ? { reason_code: reason } : {}) };
  current.value = optimistic;
  props.message.my_feedback = optimistic;
  pending.value = type;
  try {
    const response: any = await putMessageFeedback(props.sessionId, props.message.id, type, reason);
    if (!operations.isCurrent(op)) return;
    current.value = response?.data ?? optimistic;
    props.message.my_feedback = current.value;
  } catch {
    if (!operations.isCurrent(op)) return;
    current.value = previous;
    props.message.my_feedback = previous;
    MessagePlugin.error(t('feedback.saveFailed'));
  } finally {
    if (operations.isCurrent(op)) pending.value = null;
  }
};
</script>

<style scoped lang="less">
.answer-feedback {
  display: inline-flex;
  gap: 6px;
}

.answer-feedback__reasons {
  width: min(240px, calc(100vw - 32px));
  padding: 10px;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 4px;

  strong {
    padding: 2px 8px 6px;
  }
}

@media (max-width: 390px) {
  .answer-feedback {
    gap: 4px;
  }
}
</style>
