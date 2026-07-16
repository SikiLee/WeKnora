# WeKnora RAG 评测规格决策记录

## 1. 记录规则

- 本文只记录已经确定且会影响七份规格一致性的决定。
- 日期使用 Asia/Shanghai 当前工作日期 `2026-07-16`。
- `PROJECT_RESEARCH_REVIEW(1).md` 视为正式的 `PROJECT_RESEARCH_REVIEW.md`，文件名差异不是待确认项。
- 冲突处理优先级固定为：腾讯项目原题 → `CODEBASE_MAP.md` → `BASELINE_REPORT.md` → `PROJECT_RESEARCH_REVIEW.md` → 当前七份规格；代码事实仍以代码地图为依据，运行事实以基线报告为依据。

## 2. 决策摘要

| ID | 决策 | 状态 |
| --- | --- | --- |
| D-001 | 只实现原题最小完整闭环，不建设通用评测平台 | Accepted |
| D-002 | MVP 自修复止于临时实验和只读推荐；confirm/apply/reparse/rollback 属于增强阶段 | Accepted |
| D-003 | 定时评测脚手架属于交付范围 | Accepted |
| D-004 | WeKnora 是权威业务数据源，Langfuse 只负责可观测性 | Accepted |
| D-005 | Gold Evidence 与临时执行文档统一映射到稳定 source identity | Accepted |
| D-006 | 测试集发布版本不可变 | Accepted |
| D-007 | 正式评测使用持久化状态和 Asynq | Accepted |
| D-008 | MVP 生成指标为 Faithfulness、Correctness、无答案拒答质量；不生成综合总分 | Accepted |
| D-009 | 旧 Evaluation API 完整迁移降为非 MVP，兼容策略待原型确认 | Accepted |
| D-010 | 实验基线读取实际生效 Chunking 配置 | Accepted |
| D-011 | `recursive` 暂不作为独立候选策略 | Accepted |
| D-012 | Langfuse 只最小补充 Score 写入 | Accepted |
| D-013 | 90/365 天是留存目标；自动清理执行器为非 MVP | Accepted |
| D-014 | tuning 选 provisional，holdout 按多指标硬门槛通过/拒绝 | Accepted |
| D-015 | 知识库级 UI 使用固定 evaluation 路由 | Accepted |
| D-016 | 实验前先做 FailureDiagnosis，只有 chunking_likely 可创建实验 | Accepted |
| D-017 | Testset 增加最小质量门禁与 answerability 治理 | Accepted |
| D-018 | Judge 只有通过人工校准门禁后才能参与 Recommendation | Accepted |
| D-019 | Run 最多 100 Case、Experiment 最多两个 candidate，并设置 tenant 并发保护 | Accepted |
| D-020 | Schedule 使用 skip_if_active，不自动因连续低分创建实验 | Accepted |
| D-021 | JudgeExecution 合并进 MetricResult，状态与 retry 语义统一 | Accepted |
| D-022 | 新 API 优先复用现有角色/KB 权限；新增 API Key 映射待确认 | Accepted |
| D-023 | 未完成稳定定位、映射和增强应用原型一律标记 Pending Verification | Accepted |
| D-024 | 空 draft 在第一次成功接纳 Generation 时原子锁定 Generation Profile | Accepted |
| D-025 | Schedule skipped/failed 时间由专用 EvalScheduleSlot 持久化 | Accepted |
| D-026 | 采用最小 evaluation_data_policy 与受限 KB 安全默认值 | Accepted |

## 3. 详细决策

### D-001 只实现原题最小完整闭环

**决定**

交付范围固定为：

```text
文档生成测试集
→ 人工审核和发布
→ 手动/定时真实 RAG 评测
→ 检索指标与 Faithfulness/Correctness/无答案拒答质量
→ Langfuse 观测和 Web 仪表盘
→ 失败诊断
→ Chunking 候选对比与推荐
```

不设计模型训练、新向量库、通用 Agent、通用评测平台、非 Chunking RAG 优化、复杂预算、告警或跨 KB 排行。

**原因**

用户明确要求“只做题目涉及的，扩展一律不做，先完成再完美”。

### D-002 自修复止于只读推荐

**决定**

MVP 的 ChunkExperiment 可以在临时知识库中测试有限候选，并产生 ChunkRecommendation。Recommendation 只能展示和复制，不自动修改源知识库配置，不触发源知识库重解析，不切换活动索引，也不执行 rollback。

confirm/apply、源知识库 reparse、切换与 rollback 不是永久非目标，而是非 MVP 增强阶段。其 API、状态、安全门禁和权限原型均为 **Pending Verification**；MVP API 不暴露这些操作。

**原因**

原题允许“推荐或自动测试”更优 Chunking；推荐已经构成最小反馈闭环。自动应用会引入索引一致性和数据破坏风险，并非完成核心题目所必需。

### D-003 定时评测纳入交付

**决定**

提供 EvalSchedule CRUD、启停、立即触发和 cron 自动触发，并使用 EvalScheduleSlot 持久化每个计划时间。复用现有 scheduler/Asynq；cron 的正式字段数和语法为 **Pending Verification**。调度不包含趋势告警和通知。

**原因**

“一套可定时触发的自动化评估脚手架”是原题预期产出。

### D-004 WeKnora 是业务事实源

**决定**

Testset、Version、Generation、Case、Evidence、Run、Item、Metric、JudgeCalibration、FailureDiagnosis、Schedule、ScheduleSlot、Experiment、Variant 和 Recommendation 全部由 WeKnora 持久化。Langfuse 只镜像 Trace/Span/Generation/Score。

Langfuse 关闭、未采样或写入失败不改变 EvalRun 的业务成功状态。

**原因**

现有 Langfuse 集成是可选、环境变量启用且支持 no-op；业务调度、权限、版本和推荐不应依赖外部观测系统可用性。

### D-005 Gold Evidence 绑定原文

**决定**

GoldEvidence 至少保存 `source_document_id`、source document hash、`offset_unit`、`source_normalization_version`、`[start_at,end_at)`、文本快照和 Evidence hash。生成时 execution document/Chunk ID 仅用于映射和辅助排查。

临时知识库显式区分 `source_document_id` 与 `execution_document_id`。Retrieved Context 必须先映射回 source identity/hash/offset，Metrics 只能按稳定 source identity 匹配 Gold Evidence；映射失败时指标 invalid，不能回退到 execution ID 或 Chunk ID。

文档哈希变化后 Case 为 stale，不做模糊迁移；无法提供稳定原文位置的样本不能发布。

**原因**

重解析会改变 Chunk ID 和边界。只绑定 Chunk ID 会使测试集随被评对象变化，无法稳定计算召回指标。

### D-006 发布版本不可变

**决定**

TestsetVersion 状态为 `draft/published/archived`。只有 draft 可编辑；published 内容不可修改，修改必须克隆新 draft。

**原因**

运行必须能长期引用相同的问题、参考答案和 Evidence 快照。

### D-007 正式任务持久化并使用 Asynq

**决定**

新 EvalRun 和 TestsetGeneration 在入队前持久化。Worker 使用条件状态更新，Item 使用唯一键防重。现有内存 Map 和裸 goroutine 不再承载正式任务。

**原因**

当前接口在进程重启后会丢失任务和结果，不能满足手动/定时生产评测。

### D-008 指标范围固定且无综合分

**决定**

MVP 规则指标包括：

- Context Precision@K。
- Hit@K。
- Evidence Recall@K。
- MRR@K。
- Gold Coverage@K。

MVP 生成指标至少包括 Answer Faithfulness、Answer Correctness 和无答案拒答质量。默认 K 为 5 和 10。Answer Relevance、Answer Completeness、Citation Correctness/Completeness 属于非 MVP 增强，不是永久放弃；任何阶段都不引入加权 Overall Score。

**原因**

三项生成指标覆盖忠实、参考答案一致性和无答案安全边界；其余指标延后以控制 MVP 校准和成本范围。

### D-009 旧 Evaluation API 完整迁移降为非 MVP

**决定**

MVP 新能力统一位于 `/api/v1/evaluation/*` 子资源，但不承诺把现有 `POST/GET /api/v1/evaluation` 完整迁移到新持久化 EvalRun。旧接口在 MVP 中保持现状，不暴露 Testset、Judge、Schedule 或 Experiment 新能力。

旧接口后续的兼容适配、弃用周期和 DTO 映射需要调用方调查与技术原型，当前为 **Pending Verification**。

**原因**

在调用方和当前内存任务语义未验证前承诺迁移，会扩大 MVP 并制造不可证实的兼容保证。

### D-010 Chunk overlap 冲突不通过新默认值解决

**事实**

代码和配置中存在 overlap 50 与 80 的默认/回退差异。

**决定**

EvalRun 和 Experiment 不选择一个新的硬编码默认值。它们从目标文档/知识库解析出本次实际生效 Chunking 配置，规范化后保存配置及哈希。所有候选围绕该 baseline 生成。

可复现 snapshot 同时冻结 source identity/normalization/offset、split algorithm/seed、真实 RAG answer Prompt version/hash、上下文构造版本、`history_policy=no_history`、模型关键参数、Judge calibration、检索参数和代码版本；影响比较的固定变量不同则不能进入同一实验。

**原因**

评测必须描述真实被评系统，不能因评测模块的默认值改变 baseline。

### D-011 `recursive` 不作为独立候选

**事实**

当前 `recursive` 与 legacy 行为没有独立实现差异。

**决定**

候选策略只从当前真正不同的 `auto/heading/heuristic/legacy` 中选择。`recursive` 不单独出现。

**原因**

将等价实现当作不同候选会浪费解析/Embedding 成本并产生伪比较。

### D-012 Langfuse 只补充 Score

**事实**

当前 `internal/tracing/langfuse` 支持 Trace、Span、Generation，不支持 Score 写入，也没有 Dataset/Experiment 业务能力。

**决定**

规格只要求在现有轻量 ingestion client 上增加 Score 事件，以内部 MetricResult 为输入。Dataset/Experiment 不引入。

**原因**

Score 是把每题指标与 Trace 关联的必要最小能力；其他 Langfuse 资源不是原题闭环的业务前提。

### D-013 分层留存

**决定**

- 原始召回上下文、生成答案和 Judge 原始输入输出以 90 天为目标留存期。
- 聚合指标、版本哈希、状态、错误摘要、成本和审计元数据以 365 天为目标留存期。
- 数据模型保留到期时间与 `raw_payload_expired` 表达能力。
- 完整自动清理执行器、调度和合规参数确认属于非 MVP，并标记 **Pending Verification**。

**原因**

保留数据治理方向，同时避免把尚未确认的清理基础设施扩大为 MVP 交付。

### D-014 推荐使用硬门槛

**决定**

所有 candidate 先在 tuning 上比较并只选择一个 provisional candidate；holdout 只比较 baseline 与该 provisional candidate并作 pass/reject，不得在 holdout 重新选择。Recommendation 需要：

- 至少 30 个共同有效 holdout 样本。
- Context Precision 至少提升 0.03。
- Evidence Recall 与 Gold Coverage 不退化。
- 通过校准门禁的 Faithfulness 下降不超过 0.02；适用的 Correctness 与无答案拒答质量也不得超过各自允许退化门槛。
- 失败率不高于 baseline，且无 Run 级失败。

tuning 的确定性排序可以使用 Context Precision、Faithfulness、失败率和 P95 延迟，但 Context Precision 不能单独决定最终推荐。比较只使用每对 baseline/candidate 的共同 valid Item，并保存 eligible、双方 valid/invalid/abstain/failed、common valid 和排除原因。MVP `allowed_drop=0`，candidate valid coverage 不得低于 baseline。Chunk Redundancy、chunk count、index build time、P95 和可获得的 embedding/index 成本只展示并用于 tuning tie-break，不参与 holdout pass/reject。任一最终门槛失败时输出 `no_better_strategy`，不能改选 tuning 第二名。

**原因**

推荐必须由可解释实验结果决定，不能由 LLM 主观总结或不透明综合分决定。

### D-015 知识库级 UI 入口

**决定**

前端固定使用：

```text
/platform/knowledge-bases/:kbId/evaluation
```

页面只包含 Overview、Testsets、Runs、Run Detail、Chunk Experiments、Schedules/Slots 六个区域。

**原因**

Testset、Run 和 Chunking baseline 都以知识库为权限和配置边界；无需新增全局评测平台。

### D-016 先诊断再实验

**决定**

评测失败由状态为 `pending/running/completed/failed` 的版本化 FailureDiagnosis 输出 `chunking_likely/non_chunking_likely/unknown`，保存 `target_k`、eligible/valid 数、规定 reason codes 和 evidence signals。blocked Run 不得得到 chunking_likely；只有 snapshot 仍有效的 `completed + chunking_likely` 允许手动创建绑定单一 source_run_id 的 Experiment。reason code 限制候选维度，禁止通用网格。连续低分自动创建实验草稿属于非 MVP 增强阶段。

### D-017 Testset 最小质量治理

**决定**

发布前必须通过 Schema、重复、答案泄漏、歧义和 claim-evidence support 检查。Case 显式保存 `answerability=answerable/unanswerable`；无答案题保存检查范围、稳定原因和 expected refusal。Version 保存 split algorithm/seed，发布后不可变。

### D-018 Judge 校准是推荐硬门禁

**决定**

每种 Judge metric 绑定人工校准集和 calibration version。MVP 的 Calibration 绑定一个 knowledge base 作为授权范围，读写复用该 KB 的 read/write access，不引入新角色常量。至少 60 个双人标注样本，并验证 Parser 失败率、Macro-F1、Exact Agreement、weighted Kappa 和 20 条样本三次重复稳定性；MVP 为 Pointwise Judge，不保留 pairwise/swap 门槛；阈值以 `METRICS.md` 为唯一公式来源。未通过的 Judge 结果可显示但不参与 provisional 选择、holdout 或 Recommendation。所有差值只在 baseline/candidate 共同 valid Item 上计算。

### D-019 最小成本与并发保护

**决定**

单 Run 最多 100 Case；MVP 每 Experiment 最多 2 个 candidate；每 tenant 同时最多 2 个普通 EvalRun 和 1 个 active Experiment。实验启动前展示预计文档解析、chunk/Embedding、RAG 与 Judge 调用规模。500 Case、4 个 candidate 与复杂预算系统不进入 MVP。

### D-020 Schedule 不重叠

**决定**

Schedule 的 `sample_limit<=100`，保存完整明确版本的 Run template、确定性采样 algorithm/seed 和归档字段，并固定使用 `skip_if_active`。Scheduler 先原子创建唯一 EvalScheduleSlot；active Run 命中时 Slot=`skipped_active/SCHEDULE_SKIPPED_ACTIVE` 且不创建 Run，未命中时 Slot 关联新 Run，Run 创建前失败为 `failed_before_run`。趋势告警与低分自动实验不进入 MVP。

### D-021 状态、Judge 存储与重试语义统一

**决定**

JudgeExecution 不建立独立业务实体，其输入摘要、输出、Parser 状态、calibration 和尝试次数并入 MetricResult；execution failed 与 invalid/abstain/合法 0 分分离。TestsetGeneration 提供 cancel API；Schedule POST 固定 active、DELETE 归档，enabled 只控制 active Schedule。Run retry scope 为 failed/unfinished/all：partial 支持三种，canceled 支持 unfinished/all，failed 支持 failed/all，blocked 只支持 all；均创建保存精确 Case 集的新 Run，旧 Run 不变。资源 readiness 失败在可持久化接纳时返回 202+blocked；接纳基础设施不可用时才返回 503 且不创建 Run。

### D-022 权限与错误契约只承诺已证实部分

**已验证事实**

`CODEBASE_MAP.md` 只确认 tenant context、通用 RBAC/permission middleware、knowledge base access checks 和部分审计基础。它没有充分确认新端点的 Owner/Admin/Contributor/Viewer 常量装配、`FullAccess`、`run_evaluations`、API Key capability、整数错误码或成功包络。

**决定**

MVP 只复用当前实际 tenant 隔离与 KB read/write access，不新增细粒度权限体系。新增 API Key 写端点在映射确认前默认拒绝。HTTP status 与稳定字符串 reason code 是当前规格契约；成功/错误包络字段和公共码类型在源码原型前均为 **Pending Verification**，不得写成已确认事实。
### D-023 技术原型待确认的统一标记

**决定**

以下内容在完成原型前统一标记 **Pending Verification**：非文本/复杂文档的稳定 source offset；临时解析器的 source identity mapping 完整性；新 API Key 逐端点能力映射；成功/错误包络与公共码类型；cron 正式语法；幂等记录物理存储与 TTL；人工 Calibration 资产流程；KB 敏感级别到 evaluation_data_policy 的映射；自动留存清理；旧 API 迁移；confirm/apply/reparse/rollback 增强安全流程。Pending Verification 项不得成为 MVP 隐式依赖或被描述为已确认事实。

### D-024 Draft Generation Profile Lock

**决定**

TestsetVersion 新建空 draft 时允许文档和生成配置字段为空。第一次合法 TestsetGeneration 被 API 成功接纳并持久化时，在同一事务内锁定文档集合/hash、normalization/offset、split algorithm/seed、generator model/Prompt 和 evaluation data policy；后续请求必须完全一致，否则 409/`GENERATION_PROFILE_CONFLICT`。Generation 失败或取消不解除锁定；不同范围/配置创建新 draft。Case/Generation 是具体 provenance 事实源，Version 字段只是摘要。

### D-025 EvalScheduleSlot

**决定**

新增唯一最小实体 EvalScheduleSlot，唯一约束 `(schedule_id, scheduled_at_utc)`。Scheduler 先以 `outcome=null` 原子占 Slot，再检查 active Run并最终写入 `run_created/skipped_active/failed_before_run`；null 只是内部 claim，不是第四种 outcome。中断 claim 恢复为 `failed_before_run/SCHEDULE_SLOT_INTERRUPTED`，不得补建 Run；skipped Slot 永久防重。Schedule `last_status` 不能替代 Slot 历史。

### D-026 evaluation_data_policy

**决定**

不建设通用 DLP 平台；在 TestsetVersion/Schedule/Run/Experiment 冻结最小数据策略。受限 KB 的默认值是禁止外部 Judge；Langfuse 的 Question/Answer 仅发送脱敏值或 hash，Context 仅发送 correlation/source ID、hash、rank、长度和脱敏摘要；无合规 Judge 时结果为 invalid/blocked，不伪造。Testset 归档仍按引用/留存保留 Evidence，源文档删除或 hash 变化使其 stale 并禁止新 Run。MetricResult 不保存无界长推理。

真实敏感级别映射仍为 **Pending Verification**。

## 4. 已知事实与固定处理

以下事实在当前基线中未完全具备，但处理方式已确定，不留给实现者临时决定：

| 事实 | 固定处理 |
| --- | --- |
| 基线运行缺少 Embedding/Chat 模型，文档停留 processing | Run readiness gate 创建 blocked 终态并持久化原因 |
| 基线 Langfuse 服务未启动 | 核心 Run 正常完成，observability status 标识失败/禁用 |
| 前端没有评测路由和图表依赖 | 新增知识库级路由；用现有 TDesign/CSS/SVG 表达核心数据 |
| 不同文档解析器的稳定位置能力可能不同 | 未经原型确认的文档类型标记 **Pending Verification**；不能稳定定位的 Case 不允许发布 |
| 各向量后端没有统一蓝绿索引能力 | 实验使用临时 KB/索引，Recommendation 只读，不触碰源索引 |
| Langfuse Trace URL 缺少统一项目配置 | 始终返回 Trace ID；只有配置充分时返回可空 Trace URL |
| 前端存在 rebuild-index 路由契约不一致 | 本范围不复用或修复该路由，因为没有推荐应用/重解析功能 |
| 新端点精确角色/API Key/错误码/包络没有事实证据 | 只承诺 tenant+KB read/write；API Key 写默认拒绝；包络与公共码类型标记 **Pending Verification** |
| 临时 KB execution document 到 source offset 的完整映射未有原型 | 将 mapping 作为实验硬前置；失败指标 invalid，标记 **Pending Verification** |
| KB 敏感级别到评测数据策略的映射未确认 | 冻结 evaluation_data_policy；受限 KB 使用禁止外部 Judge/Context metadata-only 的安全默认值 |
| scheduler 的正式 cron 字段数/语法未由事实资料确认 | 复用现有 parser 并标记 **Pending Verification**，不声明六字段为已确认事实 |

## 5. 永久范围外与增强阶段边界

以下内容是与原题无关的永久范围外：

- Langfuse Dataset/Experiment 同步。
- 趋势告警、通知、跨 KB 排行。
- 新向量数据库或统一索引抽象改造。
- 通用评测任务 DSL、通用 Agent 和模型训练。

以下内容不是永久放弃，但不属于 MVP：

- Recommendation confirm/apply、线上 ChunkingConfig 修改、源知识库 reparse、索引切换与 rollback。
- Answer Relevance、Answer Completeness、Citation Correctness/Completeness。
- 连续低分自动创建 Experiment draft。
- 旧 Evaluation API 完整迁移/弃用适配。
- 90/365 天完整自动清理执行器。

上述增强项都必须在单独原型与权限/回滚设计确认后再进入规格，当前统一为 **Pending Verification**。

## 6. 修订后自检

### 6.1 23 项跨文档一致性矩阵

本矩阵为本轮重新逐项生成，恰好 23 行；`Pending Verification` 表示规格已给出安全边界，但事实或技术原型仍未完成，不等同于 Conflict/Missing。

| # | 检查项 | 状态 | 结论/证据落点 |
| ---: | --- | --- | --- |
| 1 | 原题最小闭环和永久范围外 | Pass | 七份文档只保留 Testset→Run→Diagnosis→Experiment→只读 Recommendation；通用平台/模型训练等永久排除 |
| 2 | MVP 只读 Recommendation | Pass | confirm/apply/reparse/rollback 统一为增强阶段，MVP 无相关 API |
| 3 | Testset 生成、人工审核、不可变发布 | Pass | PRD FR-TS；Data Model Version/Case；API Testset；AC-TS |
| 4 | Testset 质量门禁 | Pass | Schema/重复/泄漏/歧义/claim-evidence support 跨文档一致 |
| 5 | answerability 和拒答 | Pass | answerable/unanswerable 条件字段、Gold 条件与拒答指标一致 |
| 6 | Gold Evidence identity/hash/offset | Pass | Gold 绑定 source identity/hash/[start,end)，不绑定临时 Chunk ID |
| 7 | offset unit 和 normalization | Pending Verification | 契约一致；非文本/复杂文档的稳定定位原型未完成，未通过类型不得发布 |
| 8 | source/execution mapping | Pending Verification | 契约一致；临时 KB 全路径映射原型未完成，失败时 invalid/禁止实验 |
| 9 | EvalRun/Item 持久化和恢复 | Pass | WeKnora DB+Asynq、Item 防重、重启恢复与旧 Run 不复活一致 |
| 10 | 可复现快照 | Pass | Run/Schedule/Experiment 均冻结模型参数、answer Prompt、context builder、no_history、split、policy 和代码版本 |
| 11 | 所有状态入口和终态 | Pass | Acceptance §12 覆盖 15 类实体/决策；Experiment draft 可取消、Variant 随父取消、Slot claim 可终结恢复 |
| 12 | blocked 202 / 503 | Pass | 资源已持久化才 202+blocked；无法接纳不创建资源才 503；外层包络仍为 Pending Verification |
| 13 | 五项检索指标和 Redundancy | Pass | 公式、source mapping、unanswerable abstain 和手算一致 |
| 14 | Faithfulness/Correctness/拒答质量 | Pass | 三项独立 Pointwise Judge 指标；不适用时 abstain，不以 0 替代 |
| 15 | Judge Calibration | Pending Verification | Pointwise 门槛、KB 授权范围、状态/API 已闭合；实际人工标注资产与发布流程未原型验证 |
| 16 | invalid/abstain/failed/0 分 | Pass | MetricResult execution_status 与 result status 分离，三类不进入分母 |
| 17 | FailureDiagnosis | Pass | 状态、label、规定 reason、target K、计数、retry 和候选映射一致 |
| 18 | baseline 和两个 candidate | Pass | baseline=实际配置；1–2 candidate；recursive 排除；单一 source Run |
| 19 | tuning/holdout | Pass | tuning 唯一 provisional；holdout 只 pass/reject，不重选 |
| 20 | Recommendation | Pass | 多指标、双方 failed/invalid/abstain、valid coverage、no_better_strategy；资源采用方案 B |
| 21 | 成本并发和 ScheduleSlot | Pending Verification | 上限、Slot claim/最终 outcome/中断恢复与 estimate 契约一致；跨进程并发计数和 cron 正式语法待原型 |
| 22 | Langfuse best effort | Pending Verification | DB 事实源与敏感策略一致；Score 写入及运行环境尚未验证 |
| 23 | 权限、留存和 Pending Verification | Pending Verification | tenant+KB access（含 Calibration）与敏感 Q/C/A 安全默认值已定；精确角色/API Key/包络/公共码/敏感级别映射待验证 |

结果：Pass 17；Conflict 0；Missing 0；Pending Verification 6。不存在已知跨文档冲突。

### 6.2 57 条 FR 追踪统计

逐条表见 `ACCEPTANCE_CRITERIA.md` §13：

- FR 总数：57。
- 完整 Data Model/API/Acceptance 覆盖：57。
- Conflict：0。
- Missing：0。
- 含显式 Pending Verification：6（FR-TS-04、FR-TS-05、FR-RUN-09、FR-MET-09、FR-SCH-01、FR-EXP-05）。

### 6.3 状态机审查

Acceptance §12 已逐项覆盖 Testset、TestsetVersion、EvalCase review/quality、TestsetGeneration、EvalRun、EvalRunItem、MetricResult、JudgeCalibration、FailureDiagnosis、EvalSchedule、EvalScheduleSlot、ChunkExperiment、Variant、holdout decision、Recommendation decision，共 15 类。每类均有创建/API 入口、合法迁移、成功/失败出口、适用的取消出口、终态和 retry 语义；Experiment draft 可取消，Variant 由父 Experiment 取消，Slot claim 中断可恢复为 failed_before_run。JudgeCalibration failed 可幂等 rerun；其他业务终态不原地复活。

### 6.4 指标手工复算

- 既有示例 `rel=[1,0,1]`、两条 Gold：Hit@3=`1`；Evidence Recall@3=`2/2=1`；MRR@3=`1/1=1`；Context Precision@3=`(1+2/3)/2=0.8333`；Gold Coverage@3=`(30+20)/(40+40)=0.625`。
- Redundancy 示例：同一 source 文档 `[90,130)` 与 `[110,150)`，总长度 80，并集长度 60，Chunk Redundancy@2=`1-60/80=0.25`。
- Faithfulness：2 supported、1 unsupported，`2/(2+1)=0.6667`。
- Correctness：3 个 key point 中 2 matched、1 missing，`2/(2+1)=0.6667`。
- unanswerable refusal：correct_refusal=`valid/1`；hallucinated_answer=`valid/0`；unclear=`abstain/null`；调用失败=`failed/null`。
- source mapping 缺失=`invalid/null`；unanswerable 五项 Gold-dependent metrics=`abstain/null/NOT_APPLICABLE_UNANSWERABLE`；只有合法 Gold/mapping 下明确未命中才是 `valid/0`。
- baseline/candidate 只在共同 valid Item 上计算 delta，并要求 candidate valid coverage 不低于 baseline，因此不能靠制造更多 invalid 提高结果。
## 7. 修改摘要

- 修复空 draft 与必填快照冲突：增加 Draft Generation Profile Lock、409 冲突和发布完整性校验。
- 修复 unanswerable 与 Gold 指标冲突：Gold 必须为空，五项检索指标 abstain/null；answerable 缺 Gold 阻止发布或 invalid。
- 新增最小 EvalScheduleSlot，持久化 run_created/skipped_active/failed_before_run 并永久防重。
- 补齐 EvalRunItem answerable/unanswerable 条件快照，确保脱离 Case 表仍可复算。
- 删除未经事实材料证实的精确角色、API Key、整数错误码、成功包络和六字段 cron 断言，统一标记 Pending Verification。
- 统一 202+blocked 与 503 无资源边界。
- 完整定义版本化 FailureDiagnosis、规定 reason、target K、状态/retry 与 reason→candidate 限制。
- 补齐 Schedule 完整 Run 模板和固定 seed 的确定性采样。
- Judge 固定为 Pointwise，删除 pairwise/swap 门槛，闭合 Calibration 幂等与状态。
- 增加共同 valid/coverage 防选择性缺失；资源采用方案 B，仅展示/tuning tie-break。
- 统一 partial/canceled 推导和 failed/unfinished/all retry 精确 Case 集。
- 闭合 Version、Calibration、Diagnosis、Schedule、Slot 等状态/API。
- 增加最小 evaluation_data_policy、受限 KB 安全默认值、Evidence/stale/留存约束。
- 删除残留的 Pointwise 顺序交换门槛，并统一 Parser invalid 与调用 failed 语义。
- 补齐 ScheduleSlot 的短暂 claim/outcome 终结与中断恢复规则。
- 统一 Recommendation 的双方 failed 计数、Calibration 的 KB 授权范围和六区域 UI 定义。
- 重新生成 57 条 FR 逐项追踪、23 项一致性矩阵、15 类状态审查和指标手算。

## 8. 仍待确认问题

### 8.1 MVP blocker

1. 至少一种 MVP 文本文档的稳定 `offset_unit + source_normalization_version + source offset` 原型必须通过；未通过前无法发布真实 Gold Evidence。
2. 临时 KB 的 `source_document_id ↔ execution_document_id ↔ source offset` 映射必须通过原型；未通过前不得执行 Recommendation 实验。
3. 三种 Pointwise Judge 的人工校准资产、双人标注、版本发布和门槛结果必须真实完成；未 passed 时不得参与 Recommendation。
4. 受限 KB 的真实敏感级别来源及合规私有 Judge 可用性必须在上线前确认；在此之前使用安全默认值。

### 8.2 MVP 非阻塞 Pending Verification

1. 新端点的精确角色常量、API Key capability 映射；确认前写端点默认拒绝 API Key。
2. 仓库实际成功/错误包络字段、公共错误码类型和具体值；HTTP status 与字符串 reason 已冻结。
3. scheduler 正式 cron 字段数/语法；实现以当前 parser 原型为准。
4. Idempotency-Key 的物理存储、TTL 和清理机制。
5. tenant 并发上限的跨进程原子计数实现。
6. 各模型/向量后端可提供的 embedding/index 成本；不可获得时返回 null+reason。
7. Langfuse Score 轻量写入和实际 Trace URL 构造；失败不得影响业务结果。
8. 90/365 天最终合规参数；完整自动清理执行器不属于 MVP。
9. 旧 Evaluation API 调用方和迁移/弃用方式；完整迁移不属于 MVP。

### 8.3 增强阶段

- Recommendation confirm/reject、ChunkConfigVersion、canary/batch apply、源 KB reparse、index switch、rollback。
- 连续低分自动创建 Experiment draft。
- Answer Relevance、Answer Completeness、Citation Correctness/Completeness。
- 完整自动留存清理执行器。

## 9. 下一轮独立规格审计条件

七份规格已完成本轮 P0/P1 定向修订，57 条 FR 无 Conflict/Missing，23 项矩阵无已知冲突，15 类状态机均有合法入口和出口；所有未完成原型均明确标记 Pending Verification，并有安全失败方式。**已具备进入下一轮独立规格审计的条件。**