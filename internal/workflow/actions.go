package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/queue"
	"shopee-automation/internal/workflow/workflowdb"
)

var (
	errRunNotFound      = apperr.NotFound("Workflow run not found.")
	errStepNotFound     = apperr.NotFound("Step not found.")
	errApprovalNotFound = apperr.NotFound("Approval not found.")
)

// ApprovalPolicy decides whether the caller may decide an approval of run.
type ApprovalPolicy func(ctx context.Context, p authctx.Principal, t authctx.Tenant, run workflowdb.WorkflowRun) error

// DecisionInput is a user's approval decision.
type DecisionInput struct {
	Decision            string   `json:"decision"` // APPROVE | REJECT
	SelectedChangePaths []string `json:"selected_change_paths"`
	Comment             *string  `json:"comment"`
}

// Decide approves or rejects a pending approval.
func (e *Engine) Decide(ctx context.Context, p authctx.Principal, t authctx.Tenant, approvalID uuid.UUID, in DecisionInput, policy ApprovalPolicy) (ApprovalView, error) {
	if in.Decision != "APPROVE" && in.Decision != "REJECT" {
		return ApprovalView{}, apperr.Invalid(apperr.FieldError{Field: "decision", Code: "invalid", Message: "Must be APPROVE or REJECT."})
	}
	if in.Comment != nil && len([]rune(*in.Comment)) > 1000 {
		return ApprovalView{}, apperr.Invalid(apperr.FieldError{Field: "comment", Code: "too_long", Message: "Must be at most 1000 characters."})
	}
	var out ApprovalView
	var kick bool
	var runID uuid.UUID
	err := e.tx(ctx, t.OrgID, p.UserID, func(q *workflowdb.Queries, tx pgx.Tx) error {
		a, err := q.LockApproval(ctx, workflowdb.LockApprovalParams{OrgID: t.OrgID, ID: approvalID})
		if err != nil {
			return wrap(err, errApprovalNotFound)
		}
		if a.Status != "PENDING" {
			return apperr.Conflict("already_decided", "This approval was already decided.")
		}
		run, err := q.LockRun(ctx, workflowdb.LockRunParams{OrgID: t.OrgID, ID: a.RunID})
		if err != nil {
			return err
		}
		runID = run.ID
		if policy != nil {
			if err := policy(ctx, p, t, run); err != nil {
				return err
			}
		}
		now := e.now()
		var selected []byte
		status := "REJECTED"
		if in.Decision == "APPROVE" {
			status = "APPROVED"
			if in.SelectedChangePaths == nil {
				in.SelectedChangePaths = []string{}
			}
			if d := e.defs[run.DefinitionKey]; d.ValidateSelection != nil {
				if err := d.ValidateSelection(a.DiffSnapshot, in.SelectedChangePaths); err != nil {
					return apperr.Invalid(apperr.FieldError{Field: "selected_change_paths", Code: "invalid", Message: err.Error()})
				}
			}
			selected = marshal(in.SelectedChangePaths)
		}
		a, err = q.DecideApproval(ctx, workflowdb.DecideApprovalParams{Status: status, SelectedChanges: selected, DecidedBy: &p.UserID,
			Comment: in.Comment, Now: now, OrgID: t.OrgID, ID: a.ID})
		if err != nil {
			return err
		}
		step, err := q.LockStep(ctx, workflowdb.LockStepParams{OrgID: t.OrgID, ID: a.StepID})
		if err != nil {
			return err
		}
		ev := EvReject
		if status == "APPROVED" {
			ev = EvApprove
		}
		next, err := Transition(step.Status, ev)
		if err != nil {
			return apperr.Conflict("invalid_state", "The step is not waiting for approval.")
		}
		fin := &now
		if next == StepPending {
			fin = nil
		}
		if _, err := q.SetStepStatus(ctx, workflowdb.SetStepStatusParams{Status: next, NextAttemptAt: now, FinishedAt: fin,
			OrgID: t.OrgID, ID: step.ID}); err != nil {
			return err
		}
		if status == "REJECTED" {
			if err := e.cancelRun(ctx, q, run, "approval_rejected"); err != nil {
				return err
			}
		}
		if err := e.event(ctx, q, t.OrgID, run.ID, &step.ID, "APPROVAL_DECIDED", "info", "Approval "+strings.ToLower(status)+".",
			map[string]any{"approval_id": a.ID.String(), "decision": status, "selected": len(in.SelectedChangePaths), "by": p.UserID.String()}); err != nil {
			return err
		}
		run, err = q.GetRun(ctx, workflowdb.GetRunParams{OrgID: t.OrgID, ID: run.ID})
		if err != nil {
			return err
		}
		if _, err := e.syncRun(ctx, q, run); err != nil {
			return err
		}
		out = toApproval(a, run)
		kick = status == "APPROVED"
		return e.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "workflow.approval_" + strings.ToLower(status),
			TargetType: "workflow_run", TargetID: &run.ID, Metadata: map[string]any{"approval_id": a.ID.String(), "selected_count": len(in.SelectedChangePaths)}})
	})
	if err != nil {
		return ApprovalView{}, wrap(err, nil)
	}
	if kick {
		e.Kick(ctx, t.OrgID, runID)
	}
	return out, nil
}

// cancelRun requests cancellation and cancels every step that is not
// running or finished. Running steps stop at their next checkpoint.
func (e *Engine) cancelRun(ctx context.Context, q *workflowdb.Queries, run workflowdb.WorkflowRun, reason string) error {
	now := e.now()
	if err := q.RequestCancel(ctx, workflowdb.RequestCancelParams{Now: now, Reason: &reason, OrgID: run.OrgID, ID: run.ID}); err != nil {
		return err
	}
	steps, err := q.ListSteps(ctx, workflowdb.ListStepsParams{OrgID: run.OrgID, RunID: run.ID})
	if err != nil {
		return err
	}
	for _, s := range steps {
		if _, err := Transition(s.Status, EvCancel); err != nil || s.Status == StepRunning {
			continue
		}
		if _, err := q.SetStepStatus(ctx, workflowdb.SetStepStatusParams{Status: StepCancelled, NextAttemptAt: now, FinishedAt: &now,
			OrgID: run.OrgID, ID: s.ID}); err != nil {
			return err
		}
	}
	if err := q.ExpirePendingApprovals(ctx, workflowdb.ExpirePendingApprovalsParams{Now: now, OrgID: run.OrgID, RunID: run.ID}); err != nil {
		return err
	}
	return e.event(ctx, q, run.OrgID, run.ID, nil, "CANCEL_REQUESTED", "warn", "Cancellation requested.", map[string]any{"reason": reason})
}

// CancelInput optionally explains a cancellation.
type CancelInput struct {
	Reason *string `json:"reason"`
}

// Cancel stops a run. Changes already applied on Shopee are not rolled back.
func (e *Engine) Cancel(ctx context.Context, p authctx.Principal, t authctx.Tenant, runID uuid.UUID, in CancelInput) (RunView, error) {
	return e.userAction(ctx, p, t, runID, "workflow.cancelled", false, func(q *workflowdb.Queries, run workflowdb.WorkflowRun) error {
		if IsTerminalRun(run.Status) || run.CancelRequestedAt != nil {
			return apperr.Conflict("not_cancellable", "This run has already finished or is being cancelled.")
		}
		reason := "cancelled_by_user"
		if in.Reason != nil && strings.TrimSpace(*in.Reason) != "" {
			reason = truncate(strings.TrimSpace(*in.Reason), 500)
		}
		return e.cancelRun(ctx, q, run, reason)
	})
}

// Retry resets failed steps of a FAILED or PARTIALLY_COMPLETED run.
// Succeeded steps are never run again.
func (e *Engine) Retry(ctx context.Context, p authctx.Principal, t authctx.Tenant, runID uuid.UUID) (RunView, error) {
	return e.userAction(ctx, p, t, runID, "workflow.retried", true, func(q *workflowdb.Queries, run workflowdb.WorkflowRun) error {
		if run.Status != RunFailed && run.Status != RunPartiallyCompleted {
			return apperr.Conflict("not_retryable", "Only failed or partially completed runs can be retried.")
		}
		steps, err := q.ListSteps(ctx, workflowdb.ListStepsParams{OrgID: run.OrgID, RunID: run.ID})
		if err != nil {
			return err
		}
		firstFailed := int32(-1)
		for _, s := range steps {
			if s.Status == StepFailed {
				if firstFailed < 0 || s.Position < firstFailed {
					firstFailed = s.Position
				}
				if err := q.ResetStepForRetry(ctx, workflowdb.ResetStepForRetryParams{Now: e.now(), OrgID: run.OrgID, ID: s.ID}); err != nil {
					return err
				}
			}
		}
		// Report steps after a retried step run again, so the run output
		// reflects the final outcome.
		def := e.defs[run.DefinitionKey]
		for _, s := range steps {
			if spec, ok := def.step(s.StepKey); ok && spec.Report && s.ItemKey == "" && s.Position > firstFailed && firstFailed >= 0 {
				if err := q.ResetSucceededStep(ctx, workflowdb.ResetSucceededStepParams{Now: e.now(), OrgID: run.OrgID, ID: s.ID}); err != nil {
					return err
				}
			}
		}
		return e.event(ctx, q, run.OrgID, run.ID, nil, "LOG", "info", "Retry requested.", nil)
	})
}

// Resume continues steps that wait for a user (reconnect, ambiguous
// outcome checked). Steps with manual tasks are completed instead.
func (e *Engine) Resume(ctx context.Context, p authctx.Principal, t authctx.Tenant, runID uuid.UUID) (RunView, error) {
	return e.userAction(ctx, p, t, runID, "workflow.resumed", true, func(q *workflowdb.Queries, run workflowdb.WorkflowRun) error {
		if run.Status != RunWaitingUser {
			return apperr.Conflict("not_waiting", "This run is not waiting for you.")
		}
		steps, err := q.ListSteps(ctx, workflowdb.ListStepsParams{OrgID: run.OrgID, RunID: run.ID})
		if err != nil {
			return err
		}
		resumed := 0
		for _, s := range steps {
			if s.Status != StepWaitingUser || waitReason(s.Output) == "manual_tasks" {
				continue
			}
			next, err := Transition(s.Status, EvResume)
			if err != nil {
				return err
			}
			if _, err := q.SetStepStatus(ctx, workflowdb.SetStepStatusParams{Status: next, NextAttemptAt: e.now(), OrgID: run.OrgID, ID: s.ID}); err != nil {
				return err
			}
			resumed++
		}
		if resumed == 0 {
			return apperr.Conflict("manual_tasks_open", "Mark the manual tasks as done to continue.")
		}
		return e.event(ctx, q, run.OrgID, run.ID, nil, "USER_ACTION_COMPLETED", "info", "Run resumed.", nil)
	})
}

// CompleteInput confirms a manual task.
type CompleteInput struct {
	Note *string `json:"note"`
}

// CompleteManual marks a step's manual tasks as done.
func (e *Engine) CompleteManual(ctx context.Context, p authctx.Principal, t authctx.Tenant, runID, stepID uuid.UUID, in CompleteInput) (RunView, error) {
	return e.userAction(ctx, p, t, runID, "workflow.manual_task_completed", true, func(q *workflowdb.Queries, run workflowdb.WorkflowRun) error {
		s, err := e.lockManualStep(ctx, q, t.OrgID, run.ID, stepID)
		if err != nil {
			return err
		}
		return e.completeManualStep(ctx, q, run, s, p, in.Note, "Manual tasks marked as done.", nil)
	})
}

var errTasksOpen = apperr.Conflict("tasks_open", "Some tasks of this step are not done by the browser extension.")

// CompleteFromExtension completes a manual step when every one of its
// tasks has a browser task and all of them succeeded. Otherwise it does
// nothing: the user finishes the remaining tasks and marks the step done.
func (e *Engine) CompleteFromExtension(ctx context.Context, p authctx.Principal, t authctx.Tenant, runID, stepID uuid.UUID, succeeded []uuid.UUID) error {
	done := map[string]bool{}
	for _, id := range succeeded {
		done[id.String()] = true
	}
	_, err := e.userAction(ctx, p, t, runID, "workflow.manual_task_completed", true, func(q *workflowdb.Queries, run workflowdb.WorkflowRun) error {
		s, err := e.lockManualStep(ctx, q, t.OrgID, run.ID, stepID)
		if err != nil {
			return err
		}
		var o struct {
			Tasks []ManualTask `json:"tasks"`
		}
		_ = json.Unmarshal(s.Output, &o)
		if len(o.Tasks) == 0 {
			return errTasksOpen
		}
		for _, task := range o.Tasks {
			if task.BrowserTaskID == "" || !done[task.BrowserTaskID] {
				return errTasksOpen
			}
		}
		return e.completeManualStep(ctx, q, run, s, p, nil, "All tasks were done by the browser extension.", map[string]any{"via": "browser_extension"})
	})
	if ae, ok := apperr.As(err); ok && (ae.Code == errTasksOpen.Code || ae.Code == "no_manual_task") {
		return nil
	}
	return err
}

func (e *Engine) lockManualStep(ctx context.Context, q *workflowdb.Queries, orgID, runID, stepID uuid.UUID) (workflowdb.WorkflowStep, error) {
	s, err := q.LockStep(ctx, workflowdb.LockStepParams{OrgID: orgID, ID: stepID})
	if err != nil || s.RunID != runID {
		return s, wrap(err, errStepNotFound)
	}
	if s.Status != StepWaitingUser || waitReason(s.Output) != "manual_tasks" {
		return s, apperr.Conflict("no_manual_task", "This step has no open manual task.")
	}
	return s, nil
}

func (e *Engine) completeManualStep(ctx context.Context, q *workflowdb.Queries, run workflowdb.WorkflowRun, s workflowdb.WorkflowStep,
	p authctx.Principal, note *string, message string, extra map[string]any) error {
	next, err := Transition(s.Status, EvCompleteManual)
	if err != nil {
		return err
	}
	var out map[string]any
	_ = json.Unmarshal(s.Output, &out)
	if out == nil {
		out = map[string]any{}
	}
	out["completed_by"] = p.UserID.String()
	out["completed_at"] = e.now()
	if note != nil {
		out["note"] = truncate(*note, 1000)
	}
	for k, v := range extra {
		out[k] = v
	}
	now := e.now()
	if _, err := q.SetStepStatus(ctx, workflowdb.SetStepStatusParams{Status: next, NextAttemptAt: now, Output: marshal(out),
		FinishedAt: &now, OrgID: run.OrgID, ID: s.ID}); err != nil {
		return err
	}
	data := map[string]any{"step": s.StepKey, "by": p.UserID.String()}
	for k, v := range extra {
		data[k] = v
	}
	return e.event(ctx, q, run.OrgID, run.ID, &s.ID, "USER_ACTION_COMPLETED", "info", message, data)
}

func waitReason(output []byte) string {
	var o struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(output, &o)
	return o.Reason
}

func (e *Engine) userAction(ctx context.Context, p authctx.Principal, t authctx.Tenant, runID uuid.UUID, action string, kick bool,
	fn func(q *workflowdb.Queries, run workflowdb.WorkflowRun) error) (RunView, error) {
	err := e.tx(ctx, t.OrgID, p.UserID, func(q *workflowdb.Queries, tx pgx.Tx) error {
		run, err := q.LockRun(ctx, workflowdb.LockRunParams{OrgID: t.OrgID, ID: runID})
		if err != nil {
			return wrap(err, errRunNotFound)
		}
		if err := fn(q, run); err != nil {
			return err
		}
		run, err = q.GetRun(ctx, workflowdb.GetRunParams{OrgID: t.OrgID, ID: runID})
		if err != nil {
			return err
		}
		if _, err := e.syncRun(ctx, q, run); err != nil {
			return err
		}
		return e.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: action,
			TargetType: "workflow_run", TargetID: &runID})
	})
	if err != nil {
		return RunView{}, wrap(err, nil)
	}
	if kick {
		e.Kick(ctx, t.OrgID, runID)
	}
	return e.GetRun(ctx, p, t, runID)
}

// Sweep re-enqueues runnable steps without a live task and steps whose
// lease expired. It runs on the system pool (cross-tenant, read-only).
func (e *Engine) Sweep(ctx context.Context) (int, error) {
	if e.System == nil {
		return 0, errors.New("workflow: system database is not configured")
	}
	now := e.now()
	var rows []workflowdb.SystemRunnableStepsRow
	err := e.System.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		var err error
		rows, err = workflowdb.New(tx).SystemRunnableSteps(ctx, workflowdb.SystemRunnableStepsParams{
			StaleBefore: now.Add(-30 * time.Second), Now: &now, Lim: 500})
		return err
	})
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		id := "sweep:" + r.ID.String() + ":" + strconv.Itoa(int(r.AttemptCount)) + ":" + strconv.FormatInt(now.Unix()/15, 10)
		if _, err := e.Queue.Enqueue(ctx, queue.TypeStepExecute, StepPayload{OrgID: r.OrgID, StepID: r.ID},
			queue.Options{ID: id, NoRetry: true, Timeout: 30 * time.Minute}); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

func wrap(err error, notFound *apperr.Error) error {
	if err == nil {
		return nil
	}
	if _, ok := apperr.As(err); ok {
		return err
	}
	if db.IsNoRows(err) && notFound != nil {
		return notFound
	}
	return apperr.Internal(err)
}
