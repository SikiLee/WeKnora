# WeKnora RAG Evaluation Metrics Specification

## 1. 目标

本文定义本课题唯一使用的质量指标：

- 检索阶段：Context Precision@K、Hit@K、Evidence Recall@K、MRR@K、Gold Coverage@K。
- 生成阶段：Answer Faithfulness、Answer Correctness、无答案拒答质量。
- 诊断与系统数据：Chunk Redundancy、延迟、Token、失败率、chunk count、index build time 和可获得的 embedding/index 成本。

Answer Relevance、Answer Completeness、Citation Correctness/Completeness 属于非 MVP 增强指标；当前不把任何多个指标合成为总分。

## 2. 版本与输入

### 2.1 指标版本

- 规则指标算法版本：`source-overlap-v1`。
- Faithfulness、Correctness、无答案拒答质量各自的 Judge Prompt/Parser 分别记录版本与内容哈希，并绑定模型关键参数和 calibration version。
- MetricResult 必须保存 `metric_name`、`metric_version`、`status`、`value` 和 details。

指标输入必须引用 Run snapshot 中的真实 RAG answer Prompt version/hash、上下文构造版本、`history_policy=no_history`、source normalization version、split seed 和模型关键参数；上述任一影响比较的字段不同，不得作为同一 Experiment 的可比 Variant。

算法或 Prompt 变化后必须产生新版本；不同版本结果不得直接合并为同一时间序列或实验比较。

### 2.2 默认 K

默认计算：

```text
K = {5, 10}
```

EvalRun 可以提供其他 K，但必须为 1–100 的升序去重整数。某次运行内所有 Item 使用同一组 K。检索返回数量少于 K 时，不足位置视为不存在，不重复最后结果。

### 2.3 Gold Evidence

对一个 Case，Gold Evidence 集合记为：

```text
G = {g1, g2, ..., gm}
```

每个 `g` 包含：

- `source_document_id` 与发布时的 source 文档内容哈希。
- `source_normalization_version` 与 `offset_unit`。
- 原文闭开区间 `[start_at, end_at)`。
- Evidence 文本快照与内容哈希。

若文档哈希已变化，Case 为 stale，不进入新 EvalRun。指标计算不会尝试把旧 Evidence 模糊匹配到新文档。

`answerability=unanswerable` 时 Gold Evidence 集合必须为空。五项 Gold-dependent retrieval metrics 不适用，统一返回：

```text
execution_status = completed
status = abstain
value = null
reason_code = NOT_APPLICABLE_UNANSWERABLE
```

这些结果不进入对应 Run/Variant 聚合分母。`m=0` 非法规则只适用于 answerable Case；answerable 缺 Gold 时发布被阻止，若历史异常仍进入运行则指标为 invalid/null，而不是合法 0。

### 2.4 Retrieved Context

检索结果按最终送入生成阶段的顺序记为：

```text
R = [r1, r2, ..., rn]
```

每个 `r` 至少包含 rank、execution document/Chunk ID，以及通过 identity mapping 还原的 source document ID/hash、normalization、offset unit、source `start_at/end_at`、内容快照和检索/重排分数。若经过重排，以重排后顺序为准；未启用重排时使用检索顺序。

临时知识库中的 `execution_document_id` 绝不能直接与 Gold Evidence 的 source ID 比较。mapping 缺失或不一致时，该 Item 的位置型指标为 invalid，而不是 0。

### 2.5 Testset 质量门禁（非评分）

发布前逐 Case 保存以下检查的 `passed/failed/pending`、规则/Prompt 版本、稳定 reason code 和证据；这些检查不产生评测分数或 Overall Score：

- **Schema**：answerability、条件必填字段、split、题型、难度、Evidence 结构完整且枚举合法。
- **重复**：规范化 question 完全相同为 exact duplicate；近重复只标记候选并要求人工确认，阈值原型为 **Pending Verification**。
- **答案泄漏**：question 不得直接包含 reference answer、完整 key point 或足以直接暴露答案的 Evidence 片段；自动阈值为 **Pending Verification**，首版必须人工复核命中项。
- **歧义**：问题必须能在保存的 source scope 内得到唯一、可审核的解释；自动筛查不能替代人工批准。
- **claim-evidence support**：answerable Case 的每个 reference key point 至少由一条 Gold Evidence 明确支持；不支持或冲突则 failed。
- **无答案有效性**：unanswerable Case 必须保存 checked scope、稳定 reason 和 expected refusal，并经人工确认在该 scope 内不可回答；不得为其伪造 Gold Evidence。

任一 required gate 未 passed 的 enabled Case 不得发布。

## 3. Evidence 匹配规则

### 3.1 区间相交

`source-overlap-v1` 中，Retrieved Context `r` 与 Gold Evidence `g` 相关，当且仅当：

1. `r.source_document_id == g.source_document_id`；
2. `r.source_content_hash == g.document_content_hash`；
3. `r.source_normalization_version == g.source_normalization_version`；
4. `r.offset_unit == g.offset_unit`；
5. 两个 source 闭开区间存在正长度交集：

```text
max(r.start_at, g.start_at) < min(r.end_at, g.end_at)
```

相关性为二值，不使用向量相似度、LLM、execution document ID 或 Chunk ID 判定。微小交集可能使 Context 标记为相关，但 Gold Coverage 会按真实覆盖长度反映覆盖不足；两者必须同时展示。

### 3.2 rel(i)

对排名 i：

```text
rel(i) = 1  若 ri 与任一 Gold Evidence 相关
rel(i) = 0  其他情况
```

同一个 Retrieved Context 即使覆盖多条 Evidence，`rel(i)` 仍只为 1。

### 3.3 Evidence hit

对 Evidence `gj`：

```text
hit(gj, K) = 1  若前 K 个 Context 中至少一个与 gj 相关
hit(gj, K) = 0  其他情况
```

## 4. 检索指标

### 4.0 适用性

以下 4.1–4.5 五项指标只对 answerable Case 计算。只有 source identity/mapping 完整且 Gold 合法时，“明确未命中”才是 `valid/0`；mapping、hash、normalization、offset 或 Gold 缺失是 `invalid/null`。unanswerable 按 2.3 的 `abstain/null` 处理。

### 4.1 Hit@K

衡量前 K 条是否至少召回一条有效证据。

```text
Hit@K = 1  若 Σ(i=1..min(K,n)) rel(i) > 0
Hit@K = 0  其他情况
```

取值 0 或 1。Run 聚合时对有效 Item 求均值，表示命中率。

### 4.2 Evidence Recall@K

衡量 Gold Evidence 单元的召回比例。

```text
Evidence Recall@K = Σ(j=1..m) hit(gj, K) / m
```

一个 Context 覆盖多条 Evidence 时可以同时命中多条。`m=0` 只对 answerable Case 非法并阻止发布；unanswerable 合法地没有 Gold，且本指标 abstain/null。

### 4.3 MRR@K

衡量第一条相关 Context 的排名。

设：

```text
rank_first = min{i | 1 <= i <= min(K,n) 且 rel(i)=1}
```

则：

```text
MRR@K = 1 / rank_first  若存在相关 Context
MRR@K = 0               否则
```

虽然单 Item 是 Reciprocal Rank，Run 层对 Item 求均值后才是 Mean Reciprocal Rank。

### 4.4 Context Precision@K

衡量相关 Context 是否集中在靠前位置。

逐点 Precision：

```text
Precision@i = Σ(j=1..i) rel(j) / i
```

设前 K 条中的相关数：

```text
relevant_K = Σ(i=1..min(K,n)) rel(i)
```

则：

```text
Context Precision@K =
  Σ(i=1..min(K,n)) [Precision@i × rel(i)] / relevant_K，若 relevant_K > 0
  0，若 relevant_K = 0
```

该指标衡量排序纯度，不替代 Evidence Recall。只命中一条 Evidence 且位于 rank 1 时 Context Precision 可以为 1，但缺失的其他 Evidence 会由 Evidence Recall 和 Gold Coverage 体现。

### 4.5 Gold Coverage@K

衡量前 K 条 Context 对 Gold Evidence 原文区间的去重字符覆盖比例。

对每条 Evidence `g`，取前 K 条中与其相交的区间，并裁剪到 `g` 的范围内。合并重叠区间后得到覆盖长度 `covered(g,K)`。

```text
Gold Coverage@K =
  Σ(g∈G) covered(g,K) / Σ(g∈G) (g.end_at - g.start_at)
```

合并区间避免多个重叠 Chunk 重复计数。取值 0–1。

### 4.6 Chunk Redundancy@K（诊断项）

衡量 Top-K Context 在稳定 source 区间上的重复程度。先把每个 Context 裁剪为可映射的 source 区间，按 source document 合并区间：

```text
total_source_length = Σ(i=1..min(K,n)) length(ri.source_interval)
unique_source_length = 各 source_document 内区间并集长度之和

Chunk Redundancy@K =
  1 - unique_source_length / total_source_length，若 total_source_length > 0
  invalid，若 source mapping 不完整
```

取值 0–1，越高表示相邻或重叠 Chunk 占用了更多 Top-K 容量。它是失败诊断和推荐解释项，不替代 Context Precision、Recall 或 Coverage。

## 5. 检索指标示例

Case 有两条 Evidence：

```text
g1 = doc-A [100, 140)  长度 40
g2 = doc-A [300, 340)  长度 40
```

前三条结果：

```text
r1 = doc-A [90, 130)   与 g1 相交 30
r2 = doc-B [0, 100)    不相关
r3 = doc-A [300, 320)  与 g2 相交 20
```

因此：

- `rel = [1,0,1]`
- `Hit@3 = 1`
- `Evidence Recall@3 = 2/2 = 1`
- `MRR@3 = 1`
- `Context Precision@3 = (1/1 + 2/3) / 2 = 0.8333`
- `Gold Coverage@3 = (30+20)/(40+40) = 0.625`

## 6. Answer Faithfulness

### 6.1 定义

Faithfulness 只回答：生成答案中的可验证事实声明，是否被本次实际送入生成阶段的 Context 支持。

它不判断答案是否覆盖参考答案的全部要点，也不判断语言风格或业务偏好。

### 6.2 两阶段 Judge

Judge 在同一次受控调用中执行两个逻辑步骤，并返回一个结构化对象：

1. 从生成答案抽取最小可验证 claims。
2. 对每条 claim，根据实际 Context 判定 `supported/unsupported/unclear`，并引用支持或冲突的 context ranks。

Judge 输入：

- Question。
- Generated Answer。
- 最终送入回答模型的 Context，带稳定 rank 和截断标记。
- 不提供 Reference Answer，避免把正确性混入忠实度。
- 输入必须符合 Run 的 `evaluation_data_policy`；受限 KB 禁止外部 Judge。没有合规私有 Judge 时执行标记 failed/invalid/`EVALUATION_DATA_POLICY_BLOCKED`，不得发送敏感 Context 或伪造结果。

Judge 配置：

- temperature 固定为 0。
- 模型、参数、Prompt 版本和 Prompt hash 固定在 Run snapshot。
- 首次 JSON 解析失败允许一次使用相同模型/Prompt 的格式修复重试。
- 第二次仍失败则 MetricResult 为 invalid。
- 必须绑定 passed JudgeCalibration 才能参与 Recommendation；未通过校准的结果标记 `calibration_status=failed/not_evaluated`，可展示但不可作门禁。

### 6.3 输出 Schema

```json
{
  "status": "valid",
  "claims": [
    {
      "claim": "用户需要先创建数据源。",
      "verdict": "supported",
      "context_ranks": [1],
      "reason": "Context 1 明确描述了创建数据源步骤。"
    },
    {
      "claim": "同步会在五分钟内完成。",
      "verdict": "unsupported",
      "context_ranks": [],
      "reason": "提供的 Context 未说明五分钟限制。"
    }
  ],
  "summary_reason": "一个声明有支持，一个声明无支持。"
}
```

Schema 约束：

- `status`：`valid/invalid/abstain`。
- valid 必须至少有一个 claim。
- verdict：`supported/unsupported/unclear`。
- `context_ranks` 必须引用输入中存在的 rank；unsupported 可以为空。
- reason 必须非空，不能包含新的事实判断作为评分依据。

### 6.4 计算公式

仅对 `supported` 和 `unsupported` 计分，`unclear` 不能被强行当作 0：

```text
scorable_claims = supported_count + unsupported_count

Faithfulness = supported_count / scorable_claims
```

状态规则：

- `status=valid` 且 `scorable_claims > 0`：产生 0–1 分数。
- 答案为空或生成失败：Faithfulness invalid，Item 保存生成失败原因。
- 答案只有拒答/无事实内容：Faithfulness abstain，value=null。
- 所有 claim 都为 unclear：Faithfulness abstain，value=null。
- 输出不符合 Schema、引用不存在 rank：Faithfulness invalid，value=null。
- Judge 调用超时/服务失败：execution_status=failed，value/status=null；失败与 invalid 分开统计。

invalid 和 abstain 不参与 Run 平均值，但必须分别统计数量。

### 6.5 参考答案的作用

Reference Answer 只用于 UI 对照、人工审核和独立的 Answer Correctness Judge，不进入 Faithfulness Judge。这样保持“回答是否受 Context 支持”与“回答是否符合参考答案”的边界。

## 7. Answer Correctness

### 7.1 定义

Correctness 只对 `answerability=answerable` 计算，判断 Generated Answer 与 Reference Answer/reference key points 在事实和结论上是否一致。输入包含 Question、Generated Answer、Reference Answer、Reference Key Points 和 Gold Evidence；不把 Retrieved Context 作为正确性替代标准。

Judge 输出逐 key point：`matched/contradicted/missing/unclear`，并保存生成答案中的额外错误 claims。计分：

```text
matched = matched key points
contradicted = contradicted key points + extra false claims
scorable = matched + contradicted + missing

Answer Correctness = matched / scorable
```

`unclear` 不进入分母；`scorable=0`、Parser 失败或 Reference 门禁失败时为 invalid。Judge 调用失败单独记 execution failed。Correctness 与 Faithfulness 分开存储和展示。unanswerable Case 的 Correctness 固定为 `abstain/null/NOT_APPLICABLE_UNANSWERABLE`。

## 8. 无答案拒答质量

仅对 `answerability=unanswerable` 计算。输入包含 Question、Generated Answer、`checked_scope`、`unanswerable_reason_code` 和期望拒答原则，不提供虚构 Reference Answer。

输出 verdict：

- `correct_refusal`：明确说明无法从给定范围回答，且没有编造具体事实，value=1。
- `hallucinated_answer`：给出无证据的具体事实，value=0。
- `over_refusal`：拒答理由与检查范围不符或拒绝了范围内可回答部分，value=0。
- `unclear`：无法可靠判定，status=abstain/value=null。
- Parser/Schema 失败：`execution_status=completed/status=invalid/value=null`。
- Judge 超时或服务调用失败：`execution_status=failed/status=null/value=null`。

answerable Case 的拒答质量固定为 `abstain/null/NOT_APPLICABLE_ANSWERABLE`；unanswerable Case 的 Answer Correctness 为 `abstain/null/NOT_APPLICABLE_UNANSWERABLE`。Faithfulness 只在其回答包含可验证事实 claims 时计算，否则 abstain。

## 9. Judge 上线门禁

每个 Judge metric/model/Prompt/Parser 组合必须有独立 calibration version。最低人工校准集为 60 条，由两名审核者标注并对分歧裁决，至少包含正常、低分、无答案和边界案例。

passed 标准：

| 检查 | 门槛 |
| --- | --- |
| Macro-F1 | `>= 0.80` |
| Exact Agreement | `>= 0.85` |
| Weighted Kappa | `>= 0.70` |
| 相同样本三次标签一致率 | `>= 0.90` |
| Parser failure rate | `< 0.01` |

MVP 只使用 Pointwise Judge；不定义 pairwise、A/B 顺序交换或 swap 稳定性门槛。Calibration `/run` 支持幂等：`draft/failed` 可进入 running，running 重复调用返回同一执行或 409，passed 不原地重跑；模型、关键参数、Prompt 或 Parser 任一变化都要求新 Calibration Version。MVP 状态仅为 `draft/running/passed/failed`，不保留 archived。

未通过时：

- MetricResult 可以保存并在 UI 标为 uncalibrated。
- 不得参与 ChunkExperiment 的 provisional 选择、holdout 门禁或 Recommendation。
- 不得使用未校准 fallback 替代。

## 10. 聚合规则

### 10.1 Run 聚合

对每个 metric：

- 仅聚合 `execution_status=completed + status=valid` 的 Item。
- 输出 `eligible_count`、`valid_count`、`invalid_count`、`abstain_count`、`failed_count`、`mean`、P50 和 P95（数值指标适用时）。
- unanswerable 的五项 Gold-dependent metrics 与 answerable 的不适用拒答指标以 abstain 记录，但不进入相应分母。
- 检索指标额外输出按 question_type、difficulty、split 和 document_id 的 mean/count。
- Faithfulness、Correctness、拒答质量分别输出 mean/count、calibration version/status 和各自判定计数。
- 不填补缺失值，不把 invalid/abstain 当 0。

示例：

```json
{
  "metric_name": "faithfulness",
  "metric_version": "faithfulness-prompt-v1",
  "mean": 0.86,
  "eligible_count": 41,
  "valid_count": 37,
  "invalid_count": 2,
  "abstain_count": 1,
  "failed_count": 1,
  "total_count": 41
}
```

### 10.2 Experiment tuning/holdout 聚合

- baseline 与全部 candidates 先在 tuning 上运行；每对比较只使用 baseline/candidate 的共同 valid Item。
- 每个 metric 必须保存 eligible holdout count、baseline/candidate valid count、common valid count、双方 invalid/abstain/failed count 和排除原因。
- `valid_coverage = valid_count / eligible_count`；MVP `allowed_drop=0`，candidate valid coverage 不得低于 baseline，防止通过制造 invalid 选择性提高均值。
- tuning 通过硬门禁和确定性排序选择唯一 provisional candidate。
- holdout 只比较 baseline 与 provisional candidate，并使用二者共同 valid Item。
- holdout 只能 pass/reject，不能重新比较其他 candidate 或改变 provisional candidate。
- 最终共同 valid holdout 少于 30，输出 `no_better_strategy/INSUFFICIENT_VALID_CASES`。
- 任一参与 Recommendation 的 Judge 指标 calibration 未 passed，输出 `no_better_strategy/JUDGE_CALIBRATION_REQUIRED`。

### 10.3 百分点

实验差异统一为绝对分数差：

```text
delta(metric) = candidate_mean - baseline_mean
```

例如 0.62 到 0.66 为 `+0.04`，展示为 `+4.0 pp`，不能展示为“提升 6.45%”混淆相对变化。

## 11. 系统与资源指标

### 11.1 延迟

每个 Item 保存：

- `retrieval_ms`
- `rerank_ms`（未启用时为 null）
- `generation_ms`
- `judge_ms`
- `total_ms`

Run/Variant 聚合输出 mean、P50 和 P95。失败 Item 的已完成阶段延迟保留，但不进入完整 `total_ms` 分位数，并单独计入失败率。

### 11.2 Token

分别保存：

- generation input/output/total tokens。
- judge input/output/total tokens。

只有模型返回用量时记录；未知值为 null，不能估算后伪装成精确值。

### 11.3 Cost

只有模型适配器提供明确费用或配置了可追溯的单价版本时才计算。Cost 必须附 currency 和 pricing version。没有可靠来源时为 null。

### 11.4 失败率

```text
Failure Rate = failed_item_count / planned_item_count
```

canceled Item 单独展示，不混入 Failure Rate。Run 被 blocked 时没有 Item Failure Rate，Run 状态本身表达失败原因。

### 11.5 索引与 Chunk 规模

每个 Experiment Variant 记录：

- `chunk_count`。
- `index_build_time_ms`，从解析开始到临时索引可查询。
- `embedding_count` 与可获得的 embedding token/cost。
- 可获得的 index size/build cost；向量后端不提供时为 null，并注明来源缺失。

成本不能由模型臆测。Experiment 启动前的 estimate 与实际值分开保存和展示。

### 11.6 FailureDiagnosis 与候选维度

Diagnosis 的 `target_k` 必须显式存在，Recommendation 不使用隐式默认 K。诊断顺序先排除 Gold、解析/索引、Embedding/Search、Rerank、Context 已充分但 Generator 失败等非 Chunking 原因，再分析边界、尺寸、标题、跨证据和冗余信号。

| reason code | candidate 可变维度 |
| --- | --- |
| `CHUNK_TOO_LARGE_NOISY` | 减小 size，或测试更结构化的现有策略 |
| `CHUNK_TOO_SMALL_INCOMPLETE` | 增大 size，或调整现有父子上下文 |
| `CHUNK_BOUNDARY_SPLIT` | 增加 overlap 或 size |
| `TOPK_REDUNDANCY_HIGH` | 降低 overlap |
| `HEADING_BODY_DETACHED` | heading strategy |
| `CROSS_EVIDENCE_NOT_CO_RETRIEVED` | 仅按证据指向调整 size/父子上下文 |

非 Chunking reason 和 `UNKNOWN_CAUSE/INSUFFICIENT_VALID_CASES` 不生成候选。blocked Run 不得被分类为 chunking_likely。

## 12. Recommendation 判定

候选 Variant 只有同时满足以下条件才可被推荐：

1. 与 baseline 有至少 30 个共同有效 holdout Item，并保存 eligible、双方 valid/invalid/abstain/failed、common valid 和排除原因。
2. candidate 对每个参与门禁指标的 valid coverage 不低于 baseline（MVP `allowed_drop=0`）。
3. `delta(Context Precision@targetK) >= 0.03`，但不得单独决定推荐。
4. `delta(Evidence Recall@targetK) >= 0`。
5. `delta(Gold Coverage@targetK) >= 0`。
6. passed calibration 的 `delta(Faithfulness) >= -0.02`。
7. answerable 共同 valid Item 上 `delta(Answer Correctness) >= -0.02`。
8. 存在 unanswerable 共同 valid Item 时，`delta(Unanswerable Refusal Quality) >= -0.02`。
9. `candidate_failure_rate <= baseline_failure_rate`。
10. Candidate Run 和临时资源处理没有 Run 级失败。

目标 K 必须来自触发 Experiment 的 completed `chunking_likely` FailureDiagnosis 并冻结到 Experiment snapshot；Diagnosis 缺少 target K 时不得启动 Experiment。

provisional candidate 只在 tuning 阶段从多个候选中选择，依次比较：

1. Context Precision delta 更高。
2. Faithfulness delta 更高。
3. Failure Rate 更低。
4. P95 total latency 更低。
5. Chunk Redundancy 更低。
6. chunk count、index build time 与可获得的成本更低。
7. Config hash 字典序，用于完全相同结果的确定性选择。

资源方案固定为 MVP 方案 B：P95、chunk count、index build time、embedding/index token/cost 只展示，并可用于 tuning tie-break；它们不参与 holdout pass/reject，规格不得称其为 Recommendation 硬门禁。

holdout 只决定该 provisional candidate 是否通过。任何门槛失败都明确输出 `no_better_strategy` 及失败门禁，不能改选第二名。推荐结果必须同时展示所有核心指标和资源数据，不能只展示获胜指标。

## 13. Langfuse Score 映射

每个 valid 规则指标和适用的生成指标写入对应 Item Trace：

| MetricResult | Langfuse Score name | value |
| --- | --- | --- |
| `hit@5` | `rag.hit@5` | 0/1 |
| `evidence_recall@5` | `rag.evidence_recall@5` | 0–1 |
| `mrr@5` | `rag.mrr@5` | 0–1 |
| `context_precision@5` | `rag.context_precision@5` | 0–1 |
| `gold_coverage@5` | `rag.gold_coverage@5` | 0–1 |
| `chunk_redundancy@5` | `rag.chunk_redundancy@5` | 0–1 |
| `faithfulness` | `rag.faithfulness` | 0–1 |
| `answer_correctness` | `rag.answer_correctness` | 0–1 |
| `unanswerable_refusal_quality` | `rag.unanswerable_refusal_quality` | 0/1 |

其他 K 按相同命名规则。invalid/abstain 不写数值 Score；Trace metadata 保存内部 metric status 和 reason。Langfuse 写入失败不会改变内部 MetricResult。

## 14. 非 MVP 增强指标

以下指标不是永久放弃，但不进入 MVP：

- Answer Relevance。
- Answer Completeness。
- Citation Correctness。
- Citation Completeness/Coverage。

它们必须采用独立 metric name、Prompt、calibration 和 valid Item 统计，不能复用 Faithfulness 或 Overall Score 替代。Citation 指标还依赖稳定的 claim↔citation 数据关联原型，当前为 **Pending Verification**。

## 15. 展示要求

- UI 以 0–1 原值存储，以百分比展示时保留一位小数。
- 指标卡必须同时展示有效样本数，例如 `86.0% · 37 valid`。
- invalid、abstain、failed、canceled 分开显示。
- 实验差异使用 `pp` 表示绝对百分点。
- 不对不同 metric version 的结果绘制为连续可比结果。
- 不展示单一 Overall Score。
