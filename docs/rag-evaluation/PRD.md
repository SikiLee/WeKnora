# WeKnora RAG 自适应评测与 Chunking 策略推荐 PRD

## 1. 文档信息

| 项目 | 内容 |
| --- | --- |
| 课题 | RAG 自适应评测与 Chunking 策略自修复流水线（算法应用与数据治理） |
| 交付形态 | WeKnora 内置评测能力的产品规格 |
| 目标仓库基线 | `7b2c9e73bf6950edde8165e4fe352714b7e5c16e` |
| 代码事实来源 | `docs/rag-evaluation/CODEBASE_MAP.md` |
| 运行事实来源 | `docs/rag-evaluation/BASELINE_REPORT.md` |
| 研究来源 | `C:/Users/Administrator/Downloads/PROJECT_RESEARCH_REVIEW(1).md`，视为正式的 `PROJECT_RESEARCH_REVIEW.md` |

本文只定义原题直接涉及的最小完整闭环，不规划通用评测平台或其他 RAG 优化能力。

## 2. 背景与问题

WeKnora 已具备真实 RAG 链路、可配置 Chunking、重排、回答引用、异步任务基础设施和 Langfuse Trace/Span/Generation 写入能力，但当前评测接口仍把任务和结果保存在进程内存中，没有可管理的测试集、持久化运行、LLM Judge、定时执行和 Web 仪表盘。

生产使用者因此无法稳定回答以下问题：

- 某个知识库当前能否召回回答问题所需的证据？
- 生成回答中的声明是否都能被召回上下文支持？
- 指标下降是个别问题还是某类文档的持续现象？
- 修改 Chunk size、overlap 或切分策略是否真的改善了质量？

## 3. 产品目标

建立一条可复现的最小评测闭环：

1. 从知识库中的选定文档自动生成有证据依据的 Q&A 测试集。
2. 由具备知识库写权限的用户审核并发布不可变的测试集版本。
3. 手动或定时运行 WeKnora 的真实 RAG 链路。
4. 分别计算检索质量、Answer Faithfulness、Answer Correctness 和无答案拒答质量，不使用不透明综合分。
5. 在 WeKnora UI 查看汇总、单题证据、失败原因和 Langfuse Trace。
6. 先把失败诊断为 `chunking_likely/non_chunking_likely/unknown`，只有 `chunking_likely` 才能进入有限 Chunking 候选实验并产生只读推荐。

## 4. 用户与权限

### 4.1 具备目标知识库写权限的已认证主体

- 创建和管理测试集。
- 发起测试集生成、手动评测和 Chunking 实验。
- 创建和运行绑定当前知识库的 Judge Calibration。
- 创建、启停和手动触发定时评测。
- 审核、编辑、启用、删除测试样本并发布版本。
- 查看其有权访问知识库的全部评测结果。

MVP 只承诺复用当前仓库已存在的 tenant context、通用 RBAC/permission middleware 和知识库 read/write access。Owner/Admin/Contributor/Viewer 等精确角色常量及其在新增端点上的装配方式未由 `CODEBASE_MAP.md` 完整证明，统一标记 **Pending Verification**；任何 tenant 角色都不能绕过目标知识库访问检查。

### 4.2 具备目标知识库读权限的已认证主体

- 查看该知识库下已发布测试集、运行结果、实验结果和推荐。
- 没有知识库写权限时不能生成测试集、修改样本、发起运行、管理调度或发起实验。

### 4.3 API Key

- 新 Testset/Schedule/Experiment/Calibration 端点的现有 capability 映射没有源码事实支持，标记 **Pending Verification**。
- MVP 不新增细粒度 API Key capability；能力映射确认前，新增写端点对 API Key 默认拒绝。
- 非 MVP 的确认、应用、重解析和回滚不在现行 API Key 权限承诺内。

## 5. 核心用户流程

### 5.1 生成并发布测试集

1. 具备写权限的用户进入知识库的“RAG 评测”页面。
2. 创建 Testset 及空 draft version。空 draft 的文档范围和 Generation Profile 允许为空。
3. 第一次合法 TestsetGeneration 请求被系统成功接纳并持久化时，原子锁定该 draft 的 Generation Profile：文档集合及内容 hash、`offset_unit`、`source_normalization_version`、split algorithm/seed、generator model 和 generator Prompt version/hash。后续 Generation 只有在 Profile 完全一致时才可被接纳；不一致返回稳定 409，用户必须新建 draft version。
4. 系统异步读取文档内容，由 LLM 生成问题、参考答案、题型、难度、answerability 和 Gold Evidence；无答案题同时保存检查范围与原因。具体 Case 和 TestsetGeneration 保留各自 provenance，Version 级 generator 字段仅为已锁定 Profile 摘要。
5. 系统依次执行 Schema、重复、答案泄漏、歧义和 claim-evidence support 检查，并校验证据的 source identity、offset unit 和 normalization version；失败样本进入拒绝记录而不是可发布草稿。
6. 具备写权限的用户逐条审核、编辑、启用或删除样本。
7. 所有启用样本审核通过且 Generation Profile 完整后发布 TestsetVersion。
8. 发布版本不可修改；继续修改时创建新的草稿版本。

### 5.2 手动评测

1. 具备知识库写权限的用户选择已发布 TestsetVersion、生成模型、Judge 模型和可选重排模型。
2. 系统冻结测试集、文档、source normalization、split seed、实际生效 Chunking、模型关键参数、真实回答 Prompt、上下文构造版本、无历史会话策略、Judge/校准版本、检索参数和代码版本。
3. Worker 对每个启用样本执行真实检索和回答生成。
4. 系统计算规则指标，再对 answerable 样本执行 Faithfulness/Correctness，对 unanswerable 样本执行 Faithfulness/拒答质量；未通过上线门禁的 Judge 结果可以展示，但不能参与 Recommendation。
5. UI 持续展示进度；完成后展示汇总和单题结果。
6. 用户可以通过 Trace ID 或可用的 Trace URL 进入 Langfuse 排查。

### 5.3 定时评测

1. 具备写权限的用户为一个已发布 TestsetVersion 配置 cron、时区、最多 100 个 Case、确定性采样算法/seed、完整 Run 模板和 `skip_if_active` 不重叠策略；不得在触发时隐式选择“最新”模型、Prompt 或 Calibration。
2. 到达触发时间后，系统先原子占用唯一的 EvalScheduleSlot。已有相同 `schedule_id + scheduled_at_utc` 的 Slot 时幂等返回。
3. 若同 Schedule 已有 active Run，Slot 持久化为 `skipped_active`/`SCHEDULE_SKIPPED_ACTIVE` 且不创建 EvalRun；否则创建一次 EvalRun，并把 Slot 记为 `run_created`。
4. 调度运行与手动运行使用同一执行链路、状态和结果结构，并把 Schedule 模板解析成完整、不可变的 Run snapshot。
5. 测试集或依赖未就绪但 Run 能可靠持久化时，创建可查询的 `blocked` Run；在创建 Run 前发生的可记录失败写入 Slot `failed_before_run`。

### 5.4 失败诊断与 Chunking 推荐

1. EvalRun 通过版本化 Diagnosis Worker 生成 FailureDiagnosis，状态为 `pending/running/completed/failed`，输出 `chunking_likely`、`non_chunking_likely` 或 `unknown`，并保存 `diagnosis_version`、`target_k`、eligible/valid Case 数、稳定 reason codes 与诊断证据。blocked Run 只能得到 `non_chunking_likely` 或 `unknown`。
2. MVP 只允许具备写权限的用户从 `completed + chunking_likely` 诊断手动创建 ChunkExperiment；连续低分自动创建实验草稿降为非 MVP。
3. 系统以源 Run 中实际生效的 Chunking 配置为 baseline，并严格按 diagnosis reason 限制候选维度，最多生成两个有限候选；不得为所有 Diagnosis 生成同一通用网格。启动前展示预计解析、Embedding、RAG 和 Judge 调用规模。
4. 临时知识库中的 execution document 必须映射到 source document/hash/offset；活动知识库不被修改。
5. 所有候选先仅在 tuning 上比较并选择一个 provisional candidate；随后 holdout 只比较 baseline 与该候选并给出通过/拒绝，禁止用 holdout 重新选择候选。
6. 推荐不能由 Context Precision 单独决定，还必须通过 Evidence Recall、Gold Coverage、共同 valid coverage、Judge 指标和失败率门禁。MVP 采用“资源不参与 holdout pass/reject”的方案：资源数据只展示，并可作为 tuning tie-break，不能被描述为 Recommendation 硬门禁。
7. 不满足门禁时明确输出“没有更优策略”，而不是返回模糊空值。
8. MVP Recommendation 只读，不自动修改线上配置。

## 6. 功能需求

### 6.1 Testset 管理

- **FR-TS-01**：Testset 必须属于一个 tenant 和一个 knowledge base。
- **FR-TS-02**：TestsetVersion 可先创建空 draft；第一次合法 Generation 被成功接纳时原子锁定文档集合/hash、normalization、offset、split、generator model/Prompt 的 Generation Profile。后续 Generation 必须完全一致，否则返回 409；所选文档必须为 `completed` 且可读取原始内容。
- **FR-TS-03**：每个样本必须包含问题、answerability、参考答案或预期拒答、题型、难度、数据分组、具体 Generation/模型/Prompt provenance、审核状态和质量检查结果；Version 级 generator 字段仅保存锁定 Profile 摘要。
- **FR-TS-04**：MVP 支持 `single_evidence`、`multi_evidence`、`unanswerable`，以及 `easy/medium/hard`；未经稳定定位原型确认的文档类型必须标记 **Pending Verification**，不能进入发布范围。
- **FR-TS-05**：Gold Evidence 必须保存 source document identity/hash、`offset_unit`、`source_normalization_version`、原文起止位置、证据快照和内容哈希；execution document/Chunk ID 只能作为映射或辅助信息。
- **FR-TS-06**：具备知识库写权限的用户可以新增、编辑、启用、禁用和删除草稿样本。
- **FR-TS-07**：只有 `approved`、evidence 未失效且 Schema/重复/答案泄漏/歧义/claim-evidence support 门禁通过的启用样本可以发布；answerable Case 必须有 Gold Evidence，unanswerable Case 必须没有 Gold Evidence并保存 `checked_scope`、稳定 reason code 与 expected refusal。
- **FR-TS-08**：TestsetVersion 发布后不可修改；编辑已发布内容必须创建新草稿版本。
- **FR-TS-09**：样本必须标记 `tuning/holdout`，TestsetVersion 保存 split algorithm 与 seed；tuning 选择唯一 provisional candidate，holdout 只对 baseline 与该候选做最终通过/拒绝，禁止重新选择。

### 6.2 EvalRun

- **FR-RUN-01**：MVP 支持手动、定时和实验触发；旧 Evaluation API 完整迁移不属于 MVP，现有接口保持独立并标记兼容策略 Pending Verification。
- **FR-RUN-02**：正式运行必须使用已发布 TestsetVersion。
- **FR-RUN-03**：运行冻结文档/source identity/normalization、split seed、实际 Chunking、Embedding/Rerank/生成/Judge 模型关键参数、真实回答 Prompt version/hash、上下文构造版本、`no_history` 会话策略、Judge calibration、指标和代码版本。
- **FR-RUN-04**：正式任务和结果必须持久化，不得仅保存在进程内存。
- **FR-RUN-05**：执行使用现有 Asynq 基础设施，不使用裸 goroutine 管理生命周期。
- **FR-RUN-06**：支持查看进度、取消和显式 retry scope：`failed` 只选择 failed Item，`unfinished` 选择 pending/running/canceled Item，`all` 选择全部原计划 Case。partial 支持三种 scope，canceled 至少支持 `unfinished/all`，failed 支持 `failed/all`，blocked 只支持 `all`；retry 始终创建保存 `parent_run_id/retry_scope` 的新 Run，且精确冻结所选 Case ID。
- **FR-RUN-07**：单个样本失败或取消不应抹掉其他结果；存在 completed Item 且同时存在 failed/canceled Item 时 Run 为 `partial`，用户取消且没有 completed Item 时为 `canceled`。
- **FR-RUN-08**：模型缺失、文档未就绪、测试集失效等前置条件错误必须记录为 `blocked`。
- **FR-RUN-09**：每个 Run 最多执行 100 个 Case；每 tenant 同时最多 2 个普通 EvalRun，超限请求返回现有 429 契约。
- **FR-RUN-10**：资源级 readiness 失败在成功持久化后返回 202 + blocked Run；只有请求无法被持久化接受时才返回 503 且不创建 Run。

### 6.3 质量指标

- **FR-MET-01**：规则指标包括 Context Precision@K、Hit@K、Evidence Recall@K、MRR@K 和 Gold Coverage@K；它们只适用于 answerable Case，unanswerable Case 统一产生 `abstain/null/NOT_APPLICABLE_UNANSWERABLE` 并从聚合分母排除。
- **FR-MET-02**：默认计算 K=5 和 K=10；调用方可以在运行创建时覆盖，但 K 必须为 1–100 的升序去重整数。
- **FR-MET-03**：MVP 生成指标至少包括 Answer Faithfulness、Answer Correctness 和无答案拒答质量；Answer Relevance、Completeness 与 Citation 指标属于非 MVP 增强阶段。
- **FR-MET-04**：Judge 输出必须可解析、可版本化并保留逐声明证据和理由。
- **FR-MET-05**：invalid、abstain 和执行失败不得作为 0 分混入均值。
- **FR-MET-06**：不得生成跨检索与生成阶段的不透明综合分。
- **FR-MET-07**：同时记录阶段延迟、Token、失败率和模型返回的可用成本信息。
- **FR-MET-08**：记录 Chunk Redundancy、chunk count、index build time，以及可获得的 embedding/index 成本，供诊断、展示和 tuning tie-break 使用；MVP 不把资源作为 holdout Recommendation 硬门禁。
- **FR-MET-09**：MVP Judge 为 Pointwise Judge，必须绑定目标知识库范围内已通过的人工校准版本；未通过解析失败率、人工一致性和三次重复稳定性门禁时，不得参与 Recommendation，不使用 pairwise/swap 门槛。

### 6.4 Langfuse

- **FR-LF-01**：每个 EvalRunItem 对应一个 Trace，`session_id` 使用 EvalRun ID。
- **FR-LF-02**：检索和重排记录为 Span，回答与 Judge 调用记录为 Generation，指标记录为 Score。
- **FR-LF-03**：WeKnora 内部结果先持久化，Langfuse 写入采用 best effort。
- **FR-LF-04**：Langfuse 关闭、采样未命中或写入失败时，评测仍可完成。
- **FR-LF-05**：UI 始终展示 Trace ID；只有后端能可靠构造 URL 时才展示外链。
- **FR-LF-06**：不引入 Langfuse Dataset 或 Experiment 作为业务依赖。

### 6.5 EvalSchedule

- **FR-SCH-01**：Schedule 必须绑定已发布 TestsetVersion、目标知识库、cron 和 IANA 时区；cron 的正式字段数和语法以现有 scheduler 源码原型为准，当前标记 **Pending Verification**。
- **FR-SCH-02**：Schedule 保存可确定性生成完整 Run snapshot 的模板：answer/Judge/Rerank/Embedding 模型及关键参数、明确的 Judge calibration versions、真实 RAG answer Prompt version/hash、context builder、no-history policy、retriever/rerank 参数、top_k、metric version、sample selection algorithm/seed、sample_limit 和 `skip_if_active`。
- **FR-SCH-03**：每个 cron 时间先原子创建唯一 `EvalScheduleSlot(schedule_id, scheduled_at_utc)`；原子占用与最终判定之间 `outcome` 可短暂为空，但必须最终写为 `run_created/skipped_active/failed_before_run`，中断后恢复为 `failed_before_run` 而不是补建 Run，因此即使没有 EvalRun 也能永久防重。
- **FR-SCH-04**：具备 KB write access 的主体可以创建、编辑、启停、归档和立即触发 Schedule；POST 状态由服务端固定 active，archived 为终态。
- **FR-SCH-05**：Schedule 不包含趋势告警或复杂费用预算；样本上限是本范围的成本保护机制。
- **FR-SCH-06**：MVP 固定采用 `skip_if_active` 或等价不重叠规则；同 tenant/KB/Schedule 已有 active Run 时记录 skipped，不创建重叠 Run。

### 6.6 ChunkExperiment

- **FR-EXP-01**：MVP 每个实验由一个 baseline 和 1–2 个候选组成，总 Variant 数不超过 3；同 tenant 同时最多 1 个 active Experiment。
- **FR-EXP-02**：候选策略只允许当前有独立实现的 `auto/heading/heuristic/legacy`；`recursive` 在仍是 legacy 别名时不能作为独立候选。
- **FR-EXP-03**：候选围绕实际 baseline 调整 Chunk size、overlap 或策略，不修改其他 RAG 参数。
- **FR-EXP-04**：Variant 使用同一 TestsetVersion、split seed、source normalization、Embedding/Rerank/生成/Judge、回答 Prompt、上下文构造和无历史会话策略；比较时只使用 baseline/candidate 的共同 valid Item。
- **FR-EXP-05**：临时知识库保存 `source_document_id ↔ execution_document_id` 映射，Retrieved Context 必须映射回 source hash/offset；Metrics 只按稳定 source identity 匹配。
- **FR-EXP-06**：创建 Experiment 前必须存在 `completed + chunking_likely` FailureDiagnosis，且一个 Experiment 只绑定一个 `source_run_id`。Diagnosis 保存版本、状态、`target_k`、eligible/valid 数、规定 reason codes 和证据；reason code 必须限制可变候选维度，`non_chunking_likely/unknown` 返回冲突。
- **FR-EXP-07**：先以 tuning 选择一个 provisional candidate，再以 holdout 对 baseline/candidate 作一次最终门禁；holdout 不得参与重新选择。
- **FR-EXP-08**：推荐至少需要 30 个共同 valid holdout 样本，并按指标保存 eligible、baseline/candidate valid、common valid、双方 invalid/abstain/failed 与排除原因；candidate 的 valid coverage 不得低于 baseline（MVP `allowed_drop=0`），同时满足 Context Precision 改善、Evidence Recall/Gold Coverage、已校准 Faithfulness/Correctness/拒答质量和失败率门禁。
- **FR-EXP-09**：启动前展示预计文档解析、chunk、Embedding、RAG、Judge 调用规模；结果展示 chunk count、index build time 和可获得的 embedding/index 成本。
- **FR-EXP-10**：系统必须能持久化并展示 `recommend/no_better_strategy`；MVP 不自动更新源知识库配置。

### 6.7 Web UI

- **FR-UI-01**：知识库详情页提供“RAG 评测”入口。
- **FR-UI-02**：单页包含总览、测试集、运行记录、结果详情、Chunking 对比和 Schedule/Slot 六个区域。
- **FR-UI-03**：总览展示 Context Precision、Hit、Recall、MRR、Coverage、Faithfulness、Correctness、拒答质量、有效样本数和失败数，不展示综合分。
- **FR-UI-04**：结果详情展示问题、参考答案、实际答案、Gold Evidence、召回 Chunk、逐项指标、Judge 理由、错误和 Trace 信息。
- **FR-UI-05**：运行中展示已完成/总数和当前状态；blocked、partial、failed 必须有可读原因。
- **FR-UI-06**：失败分析展示 diagnosis label、reason codes 与证据；Chunking 对比展示 tuning provisional、holdout 决策、配置差异、质量/冗余、valid coverage、chunk/index 构建规模、延迟、Token/成本及“推荐/没有更优策略”；Schedule 区展示最近 Slot outcome/reason 与关联 Run。
- **FR-UI-07**：所有新文案接入现有 Vue i18n，至少提供中文和英文键值。

## 7. 非功能要求

- **可复现性**：结果必须能定位到 source/execution document mapping、offset unit、normalization、split algorithm/seed、测试集、Chunking、模型关键参数、真实回答/Judge Prompt、上下文构造、无历史会话策略、代码 release 和运行时间。
- **租户隔离**：所有列表和详情查询都必须以有效 tenant 和知识库权限过滤。
- **幂等性**：测试集生成、运行、调度触发、重试和实验启动支持幂等键。
- **故障可见性**：任务不得无限停留在 processing；所有不可恢复错误必须进入持久化终态。
- **数据治理**：90/365 天是目标留存策略；MVP 保存留存分类与到期时间，完整自动清理执行器降为非 MVP并标记 **Pending Verification**。
- **评测数据策略**：每个 TestsetVersion、Run、Schedule 与 Experiment 必须冻结 `evaluation_data_policy` version/hash。真实敏感级别映射为 **Pending Verification**；安全默认值是受限 KB 禁止外部 Judge，Question/Answer 仅发送脱敏值或 hash，Context 仅发送 correlation/source ID、hash、rank、长度和脱敏摘要。无合规 Judge 时不得外发或伪造 Judge 结果。
- **成本与并发**：Run 最多 100 Case、Experiment 最多两个 candidate、tenant 并发受限、Schedule 不重叠；Experiment 启动前必须展示预计调用规模。
- **可用性**：Langfuse 是可选观测依赖，不是评测成功的前置条件。

## 8. 非 MVP 增强阶段

以下仍属于题目闭环，但不进入 MVP 验收：

- Recommendation 人工 confirm/reject，生成 ChunkConfigVersion。
- 用户确认后的 canary reparse、分批 apply、Holdout 回归和 rollback。
- 连续低分自动创建实验草稿；即使实现，也必须先得到 `chunking_likely` 诊断。
- 90/365 天完整自动清理执行器。
- Answer Relevance、Answer Completeness、Citation Correctness/Completeness。
- 旧 Evaluation API 到新持久化领域的完整迁移。

上述增强依赖配置版本、跨向量后端索引隔离/切换、reparse 状态可靠性等技术原型，统一标记 **Pending Verification**；MVP 仍止于只读 Recommendation。

## 9. 明确不做

- 模型训练、微调或奖励模型建设。
- 新增或替换向量数据库。
- 通用评测框架、通用 Agent 平台或论文实验平台。
- Query Rewrite、Embedding、Rerank、Prompt 路由等非 Chunking 优化。
- 不透明总分、复杂权重体系和复杂费用预算。
- 趋势告警、通知中心和跨知识库排行榜。
- Langfuse Dataset/Experiment 双向同步。

## 10. MVP 完成判定

当以下能力能在同一知识库中串联完成时，本课题范围才算完成：

1. 从真实文档生成并发布有 Gold Evidence 的 TestsetVersion。
2. 手动和 cron 均能创建持久化 EvalRun。
3. EvalRun 能执行真实 RAG，展示规则指标、Faithfulness、Correctness 与无答案拒答质量，并标识 Judge calibration 状态。
4. 单题结果能在 WeKnora UI 与 Langfuse Trace 间关联。
5. 失败诊断输出稳定 label/reason/evidence，只有 `chunking_likely` 可创建实验。
6. 同一测试集能以 source identity 映射在临时环境完成 tuning 选择与 holdout 二选一验证。
7. 多维门槛通过时展示推荐，否则明确展示“没有更优策略”。
8. 整个 MVP 不修改活动知识库的 Chunking 配置和索引。
