package rageval

import (
	"fmt"
	"sort"
)

type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunPartial   RunStatus = "partial"
	RunFailed    RunStatus = "failed"
	RunCanceled  RunStatus = "canceled"
	RunBlocked   RunStatus = "blocked"
)

type RunItemStatus string

const (
	RunItemPending   RunItemStatus = "pending"
	RunItemRunning   RunItemStatus = "running"
	RunItemCompleted RunItemStatus = "completed"
	RunItemFailed    RunItemStatus = "failed"
	RunItemCanceled  RunItemStatus = "canceled"
	RunItemBlocked   RunItemStatus = "blocked"
)

type RunItemState struct {
	CaseID string
	Status RunItemStatus
}

func (s RunStatus) Terminal() bool {
	switch s {
	case RunCompleted, RunPartial, RunFailed, RunCanceled, RunBlocked:
		return true
	default:
		return false
	}
}

func CanTransitionRun(from, to RunStatus) bool {
	if from == to {
		return true
	}
	if from.Terminal() {
		return false
	}
	switch from {
	case RunPending:
		return to == RunRunning || to == RunFailed || to == RunCanceled || to == RunBlocked
	case RunRunning:
		return to == RunCompleted || to == RunPartial || to == RunFailed || to == RunCanceled || to == RunBlocked
	default:
		return false
	}
}

// DeriveRunStatus deterministically folds item state after a worker pass. A
// mixture of successful and unsuccessful terminal items is partial; execution
// errors are never converted to metric zeroes.
func DeriveRunStatus(items []RunItemState) (RunStatus, error) {
	if len(items) == 0 {
		return RunPending, nil
	}
	counts := make(map[RunItemStatus]int)
	for _, item := range items {
		switch item.Status {
		case RunItemPending, RunItemRunning, RunItemCompleted, RunItemFailed, RunItemCanceled, RunItemBlocked:
			counts[item.Status]++
		default:
			return "", fmt.Errorf("unknown run item status %q", item.Status)
		}
	}
	if counts[RunItemRunning] > 0 || (counts[RunItemPending] > 0 && len(counts) > 1) {
		return RunRunning, nil
	}
	if counts[RunItemPending] == len(items) {
		return RunPending, nil
	}
	if counts[RunItemCompleted] == len(items) {
		return RunCompleted, nil
	}
	if counts[RunItemCompleted] > 0 {
		return RunPartial, nil
	}
	if counts[RunItemBlocked] > 0 && counts[RunItemFailed] == 0 && counts[RunItemCanceled] == 0 {
		return RunBlocked, nil
	}
	if counts[RunItemCanceled] > 0 && counts[RunItemFailed] == 0 {
		return RunCanceled, nil
	}
	return RunFailed, nil
}

type RetryScope string

const (
	RetryFailed     RetryScope = "failed"
	RetryUnfinished RetryScope = "unfinished"
	RetryAll        RetryScope = "all"
)

// RetryCaseIDs defines the cases copied to a new run. It never mutates or
// revives the source run.
func RetryCaseIDs(items []RunItemState, scope RetryScope) ([]string, error) {
	selected := make([]string, 0, len(items))
	for _, item := range items {
		include := false
		switch scope {
		case RetryFailed:
			include = item.Status == RunItemFailed || item.Status == RunItemBlocked
		case RetryUnfinished:
			include = item.Status != RunItemCompleted
		case RetryAll:
			include = true
		default:
			return nil, fmt.Errorf("unknown retry scope %q", scope)
		}
		if include {
			selected = append(selected, item.CaseID)
		}
	}
	sort.Strings(selected)
	return selected, nil
}
