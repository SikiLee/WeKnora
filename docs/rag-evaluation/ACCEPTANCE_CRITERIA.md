# WeKnora RAG 评测与 Chunking 推荐验收标准

## 1. 验收范围

本验收标准覆盖一个最小完整闭环：

```text
选文档 → 生成并审核 Testset → 手动/定时 EvalRun
→ 检索指标 + Faithfulness/Correctness/拒答质量 → Langfuse + Web UI
→ 失败诊断
→ 临时 Chunking 对比 → Recommendation 或明确不推荐
```

MVP 只验收只读 Recommendation。confirm/apply、源知识库配置修改、原知识库重解析、活动索引切换和 rollback 属于非 MVP 增强阶段，其安全流程和权限原型均为 **Pending Verification**。

## 2. 验收前置条件

- 使用与 `CODEBASE_MAP.md` 所记录架构相符的 WeKnora 构建。
- 配置可用的 Embedding、生成和 Judge 模型；涉及重排的场景配置 Rerank 模型。
- 至少一个 tenant、两个分别具备目标 KB read/write access 的已认证测试主体，以及两个相互隔离的知识库；精确角色常量、成功包络、整数错误码和新 API Key 能力映射均为 **Pending Verification**。
- 至少两篇已完成解析且能提供稳定原文位置的文本类文档。
- Redis/Asynq、业务数据库和目标向量存储可用。
- Langfuse 验收分为 enabled 和 disabled/unavailable 两组，避免把 Langfuse 作为核心链路的硬依赖。

## 3. Testset 验收

### AC-TS-01 创建测试集

**Given** 已认证主体对目标知识库有写权限  
**When** 创建 Testset  
**Then** 返回 201，同时创建 `generation_profile_status=unlocked` 的空 draft version 1；没有 KB write access 的主体创建返回 403；另一个 tenant 无法读取该资源。外层成功包络字段按源码原型验收，当前为 **Pending Verification**。

### AC-TS-02 从文档生成草稿

**Given** 选择 1–100 篇 completed 文档和可用生成模型  
**When** 提交 generation  
**Then** 返回 202，状态可从 pending 进入 running，再进入 completed/partial/failed/canceled；服务重启后仍能查询任务和已生成 Case。pending/running Generation 可通过 cancel API 取消，终态重复取消幂等。

每个成功 Case 必须包含：

- 非空 question，以及 `answerability=answerable/unanswerable`。
- answerable Case 有 reference answer/key points；unanswerable Case 有 expected refusal、检查范围和稳定原因。
- `single_evidence`、`multi_evidence` 或 `unanswerable`。
- `easy/medium/hard`。
- `tuning/holdout`。
- generator model ID、Prompt version/hash。
- answerable Case 至少一条 GoldEvidence；unanswerable Case 不伪造 Evidence。
- 初始 review status。
- Schema、重复、答案泄漏、歧义和 claim-evidence support 检查结果。
- split algorithm 和 seed；相同输入与 seed 的 split 可复现。

### AC-TS-03 Evidence 可定位

**Given** 一个 generation 产生的 GoldEvidence  
**When** 按 `source_document_id/source_document_hash`、`offset_unit`、`source_normalization_version` 和 `[start_at,end_at)` 回读规范化原文  
**Then** 规范化文本与 `text_snapshot` 一致，内容哈希可重算一致。

只保存 execution document/Chunk ID、缺少 source identity mapping、区间越界、文档哈希/规范化版本不一致或 Evidence 文本不一致的 Case 必须被拒绝，不得写入可发布草稿。未经稳定原文定位原型验证的文档类型必须标记 **Pending Verification**，不能进入可发布范围。

### AC-TS-04 人工审核与编辑

**Given** draft Version 中的 pending Case  
**When** 具备 KB write access 的主体编辑 question/reference/evidence  
**Then** review status 重置为 pending；只有 Evidence 校验通过后才能设为 approved。

没有 KB write access 的主体修改返回 403。Published Version 修改返回 409/`INVALID_STATE`。

### AC-TS-05 发布约束

**Given** draft Version  
**When** 发布  
**Then** 只有所有 enabled Case 均 approved、non-stale 且 Evidence 可回查时成功。

以下情况分别失败并返回可识别 reason：

- 没有启用 Case：`NO_ENABLED_CASES`。
- 存在未审核 Case：`UNREVIEWED_CASES`。
- Evidence/文档已变化：`STALE_EVIDENCE`。
- Schema、重复、答案泄漏、歧义或 claim-evidence support 门禁失败：对应稳定 quality reason code。
- unanswerable Case 缺少检查范围、原因或 expected refusal：`UNANSWERABLE_SCOPE_MISSING`。
- answerable Case 无 Gold Evidence：`ANSWERABLE_GOLD_EVIDENCE_MISSING`。
- unanswerable Case 带任何 Gold Evidence：`UNANSWERABLE_GOLD_EVIDENCE_PRESENT`；Gold 为空且其他条件完整时可发布。
- Generation Profile 未锁定或字段不完整：`INVALID_STATE`。

### AC-TS-06 发布后不可变

**Given** published Version  
**When** 尝试修改或删除 Case  
**Then** 返回 409；创建下一版本时克隆为新 draft，旧版本 ID、Case 和 Evidence 保持不变。

### AC-TS-07 文档变化

**Given** published Version 引用文档哈希 H1  
**When** 文档被重新解析并变为 H2  
**Then** 新 Run 被阻止，相关 Case/Testset 显示 stale；系统不得用新 Chunk ID 猜测迁移 Evidence。

### AC-TS-08 tuning/holdout 固化

**Given** 同一批 Case 和固定 split algorithm/seed  
**When** 创建或复算 Version split  
**Then** tuning/holdout 归属完全相同；发布后 algorithm、seed 和归属不可修改，holdout 内容不参与测试集生成 Prompt 的候选优化。

### AC-TS-09 Draft Generation Profile Lock

**Given** 一个新建的 unlocked 空 draft
**When** 第一个合法 Generation 请求被成功接纳并持久化
**Then** 文档集合/hash、normalization/offset、split algorithm/seed、generator model/Prompt 和 data policy 在同一事务内锁定，Generation 与 Version 保存相同 profile hash。

**And** 后续完全相同 Profile 的 Generation 可被接纳；任一字段不同则返回 409/`GENERATION_PROFILE_CONFLICT`，不创建 Generation、不修改已有 Lock；首个 Generation 后续 failed/canceled 也不解除 Lock。发布时 Profile 任一必填字段为空或 hash 不可重算必须失败。

## 4. EvalRun 验收

### AC-RUN-01 手动运行

**Given** published TestsetVersion、completed 文档和可用模型  
**When** 具备 KB write access 的主体创建 Run  
**Then** 返回 202；Run 持久化完整 snapshot，进入 pending/running，并最终进入 completed、partial、failed 或 canceled。

Snapshot 至少包含 Testset/Case、source document identity/hash、`source_normalization_version`、`offset_unit`、split algorithm/seed、实际 Chunking 配置与哈希、Embedding/Rerank/answer/Judge 模型 ID 与关键参数、真实 RAG answer prompt version/hash、Judge prompt/parser/calibration version、上下文构造版本、`no_history` 会话策略、metric K、检索参数和代码 commit/release。

### AC-RUN-02 readiness gate

分别构造以下情况：

- 文档仍为 processing。
- 文档解析失败。
- Embedding/生成/Judge 模型缺失。
- TestsetVersion 未发布或已归档。
- Evidence stale。

请求通过认证、授权与 Schema 校验且业务数据库可持久化时，以上资源 readiness 失败必须返回 202 并产生可查询的 blocked Run，包含稳定 `details.reason` 和相关资源 ID；不得无限停留在 pending/running，也不得只写服务端日志。若数据库/队列等接纳基础设施不可用、无法创建 Run，则返回 503 且不得伪造 Run ID。认证、授权、Schema 或状态冲突仍按相应 4xx 返回。

### AC-RUN-03 真实 RAG 链路

**Given** 一个可运行 Case  
**When** Run Worker 执行  
**Then** 保存最终送入生成阶段的排序 Context、实际答案、引用、阶段延迟和 Token；结果来自现有 WeKnora 检索/重排/生成服务，而不是测试专用替代算法。

### AC-RUN-04 部分失败

**Given** 多个 Case 中至少一个成功、至少一个检索/生成失败  
**When** Run 完成  
**Then** 成功结果保留，失败 Item 有独立错误，Run 状态为 partial，汇总只聚合 valid MetricResult。

全部 Item 无可用结果时 Run 为 failed。

### AC-RUN-05 取消

**Given** pending/running Run  
**When** 具备 KB write access 的主体调用 cancel  
**Then** Worker 在安全边界停止新增 Item，已完成结果保留，未开始 Item 为 canceled；没有 completed Item 时 Run=canceled，存在 completed 且同时存在 canceled/failed Item 时 Run=partial。

对终态 Run 重复 cancel 返回 200 且不改变结果。

### AC-RUN-06 重试

**Given** blocked/partial/failed/canceled Run  
**When** 按合法 scope 重试  
**Then** `failed` 精确选择 failed Item，`unfinished` 精确选择 pending/running/canceled Item，`all` 精确选择全部原计划 Case。partial 支持三种 scope；canceled 支持 `unfinished/all`；failed 支持 `failed/all`；blocked 只支持 `all`。每次均返回新的 Run ID并保存 `parent_run_id/retry_scope/retry_case_ids`，旧 Run、completed Item 和冻结结果不被修改。completed Run 或不匹配 scope 返回 409/`INVALID_STATE`。

### AC-RUN-07 HTTP 幂等

**Given** 相同 tenant、操作和 `Idempotency-Key`  
**When** 重复发送相同创建 Run 请求  
**Then** 返回同一 Run；请求体不同则返回 409/`IDEMPOTENCY_CONFLICT`。

### AC-RUN-08 进程恢复

**Given** Run 已入队并部分完成  
**When** API 或 Worker 进程重启  
**Then** Run、Item、进度和已完成指标仍存在；Asynq 重试不会为相同 Case 产生重复有效 Item。

### AC-RUN-09 Case 与并发硬上限

- 创建 Run 的有效 Case 数超过 100 时返回 400/`RUN_CASE_LIMIT_EXCEEDED`，且不创建 Run。
- 同 tenant 已有 2 个普通 active EvalRun 时，新普通 Run 返回 HTTP 429/`TENANT_CONCURRENCY_LIMIT`；具体外层错误包络和公共码类型为 **Pending Verification**。Experiment 内部 Run 受 Experiment 并发槽控制，不绕过 Case 上限。
- UI 在提交前展示本次 Case 数和上限。

### AC-RUN-10 旧 API 范围边界

MVP 验收不得依赖把现有 `POST/GET /api/v1/evaluation` 完整迁移为新持久化 EvalRun。旧接口的兼容/弃用适配属于非 MVP，行为在技术原型完成前标记 **Pending Verification**；新端点不得悄然改变旧调用方响应。

### AC-RUN-11 条件快照与终态推导

- answerable RunItem 必须冻结 reference answer/key points 和非空 Gold Evidence，expected refusal/unanswerable 字段为空。
- unanswerable RunItem 必须冻结 expected refusal、reason code/reason、checked scope，reference answer/key points/Gold Evidence 为空。
- Case 后续不可访问或 Testset 被归档时，仍能只凭 RunItem snapshot 复算适用指标。
- completed+failed 或 completed+canceled 组合均推导为 partial；用户取消且 completed=0 推导为 canceled；没有 completed 且非用户取消的执行期失败推导为 failed。

## 5. 失败诊断验收

### AC-DIAG-01 状态、版本与证据

创建 Diagnosis 后状态必须从 pending 进入 running，再进入 completed 或 failed；API/UI 可见 `diagnosis_version`、`target_k`、eligible/valid Case 数、label、稳定 reason codes、evidence signals、generated_at 和 error。Worker 失败不能永久停留 running。failed retry 创建带来源引用的新 Diagnosis，旧记录保持 failed；相同幂等请求不重复创建。

### AC-DIAG-02 分类边界与规定 reason

按“Gold→解析/索引→检索→Rerank→Context 充分性→Generator→Chunk 信号”的顺序验证：

- `GOLD_EVIDENCE_INVALID`、`DOCUMENT_PARSE_INCOMPLETE`、`DOCUMENT_NOT_INDEXED`、`EMBEDDING_OR_SEARCH_FAILURE`、`RERANK_DROPPED_GOLD`、`CONTEXT_COMPLETE_GENERATION_FAILED` 应产生 `non_chunking_likely` 或证据不足时 unknown。
- `CHUNK_BOUNDARY_SPLIT`、`CHUNK_TOO_LARGE_NOISY`、`CHUNK_TOO_SMALL_INCOMPLETE`、`HEADING_BODY_DETACHED`、`CROSS_EVIDENCE_NOT_CO_RETRIEVED`、`TOPK_REDUNDANCY_HIGH` 在前置健康且证据充分时产生 `chunking_likely`。
- `INSUFFICIENT_VALID_CASES/UNKNOWN_CAUSE` 产生 `unknown`。
- blocked Run 不得产生 `chunking_likely`。

### AC-DIAG-03 实验创建与候选门禁

只有当前 source snapshot 上有效的 `completed + chunking_likely` Diagnosis 才允许创建 Experiment；pending/running/failed、`non_chunking_likely/unknown` 或过期诊断返回 409。一个 Experiment 只绑定一个 source Run。MVP 只允许具备 KB write access 的主体手动创建；“连续低分后自动创建草稿”属于非 MVP 增强阶段。

候选维度必须受 reason 限制：too-large 只减小 size/结构化策略；too-small 只增大 size/父子上下文；boundary 只增加 overlap/size；redundancy 只降低 overlap；heading-detached 只测试 heading。不得让所有 Diagnosis 产生同一网格。

## 6. 指标验收

### AC-MET-01 固定序列

使用 `METRICS.md` 的两条 Evidence、三条 Retrieved Context 示例，必须得到：

- Hit@3 = 1。
- Evidence Recall@3 = 1。
- MRR@3 = 1。
- Context Precision@3 = 0.8333（允许 `1e-4` 误差）。
- Gold Coverage@3 = 0.625。

### AC-MET-02 空命中

**Given** 前 K 条均与 Gold Evidence 不相交  
**Then** Hit、Evidence Recall、MRR、Context Precision、Gold Coverage 均为 0，且 status=valid。

### AC-MET-03 重叠去重

**Given** 两个 Retrieved Context 覆盖同一 Gold Evidence 的重叠区间  
**Then** Gold Coverage 合并区间后计算，重叠字符不能重复计数，结果不超过 1。

### AC-MET-04 文档身份

**Given** 临时 KB 中 `execution_document_id` 与源文档 ID 不同，但映射回同一 `source_document_id/source_document_hash/source_normalization_version/offset_unit`  
**Then** 按 source offset 正常判定相关；若只具有相同 execution ID/区间但稳定 source identity、hash 或 normalization version 不同，则不判定相关并记录映射 reason。

### AC-MET-05 K 配置

`metric_ks` 缺省为 `[5,10]`；重复/乱序输入被升序去重；0、负数或大于 100 返回 `INVALID_METRIC_K`。

### AC-MET-06 Faithfulness 正常结果

**Given** Judge 返回两个 supported、一个 unsupported claim  
**Then** Faithfulness 为 `2/3`，details 保存三条 claim、verdict、context ranks 和 reason。

### AC-MET-07 Faithfulness 非数值状态

分别验证：

- JSON 两次解析失败 → invalid/value=null。
- 模型输出可解析但语义输入不足 → invalid/value=null。
- 模型超时/调用失败 → execution_status=failed，status/value=null。
- 所有 claim 为 unclear → abstain/value=null。
- 答案为空或生成失败 → invalid/value=null。
- 只有无事实拒答文本 → abstain/value=null。

invalid/abstain 均不得作为 0 进入 Run mean。

### AC-MET-08 Answer Correctness

**Given** answerable Case 有三个可评分 reference key points，答案匹配两个、遗漏一个  
**Then** Answer Correctness 为 `2/3`，保存逐 key point 判定和理由；unclear 不进入分母，Parser 失败为 invalid。unanswerable Case 的 Correctness 为 `abstain/null/NOT_APPLICABLE_UNANSWERABLE`。

### AC-MET-09 无答案拒答质量

**Given** `answerability=unanswerable` Case 及已审核的 expected refusal  
**When** 分别返回正确拒答、编造事实、无关内容和 Judge 无法解析  
**Then** 分别得到 valid 的正确拒答分、valid 的不正确拒答分、valid 的不正确拒答分和 invalid；answerable Case 的本指标为 `abstain/null/NOT_APPLICABLE_ANSWERABLE`。结果保存检查范围、reason 和 calibration version，invalid/abstain/failed 不按 0 聚合。

### AC-MET-10 Judge 上线门禁

MVP 仅验收 Pointwise Judge。每个 metric+model+Prompt+Parser 的 calibration 至少包含 60 个经两名人工审核者标注的样本并绑定 version，且 Calibration 必须属于目标知识库；读写分别复用该 KB 的 read/write access。门禁同时验证：Parser 失败率 `<1%`、Macro-F1 `>=0.80`、Exact Agreement `>=0.85`、weighted κ `>=0.70`、20 条样本三次运行一致率 `>=0.90`；不得保留 pairwise/swap 门槛。`draft/failed` 可运行，`running` 重复调用按 Idempotency-Key 返回同一执行或 409，passed 的模型/Prompt/Parser 变化创建新 Version。任一未通过时结果可展示但不得参与 provisional、holdout 或 Recommendation。

### AC-MET-11 共同 valid Item

baseline/candidate 的每个差值只使用二者在同一 Case、同一 metric 上均为 valid 的交集；invalid、abstain、failed、缺失和单边成功 Item 均不进入差值。API/UI 按 metric 展示 eligible、双方 valid、common valid、双方 invalid/abstain/failed、排除原因和 valid coverage；MVP `allowed_drop=0`，candidate coverage 低于 baseline 时必须拒绝。

### AC-MET-12 Chunk Redundancy 与资源量

固定召回区间能按 `METRICS.md` 公式验证 Chunk Redundancy；Run/Variant 同时保存并展示 chunk count、index build time，以及提供方可获得时的 embedding/index 成本。成本不可获得时值为 null 并带原因，不得伪装为 0。

### AC-MET-13 不存在综合分

API、持久化模型、Langfuse Score 和 UI 中均不得出现把检索和生成指标加权后的 Overall Score。

### AC-MET-14 unanswerable 检索指标适用性

**Given** 一个已审核且 Gold Evidence 为空的 unanswerable Case  
**When** 计算 Hit@K、Evidence Recall@K、MRR@K、Context Precision@K 和 Gold Coverage@K  
**Then** 五项均为 `execution_status=completed/status=abstain/value=null/reason_code=NOT_APPLICABLE_UNANSWERABLE`，不进入聚合分母。answerable Case 缺 Gold 或 source mapping 时为 invalid/null；只有证据与 mapping 合法且明确未命中时才是 valid/0。

## 7. Langfuse 验收

### AC-LF-01 观测树

启用 Langfuse 时，每个 completed Item 至少产生：

- 一个名为 `rag-evaluation.item` 的 Trace，session ID 为 Run ID。
- retrieval Span；启用重排时有 rerank Span。
- answer Generation，以及 Faithfulness、Correctness、拒答质量中本 Item 适用的 Judge Generation。
- valid 规则指标、生成指标和 Chunk Redundancy Score。

Trace metadata 能定位 tenant、KB、Run、Item、Case 和版本哈希，但不能包含模型凭据。

### AC-LF-02 Langfuse 不可用

**Given** Langfuse 关闭、端口不可达或 ingestion 返回错误  
**When** 执行 Run  
**Then** Run 仍按内部结果完成，`observability_status` 反映 disabled/partial/failed，内部 MetricResult 不丢失。

### AC-LF-03 Trace URL

没有明确项目 URL 配置时 API 返回 trace ID 且 trace URL 为 null；UI 支持复制 ID。配置充分时 URL 可打开对应 Trace。

## 8. Schedule 验收

### AC-SCH-01 创建与校验

由现有 scheduler parser 验证通过的 cron、IANA 时区、published Version、完整明确版本的 Run template、sample selection algorithm/seed、`sample_limit<=100` 和 `skip_if_active` 返回 201；客户端提交 status 被拒绝，服务端固定 active。六字段是否为当前正式格式标记 **Pending Verification**，验收以源码原型为准。

### AC-SCH-02 到时触发

**Given** enabled Schedule 到达计划时间  
**Then** 先创建唯一 Slot，再使用 Schedule 中明确版本的 answer/Judge/Rerank/Embedding、calibration versions、answer Prompt/hash、context builder、no_history、retriever/rerank、top_k、metric version 和 data policy 创建 trigger_type=schedule 的 Run。样本超过上限时按固定 hash+seed 选出完全相同的 Case ID 集，不同 Worker 不得自行抽样。

### AC-SCH-03 时间槽幂等

重复加载 scheduler、API 重启或重复处理同一个 `schedule_id + scheduled_at_utc`，只能存在一个 EvalScheduleSlot；原子 claim 时 outcome 可短暂为 null，但必须最终化。outcome=run_created 时只能关联一个 Run，outcome=skipped_active 时没有 Run 且后续不得补建；claim 后进程中断必须恢复为 `failed_before_run/SCHEDULE_SLOT_INTERRUPTED`，不得无限保持 null 或补建 Run。

### AC-SCH-04 立即触发

调用 `/trigger` 创建 Run，但不改变下一次 cron 时间，也不占用未来或现有 cron Slot。相同 Idempotency-Key 返回同一 Run。

### AC-SCH-05 blocked 可见

Schedule 触发时若模型或文档未就绪，必须创建 blocked Run 并写入 last status/error，不能静默跳过。

### AC-SCH-06 不重叠

**Given** 同 tenant/KB/Schedule 已有 pending/running Run  
**When** 到达下一个 cron 时间槽  
**Then** Slot 持久化 `outcome=skipped_active/reason_code=SCHEDULE_SKIPPED_ACTIVE`，不创建重叠 Run；时间槽唯一键永久占用，后续重复投递和进程重启不能补建 Run。

### AC-SCH-07 归档

POST 时 status 由服务端固定 active；PATCH 只能修改可编辑模板和 enabled，不能提交任意 status。DELETE 归档时持久化 `status=archived`、`archived_at/by`，停止生成后续 Run；已创建 Run 不受影响。archived Schedule 不能编辑、启用或普通触发，重复归档幂等；恢复时创建新 Schedule。

### AC-SCH-08 Slot 历史与失败

Schedule detail/slots API 和页面必须按时间展示最近 `run_created/skipped_active/failed_before_run`、关联 Run、稳定 reason 与时间。Run 创建前发生可持久化错误时 Slot 为 failed_before_run；中断 claim 必须以 `SCHEDULE_SLOT_INTERRUPTED` 终结。Schedule `last_status` 只能作为缓存，不能替代历史；重复处理同一 cron 时间槽返回同一 Slot 事实。

### AC-SCH-09 MVP 范围边界

Schedule API 不接受 low-score 自动实验、趋势告警、通知或复杂预算字段；重复低分不会自动创建 Experiment。样本上限、tenant 并发和 `skip_if_active` 是本范围的成本/不重叠保护。

## 9. ChunkExperiment 验收

### AC-EXP-01 候选边界

Experiment 必须包含一个实际生效配置的 baseline 和 1–2 个不同 candidate，总 Variant 数不超过 3。配置哈希重复的候选被去重；请求 0 个或超过 2 个 candidate 返回 400/`EXPERIMENT_LIMIT_EXCEEDED`。每 tenant 同时最多一个 active Experiment。

### AC-EXP-02 策略边界

候选策略只能使用 `auto/heading/heuristic/legacy` 中当前确实不同的实现。`recursive` 仍为 legacy 别名时不得出现在候选列表中。

### AC-EXP-03 固定变量

所有 Variant 必须保存相同的 TestsetVersion、source document hash/normalization、split seed、Embedding、Rerank、生成、Judge calibration、真实 answer Prompt、上下文构造版本、`no_history` 会话策略、metric K 和非 Chunking 检索配置。只有 Chunking 配置不同。

### AC-EXP-04 活动知识库隔离

**Given** Experiment 执行  
**Then** 每个 Variant 使用临时 KB/索引；实验前后源 KB 的 Chunking 配置哈希、Chunk 数和活动索引内容不因实验被覆盖。

临时资源清理失败被记录，不得因此删除 Variant 的持久化结果。

每个临时执行文档必须有 `source_document_id ↔ execution_document_id` 映射；Retrieved Context 必须能还原 source hash、normalization version、offset unit 和 source offset。映射缺失的 Item 指标为 invalid，不得退化为按 execution document ID 或 Chunk ID 匹配。

### AC-EXP-05 tuning/holdout 两阶段

**Given** baseline 和 1–2 个 candidate  
**When** tuning 完成  
**Then** 只在 tuning 共同 valid Item 上按固定门禁/排序选择唯一 provisional candidate，并保存选择证据；随后 holdout 只执行/比较 baseline 与该 provisional candidate。holdout 只能 pass/reject，不得重选另一个 candidate；拒绝后输出“没有更优策略”。

### AC-EXP-06 启动规模预估

启动前 API/UI 必须展示预计文档解析数、预计 chunk/Embedding 数、RAG Case 调用数和适用 Judge 调用数；用户未取得预估或预估超过硬上限时不得启动。预估只用于保护和展示，不承诺等同实际计费。

### AC-EXP-07 推荐成功

**Given** provisional candidate 与 baseline 有至少 30 个共同有效 holdout Case，Context Precision 提升至少 0.03，Evidence Recall 和 Gold Coverage 均不低于 baseline，passed calibration 下适用的 Faithfulness/Correctness/拒答质量均不超过允许退化门槛，失败率不升高且无 Run 级失败  
**Then** 生成只读 Recommendation，包含配置、各门槛差异、eligible holdout、baseline/candidate valid、common valid、双方 invalid/abstain/failed、排除原因、valid coverage、Chunk Redundancy、chunk count、index build time、可获得的 embedding/index 成本、运行证据和限制。Candidate valid coverage 不得低于 baseline（allowed_drop=0）；Context Precision 单独改善不能通过。

### AC-EXP-08 明确不推荐

任何推荐门槛不满足或没有 tuning candidate 通过时 Experiment 仍可 completed，`decision=no_better_strategy`；API/UI 显示具体 reason、失败门槛和实际差异，不以 null/空白代替结论。

### AC-EXP-09 MVP 应用边界

MVP API/UI 只允许查看和复制 Recommendation，不能 confirm/apply、修改线上配置、触发原知识库 reparse 或 rollback。以上操作属于非 MVP 增强阶段并标记 **Pending Verification**，不得描述为永久放弃。

### AC-EXP-10 资源语义一致

MVP 资源方案固定为 B：P95 latency、chunk count、index build duration、embedding/index token/cost 必须保存和展示，可用于 tuning tie-break，但任何值都不作为 holdout pass/reject 硬门禁。API、UI、PRD 和 Metrics 不得同时声称资源“阻止推荐”和“只展示”。

## 10. Web UI 验收

### AC-UI-01 路由与入口

知识库详情页可进入 `/platform/knowledge-bases/:kbId/evaluation`；无读权限用户不能通过直接 URL 查看数据。

### AC-UI-02 页面区域

页面至少包含：

- Overview。
- Testsets。
- Runs。
- Run Detail。
- Chunk Experiments。
- Schedules 与最近 Slot。

### AC-UI-03 总览

展示 Context Precision、Hit、Evidence Recall、MRR、Gold Coverage、Faithfulness、Correctness、无答案拒答质量、valid/invalid/abstain、失败数和运行状态；不展示综合分。

### AC-UI-04 单题详情

能并排查看 Question、Reference Answer、Generated Answer、Gold Evidence、排序 Context、MetricResult、Judge claims/reasons、错误和 Trace 信息。

### AC-UI-05 状态反馈

pending/running 显示进度；blocked、partial、failed、canceled 显示可操作的原因文本；原始载荷过期时明确标识，不显示空白假装未生成。

### AC-UI-06 Experiment 对比

展示 FailureDiagnosis label/reason/evidence、baseline 和所有候选的配置 diff、tuning provisional 选择、holdout 判定、核心指标、共同 valid 样本数、Chunk Redundancy、失败率、chunk count、index build time、P95 延迟、Token/可获得成本，以及 Recommendation 或“没有更优策略”的原因。

### AC-UI-07 国际化

所有新增固定文案使用 Vue i18n，中文和英文均有对应键；API error reason 有可读映射和未知 reason 兜底。

### AC-UI-08 Schedule Slot 展示

Schedule 区域展示完整模板摘要、enabled、next run、最近 Slot outcome/reason、run_created 对应 Run 和 skipped_active；覆盖 loading、empty、error 和 archived 状态，不能把 skipped 显示为空白或伪造 blocked Run。

## 11. 权限与数据治理验收

### AC-SEC-01 租户隔离

同一资源 ID 在其他 tenant 请求中返回 404 或 403，列表绝不包含跨租户资源。Worker 重新从数据库读取资源 tenant，不信任任务 payload 中的 tenant 值。

### AC-SEC-02 KB 权限

只以当前仓库实际 tenant context 和目标 KB read/write access 验收：无 KB read access 不能读取，无 KB write access 不能执行新增写操作；JudgeCalibration 也必须绑定一个目标 KB 并遵循同一规则。Owner/Admin/Contributor/Viewer、`run_evaluations`、`FullAccess` 及新 API Key capability 的精确常量/映射均为 **Pending Verification**，不能作为唯一验收条件；确认前 API Key 对新增写端点默认拒绝，且不得新增细粒度权限体系。

### AC-SEC-03 数据脱敏

Langfuse metadata、日志、审计和 API error 不包含模型密钥、认证头或完整敏感文档。每次 Run 冻结 evaluation_data_policy；受限 KB 默认禁止外部 Judge，Langfuse 不发送完整 Context，只记录 correlation/source ID、hash、rank、长度和脱敏摘要。没有合规私有 Judge 时 Judge Metric 为 invalid/blocked，不得外发或伪造。

### AC-SEC-04 留存目标与 MVP 边界

数据模型必须能表达原始 Context/Answer/Judge 输出的 90 天目标留存期，以及聚合指标/审计元数据的 365 天目标留存期；API/UI 能在载荷已由外部治理流程清理时返回 `raw_payload_expired=true`。完整自动清理执行器、清理调度与合规参数确认属于非 MVP，标记 **Pending Verification**，不作为 MVP 完成门槛。

### AC-SEC-05 错误契约

所有新增端点必须复用仓库实际 HTTP/错误适配器。成功包络字段、错误包络字段、公共错误码类型和具体整数值没有被 `CODEBASE_MAP.md` 完整证明，统一标记 **Pending Verification**；验收只先冻结 HTTP status、稳定字符串 reason code、details 语义和资源是否创建。任何 API Key 能力或角色常量也不得伪装为已存在。

### AC-SEC-06 evaluation_data_policy 与留存

至少验证一个受限 KB：外部 Judge 请求被阻止；Langfuse 不出现完整 Question/Context/Answer；内部只保存业务所需最小结果；MetricResult 不保存无界长推理，只保存简短 reason、claim verdict 和 evidence ID。Testset 归档后 Evidence 在引用/留存期内仍可用于历史 Run；源文档删除或 hash 变化后 Version/Case stale 并禁止新 Run。

## 12. 状态入口、终态与重试验收

| 实体/决策 | 创建/API 入口 | 合法迁移 | 成功/失败/取消出口 | 终态与 retry 语义 |
| --- | --- | --- | --- | --- |
| Testset | POST `/testsets`→active | active→archived | archive 成功；冲突/权限为请求失败；无运行态取消 | archived 终态，MVP 不恢复 |
| TestsetVersion | Testset 同步创建或 POST `/versions`→draft | draft→published；随 Testset 归档→archived | publish 成功或 409；无 cancel | published 内容终态不可变；archived 不恢复；新建 draft 继续修改 |
| EvalCase review/quality | POST/PATCH Case→pending | review pending→approved/rejected；quality pending→passed/failed；内容变更回 pending | approved+passed 为发布成功条件；failed/rejected 阻止发布；无 cancel | Version 发布后冻结；draft 可编辑后重跑 |
| TestsetGeneration | POST `/generations`→pending | pending→running→completed/partial/failed/canceled | worker 完成/失败；POST cancel | 全部终态；重新生成创建新 Generation，Profile Lock 不解除 |
| EvalRun | POST/trigger/experiment→pending；readiness 可直接 blocked | pending→running；running→completed/partial/failed/canceled | completed；执行失败；POST cancel | 全部终态；合法 scope 创建新 Run，旧 Run 不复活 |
| EvalRunItem | Run 创建→pending；GET `/runs/{id}/items[/{item_id}]` 读取 | pending→running→completed/failed/canceled | completed/failed；由 Run cancel | 终态；只由新 retry Run 重新执行，不原地重开 |
| MetricResult | 指标执行创建 execution record；GET Item detail 读取 | execution completed→valid/invalid/abstain；或 execution failed | valid/invalid/abstain；调用失败 | 不可改；重算生成新 MetricResult 并使用新 metric version，同一执行内格式修复记录在 judge_attempts；四类均与合法 0 分分离 |
| JudgeCalibration | POST `/judge-calibrations`→draft | draft/failed→running→passed/failed | passed/failed；无 cancel API | passed 终态；failed 可幂等 rerun；模型/Prompt/Parser 变化新建 Version；MVP 无 archived |
| FailureDiagnosis | Run 聚合自动创建或 POST `/runs/{id}/diagnoses`→pending | pending→running→completed/failed | completed/failed；无 cancel | completed 终态；failed retry 创建新 Diagnosis；Experiment 仅接受 completed+chunking_likely |
| EvalSchedule | POST `/schedules` 服务端固定 active | active enabled true/false；DELETE→archived | trigger/Slot 表达成功或失败；无 cancel | archived 终态；PATCH 不恢复，需新建 Schedule |
| EvalScheduleSlot | Scheduler cron 原子 claim；GET `/schedules/{id}/slots` 读取；手动 trigger 仅用 Idempotency-Key | claim(outcome=null)→run_created/skipped_active/failed_before_run | 三种 outcome 均为历史出口；中断 claim→failed_before_run | outcome 终态；相同 schedule/time 幂等返回，不 retry/补建 |
| ChunkExperiment | POST `/chunk-experiments`→draft | draft→running/canceled；running→completed/failed/canceled | completed/failed；POST cancel | 终态只读；重新实验创建新资源 |
| Variant | Experiment 创建→pending；GET `/chunk-experiments/{id}/variants` 读取 | pending→preparing→running→completed/failed；父 Experiment cancel→canceled | completed/failed/canceled | 终态；不单独 retry/cancel，由父 Experiment 或新 Experiment 控制 |
| holdout decision | tuning 选出 provisional 后为 not_run；GET Experiment detail 读取 | not_run→pass/reject | pass/reject；Experiment canceled 时保持 not_run | pass/reject 终态；不得用 holdout 重选 candidate |
| Recommendation decision | Experiment 创建→pending；GET `/chunk-experiments/{id}/recommendation` 读取 | pending→recommend/no_better_strategy | recommend 或明确 no_better_strategy | 两者终态只读；MVP 无 confirm/apply/reparse/rollback |

逐状态测试必须证明：每个运行态有成功或失败出口；有 cancel 的实体均有显式 API；Worker 异常不会永久悬挂；不存在从业务终态原地回到 running 的路径。
## 13. MVP PRD 需求追踪（57 条）

每一行均逐条核对 `FR → Data Model → API → Acceptance`；不存在用范围写法代替逐条检查。

| FR | Data Model | API | Acceptance | 结果 |
| --- | --- | --- | --- | --- |
| FR-TS-01 | Testset tenant/KB | Testsets | AC-TS-01 | Pass |
| FR-TS-02 | TestsetVersion Profile Lock；TestsetGeneration | Generations/Versions | AC-TS-02、AC-TS-09 | Pass |
| FR-TS-03 | EvalCase provenance；Version summary | Cases/Generation detail | AC-TS-02 | Pass |
| FR-TS-04 | EvalCase type/difficulty；document snapshot | Cases/Publish | AC-TS-02、AC-TS-03 | Pending Verification：可发布文档类型 |
| FR-TS-05 | GoldEvidence；source mapping | Cases/Run Items | AC-TS-03、AC-MET-04 | Pending Verification：复杂文档 offset |
| FR-TS-06 | draft Case lifecycle | Case CRUD | AC-TS-04 | Pass |
| FR-TS-07 | quality/answerability 条件约束 | Publish | AC-TS-05 | Pass |
| FR-TS-08 | immutable Version | Versions/Publish | AC-TS-06 | Pass |
| FR-TS-09 | split algorithm/seed/split | Versions/Experiment | AC-TS-08、AC-EXP-05 | Pass |
| FR-RUN-01 | EvalRun trigger_type | Runs；旧 API 非 MVP | AC-RUN-01、AC-RUN-10 | Pass |
| FR-RUN-02 | Run→published Version | POST /runs | AC-RUN-01、AC-RUN-02 | Pass |
| FR-RUN-03 | EvalRun snapshot | Run create/detail | AC-RUN-01 | Pass |
| FR-RUN-04 | EvalRun/Item/MetricResult | Runs/Items | AC-RUN-08 | Pass |
| FR-RUN-05 | task reference/status | async Runs | AC-RUN-08 | Pass |
| FR-RUN-06 | parent/retry scope/case IDs | Cancel/Retry | AC-RUN-06 | Pass |
| FR-RUN-07 | Run/Item 状态计数 | Run detail | AC-RUN-04、AC-RUN-11 | Pass |
| FR-RUN-08 | blocked/error | Run create/detail | AC-RUN-02 | Pass |
| FR-RUN-09 | case_count/tenant concurrency | POST /runs | AC-RUN-09 | Pending Verification：跨进程计数实现 |
| FR-RUN-10 | blocked 与接纳失败 | POST /runs | AC-RUN-02 | Pass |
| FR-MET-01 | MetricResult applicability | Run Items | AC-MET-01、AC-MET-02、AC-MET-03、AC-MET-04、AC-MET-14 | Pass |
| FR-MET-02 | metric_ks | POST /runs | AC-MET-05 | Pass |
| FR-MET-03 | Judge MetricResult | Item detail | AC-MET-06、08、09 | Pass |
| FR-MET-04 | MetricResult details/version | Item detail | AC-MET-06、AC-MET-07、AC-MET-08、AC-MET-09 | Pass |
| FR-MET-05 | execution/status/value 条件 | Item detail/Aggregate | AC-MET-07、11、14 | Pass |
| FR-MET-06 | 无 Overall Score 字段 | Read APIs | AC-MET-13 | Pass |
| FR-MET-07 | Run/Item latency/token/cost | Runs/Items | AC-MET-12 | Pass |
| FR-MET-08 | Variant resource fields | Variants/Estimate | AC-MET-12、AC-EXP-10 | Pass |
| FR-MET-09 | JudgeCalibration/MetricResult | Calibration API | AC-MET-10、11 | Pending Verification：人工校准资产流程 |
| FR-LF-01 | trace/session fields | Item detail | AC-LF-01 | Pass |
| FR-LF-02 | observation/score mapping | Item detail | AC-LF-01 | Pass |
| FR-LF-03 | internal result first | Run/Item detail | AC-LF-02 | Pass |
| FR-LF-04 | observability_status | Run detail | AC-LF-02 | Pass |
| FR-LF-05 | trace_id/url | Item detail | AC-LF-03 | Pass |
| FR-LF-06 | 无 Dataset/Experiment 依赖 | 全部 API | AC-LF-01、AC-LF-02 | Pass |
| FR-SCH-01 | EvalSchedule binding/cron/timezone | Schedules | AC-SCH-01 | Pending Verification：cron 正式语法 |
| FR-SCH-02 | full run_template/sample policy | Schedules | AC-SCH-01、AC-SCH-02 | Pass |
| FR-SCH-03 | EvalScheduleSlot | Slots/Trigger | AC-SCH-03、06、08 | Pass |
| FR-SCH-04 | Schedule active/enabled/archive | Schedule CRUD | AC-SCH-04、07 | Pass |
| FR-SCH-05 | sample limit；无 low-score policy | Schedule create | AC-SCH-09 | Pass |
| FR-SCH-06 | skip_if_active/Slot outcome | Trigger/Slots | AC-SCH-06、08 | Pass |
| FR-EXP-01 | Experiment/Variant count | Experiments | AC-EXP-01 | Pass |
| FR-EXP-02 | strategy enum/hash | Variants | AC-EXP-02 | Pass |
| FR-EXP-03 | baseline/fixed variables | Experiment create/detail | AC-EXP-03 | Pass |
| FR-EXP-04 | fixed snapshot/common valid | Variants/Recommendation | AC-EXP-03、05、07 | Pass |
| FR-EXP-05 | source identity mappings | Variants/Items | AC-EXP-04 | Pending Verification：临时映射原型 |
| FR-EXP-06 | FailureDiagnosis/source_run_id | Diagnoses/Experiment create | AC-DIAG-01、AC-DIAG-02、AC-DIAG-03 | Pass |
| FR-EXP-07 | provisional/holdout decision | Experiment detail | AC-EXP-05 | Pass |
| FR-EXP-08 | validity/recommendation policy | Recommendation | AC-EXP-07、AC-EXP-10 | Pass |
| FR-EXP-09 | estimate/resources | Estimate/Variants | AC-EXP-06 | Pass |
| FR-EXP-10 | decision/recommendation | Recommendation | AC-EXP-08、09 | Pass |
| FR-UI-01 | KB-scoped read model | route/read APIs | AC-UI-01 | Pass |
| FR-UI-02 | 页面六区域 read model | all read APIs/Slots | AC-UI-02、08 | Pass |
| FR-UI-03 | summary_metrics/counts | Run detail | AC-UI-03 | Pass |
| FR-UI-04 | RunItem/Metric details | Item detail | AC-UI-04 | Pass |
| FR-UI-05 | status/error/progress | Run/Generation/Experiment | AC-UI-05 | Pass |
| FR-UI-06 | Diagnosis/Variant/Slot comparison | Experiment/Slots | AC-UI-06、08 | Pass |
| FR-UI-07 | reason/i18n keys | API reason contract | AC-UI-07 | Pass |

统计：FR 总数 57；完整规格覆盖 57；Conflict 0；Missing 0；其中 6 条含显式 Pending Verification（FR-TS-04、FR-TS-05、FR-RUN-09、FR-MET-09、FR-SCH-01、FR-EXP-05），均已定义安全失败方式，不得写成已确认实现事实。
## 14. 最终完成标准

只有当以下端到端场景全部通过，功能才视为完成：

1. 具备知识库写权限的用户从真实文档生成、审核并发布 TestsetVersion。
2. 同一 Version 可由手动和 cron 创建持久化 EvalRun。
3. Run 通过真实 RAG 生成检索指标、Faithfulness、Correctness 和无答案拒答质量，并在重启后仍可查询。
4. UI 能从汇总下钻到单题证据和 Langfuse Trace。
5. Langfuse 故障不影响内部结果。
6. 系统保存可解释 FailureDiagnosis，且只有 `chunking_likely` 能进入实验创建。
7. 同一 source 文档和 Testset 能通过稳定 identity mapping 在临时 KB 中比较 baseline 与最多两个候选 Chunking。
8. tuning 选择唯一 provisional candidate，holdout 只做最终 pass/reject；系统依据多指标门槛生成 Recommendation 或明确“没有更优策略”。
9. 整个 MVP 闭环没有修改活动知识库配置、触发原知识库重解析或执行回滚。
