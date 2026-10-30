// Package workflow is a durable step engine. PostgreSQL is the source of
// truth; the queue only says "work on step X". It knows nothing about
// Shopee or templates: concrete workflows register step handlers.
package workflow

import "fmt"

// Step statuses.
const (
	StepPending         = "PENDING"
	StepRunning         = "RUNNING"
	StepSucceeded       = "SUCCEEDED"
	StepFailed          = "FAILED"
	StepSkipped         = "SKIPPED"
	StepCancelled       = "CANCELLED"
	StepWaitingApproval = "WAITING_APPROVAL"
	StepWaitingUser     = "WAITING_USER"
)

// Run statuses.
const (
	RunPending            = "PENDING"
	RunRunning            = "RUNNING"
	RunWaitingApproval    = "WAITING_APPROVAL"
	RunWaitingUser        = "WAITING_USER"
	RunCompleted          = "COMPLETED"
	RunPartiallyCompleted = "PARTIALLY_COMPLETED"
	RunFailed             = "FAILED"
	RunCancelled          = "CANCELLED"
)

// StepEvent is something that happens to a step.
type StepEvent string

// Step events.
const (
	EvClaim          StepEvent = "claim"
	EvSucceed        StepEvent = "succeed"
	EvRetryableFail  StepEvent = "retryable_fail"
	EvPermanentFail  StepEvent = "permanent_fail"
	EvNeedUser       StepEvent = "need_user"
	EvAwaitApproval  StepEvent = "await_approval"
	EvLeaseExpired   StepEvent = "lease_expired"
	EvApprove        StepEvent = "approve"
	EvReject         StepEvent = "reject"
	EvResume         StepEvent = "resume"
	EvCompleteManual StepEvent = "complete_manual"
	EvSkip           StepEvent = "skip"
	EvUserRetry      StepEvent = "user_retry"
	EvCancel         StepEvent = "cancel"
)

// AllStepStates and AllStepEvents are used by the exhaustive table test.
var (
	AllStepStates = []string{StepPending, StepRunning, StepSucceeded, StepFailed, StepSkipped, StepCancelled,
		StepWaitingApproval, StepWaitingUser}
	AllStepEvents = []StepEvent{EvClaim, EvSucceed, EvRetryableFail, EvPermanentFail, EvNeedUser, EvAwaitApproval,
		EvLeaseExpired, EvApprove, EvReject, EvResume, EvCompleteManual, EvSkip, EvUserRetry, EvCancel}
)

type stepKey struct {
	from string
	ev   StepEvent
}

// stepTable is the complete list of legal step transitions
// (workflow-design.md §3.2). Two refinements: an approval gate is reached
// while the step runs (RUNNING → WAITING_APPROVAL), and a running handler
// may decide to skip (RUNNING → SKIPPED).
var stepTable = map[stepKey]string{
	{StepPending, EvClaim}:              StepRunning,
	{StepPending, EvSkip}:               StepSkipped,
	{StepPending, EvCancel}:             StepCancelled,
	{StepRunning, EvSucceed}:            StepSucceeded,
	{StepRunning, EvRetryableFail}:      StepPending,
	{StepRunning, EvPermanentFail}:      StepFailed,
	{StepRunning, EvNeedUser}:           StepWaitingUser,
	{StepRunning, EvAwaitApproval}:      StepWaitingApproval,
	{StepRunning, EvLeaseExpired}:       StepPending,
	{StepRunning, EvSkip}:               StepSkipped,
	{StepRunning, EvCancel}:             StepCancelled,
	{StepWaitingApproval, EvApprove}:    StepPending,
	{StepWaitingApproval, EvReject}:     StepCancelled,
	{StepWaitingApproval, EvCancel}:     StepCancelled,
	{StepWaitingUser, EvResume}:         StepPending,
	{StepWaitingUser, EvCompleteManual}: StepSucceeded,
	{StepWaitingUser, EvCancel}:         StepCancelled,
	{StepFailed, EvUserRetry}:           StepPending,
}

// Transition returns the next step status, or an error for an illegal move.
func Transition(from string, ev StepEvent) (string, error) {
	if to, ok := stepTable[stepKey{from, ev}]; ok {
		return to, nil
	}
	return "", fmt.Errorf("workflow: illegal step transition %s --%s-->", from, ev)
}

// IsTerminalStep reports whether a step will not change without a user retry.
func IsTerminalStep(s string) bool {
	return s == StepSucceeded || s == StepSkipped || s == StepCancelled || s == StepFailed
}

// IsTerminalRun reports whether a run is finished (retry may reopen
// FAILED and PARTIALLY_COMPLETED).
func IsTerminalRun(s string) bool {
	return s == RunCompleted || s == RunPartiallyCompleted || s == RunFailed || s == RunCancelled
}

// StepView is the part of a step the run status depends on.
type StepView struct {
	Status   string
	Blocking bool // false: ContinueOnFailure
	Started  bool // attempted at least once
}

// DeriveRunStatus computes a run's status from its steps. The run status is
// never set directly; it always follows the steps.
func DeriveRunStatus(steps []StepView, cancelRequested bool) string {
	var running, waitApproval, waitUser, blockingFailed, softFailed, started, open bool
	for _, s := range steps {
		started = started || s.Started
		switch s.Status {
		case StepRunning:
			running = true
		case StepWaitingApproval:
			waitApproval = true
		case StepWaitingUser:
			waitUser = true
		case StepFailed:
			if s.Blocking {
				blockingFailed = true
			} else {
				softFailed = true
			}
		case StepPending:
			open = true
		}
	}
	switch {
	case running:
		return RunRunning
	case cancelRequested:
		return RunCancelled
	case waitApproval:
		return RunWaitingApproval
	case waitUser:
		return RunWaitingUser
	case blockingFailed:
		return RunFailed
	case open && started:
		return RunRunning
	case open:
		return RunPending
	case softFailed:
		return RunPartiallyCompleted
	default:
		return RunCompleted
	}
}

// runMoves lists the legal run status changes (workflow-design.md §3.1).
// Staying in the same status is always allowed.
var runMoves = map[string][]string{
	RunPending:            {RunRunning, RunCancelled, RunWaitingApproval, RunWaitingUser},
	RunRunning:            {RunWaitingApproval, RunWaitingUser, RunCompleted, RunPartiallyCompleted, RunFailed, RunCancelled},
	RunWaitingApproval:    {RunRunning, RunPending, RunCancelled},
	RunWaitingUser:        {RunRunning, RunPending, RunCancelled, RunCompleted, RunPartiallyCompleted, RunFailed, RunWaitingApproval},
	RunFailed:             {RunRunning, RunPending, RunCancelled},
	RunPartiallyCompleted: {RunRunning, RunPending},
	RunCompleted:          {},
	RunCancelled:          {},
}

// CanMoveRun reports whether a run may change from one status to another.
func CanMoveRun(from, to string) bool {
	if from == to {
		return true
	}
	for _, t := range runMoves[from] {
		if t == to {
			return true
		}
	}
	return false
}
