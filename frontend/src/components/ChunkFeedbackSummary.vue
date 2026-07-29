<template>
  <t-popup
    :visible="visible"
    trigger="click"
    placement="bottom-right"
    destroy-on-close
    @visible-change="setVisible"
  >
    <button
      type="button"
      class="chunk-feedback-summary"
      :class="{ 'needs-optimization': chunk.needs_optimization }"
      :aria-label="t('feedback.chunkDetails')"
    >
      <span>👍 {{ chunk.like_count || 0 }}</span>
      <span>👎 {{ chunk.dislike_count || 0 }}</span>
      <span>{{ positiveRate }}</span>
      <span>×{{ Number(chunk.recall_weight || 1).toFixed(1) }}</span>
    </button>
    <template #content>
      <div class="chunk-feedback-detail" @click.stop>
        <strong>{{ t('feedback.chunkDetails') }}</strong>
        <div class="chunk-feedback-detail__metric">
          {{ t('feedback.sessionCount', { count: chunk.session_count || 0 }) }}
        </div>
        <t-loading v-if="loading" size="small" />
        <template v-else>
          <div v-for="reason in reasons" :key="reason" class="chunk-feedback-detail__row">
            <span>{{ t(`feedback.reasons.${reason}`) }}</span>
            <span>{{ details?.reason_counts?.[reason] || 0 }}</span>
          </div>
          <div v-if="details?.audits?.length" class="chunk-feedback-detail__audits">
            <div v-for="audit in details.audits.slice(0, 5)" :key="audit.id">
              {{ audit.action }} · {{ t(`feedback.sources.${audit.trigger_source || 'legacy'}`) }}
              · {{ audit.old_weight }} → {{ audit.new_weight }}
            </div>
          </div>
        </template>
        <t-button
          v-if="canReset"
          size="small"
          theme="danger"
          variant="outline"
          :loading="resetting"
          @click="reset"
        >
          {{ t('feedback.reset') }}
        </t-button>
      </div>
    </template>
  </t-popup>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { MessagePlugin } from 'tdesign-vue-next';
import { useI18n } from 'vue-i18n';
import { getChunkFeedbackDetails, resetChunkFeedback } from '@/api/knowledge-base';
import type { ChunkFeedbackDetails } from '@/api/knowledge-base';

const props = defineProps<{
  chunk: Record<string, any>;
  knowledgeBaseId: string;
  canReset: boolean;
}>();
const emit = defineEmits<{ reset: [] }>();
const { t } = useI18n();
const visible = ref(false);
const loading = ref(false);
const resetting = ref(false);
const details = ref<ChunkFeedbackDetails | null>(null);
const reasons = ['inaccurate', 'irrelevant', 'incomplete', 'outdated', 'other'];
let loadSequence = 0;

const positiveRate = computed(() => {
  if (props.chunk.positive_rate === null || props.chunk.positive_rate === undefined) return '—';
  return `${Math.round(Number(props.chunk.positive_rate) * 100)}%`;
});

const setVisible = async (next: boolean) => {
  visible.value = next;
  if (!next || details.value) return;
  const sequence = ++loadSequence;
  loading.value = true;
  try {
    const response = await getChunkFeedbackDetails(props.chunk.id);
    if (sequence === loadSequence) details.value = response?.data || {};
  } catch {
    if (sequence === loadSequence) MessagePlugin.error(t('feedback.loadFailed'));
  } finally {
    if (sequence === loadSequence) loading.value = false;
  }
};

const reset = async () => {
  resetting.value = true;
  try {
    await resetChunkFeedback(props.knowledgeBaseId, props.chunk.id);
    Object.assign(props.chunk, {
      like_count: 0,
      dislike_count: 0,
      positive_rate: null,
      recall_weight: 1,
      needs_optimization: false,
    });
    details.value = null;
    visible.value = false;
    emit('reset');
    MessagePlugin.success(t('feedback.resetSuccess'));
  } catch {
    MessagePlugin.error(t('feedback.resetFailed'));
  } finally {
    resetting.value = false;
  }
};
</script>

<style scoped lang="less">
.chunk-feedback-summary {
  border: 0;
  border-radius: 12px;
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-secondary);
  display: inline-flex;
  gap: 7px;
  padding: 4px 8px;
  cursor: pointer;
  font-size: 12px;

  &.needs-optimization {
    color: var(--td-error-color);
    background: var(--td-error-color-1);
  }
}

.chunk-feedback-detail {
  width: min(280px, calc(100vw - 32px));
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.chunk-feedback-detail__row {
  display: flex;
  justify-content: space-between;
}

.chunk-feedback-detail__metric,
.chunk-feedback-detail__audits {
  color: var(--td-text-color-secondary);
  font-size: 12px;
}
</style>
