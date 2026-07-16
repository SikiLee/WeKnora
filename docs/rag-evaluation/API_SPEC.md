# WeKnora RAG Evaluation API Specification

## 1. 范围

本文定义 RAG 测试集、评测运行、定时调度和 Chunking 候选实验的 HTTP API。所有新接口位于：

```text
/api/v1/evaluation
```

MVP 接口止于只读 Recommendation。人工 confirm/reject、配置版本、重解析、索引切换和 rollback 属于非 MVP 增强契约，并在完成技术原型前标记 **Pending Verification**。

## 2. 通用约定

### 2.1 Content Type 与时间

- 请求和响应：`application/json; charset=utf-8`。
- 时间：RFC 3339 UTC，例如 `2026-07-16T08:00:00Z`。
- ID：JSON string。
- 小数指标：0–1 之间的 JSON number；百分点差异仍以小数表示，例如 3 个百分点为 `0.03`。

### 2.2 响应适配与分页

`CODEBASE_MAP.md` 没有完整证明新增端点所使用的成功包络字段、错误包络字段、错误码类型或具体整数值。它们在源码原型确认前统一为 **Pending Verification**。实现必须复用当前仓库实际 HTTP/错误适配器，不能为本项目另建第二套包络。

本文后续 JSON 只描述逻辑 `data` 载荷，不把外层 `success/code/message/data/error` 的精确形状写成已确认事实。稳定领域契约是：HTTP status、字符串 `reason_code`、可选 `details` 和资源 ID；最终字段嵌套位置由现有适配器原型冻结。

分页参数：

- `page` 默认 1，最小 1。
- `page_size` 默认 20，范围 1–100。
- 逻辑分页载荷包含 `items/page/page_size/total`；其在现有成功包络中的精确位置为 **Pending Verification**。
- 排序默认 `created_at desc`；接口只接受为该资源明确列出的排序字段。

### 2.3 HTTP 与稳定 reason code

| HTTP | 使用场景 |
| --- | --- |
| 400 | JSON、字段、cron、状态或范围校验失败 |
| 401 | 未认证 |
| 403 | tenant/KB access 不足；新增 API Key 写能力未确认时默认拒绝 |
| 404 | 资源不存在或在当前 tenant/KB 下不可见 |
| 409 | 状态、幂等、Generation Profile、版本或 Diagnosis 门禁冲突 |
| 429 | 本规格定义的 Run/Experiment 并发上限 |
| 500 | 未分类内部错误 |
| 503 | 请求无法被可靠接纳和持久化，且没有可依赖的新资源 ID |
| 504 | 同步依赖检查超时；异步执行超时应写入持久资源状态 |

具体公共错误码是否为整数、已有值为何，均为 **Pending Verification**。领域稳定 `reason_code` 至少包括：

- `INVALID_STATE`
- `IDEMPOTENCY_CONFLICT`
- `GENERATION_PROFILE_CONFLICT`
- `TESTSET_VERSION_NOT_PUBLISHED`
- `TESTSET_VERSION_ARCHIVED`
- `NO_ENABLED_CASES`
- `UNREVIEWED_CASES`
- `TESTSET_SCHEMA_INVALID`
- `DUPLICATE_CASE`
- `ANSWER_LEAKAGE_DETECTED`
- `AMBIGUOUS_QUESTION`
- `CLAIM_EVIDENCE_UNSUPPORTED`
- `ANSWERABLE_GOLD_EVIDENCE_MISSING`
- `UNANSWERABLE_GOLD_EVIDENCE_PRESENT`
- `UNANSWERABLE_SCOPE_MISSING`
- `STALE_EVIDENCE`
- `DOCUMENT_NOT_READY`
- `MODEL_NOT_READY`
- `INVALID_CRON`
- `INVALID_TIMEZONE`
- `INVALID_METRIC_K`
- `EXPERIMENT_LIMIT_EXCEEDED`
- `INSUFFICIENT_VALID_CASES`
- `DIAGNOSIS_NOT_COMPLETED`
- `DIAGNOSIS_NOT_CHUNKING_LIKELY`
- `SOURCE_IDENTITY_MAPPING_INVALID`
- `JUDGE_CALIBRATION_REQUIRED`
- `EVALUATION_DATA_POLICY_BLOCKED`
- `TENANT_CONCURRENCY_LIMIT`
- `RUN_CASE_LIMIT_EXCEEDED`
- `SCHEDULE_SKIPPED_ACTIVE`
- `SCHEDULE_SLOT_INTERRUPTED`
- `NO_BETTER_STRATEGY`
- `QUEUE_UNAVAILABLE`
- `NOT_APPLICABLE_UNANSWERABLE`
- `NOT_APPLICABLE_ANSWERABLE`

### 2.4 202 blocked 与 503 边界

- 请求通过认证、授权和 Schema/状态校验，且系统能够可靠持久化 EvalRun 时，即使模型缺失、文档未完成索引、Version stale、Calibration 未通过、数据策略禁止所选 Judge或其他执行前置不满足，也返回 HTTP 202，并创建 `status=blocked` 的 Run 与稳定 reason。
- 数据库不可用、事务提交失败、队列/核心服务故障导致请求无法可靠接纳且无法留下可依赖资源时，返回 HTTP 503，不创建 Run，不返回伪造 ID。
- 若 Run 已成功持久化后队列才不可用，返回 202 + blocked/`QUEUE_UNAVAILABLE`；不得再把同一请求表述为“503 且资源已创建”。
- 认证、授权、请求字段和业务状态冲突仍返回相应 4xx，不创建 blocked Run。

### 2.5 幂等

以下异步 POST 接受 `Idempotency-Key` 请求头：

- `POST /testsets/{id}/generations`
- `POST /runs`
- `POST /runs/{id}/retry`
- `POST /runs/{id}/diagnoses`
- `POST /diagnoses/{id}/retry`
- `POST /schedules/{id}/trigger`
- `POST /chunk-experiments/{id}/start`
- `POST /judge-calibrations/{id}/run`

规则：

1. key 非空时最大 128 字符。
2. 服务端以 `tenant + operation + key` 查找记录。
3. 相同请求摘要返回原资源及第一次创建语义；running Calibration/Diagnosis 可返回同一执行。
4. 不同请求摘要返回 409/`IDEMPOTENCY_CONFLICT`。
5. 幂等记录物理实现和保留期限为 **Pending Verification**；契约上不得在可重试窗口内重复创建资源。
6. Schedule cron 触发不依赖请求头，使用永久唯一的 `schedule_id + scheduled_at_utc` Slot 防重。

### 2.6 权限事实与 MVP 契约

`CODEBASE_MAP.md` 只确认 tenant context、通用 RBAC/permission middleware、KB access checks 和部分审计基础。Owner/Admin/Contributor/Viewer、`FullAccess`、`run_evaluations` 及新增 API Key capability 在新端点上的精确契约均为 **Pending Verification**。

MVP 只承诺：

- GET 端点复用当前有效 tenant 隔离与目标 KB read access。
- 创建、修改、运行、调度、校准和实验端点复用目标 KB write access，以及仓库当前实际存在的认证/授权链。
- 新增 API Key 写端点在 capability 映射确认前默认拒绝；不为本项目新建细粒度权限体系。
- 路径资源必须从数据库重新读取 tenant/KB 归属，客户端不能通过请求体覆盖。
## 3. 核心表示

### 3.1 TestsetSummary

```json
{
  "id": "ts-1",
  "knowledge_base_id": "kb-1",
  "name": "产品手册评测集",
  "description": "核心问答",
  "status": "active",
  "current_published_version": {
    "id": "tsv-2",
    "version_number": 2,
    "enabled_case_count": 40,
    "published_at": "2026-07-16T08:00:00Z"
  },
  "created_by": "user-1",
  "created_at": "2026-07-16T06:00:00Z",
  "updated_at": "2026-07-16T08:00:00Z"
}
```

### 3.2 EvalRunSummary

```json
{
  "id": "run-1",
  "knowledge_base_id": "kb-1",
  "testset_version_id": "tsv-2",
  "trigger_type": "manual",
  "status": "running",
  "stage": "judging",
  "progress": {
    "total": 40,
    "completed": 25,
    "failed": 1,
    "canceled": 0
  },
  "summary_metrics": null,
  "created_at": "2026-07-16T09:00:00Z",
  "started_at": "2026-07-16T09:00:01Z",
  "finished_at": null
}
```

Run/Item 中所有实验检索结果必须同时携带 `execution_document_id` 与可验证的 `source_document_id/source_content_hash/source_normalization_version/offset_unit/source_start_at/source_end_at`。

### 3.3 ChunkingConfig

Chunking 配置沿用现有 `ChunkingConfig` 字段命名。实验 API 至少返回：

```json
{
  "strategy": "heading",
  "chunk_size": 800,
  "chunk_overlap": 80,
  "separators": ["\n\n", "\n"],
  "parent_chunk_size": 0,
  "child_chunk_size": 0,
  "token_limit": 0,
  "config_hash": "sha256:..."
}
```

服务端冻结实际生效值，不以代码默认值补写历史快照。

## 4. Testset API

### 4.1 列表与创建

#### `GET /testsets`

Query：

- `knowledge_base_id`：必填。
- `status`：可选，`active/archived`。
- `keyword`：可选，匹配名称。
- `page/page_size`。

#### `POST /testsets`

请求：

```json
{
  "knowledge_base_id": "kb-1",
  "name": "产品手册评测集",
  "description": "从核心产品文档生成"
}
```

行为：创建 Testset 及 `generation_profile_status=unlocked` 的空 draft version 1。文档范围、normalization/offset、split、generator 摘要和 data policy 字段此时允许为空。返回 201，逻辑载荷同时包含 `testset` 和 `draft_version`。

校验：名称 1–128 字符；description 最大 2000 字符；同一 KB active 名称不可重复。

### 4.2 详情、修改与归档

#### `GET /testsets/{testset_id}`

返回 Testset、当前 draft（如有）、当前 published（如有）和计数。

#### `PATCH /testsets/{testset_id}`

允许字段：

```json
{
  "name": "新名称",
  "description": "新说明"
}
```

不允许直接修改 status 或 published version 指针。

#### `DELETE /testsets/{testset_id}`

行为：归档 Testset 和其可归档 Version，返回 200。若存在 running Run/Experiment，返回 409/`INVALID_STATE`。历史结果继续可读。

### 4.3 异步生成

#### `POST /testsets/{testset_id}/generations`

请求：

```json
{
  "testset_version_id": "tsv-1",
  "document_ids": ["doc-1", "doc-2"],
  "case_count": 60,
  "question_type_distribution": {
    "single_evidence": 0.6,
    "multi_evidence": 0.25,
    "unanswerable": 0.15
  },
  "difficulty_distribution": {
    "easy": 0.2,
    "medium": 0.5,
    "hard": 0.3
  },
  "split_algorithm": "stratified-hash-v1",
  "split_seed": "20260716",
  "holdout_ratio": 0.3,
  "generator_model_id": "model-1",
  "generator_prompt_version": "rag-testset-v1"
}
```

约束：

- Version 必须为该 Testset 的 draft。
- 文档数 1–100，且全部属于目标 KB、状态 completed。
- `case_count` 1–100。
- 分布各自之和必须为 1，允许浮点误差 `1e-6`。
- `holdout_ratio` 范围 0.2–0.5。
- 服务端先规范化本请求的 Generation Profile：排序后的文档 ID 与各自内容 hash、`offset_unit`、`source_normalization_version`、split algorithm/seed、generator model 和 Prompt version/hash、`evaluation_data_policy` version/hash。
- Version 为 unlocked 时，创建 Generation 与锁定 Profile 必须在同一事务内完成；并发请求只有一个能首次锁定。
- Version 已 locked 时，Profile 必须完全一致。任何字段不一致返回 409/`GENERATION_PROFILE_CONFLICT`，`details` 至少提供 existing/requested profile hash 与不一致字段名；不得创建 Generation 或静默合并。
- Generation 之后 failed/canceled 不解除 Lock；不同范围或配置必须创建新 draft Version。

返回 202：

```json
{
  "generation_id": "gen-1",
  "status": "pending",
  "testset_version_id": "tsv-1"
}
```

#### `GET /testsets/{testset_id}/generations/{generation_id}`

返回请求参数快照、Generation Profile/hash、具体模型/Prompt provenance、状态、生成数、校验拒绝数、错误和时间。Generation 必须属于路径中的 Testset；Version 级 generator 摘要不能覆盖本记录。

#### `POST /testsets/{testset_id}/generations/{generation_id}/cancel`

请求体 `{}`。pending/running 设置取消请求，Worker 在 Case 边界停止；终态幂等返回当前 Generation。返回 200。

### 4.4 Version

#### `GET /testsets/{testset_id}/versions`

支持 `status`、`page/page_size`。

#### `POST /testsets/{testset_id}/versions`

请求：

```json
{
  "base_version_id": "tsv-2"
}
```

行为：从 published Version 克隆 Case/Evidence 与已冻结 Profile 为下一 draft；不传 base 时创建 `generation_profile_status=unlocked` 的空 draft。一个 Testset 同时只允许一个 draft。返回 201。若需要不同文档范围或生成配置，必须使用空 draft，而不能覆盖已锁定 Profile。

#### `POST /testsets/{testset_id}/versions/{version_id}/publish`

请求体为空对象 `{}`。

发布条件：

- Version 为 draft。
- 至少一个 enabled Case。
- 所有 enabled Case 为 approved、非 stale、Evidence 可回查。
- 所有 enabled Case 的 Schema、重复、答案泄漏、歧义和 claim-evidence support 门禁为 passed。
- unanswerable Case 保存 checked_scope、reason code 和 expected refusal；answerable Case 保存 reference key points。
- 文档哈希与 Version snapshot 一致。
- Generation Profile 已 locked，document snapshots/hash、normalization/offset、split、generator model/Prompt 摘要和 evaluation data policy 均非空且 profile hash 可重算一致。
- answerable Case 至少一条 Gold Evidence；unanswerable Case 的 Gold Evidence 必须为空。

成功返回 200 和 published Version。未审核为 409/`UNREVIEWED_CASES`，Evidence 失效为 409/`STALE_EVIDENCE`，answerable 缺 Gold 为 409/`ANSWERABLE_GOLD_EVIDENCE_MISSING`，unanswerable 带 Gold 为 409/`UNANSWERABLE_GOLD_EVIDENCE_PRESENT`。

### 4.5 Case

#### `GET /testsets/{testset_id}/versions/{version_id}/cases`

Query：`review_status`、`enabled`、`question_type`、`difficulty`、`split`、`stale`、`page/page_size`。

#### `POST /testsets/{testset_id}/versions/{version_id}/cases`

请求：

```json
{
  "question": "产品如何配置数据源？",
  "answerability": "answerable",
  "reference_answer": "先创建数据源，再完成授权和同步配置。",
  "reference_key_points": ["创建数据源", "完成授权", "配置同步"],
  "question_type": "single_evidence",
  "difficulty": "medium",
  "split": "tuning",
  "enabled": true,
  "gold_evidence": [
    {
      "source_document_id": "doc-1",
      "document_file_hash": "...",
      "document_content_hash": "...",
      "source_normalization_version": "weknora-source-v1",
      "offset_unit": "unicode_codepoint",
      "start_at": 120,
      "end_at": 180,
      "text_snapshot": "先创建数据源，再完成授权和同步配置。",
      "source_locator": {"section": "配置数据源"}
    }
  ]
}
```

示例中的 normalization/offset 值只是接口形状；具体被支持的值和文档类型必须经过原型确认，当前为 **Pending Verification**。unanswerable Case 使用 `answerability=unanswerable`、空 Gold Evidence，并必须提交 `expected_refusal`、`unanswerable_reason_code` 和 `checked_scope`。

手工创建的 Case 初始 `review_status=pending`。返回 201。

#### `GET /testsets/{testset_id}/versions/{version_id}/cases/{case_id}`

返回完整 Case 和 Evidence。

#### `PATCH /testsets/{testset_id}/versions/{version_id}/cases/{case_id}`

Version 必须为 draft。允许修改创建请求中的业务字段以及：

```json
{
  "review_status": "approved",
  "review_comment": "证据和答案一致",
  "enabled": true
}
```

修改 question/reference/evidence 后，`review_status` 自动重置为 pending；具备 KB write access 的主体可在同一次 PATCH 中重新设为 approved，但仍必须通过 Evidence 校验。

#### `DELETE /testsets/{testset_id}/versions/{version_id}/cases/{case_id}`

只允许删除 draft Case，返回 200。Published Version 返回 409/`INVALID_STATE`。

## 5. EvalRun API

### 5.1 列表与创建

#### `GET /runs`

Query：

- `knowledge_base_id`：必填。
- `status`、`trigger_type`、`testset_version_id`、`schedule_id`、`experiment_id`。
- `created_from/created_to`：RFC 3339。
- `page/page_size`。

#### `POST /runs`

请求：

```json
{
  "knowledge_base_id": "kb-1",
  "testset_version_id": "tsv-2",
  "case_ids": [],
  "metric_ks": [5, 10],
  "generation_model_id": "chat-model-1",
  "judge_model_id": "judge-model-1",
  "rerank_model_id": "rerank-model-1",
  "judge_calibration_versions": {
    "faithfulness": "cal-f-v1",
    "correctness": "cal-c-v1",
    "unanswerable_refusal_quality": "cal-r-v1"
  }
}
```

语义：

- `case_ids` 为空时运行全部 enabled Case；非空时必须均属于 Version 且 enabled。
- `metric_ks` 省略时为 `[5,10]`，每项范围 1–100，服务端升序去重。
- `rerank_model_id` 可空；其他两个模型必填。
- `judge_calibration_versions` 引用的记录必须属于同一 tenant 和目标 knowledge base；跨 KB 引用返回 404/403，未 passed 按 readiness 契约处理。
- 单 Run 的 Case 数硬上限为 100；显式 `case_ids` 或展开后的 enabled Case 超限返回 400/`RUN_CASE_LIMIT_EXCEEDED`。
- 创建 RunItem 时冻结 `answerability/question_type/difficulty`；answerable 冻结 reference answer/key points/非空 Gold Evidence，unanswerable 冻结 expected refusal、reason code/reason、checked scope且 reference answer/Gold 为空。
- Run 同时冻结 `evaluation_data_policy` version/hash；数据策略禁止所选外部 Judge 时不得外发，依契约创建 blocked Run或把 Judge 指标标为 policy-blocked/invalid。
- readiness gate 成功时创建 pending Run 并返回 202。
- 文档、测试集、模型、source mapping、Judge calibration 等资源级前置条件失败，在 Run 已可持久化时创建 blocked Run 并返回 202。
- DB/队列接纳等基础设施错误导致请求无法持久化接受时返回 503 且不创建 Run；若 Run 已持久化但 Asynq 暂不可用，则返回 202 + blocked/`QUEUE_UNAVAILABLE`，可通过 retry 恢复。
- 每 tenant 同时最多 2 个 active ordinary Run；超限返回 HTTP 429/`TENANT_CONCURRENCY_LIMIT`。跨进程原子计数实现为 **Pending Verification**。
- 请求字段本身非法或无权限时直接返回 4xx，不创建 Run。

返回：

```json
{
  "run_id": "run-1",
  "status": "pending"
}
```

### 5.2 详情与 Item

#### `GET /runs/{run_id}`

返回：

- RunSummary。
- 完整 snapshot（敏感模型凭据永不返回），包括 source/execution mapping、normalization/offset、split seed、真实 answer Prompt、context construction、no_history、模型关键参数和 calibration version。
- summary metrics、延迟、Token、可用成本。
- blocked/failed 原因。
- 来源 Schedule/Experiment/parent Run 链接。

#### `GET /runs/{run_id}/items`

Query：`status`、`metric_status`、`question_type`、`difficulty`、`split`、`document_id`、`page/page_size`。

列表返回问题摘要、状态、核心指标、延迟、Trace ID，不默认返回完整上下文和 Judge details。

#### `GET /runs/{run_id}/items/{item_id}`

返回完整冻结输入：question、answerability、question type、difficulty；answerable 的 reference answer/key points/Gold Evidence；unanswerable 的 expected refusal、reason code/reason、checked scope且 reference answer/Gold 为空；以及排序召回上下文、实际答案、引用、MetricResult、Judge details、错误和 Trace 信息。五项 Gold-dependent retrieval metrics 对 unanswerable 必须是 abstain/null/`NOT_APPLICABLE_UNANSWERABLE`。

原始载荷到期后字段返回 null，并增加：

```json
{
  "raw_payload_expired": true
}
```

#### `GET /runs/{run_id}/diagnoses`

返回 FailureDiagnosis 列表：`diagnosis_status`、`scope`、`label`、稳定 reason codes、`target_k`、eligible/valid Case 数、诊断版本、证据、错误和生成时间。MVP label 仅为 `chunking_likely/non_chunking_likely/unknown`；只有 `completed + chunking_likely` 的 diagnosis ID 可创建 Experiment。

#### `POST /runs/{run_id}/diagnoses`

请求：

```json
{
  "target_k": 5,
  "diagnosis_version": "chunk-diagnosis-v1"
}
```

为已达到可诊断状态的 Run 创建 pending Diagnosis并返回 202。相同幂等键或已有同 Run/version/target 的 pending/running 记录返回同一执行。blocked Run 可诊断，但不得产生 `chunking_likely`。

#### `POST /diagnoses/{diagnosis_id}/retry`

只允许 failed Diagnosis。创建带 `retry_of_diagnosis_id` 的新 pending Diagnosis并返回 202，旧记录保持 failed；completed/pending/running 返回 409 或同一执行。Diagnosis Worker 的失败不得遗留 running。

### 5.3 取消与重试

#### `POST /runs/{run_id}/cancel`

请求体 `{}`。pending/running 设置取消请求；终态幂等返回当前资源。返回 200。

#### `POST /runs/{run_id}/retry`

请求：

```json
{
  "scope": "failed"
}
```

`scope` 必填，语义固定：

- `failed`：只选择旧 Run 中 status=failed 的 Item。
- `unfinished`：只选择 pending/running/canceled Item。
- `all`：选择旧 Run 的全部原计划 Case。

合法矩阵：partial 支持三种 scope；canceled 支持 `unfinished/all`；failed 支持 `failed/all`；blocked 只支持 `all`；completed 不允许 retry。新 Run 精确保存 `parent_run_id/retry_scope/retry_case_ids` 并复制其他冻结 snapshot，返回 202。旧 Run 与 completed Item 永不被覆盖；没有可重试 Case或 scope 不合法时返回 409/`INVALID_STATE`。

## 6. EvalSchedule API

### 6.1 列表与创建

#### `GET /schedules`

Query：`knowledge_base_id` 必填、`enabled`、`page/page_size`。

#### `POST /schedules`

请求逻辑字段：

```json
{
  "knowledge_base_id": "kb-1",
  "name": "每日核心文档评测",
  "testset_version_id": "tsv-2",
  "cron_expression": "<existing-scheduler-expression>",
  "timezone": "Asia/Shanghai",
  "enabled": true,
  "sample_limit": 100,
  "sample_selection_algorithm": "stable-hash-v1",
  "sample_selection_seed": "schedule-seed-1",
  "overlap_policy": "skip_if_active",
  "run_template": {
    "embedding_model": {"id":"embedding-1","parameters":{}},
    "rerank_model": {"id":"rerank-1","parameters":{}},
    "answer_model": {"id":"chat-1","parameters":{"temperature":0}},
    "judge_models": {},
    "judge_calibration_versions": {
      "faithfulness":"cal-f-v1",
      "correctness":"cal-c-v1",
      "unanswerable_refusal_quality":"cal-r-v1"
    },
    "rag_answer_prompt_version":"answer-v1",
    "rag_answer_prompt_hash":"sha256:...",
    "context_builder_version":"context-v1",
    "history_policy":"no_history",
    "retriever_parameters":{},
    "rerank_parameters":{},
    "top_k":[5,10],
    "metric_version":"source-overlap-v1"
  },
  "evaluation_data_policy_version":"policy-v1"
}
```

约束：

- 客户端不得提交 `status`；服务端固定创建为 `status=active`。
- cron 由现有 scheduler parser 校验；六字段或其他精确格式是否为当前仓库契约标记 **Pending Verification**。
- timezone 必须为有效 IANA 时区。
- Version 必须 published，且与 KB 一致。
- `sample_limit` 范围 1–100。
- MVP `overlap_policy` 只接受 `skip_if_active`。
- Run template 中的模型、Prompt、Parser/Calibration、context builder、retrieval/rerank、top_k 和 metric version 必须是明确版本，不接受隐式 `latest`。
- 当样本数超过 sample_limit 时，按 `hash(seed || case_id)` 升序、Case ID 并列排序取前 N；触发时冻结 Case ID 列表。

返回 201。
### 6.2 详情、修改、删除和触发

#### `GET /schedules/{schedule_id}`

返回配置、`status/archived_at/archived_by`、next/last run、最近状态和最近错误。

#### `PATCH /schedules/{schedule_id}`

只允许修改 name、cron/timezone、enabled、sample 配置、overlap policy、明确版本的 Run template 和 evaluation data policy；不得提交 status、knowledge_base_id 或直接恢复 archived。修改后重新计算 `next_run_at`，正在执行的 Run 不受影响。archived Schedule 的 PATCH 返回 409。

#### `DELETE /schedules/{schedule_id}`

把 `status` 设置为 archived、`enabled=false` 并保存 archived_at/by；历史 Run 保留。返回 200。

#### `GET /schedules/{schedule_id}/slots`

按 `scheduled_at_utc desc` 分页返回 Slot 历史：最终 `run_created/skipped_active/failed_before_run`、关联 run_id、稳定 reason code 和时间。若查询恰好命中内部 claim 窗口，可短暂返回 `outcome=null/processing=true`；该状态必须被 Worker 恢复为最终 outcome，不能长期存在。Schedule 页面不能只依赖 `last_status`。

#### `POST /schedules/{schedule_id}/trigger`

请求体 `{}`，立即创建 trigger_type=schedule 的 Run，但不改变下一 cron 时间。立即触发使用 `Idempotency-Key` 防重，不占用未来或现有 cron 的 `scheduled_at_utc` Slot。返回 202。

cron 自动触发时执行顺序为：原子创建 `EvalScheduleSlot(schedule_id, scheduled_at_utc, outcome=null)`；已存在则幂等返回；再检查 active Run；命中时写 `outcome=skipped_active/reason_code=SCHEDULE_SKIPPED_ACTIVE` 且不创建 Run；未命中时用已冻结 Case IDs 和模板创建 Run并写 `run_created/run_id`；Run 创建前的可记录失败写 `failed_before_run`。claim 后进程中断时恢复为 `failed_before_run/SCHEDULE_SLOT_INTERRUPTED`，不得补建 Run。readiness gate 失败但 Run 可持久化时仍创建 blocked Run。

连续低分自动创建实验不属于 MVP，Schedule API 不接受 low-score policy。该能力进入增强阶段且必须引用 `chunking_likely` diagnosis。

## 7. ChunkExperiment API

### 7.1 列表与创建

#### `GET /chunk-experiments`

Query：`knowledge_base_id` 必填、`status`、`source_run_id`、`page/page_size`。

#### `POST /chunk-experiments`

请求：

```json
{
  "knowledge_base_id": "kb-1",
  "name": "产品手册低召回分析",
  "source_run_id": "run-12",
  "failure_diagnosis_id": "diag-1",
  "document_ids": ["doc-1"],
  "candidate_count": 2
}
```

行为：

- Diagnosis 必须属于唯一 `source_run_id`，且 `diagnosis_status=completed`、label=`chunking_likely`、source snapshot 仍有效；否则分别返回 409/`DIAGNOSIS_NOT_COMPLETED` 或 `DIAGNOSIS_NOT_CHUNKING_LIKELY`。MVP 不接受 `source_run_ids[]`。
- 使用 source Run 的实际冻结配置作为 baseline。
- TestsetVersion、模型、Prompt、metric K 和非 Chunking 检索配置必须相同。
- `document_ids` 必须位于 Run 文档快照中。
- `candidate_count` 1–2；范围非法返回 400/`EXPERIMENT_LIMIT_EXCEEDED`。
- 服务端只能按 Diagnosis reason 允许的维度生成候选：too-large→减小 size/结构化策略，too-small→增大 size/父子上下文，boundary→增加 overlap/size，redundancy→降低 overlap，heading-detached→heading；不得对所有 reason 使用相同通用网格。
- 服务端按 `candidate_count` 生成并去重候选；无法产生请求数量的不同候选时返回 409/`INVALID_STATE`。
- 创建 draft Experiment 和 baseline/candidate Variant，返回 201。
- 同 tenant 已有 active Experiment 时返回 429/`TENANT_CONCURRENCY_LIMIT`。

### 7.2 详情、启动和取消

#### `GET /chunk-experiments/{experiment_id}`

返回 Experiment、FailureDiagnosis、split algorithm/seed、baseline、候选摘要、provisional candidate、holdout decision、`recommend/no_better_strategy`、进度和 recommendation 是否存在。

#### `GET /chunk-experiments/{experiment_id}/estimate`

在启动前返回预计 `document_parse_count`、candidate/variant 数、`estimated_chunk_count`、`estimated_embedding_count`、tuning/holdout RAG 调用数、Judge 调用数、索引构建次数和可获得的费用区间。无法可靠估算的字段为 null，并附 `estimation_basis`；不得伪造精确成本。

#### `POST /chunk-experiments/{experiment_id}/start`

请求体包含 `{"estimate_acknowledged":true}`。Experiment 必须 draft；服务端再次验证 source mapping、文档哈希、calibration、tenant 并发和估算快照后入队并返回 202。

#### `POST /chunk-experiments/{experiment_id}/cancel`

请求体 `{}`。draft/running 可取消，终态幂等返回当前资源，HTTP 200。

### 7.3 Variant 与 Recommendation

#### `GET /chunk-experiments/{experiment_id}/variants`

返回每个 Variant：

- baseline/candidate 标识。
- 完整 Chunking 配置及与 baseline 的 diff、source↔execution document mapping。
- 临时资源状态、EvalRun ID 和清理状态。
- tuning 与 holdout 分开返回；只有 baseline/provisional candidate 有 holdout。
- Context Precision、Hit、Recall、MRR、Coverage、Chunk Redundancy、Faithfulness、Correctness、拒答质量、失败率和 P95 延迟。
- 按 metric 返回 eligible holdout、baseline/candidate valid、common valid、双方 invalid/abstain/failed、排除原因和 valid coverage。
- chunk count、index build time，以及可获得的 embedding/index 调用量与成本。

#### `GET /chunk-experiments/{experiment_id}/recommendation`

holdout 通过时返回 200：

```json
{
  "id": "rec-1",
  "experiment_id": "exp-1",
  "decision": "recommend",
  "recommended_variant_id": "variant-3",
  "recommended_config": {
    "strategy": "heading",
    "chunk_size": 800,
    "chunk_overlap": 80,
    "config_hash": "sha256:..."
  },
  "metric_deltas": {
    "context_precision@5": 0.052,
    "evidence_recall@5": 0.0,
    "gold_coverage@5": 0.004,
    "chunk_redundancy@5": -0.08,
    "faithfulness": -0.004,
    "answer_correctness": 0.006,
    "failure_rate": -0.01,
    "p95_total_latency_ms": 120
  },
  "evidence": {
    "validity_by_metric": {
      "context_precision@5": {
        "eligible_count": 45,
        "baseline_valid_count": 42,
        "candidate_valid_count": 42,
        "common_valid_count": 42,
        "baseline_invalid_count": 1,
        "baseline_abstain_count": 1,
        "baseline_failed_count": 1,
        "candidate_invalid_count": 1,
        "candidate_abstain_count": 1,
        "candidate_failed_count": 1,
        "exclusion_reasons": {}
      }
    },
    "baseline_run_id": "run-baseline",
    "candidate_run_id": "run-candidate"
  },
  "limitations": [
    "Recommendation is not applied automatically."
  ],
  "generated_at": "2026-07-16T10:00:00Z"
}
```

没有候选通过时仍返回 200，并明确表达 `no_better_strategy`：

```json
{
  "decision": "no_better_strategy",
  "recommendation": null,
  "reason": "NO_BETTER_STRATEGY",
  "details": {
    "minimum_context_precision_delta": 0.03,
    "best_delta": 0.018
  }
}
```

MVP `allowed_drop=0`：每个参与门禁指标的 candidate valid coverage 不得低于 baseline；差值只使用共同 valid Item，common valid holdout 总数至少 30。资源字段必须展示，并只用于 tuning tie-break，不参与 holdout pass/reject。任何门槛失败都明确返回 `no_better_strategy`。

MVP API 没有 confirm/apply/reparse/rollback endpoint；这些不是永久非目标，见非 MVP 增强节。

## 8. JudgeCalibration API

#### `GET /judge-calibrations`

`knowledge_base_id` 必填；按当前 tenant 和目标 KB read access，并以 `metric_name`、status、model/prompt/parser version 分页列出校准记录。

#### `POST /judge-calibrations`

请求必须包含 `knowledge_base_id`。具备该 KB write access 的主体可创建 draft Calibration，引用至少 60 条双人标注 Case、人工标注版本、Judge 模型关键参数、Prompt version/hash 和 Parser version。返回 201。人工标注格式和导入原型为 **Pending Verification**，但不得用未经版本化的临时文件直接上线 Judge。

#### `GET /judge-calibrations/{calibration_id}`

校准记录必须位于调用者有 read access 的目标 KB。返回人工集版本、运行状态、Macro-F1、Exact Agreement、weighted κ、20 条样本三次重复一致率和 Parser failure rate。MVP Judge 为 Pointwise，不返回 pairwise/swap 指标。

#### `POST /judge-calibrations/{calibration_id}/run`

具备记录所属 KB write access 的主体可调用。`draft/failed` 可异步执行三次校准，返回 202；接口支持 `Idempotency-Key`。running 的相同幂等请求返回同一执行，不同请求返回 409；passed 不原地重跑。门槛为至少 60 条人工标注、Macro-F1>=0.80、Exact Agreement>=0.85、weighted κ>=0.70、20 条样本三次一致率>=0.90、Parser failure rate<1%。模型关键参数、Prompt 或 Parser 任一变化都要求新 Calibration Version。MVP 不提供 archived 状态或 archive API。

## 9. 旧接口兼容（非 MVP）

现有接口：

```text
POST /api/v1/evaluation
GET  /api/v1/evaluation?task_id={id}
```

当前接口及其 `dataset_id/chat_id/rerank_id` 语义已由源码确认，但“转换为 TestsetVersion 并完整迁移到新持久化领域”不属于 MVP。MVP 新接口不得依赖该迁移；旧接口保持现状或仅增加弃用说明的具体方式为 **Pending Verification**。不得在未完成 Dataset 字段和调用方盘点前承诺兼容 DTO 或删除日期。

## 10. 状态、并发与 202/503 语义

- 更新操作使用当前状态作为条件，状态竞争返回 409 或当前终态资源。
- 同一 Run 只允许一个 active Worker；RunItem 使用 `(run_id, case_id, attempt)` 防重。
- Schedule 修改不影响已创建 Run。
- published Version 不接受任何 Case mutation。
- running Experiment 不接受 Variant mutation；本 API 不提供直接编辑 Variant 的路径。
- 列表只返回当前 tenant 且调用者有 KB 权限的资源。
- TestsetGeneration canceled 有显式 cancel 入口；TestsetVersion 不提供独立 archive API，只随 Testset 归档；EvalSchedule POST 固定 active、DELETE 归档，enabled 只是 active Schedule 的开关且 archived 不恢复。
- JudgeCalibration 为 `draft/running/passed/failed`，MVP 无 archived；FailureDiagnosis 为 `pending/running/completed/failed`，failed retry 创建新记录。
- retry scope 严格使用 `failed/unfinished/all` 合法矩阵并创建新 Run；completed 不能 retry，旧 Run 永不原地回到 running。
- 100 Case、2 active Runs/tenant、1 active Experiment/tenant 是 MVP 硬限制；跨进程计数原型为 **Pending Verification**。
- 202 表示资源已可靠持久化，即使其状态立即为 blocked；503 仅表示请求未被可靠接受且没有可依赖的新资源 ID。

## 11. Evaluation Data Policy

Run/Testset/Schedule/Experiment 读接口返回策略 version/hash 和经过权限裁剪的发送模式，不返回敏感匹配规则本身。创建或触发时，服务端从目标 KB 的实际策略来源解析并冻结；该来源与现有敏感级别映射为 **Pending Verification**。

受限 KB 的安全默认值是：禁止外部 Judge；Langfuse Question/Answer 仅允许脱敏或 hash，Context 仅允许 source ID/hash/rank/长度/脱敏摘要；第三方模型不得接收敏感片段。没有合规私有 Judge 时，API 仍返回真实 blocked/invalid Judge 状态，不伪造 Score。Item detail 只向具有 KB read access 的主体返回内部最小业务结果；日志、错误和审计不得包含完整敏感载荷。

## 12. Trace URL

API 总是返回内部保存的 `langfuse_trace_id`。只有同时具备明确的 Langfuse 控制台地址和项目标识、且能按部署版本稳定构造 URL 时才返回 `langfuse_trace_url`；否则为 null。客户端必须支持复制 Trace ID，不能假定 URL 存在。

## 13. 非 MVP 增强 API（Pending Verification）

以下端点方向保留在题目增强阶段，但在完成 Config/Index/Reparse 原型前不是稳定 API，也不进入 MVP：

- `POST /recommendations/{id}/confirm` / `reject`。
- `GET /reparse-operations/{id}`。
- `POST /reparse-operations/{id}/cancel`。
- `POST /reparse-operations/{id}/rollback`。

其最终请求/响应、权限、幂等和状态机必须在验证现有 reparse 终态、配置版本、跨向量后端索引隔离/切换及回滚后再冻结；当前不得写成已实现事实。
