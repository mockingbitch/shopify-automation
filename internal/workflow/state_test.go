package workflow

import "testing"

// TestStepTransitionsExhaustive checks every (state, event) pair: the
// expected table below is written independently of stepTable.
func TestStepTransitionsExhaustive(t *testing.T) {
	want := map[string]map[StepEvent]string{
		StepPending:         {EvClaim: StepRunning, EvSkip: StepSkipped, EvCancel: StepCancelled},
		StepRunning:         {EvSucceed: StepSucceeded, EvRetryableFail: StepPending, EvPermanentFail: StepFailed, EvNeedUser: StepWaitingUser, EvAwaitApproval: StepWaitingApproval, EvLeaseExpired: StepPending, EvSkip: StepSkipped, EvCancel: StepCancelled},
		StepWaitingApproval: {EvApprove: StepPending, EvReject: StepCancelled, EvCancel: StepCancelled},
		StepWaitingUser:     {EvResume: StepPending, EvCompleteManual: StepSucceeded, EvCancel: StepCancelled},
		StepFailed:          {EvUserRetry: StepPending},
		StepSucceeded:       {},
		StepSkipped:         {},
		StepCancelled:       {},
	}
	n := 0
	for _, st := range AllStepStates {
		for _, ev := range AllStepEvents {
			n++
			got, err := Transition(st, ev)
			exp, legal := want[st][ev]
			switch {
			case legal && (err != nil || got != exp):
				t.Errorf("%s --%s--> got %q %v, want %s", st, ev, got, err, exp)
			case !legal && err == nil:
				t.Errorf("%s --%s--> should be illegal, got %s", st, ev, got)
			}
		}
	}
	if n != len(AllStepStates)*len(AllStepEvents) || len(want) != len(AllStepStates) {
		t.Fatal("state or event list changed; update the test")
	}
}

func TestDeriveRunStatus(t *testing.T) {
	s := func(status string, blocking, started bool) StepView { return StepView{status, blocking, started} }
	cases := []struct {
		name   string
		steps  []StepView
		cancel bool
		want   string
	}{
		{"fresh", []StepView{s(StepPending, true, false), s(StepPending, true, false)}, false, RunPending},
		{"running", []StepView{s(StepSucceeded, true, true), s(StepRunning, true, true)}, false, RunRunning},
		{"between steps", []StepView{s(StepSucceeded, true, true), s(StepPending, true, false)}, false, RunRunning},
		{"retry backoff", []StepView{s(StepPending, true, true)}, false, RunRunning},
		{"approval", []StepView{s(StepSucceeded, true, true), s(StepWaitingApproval, true, true), s(StepPending, true, false)}, false, RunWaitingApproval},
		{"user", []StepView{s(StepWaitingUser, true, true), s(StepPending, true, false)}, false, RunWaitingUser},
		{"blocking failure", []StepView{s(StepFailed, true, true), s(StepPending, true, false)}, false, RunFailed},
		{"soft failure", []StepView{s(StepSucceeded, true, true), s(StepFailed, false, true), s(StepSucceeded, true, true)}, false, RunPartiallyCompleted},
		{"soft failure, still going", []StepView{s(StepFailed, false, true), s(StepPending, true, false)}, false, RunRunning},
		{"done", []StepView{s(StepSucceeded, true, true), s(StepSkipped, true, false)}, false, RunCompleted},
		{"cancel while running", []StepView{s(StepRunning, true, true), s(StepCancelled, true, false)}, true, RunRunning},
		{"cancelled", []StepView{s(StepSucceeded, true, true), s(StepCancelled, true, false)}, true, RunCancelled},
	}
	for _, c := range cases {
		if got := DeriveRunStatus(c.steps, c.cancel); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestRunMoves(t *testing.T) {
	if CanMoveRun(RunCompleted, RunRunning) || CanMoveRun(RunCancelled, RunRunning) || CanMoveRun(RunPartiallyCompleted, RunFailed) {
		t.Fatal("terminal runs must not reopen")
	}
	if !CanMoveRun(RunFailed, RunRunning) || !CanMoveRun(RunWaitingApproval, RunCancelled) || !CanMoveRun(RunRunning, RunRunning) {
		t.Fatal("legal moves rejected")
	}
}
