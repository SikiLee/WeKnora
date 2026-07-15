<template>
  <div class="feedback-governance">
    <div class="section-header">
      <h2>{{ t('feedback.governance.title') }}</h2>
      <p>{{ t('feedback.governance.description') }}</p>
    </div>

    <div class="feedback-toolbar">
      <t-input v-model="filters.keyword" clearable :placeholder="t('feedback.governance.searchPlaceholder')"
        @enter="refreshList">
        <template #prefix-icon><t-icon name="search" /></template>
      </t-input>
      <t-select v-model="filters.feedbackStatus" @change="refreshList">
        <t-option v-for="option in statusOptions" :key="option.value" :value="option.value" :label="option.label" />
      </t-select>
      <t-select v-model="filters.optimization" @change="refreshList">
        <t-option v-for="option in optimizationOptions" :key="option.value" :value="option.value"
          :label="option.label" />
      </t-select>
      <t-select v-model="filters.sortBy" @change="refreshList">
        <t-option v-for="option in sortOptions" :key="option.value" :value="option.value" :label="option.label" />
      </t-select>
      <t-button variant="outline" shape="square" :title="sortOrderLabel"
        :aria-label="sortOrderLabel" @click="toggleSortOrder">
        <t-icon :name="filters.sortOrder === 'asc' ? 'sort-ascending' : 'sort-descending'" />
      </t-button>
      <t-button variant="outline" shape="square" :title="t('common.refresh')"
        :aria-label="t('common.refresh')" :loading="loading" @click="refreshList">
        <t-icon name="refresh" />
      </t-button>
    </div>

    <div class="feedback-table-wrap">
      <t-table row-key="chunk_id" :data="items" :columns="columns" :loading="loading"
        table-content-width="860px" size="medium" hover>
        <template #chunk="{ row }">
          <div class="chunk-identity">
            <strong>{{ row.knowledge_title || t('feedback.governance.untitledKnowledge') }}</strong>
            <span>#{{ row.chunk_index }}</span>
          </div>
        </template>
        <template #content_preview="{ row }">
          <span class="chunk-preview" :title="row.content_preview">{{ row.content_preview }}</span>
        </template>
        <template #ratings="{ row }">
          <span class="rating-count rating-count--like"><t-icon name="thumb-up" />{{ row.like_count }}</span>
          <span class="rating-count rating-count--dislike"><t-icon name="thumb-down" />{{ row.dislike_count }}</span>
        </template>
        <template #positive_rate="{ row }">
          <span class="rate-value">{{ formatRate(row.positive_rate) }}</span>
        </template>
        <template #recall_weight="{ row }">
          <code>{{ Number(row.recall_weight).toFixed(2) }}</code>
        </template>
        <template #status="{ row }">
          <t-tag v-if="row.needs_optimization" theme="danger" variant="light" size="small">
            {{ t('feedback.governance.needsOptimization') }}
          </t-tag>
          <t-tag v-else theme="success" variant="light" size="small">
            {{ t('feedback.governance.healthy') }}
          </t-tag>
        </template>
        <template #actions="{ row }">
          <t-tooltip :content="t('feedback.governance.viewDetail')">
            <t-button variant="text" shape="square" size="small"
              :aria-label="t('feedback.governance.viewDetail')" @click="openRow(row.chunk_id)">
              <t-icon name="browse" />
            </t-button>
          </t-tooltip>
        </template>
      </t-table>
      <div v-if="listError" class="feedback-error">
        <span>{{ t('feedback.governance.loadFailed') }}</span>
        <t-button variant="text" size="small" @click="refreshList">{{ t('common.retry') }}</t-button>
      </div>
      <div v-if="total > 0" class="feedback-pagination">
        <t-pagination v-model="page" v-model:page-size="pageSize" :total="total" size="small" show-jumper
          show-page-number show-page-size :page-size-options="[10, 20, 50, 100]" @change="handlePageChange" />
      </div>
    </div>

    <t-drawer v-model:visible="detailVisible" :header="t('feedback.governance.detailTitle')"
      size="min(680px, 100vw)"
      :footer="false" @close="closeDetail">
      <div v-if="detailLoading" class="drawer-loading"><t-loading /></div>
      <div v-else-if="selected" class="feedback-detail">
        <div class="detail-heading">
          <div>
            <strong>{{ selected.knowledge_title || t('feedback.governance.untitledKnowledge') }}</strong>
            <span>#{{ selected.chunk_index }}</span>
          </div>
          <t-button theme="danger" variant="outline" size="small" @click="resetDialogVisible = true">
            <template #icon><t-icon name="refresh" /></template>
            {{ t('feedback.governance.reset') }}
          </t-button>
        </div>

        <div class="detail-metrics">
          <div><span>{{ t('feedback.governance.likes') }}</span><strong>{{ selected.like_count }}</strong></div>
          <div><span>{{ t('feedback.governance.dislikes') }}</span><strong>{{ selected.dislike_count }}</strong></div>
          <div><span>{{ t('feedback.governance.positiveRate') }}</span><strong>{{ formatRate(selected.positive_rate) }}</strong></div>
          <div><span>{{ t('feedback.governance.recallWeight') }}</span><strong>{{ Number(selected.recall_weight).toFixed(2) }}</strong></div>
          <div><span>{{ t('feedback.governance.sessions') }}</span><strong>{{ selected.session_count }}</strong></div>
        </div>

        <section class="detail-section">
          <h3>{{ t('feedback.governance.chunkContent') }}</h3>
          <div class="chunk-content">{{ selected.content }}</div>
        </section>

        <section class="detail-section">
          <h3>{{ t('feedback.governance.dislikeReasons') }}</h3>
          <div v-if="selected.reason_counts?.length" class="reason-list">
            <div v-for="reason in selected.reason_counts" :key="reason.reason_code" class="reason-row">
              <span>{{ reasonLabel(reason.reason_code) }}</span>
              <strong>{{ reason.count }}</strong>
            </div>
          </div>
          <t-empty v-else :description="t('feedback.governance.noReasons')" />
        </section>

        <section class="detail-section">
          <h3>{{ t('feedback.governance.weightHistory') }}</h3>
          <t-table row-key="id" :data="logs" :columns="logColumns" :loading="logsLoading"
            table-content-width="564px" size="small">
            <template #created_at="{ row }">{{ formatDate(row.created_at) }}</template>
            <template #change="{ row }"><code>{{ Number(row.old_weight).toFixed(2) }} -> {{ Number(row.new_weight).toFixed(2) }}</code></template>
            <template #source_action="{ row }">{{ actionLabel(row.source_action) }}</template>
          </t-table>
          <div v-if="logTotal > logPageSize" class="feedback-pagination">
            <t-pagination v-model="logPage" v-model:page-size="logPageSize" :total="logTotal" size="small"
              show-page-number @change="handleLogPageChange" />
          </div>
        </section>
      </div>
      <div v-else-if="detailError" class="feedback-error">{{ t('feedback.governance.detailFailed') }}</div>
    </t-drawer>

    <Teleport to="body">
      <t-dialog v-model:visible="resetDialogVisible" :header="t('feedback.governance.resetTitle')"
        :confirm-btn="{ content: t('feedback.governance.confirmReset'), theme: 'danger', loading: resetting }"
        :cancel-btn="t('common.cancel')" width="min(460px, calc(100vw - 32px))" @confirm="confirmReset"
        @close="handleResetDialogClose">
        <div class="reset-dialog">
          <p>{{ t('feedback.governance.resetDescription') }}</p>
          <t-textarea v-model="resetReason" :placeholder="t('feedback.governance.resetReasonPlaceholder')"
            :maxlength="500" :autosize="{ minRows: 3, maxRows: 6 }" />
        </div>
      </t-dialog>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, toRef } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import { useChunkFeedbackGovernance } from '@/composables/useChunkFeedbackGovernance'

const props = defineProps<{ kbId: string }>()
const { t, locale } = useI18n()
const resetDialogVisible = ref(false)
const resetReason = ref('')

const {
  filters, items, page, pageSize, total, loading, listError,
  detailVisible, detailLoading, selected, detailError, logs,
  logPage, logPageSize, logTotal, logsLoading, resetting,
  resetRefreshFailed, loadList, loadLogs, openDetail, resetSelected, closeDetail,
} = useChunkFeedbackGovernance({ kbId: toRef(props, 'kbId') })

const statusOptions = computed(() => ['all', 'rated', 'high', 'normal', 'low', 'unrated'].map((value) => ({
  value,
  label: t(`feedback.governance.status.${value}`),
})))
const optimizationOptions = computed(() => ['all', 'yes', 'no'].map((value) => ({
  value,
  label: t(`feedback.governance.optimization.${value}`),
})))
const sortOptions = computed(() => ['feedback_updated_at', 'like_count', 'dislike_count', 'positive_rate', 'recall_weight', 'chunk_index'].map((value) => ({
  value,
  label: t(`feedback.governance.sort.${value}`),
})))
const sortOrderLabel = computed(() => t(
  filters.sortOrder === 'asc' ? 'feedback.governance.sortAscending' : 'feedback.governance.sortDescending',
))

const columns = computed(() => [
  { colKey: 'chunk', title: t('feedback.governance.columns.chunk'), width: 180 },
  { colKey: 'content_preview', title: t('feedback.governance.columns.content'), minWidth: 220, ellipsis: true },
  { colKey: 'ratings', title: t('feedback.governance.columns.ratings'), width: 122 },
  { colKey: 'positive_rate', title: t('feedback.governance.columns.positiveRate'), width: 92 },
  { colKey: 'recall_weight', title: t('feedback.governance.columns.weight'), width: 82 },
  { colKey: 'status', title: t('feedback.governance.columns.status'), width: 112 },
  { colKey: 'actions', title: '', width: 52, fixed: 'right' as const },
])

const logColumns = computed(() => [
  { colKey: 'created_at', title: t('feedback.governance.logColumns.time'), width: 164 },
  { colKey: 'change', title: t('feedback.governance.logColumns.change'), width: 130 },
  { colKey: 'source_action', title: t('feedback.governance.logColumns.action'), width: 110 },
  { colKey: 'reason', title: t('feedback.governance.logColumns.reason'), minWidth: 160, ellipsis: true },
])

const formatRate = (rate: number | null) => rate == null ? '--' : `${(rate * 100).toFixed(1)}%`
const formatDate = (value: string) => value
  ? new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))
  : '--'
const reasonLabel = (code: string) => t(`feedback.answer.reasons.${code}`)
const actionLabel = (action: string) => t(`feedback.governance.actions.${action}`)

const refreshList = async () => {
  if (!await loadList(true)) MessagePlugin.error(t('feedback.governance.loadFailed'))
}
const toggleSortOrder = async () => {
  filters.sortOrder = filters.sortOrder === 'asc' ? 'desc' : 'asc'
  await refreshList()
}
const handlePageChange = async () => {
  if (!await loadList(false)) MessagePlugin.error(t('feedback.governance.loadFailed'))
}
const openRow = async (chunkId: string) => {
  if (!await openDetail(chunkId)) MessagePlugin.error(t('feedback.governance.detailFailed'))
}
const handleLogPageChange = async () => {
  if (!await loadLogs(logPage.value)) MessagePlugin.error(t('feedback.governance.detailFailed'))
}
const confirmReset = async () => {
  if (!await resetSelected(resetReason.value)) {
    MessagePlugin.error(t('feedback.governance.resetFailed'))
    return
  }
  resetDialogVisible.value = false
  resetReason.value = ''
  if (resetRefreshFailed.value) {
    MessagePlugin.warning(t('feedback.governance.resetRefreshFailed'))
  } else {
    MessagePlugin.success(t('feedback.governance.resetSuccess'))
  }
}
const handleResetDialogClose = () => {
  resetReason.value = ''
}

onMounted(() => loadList())
</script>

<style scoped lang="less">
.feedback-governance { display: flex; flex-direction: column; gap: 20px; min-width: 0; }
.section-header h2 { margin: 0 0 8px; font-size: 20px; }
.section-header p { margin: 0; color: var(--td-text-color-secondary); }
.feedback-toolbar { display: grid; grid-template-columns: minmax(180px, 1fr) 150px 160px 176px 32px 32px; gap: 8px; align-items: center; }
.feedback-table-wrap { min-width: 0; border: 1px solid var(--td-component-stroke); border-radius: 6px; overflow: hidden; }
.chunk-identity { display: flex; flex-direction: column; min-width: 0; gap: 2px; }
.chunk-identity strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.chunk-identity span, .detail-heading span { color: var(--td-text-color-placeholder); font-size: 12px; }
.chunk-preview { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rating-count { display: inline-flex; align-items: center; gap: 3px; margin-right: 10px; }
.rating-count--like { color: var(--td-success-color); }
.rating-count--dislike { color: var(--td-error-color); }
.feedback-pagination { display: flex; justify-content: flex-end; padding: 12px; border-top: 1px solid var(--td-component-stroke); }
.feedback-error { display: flex; justify-content: center; align-items: center; gap: 8px; min-height: 80px; color: var(--td-error-color); }
.drawer-loading { display: grid; min-height: 320px; place-items: center; }
.feedback-detail { display: flex; flex-direction: column; gap: 24px; }
.detail-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.detail-heading > div { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.detail-metrics { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); border-block: 1px solid var(--td-component-stroke); }
.detail-metrics > div { display: flex; flex-direction: column; gap: 5px; padding: 14px 10px; }
.detail-metrics span { color: var(--td-text-color-secondary); font-size: 12px; }
.detail-metrics strong { font-size: 18px; }
.detail-section h3 { margin: 0 0 12px; font-size: 15px; }
.chunk-content { max-height: 260px; padding: 12px; overflow: auto; white-space: pre-wrap; word-break: break-word; border: 1px solid var(--td-component-stroke); border-radius: 4px; background: var(--td-bg-color-secondarycontainer); line-height: 1.6; }
.reason-list { border-top: 1px solid var(--td-component-stroke); }
.reason-row { display: flex; justify-content: space-between; padding: 10px 4px; border-bottom: 1px solid var(--td-component-stroke); }
.reset-dialog p { margin: 0 0 14px; color: var(--td-text-color-secondary); line-height: 1.6; }
@media (max-width: 1100px) {
  .feedback-toolbar { grid-template-columns: minmax(160px, 1fr) repeat(2, 150px) 32px 32px; }
  .feedback-toolbar > :nth-child(4) { grid-column: 1 / 2; }
  .detail-metrics { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}
@media (max-width: 768px) {
  .feedback-toolbar { grid-template-columns: minmax(0, 1fr) 32px; }
  .feedback-toolbar > :first-child { grid-column: 1 / -1; }
  .feedback-toolbar > :nth-child(2),
  .feedback-toolbar > :nth-child(3),
  .feedback-toolbar > :nth-child(4) { grid-column: 1 / 2; }
  .feedback-toolbar > :nth-child(5) { grid-column: 2 / 3; grid-row: 4; }
  .feedback-toolbar > :nth-child(6) { grid-column: 2 / 3; grid-row: 3; }
  .feedback-pagination { justify-content: flex-start; overflow-x: auto; }
  .detail-heading { align-items: flex-start; }
  .detail-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>
