package rageval

import (
	"reflect"
	"testing"
)

func TestDeriveRunStatus(t *testing.T) {
	tests := []struct {
		name  string
		items []RunItemState
		want  RunStatus
	}{
		{"pending", []RunItemState{{Status: RunItemPending}}, RunPending},
		{"running", []RunItemState{{Status: RunItemCompleted}, {Status: RunItemRunning}}, RunRunning},
		{"completed", []RunItemState{{Status: RunItemCompleted}}, RunCompleted},
		{"partial", []RunItemState{{Status: RunItemCompleted}, {Status: RunItemFailed}}, RunPartial},
		{"blocked", []RunItemState{{Status: RunItemBlocked}, {Status: RunItemBlocked}}, RunBlocked},
		{"canceled", []RunItemState{{Status: RunItemCanceled}}, RunCanceled},
		{"failed wins mixed unsuccessful", []RunItemState{{Status: RunItemCanceled}, {Status: RunItemFailed}}, RunFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DeriveRunStatus(tt.items)
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestTerminalRunCannotBeResurrected(t *testing.T) {
	for _, status := range []RunStatus{RunCompleted, RunPartial, RunFailed, RunCanceled, RunBlocked} {
		if CanTransitionRun(status, RunRunning) {
			t.Fatalf("terminal status %q can transition to running", status)
		}
	}
}

func TestRetryScopesSelectCasesForNewRun(t *testing.T) {
	items := []RunItemState{
		{CaseID: "done", Status: RunItemCompleted},
		{CaseID: "fail", Status: RunItemFailed},
		{CaseID: "block", Status: RunItemBlocked},
		{CaseID: "cancel", Status: RunItemCanceled},
	}
	failed, _ := RetryCaseIDs(items, RetryFailed)
	if !reflect.DeepEqual(failed, []string{"block", "fail"}) {
		t.Fatalf("failed scope = %#v", failed)
	}
	unfinished, _ := RetryCaseIDs(items, RetryUnfinished)
	if !reflect.DeepEqual(unfinished, []string{"block", "cancel", "fail"}) {
		t.Fatalf("unfinished scope = %#v", unfinished)
	}
}
