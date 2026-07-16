package types

const (
	TypeRAGEvaluationGenerateTestset = "rag_evaluation:generate_testset"
	TypeRAGEvaluationRun             = "rag_evaluation:run"
	TypeRAGEvaluationExperiment      = "rag_evaluation:experiment"
	TypeRAGEvaluationCleanup         = "rag_evaluation:cleanup"
)

type RAGEvaluationGenerationPayload struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	GenerationID    string `json:"generation_id"`
}

type RAGEvaluationRunPayload struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	RunID           string `json:"run_id"`
}

type RAGEvaluationExperimentPayload struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	ExperimentID    string `json:"experiment_id"`
}

type RAGEvaluationCleanupPayload struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	ExperimentID    string `json:"experiment_id"`
}
