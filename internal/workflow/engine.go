package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/queue"
	"shopee-automation/internal/workflow/workflowdb"
)

// Defaults for step specs.
const (
	defaultTimeout     = 2 * time.Minute
	defaultMaxAttempts = 5
	defaultBaseDelay   = 2 * time.Second
	defaultMaxDelay    = 5 * time.Minute
	leaseGrace         = 30 * time.Second
	busyDelay          = 5 * time.Second
)

// StepHandler runs one step.
type StepHandler func(ctx context.Context, sc *StepContext) Result

// StepSpec describes one step of a definition. Steps run in order; a step
// starts when every earlier step succeeded, was skipped, or failed with
// ContinueOnFailure.
type StepSpec struct {
	Key               string
	Capability        string // documentation and UI; "" for internal steps
	Mutating          bool   // at most one mutating step per shop runs at a time
	ContinueOnFailure bool
	Timeout           time.Duration
	MaxAttempts       int
	BaseDelay         time.Duration
	MaxDelay          time.Duration
	// Report: on success the step output becomes the run output.
	Report  bool
	Handler StepHandler
	// ItemHandler runs each item after Handler returned Expand. Items of
	// one step run in parallel; later steps wait for all of them.
	ItemHandler           StepHandler
	ItemContinueOnFailure bool
	ItemMaxAttempts       int
}

// Definition is a versioned list of steps, registered in code.
type Definition struct {
	Key     string
	Version int
	Steps   []StepSpec
	// ValidateSelection checks the change paths a user approved against
	// the diff they were shown. Optional.
	ValidateSelection func(diff json.RawMessage, selected []string) error
}

func (d Definition) step(key string) (StepSpec, bool) {
	for _, s := range d.Steps {
		if s.Key == key {
			return s, true
		}
	}
	return StepSpec{}, false
}

// ManualTask is work a person must do (no Open API, or extension missing).
type ManualTask struct {
	Key          string `json:"key"`
	Title        string `json:"title"`
	Instructions string `json:"instructions"`
	Capability   string `json:"capability,omitempty"`
	Data         any    `json:"data,omitempty"`
}

type resultKind int

const (
	kindSucceeded resultKind = iota
	kindRetryable
	kindPermanent
	kindAmbiguous
	kindNeedsUser
	kindManual
	kindApproval
	kindSkip
	kindExpand
)

// Result is what a handler returns.
type Result struct {
	kind    resultKind
	output  any
	err     error
	reason  string
	message string
	tasks   []ManualTask
	diff    any
	items   []string
}

// Succeeded finishes the step with output.
func Succeeded(output any) Result { return Result{kind: kindSucceeded, output: output} }

// Retryable fails this attempt; the engine retries with backoff.
func Retryable(err error) Result { return Result{kind: kindRetryable, err: err} }

// Permanent fails the step without retrying.
func Permanent(err error) Result { return Result{kind: kindPermanent, err: err} }

// Ambiguous means an external write may or may not have happened. The step
// waits for a person to check before it is resumed.
func Ambiguous(err error) Result {
	return Result{kind: kindAmbiguous, err: err, reason: "ambiguous", message: "The outcome is unknown. Check the shop before resuming."}
}

// NeedsUser pauses the step until a user resumes the run (e.g. reconnect).
func NeedsUser(reason, message string) Result {
	return Result{kind: kindNeedsUser, reason: reason, message: message}
}

// Manual pauses the step until a user marks the tasks done. output is kept.
func Manual(tasks []ManualTask, output any) Result {
	return Result{kind: kindManual, tasks: tasks, output: output, reason: "manual_tasks", message: "Manual tasks are waiting."}
}

// AwaitApproval stops at an approval gate showing diff.
func AwaitApproval(diff any) Result { return Result{kind: kindApproval, diff: diff} }

// Expand finishes the step and creates one item step per key, run by the
// spec's ItemHandler.
func Expand(items []string, output any) Result {
	return Result{kind: kindExpand, items: items, output: output}
}

// Skip records that the step had nothing to do, with a reason.
func Skip(reason string) Result { return Result{kind: kindSkip, reason: reason} }

// Approval is the decision a handler sees when it runs after a gate.
type Approval struct {
	ID       uuid.UUID
	Status   string
	Selected []string
}

// StepContext is what a handler gets.
type StepContext struct {
	OrgID     uuid.UUID
	RunID     uuid.UUID
	StepID    uuid.UUID
	StepKey   string
	ItemKey   string // "" unless this is an item step
	ShopID    uuid.UUID
	CreatedBy *uuid.UUID
	Attempt   int
	Input     json.RawMessage
	Outputs   map[string]json.RawMessage
	// ItemOutputs holds item step outputs: step key -> item key -> output.
	ItemOutputs map[string]map[string]json.RawMessage
	Approval    *Approval
	logs        []logEntry
}

type logEntry struct {
	level, message string
	data           map[string]any
}

// Log records a run event (shown in the execution log). Never pass tokens.
func (sc *StepContext) Log(level, message string, data map[string]any) {
	sc.logs = append(sc.logs, logEntry{level, message, data})
}

// Output decodes the output of an earlier step.
func (sc *StepContext) Output(stepKey string, v any) error {
	raw, ok := sc.Outputs[stepKey]
	if !ok || len(raw) == 0 {
		return fmt.Errorf("workflow: no output from step %s", stepKey)
	}
	return json.Unmarshal(raw, v)
}

// Engine executes workflow runs.
type Engine struct {
	DB     *db.DB
	System *db.DB // sweeper only
	Queue  queue.Enqueuer
	Clock  clock.Clock
	Audit  audit.Recorder
	Log    *slog.Logger
	defs   map[string]Definition
}

// Register adds a definition.
func (e *Engine) Register(d Definition) {
	if e.defs == nil {
		e.defs = map[string]Definition{}
	}
	e.defs[d.Key] = d
}

// Definition returns a registered definition.
func (e *Engine) Definition(key string) (Definition, bool) {
	d, ok := e.defs[key]
	return d, ok
}

func (e *Engine) now() time.Time { return e.Clock.Now().UTC() }

func (e *Engine) tx(ctx context.Context, orgID uuid.UUID, userID uuid.UUID, fn func(q *workflowdb.Queries, tx pgx.Tx) error) error {
	return e.DB.Tx(ctx, db.Scope{OrgID: orgID, UserID: userID}, func(tx pgx.Tx) error { return fn(workflowdb.New(tx), tx) })
}

// StartSpec describes one run to create.
type StartSpec struct {
	Definition string
	ShopID     uuid.UUID
	Input      any
}

// StartOptions are shared by all runs of one start request.
type StartOptions struct {
	OrgID          uuid.UUID
	UserID         uuid.UUID
	BatchID        *uuid.UUID
	IdempotencyKey *string
	RequestHash    []byte
}

// CreateRuns inserts runs and their steps inside the caller's transaction.
// The caller must call Kick after commit.
func (e *Engine) CreateRuns(ctx context.Context, tx pgx.Tx, o StartOptions, specs []StartSpec) ([]workflowdb.WorkflowRun, error) {
	q := workflowdb.New(tx)
	now := e.now()
	runs := make([]workflowdb.WorkflowRun, 0, len(specs))
	for _, sp := range specs {
		d, ok := e.defs[sp.Definition]
		if !ok {
			return nil, fmt.Errorf("workflow: unknown definition %s", sp.Definition)
		}
		input, err := json.Marshal(sp.Input)
		if err != nil {
			return nil, err
		}
		var creator *uuid.UUID
		if o.UserID != uuid.Nil {
			creator = &o.UserID
		}
		run, err := q.InsertRun(ctx, workflowdb.InsertRunParams{ID: db.NewID(), OrgID: o.OrgID, DefinitionKey: d.Key,
			DefinitionVersion: int32(d.Version), ShopConnectionID: sp.ShopID, BatchID: o.BatchID, Input: input, // #nosec G115 -- small version numbers
			IdempotencyKey: o.IdempotencyKey, RequestHash: o.RequestHash, CreatedBy: creator})
		if err != nil {
			return nil, err
		}
		for i, st := range d.Steps {
			var capKey *string
			if st.Capability != "" {
				c := st.Capability
				capKey = &c
			}
			if err := q.InsertStep(ctx, workflowdb.InsertStepParams{ID: db.NewID(), OrgID: o.OrgID, RunID: run.ID, StepKey: st.Key,
				Position: int32(i), Blocking: !st.ContinueOnFailure, Mutating: st.Mutating, CapabilityKey: capKey, // #nosec G115 -- few steps
				MaxAttempts: int32(orDefault(st.MaxAttempts, defaultMaxAttempts)), NextAttemptAt: now}); err != nil { // #nosec G115
				return nil, err
			}
		}
		if err := e.event(ctx, q, run.OrgID, run.ID, nil, "RUN_CREATED", "info", "Run created.",
			map[string]any{"definition": d.Key, "version": d.Version}); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// Kick enqueues the first runnable step of each run (after commit).
func (e *Engine) Kick(ctx context.Context, orgID uuid.UUID, runIDs ...uuid.UUID) {
	for _, id := range runIDs {
		var next []workflowdb.WorkflowStep
		err := e.tx(ctx, orgID, uuid.Nil, func(q *workflowdb.Queries, _ pgx.Tx) error {
			var err error
			next, err = e.runnable(ctx, q, orgID, id)
			return err
		})
		if err != nil {
			e.Log.WarnContext(ctx, "workflow: kick", slog.String("error", err.Error()))
			continue
		}
		e.enqueue(ctx, next, 0)
	}
}

// runnable returns the PENDING steps at the first position that is not
// done, provided every earlier position is done. Item steps share their
// parent's position and run in parallel.
func (e *Engine) runnable(ctx context.Context, q *workflowdb.Queries, orgID, runID uuid.UUID) ([]workflowdb.WorkflowStep, error) {
	steps, err := q.ListSteps(ctx, workflowdb.ListStepsParams{OrgID: orgID, RunID: runID})
	if err != nil {
		return nil, err
	}
	done := func(s workflowdb.WorkflowStep) bool {
		return s.Status == StepSucceeded || s.Status == StepSkipped || (s.Status == StepFailed && !s.Blocking)
	}
	for i := 0; i < len(steps); {
		j := i
		var pending []workflowdb.WorkflowStep
		allDone := true
		for ; j < len(steps) && steps[j].Position == steps[i].Position; j++ {
			if steps[j].Status == StepPending {
				pending = append(pending, steps[j])
			}
			allDone = allDone && done(steps[j])
		}
		if !allDone {
			return pending, nil
		}
		i = j
	}
	return nil, nil
}

// StepPayload is the queue payload for step execution.
type StepPayload struct {
	OrgID  uuid.UUID `json:"org_id"`
	StepID uuid.UUID `json:"step_id"`
}

func (e *Engine) enqueue(ctx context.Context, steps []workflowdb.WorkflowStep, delay time.Duration) {
	// No task ID: duplicate deliveries are harmless because claiming is a
	// conditional update, and a reused ID could swallow a legitimate
	// re-run (e.g. after a user retry resets a step to its first state).
	for _, s := range steps {
		if _, err := e.Queue.Enqueue(ctx, queue.TypeStepExecute, StepPayload{OrgID: s.OrgID, StepID: s.ID},
			queue.Options{NoRetry: true, Delay: delay, Timeout: 30 * time.Minute}); err != nil {
			// The sweeper picks it up later.
			e.Log.WarnContext(ctx, "workflow: enqueue step", slog.String("step_id", s.ID.String()), slog.String("error", err.Error()))
		}
	}
}

// HandleExecute is the queue handler for queue.TypeStepExecute.
func (e *Engine) HandleExecute(ctx context.Context, payload []byte) error {
	var p StepPayload
	if err := queue.Decode(payload, &p); err != nil {
		return err
	}
	return e.Execute(ctx, p.OrgID, p.StepID)
}

type claimed struct {
	step      workflowdb.WorkflowStep
	run       workflowdb.WorkflowRun
	spec      StepSpec
	def       Definition
	sc        *StepContext
	token     uuid.UUID
	claimedAt time.Time
}

// Execute claims and runs one step. Duplicate or stale deliveries return
// nil without doing anything.
func (e *Engine) Execute(ctx context.Context, orgID, stepID uuid.UUID) error {
	c, delay, err := e.claim(ctx, orgID, stepID)
	if err != nil || c == nil {
		if delay > 0 {
			e.enqueue(ctx, []workflowdb.WorkflowStep{{ID: stepID, OrgID: orgID, NextAttemptAt: e.now().Add(delay)}}, delay)
		}
		return err
	}
	res := e.runHandler(ctx, c)
	return e.finish(ctx, c, res)
}

func (e *Engine) claim(ctx context.Context, orgID, stepID uuid.UUID) (*claimed, time.Duration, error) {
	var out *claimed
	var delay time.Duration
	err := e.tx(ctx, orgID, uuid.Nil, func(q *workflowdb.Queries, _ pgx.Tx) error {
		step, err := q.LockStep(ctx, workflowdb.LockStepParams{OrgID: orgID, ID: stepID})
		if db.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		run, err := q.LockRun(ctx, workflowdb.LockRunParams{OrgID: orgID, ID: step.RunID})
		if err != nil {
			return err
		}
		def, ok := e.defs[run.DefinitionKey]
		if !ok {
			return fmt.Errorf("workflow: unknown definition %s", run.DefinitionKey)
		}
		spec, ok := def.step(step.StepKey)
		if !ok {
			return fmt.Errorf("workflow: definition %s has no step %s", def.Key, step.StepKey)
		}
		now := e.now()
		expired := step.Status == StepRunning && step.LeaseExpiresAt != nil && step.LeaseExpiresAt.Before(now)
		switch {
		case expired:
			// The previous worker died or hung. Record its attempt as timed out.
			if err := e.attempt(ctx, q, step, "TIMED_OUT", map[string]any{"message": "Lease expired."}, now); err != nil {
				return err
			}
			if err := e.event(ctx, q, orgID, run.ID, &step.ID, "STEP_ATTEMPT_FINISHED", "warn",
				"The worker running this step stopped responding; the step is retried.", map[string]any{"step": step.StepKey}); err != nil {
				return err
			}
			if int(step.AttemptCount) >= int(step.MaxAttempts) {
				_, err := q.SetStepStatus(ctx, workflowdb.SetStepStatusParams{Status: StepFailed, NextAttemptAt: now,
					FinishedAt: &now, OrgID: orgID, ID: step.ID})
				if err != nil {
					return err
				}
				_, err = e.syncRun(ctx, q, run)
				return err
			}
		case step.Status != StepPending || step.NextAttemptAt.After(now):
			return nil // already handled, or not due yet
		}
		if run.CancelRequestedAt != nil {
			if _, err := q.SetStepStatus(ctx, workflowdb.SetStepStatusParams{Status: StepCancelled, NextAttemptAt: now,
				FinishedAt: &now, OrgID: orgID, ID: step.ID}); err != nil {
				return err
			}
			_, err := e.syncRun(ctx, q, run)
			return err
		}
		done, err := q.PreviousStepsDone(ctx, workflowdb.PreviousStepsDoneParams{RunID: run.ID, Position: step.Position})
		if err != nil || done == nil || !*done {
			return err
		}
		if spec.Mutating {
			if err := q.AdvisoryLock(ctx, "shop-mutation:"+run.ShopConnectionID.String()); err != nil {
				return err
			}
			busy, err := q.OtherMutatingRunning(ctx, workflowdb.OtherMutatingRunningParams{OrgID: orgID,
				ShopConnectionID: run.ShopConnectionID, StepID: step.ID, Now: now})
			if err != nil {
				return err
			}
			if busy {
				// Wait without spending an attempt.
				delay = busyDelay
				return q.DelayStep(ctx, workflowdb.DelayStepParams{NextAttemptAt: now.Add(busyDelay), OrgID: orgID, ID: step.ID})
			}
		}
		token := uuid.New()
		lease := now.Add(orDefaultDur(spec.Timeout, defaultTimeout) + leaseGrace)
		mode := modeFor(spec)
		step, err = q.ClaimStep(ctx, workflowdb.ClaimStepParams{LeaseToken: &token, LeaseExpiresAt: &lease, Now: now,
			ExecutionMode: mode, OrgID: orgID, ID: step.ID})
		if err != nil {
			return err
		}
		if err := e.event(ctx, q, orgID, run.ID, &step.ID, "STEP_ATTEMPT_STARTED", "info", "",
			map[string]any{"step": step.StepKey, "attempt": step.AttemptCount}); err != nil {
			return err
		}
		if run, err = e.syncRun(ctx, q, run); err != nil {
			return err
		}
		sc, err := e.context(ctx, q, run, step)
		if err != nil {
			return err
		}
		out = &claimed{step: step, run: run, spec: spec, def: def, sc: sc, token: token, claimedAt: now}
		return nil
	})
	return out, delay, err
}

func modeFor(s StepSpec) *string {
	if s.Capability == "" {
		m := "INTERNAL"
		return &m
	}
	return nil
}

func (e *Engine) context(ctx context.Context, q *workflowdb.Queries, run workflowdb.WorkflowRun, step workflowdb.WorkflowStep) (*StepContext, error) {
	steps, err := q.ListSteps(ctx, workflowdb.ListStepsParams{OrgID: run.OrgID, RunID: run.ID})
	if err != nil {
		return nil, err
	}
	outputs := map[string]json.RawMessage{}
	items := map[string]map[string]json.RawMessage{}
	for _, s := range steps {
		if len(s.Output) == 0 {
			continue
		}
		if s.ItemKey == "" {
			outputs[s.StepKey] = s.Output
			continue
		}
		if items[s.StepKey] == nil {
			items[s.StepKey] = map[string]json.RawMessage{}
		}
		items[s.StepKey][s.ItemKey] = s.Output
	}
	sc := &StepContext{OrgID: run.OrgID, RunID: run.ID, StepID: step.ID, StepKey: step.StepKey, ItemKey: step.ItemKey,
		ShopID: run.ShopConnectionID, CreatedBy: run.CreatedBy, Attempt: int(step.AttemptCount), Input: run.Input,
		Outputs: outputs, ItemOutputs: items}
	a, err := q.LatestApprovalForStep(ctx, workflowdb.LatestApprovalForStepParams{OrgID: run.OrgID, StepID: step.ID})
	switch {
	case err == nil:
		ap := &Approval{ID: a.ID, Status: a.Status}
		if len(a.SelectedChanges) > 0 {
			_ = json.Unmarshal(a.SelectedChanges, &ap.Selected)
		}
		sc.Approval = ap
	case !db.IsNoRows(err):
		return nil, err
	}
	return sc, nil
}

func (e *Engine) runHandler(ctx context.Context, c *claimed) (res Result) {
	defer func() {
		if p := recover(); p != nil {
			e.Log.ErrorContext(ctx, "workflow: step panicked", slog.String("step", c.step.StepKey), slog.Any("panic", p))
			res = Retryable(fmt.Errorf("internal error in step %s", c.step.StepKey))
		}
	}()
	hctx, cancel := context.WithTimeout(ctx, orDefaultDur(c.spec.Timeout, defaultTimeout))
	defer cancel()
	if c.step.ItemKey != "" {
		if c.spec.ItemHandler == nil {
			return Permanent(fmt.Errorf("step %s has no item handler", c.step.StepKey))
		}
		return c.spec.ItemHandler(hctx, c.sc)
	}
	return c.spec.Handler(hctx, c.sc)
}

// finish records the result. The update is fenced by the lease token: a
// worker whose lease was taken over cannot overwrite the newer result.
func (e *Engine) finish(ctx context.Context, c *claimed, res Result) error {
	var next []workflowdb.WorkflowStep
	var retryDelay time.Duration
	err := e.tx(ctx, c.run.OrgID, uuid.Nil, func(q *workflowdb.Queries, _ pgx.Tx) error {
		run, err := q.LockRun(ctx, workflowdb.LockRunParams{OrgID: c.run.OrgID, ID: c.run.ID})
		if err != nil {
			return err
		}
		now := e.now()
		p := workflowdb.FinishStepParams{NextAttemptAt: now, OrgID: run.OrgID, ID: c.step.ID, LeaseToken: &c.token}
		outcome, evType, level, msg := "", "STEP_STATUS_CHANGED", "info", ""
		errJSON := func(err error) []byte {
			b, _ := json.Marshal(map[string]any{"message": safeMessage(err)})
			return b
		}
		cancelled := run.CancelRequestedAt != nil
		switch res.kind {
		case kindSucceeded:
			p.Status, outcome = StepSucceeded, "SUCCEEDED"
			p.Output = marshal(res.output)
			p.FinishedAt = &now
		case kindExpand:
			p.Status, outcome = StepSucceeded, "SUCCEEDED"
			p.Output = marshal(map[string]any{"items": res.items, "result": res.output})
			p.FinishedAt = &now
			msg = fmt.Sprintf("Expanded into %d items.", len(res.items))
		case kindSkip:
			p.Status, outcome = StepSkipped, "SKIPPED"
			p.SkipReason = &res.reason
			p.FinishedAt = &now
			msg = "Skipped: " + res.reason
		case kindRetryable:
			p.Error = errJSON(res.err)
			if cancelled {
				p.Status, outcome = StepCancelled, "CANCELLED"
				p.FinishedAt = &now
			} else if int(c.step.AttemptCount) < int(c.step.MaxAttempts) {
				retryDelay = backoff(int(c.step.AttemptCount), orDefaultDur(c.spec.BaseDelay, defaultBaseDelay), orDefaultDur(c.spec.MaxDelay, defaultMaxDelay))
				p.Status, outcome = StepPending, "RETRY"
				p.NextAttemptAt = now.Add(retryDelay)
				level, msg = "warn", "Attempt failed, retrying: "+safeMessage(res.err)
			} else {
				p.Status, outcome = StepFailed, "FAILED"
				p.FinishedAt = &now
				level, msg = "error", "Step failed after all attempts: "+safeMessage(res.err)
			}
		case kindPermanent:
			p.Status, outcome = StepFailed, "FAILED"
			p.Error = errJSON(res.err)
			p.FinishedAt = &now
			level, msg = "error", "Step failed: "+safeMessage(res.err)
		case kindAmbiguous, kindNeedsUser, kindManual:
			if cancelled {
				p.Status, outcome = StepCancelled, "CANCELLED"
				p.FinishedAt = &now
				break
			}
			p.Status, outcome = StepWaitingUser, "WAITING_USER"
			if res.kind == kindAmbiguous {
				outcome = "AMBIGUOUS"
				p.Error = errJSON(res.err)
			}
			p.Output = marshal(map[string]any{"reason": res.reason, "message": res.message, "tasks": res.tasks, "result": res.output})
			evType, level, msg = "USER_ACTION_REQUESTED", "warn", res.message
		case kindApproval:
			if cancelled {
				p.Status, outcome = StepCancelled, "CANCELLED"
				p.FinishedAt = &now
				break
			}
			p.Status, outcome = StepWaitingApproval, "WAITING_APPROVAL"
		}
		step, err := q.FinishStep(ctx, p)
		if db.IsNoRows(err) {
			// Our lease was taken over; the newer attempt owns the step.
			e.Log.WarnContext(ctx, "workflow: stale finish dropped", slog.String("step_id", c.step.ID.String()))
			return errStale
		}
		if err != nil {
			return err
		}
		if err := e.attempt(ctx, q, c.step, outcome, jsonOrNil(p.Error), c.claimedAt); err != nil {
			return err
		}
		for _, l := range c.sc.logs {
			if err := e.event(ctx, q, run.OrgID, run.ID, &step.ID, "LOG", l.level, l.message, l.data); err != nil {
				return err
			}
		}
		if res.kind == kindExpand {
			var capKey *string
			if c.spec.Capability != "" {
				ck := c.spec.Capability
				capKey = &ck
			}
			for _, item := range res.items {
				if err := q.InsertItemStep(ctx, workflowdb.InsertItemStepParams{ID: db.NewID(), OrgID: run.OrgID, RunID: run.ID,
					StepKey: step.StepKey, ItemKey: item, Position: step.Position, Blocking: !c.spec.ItemContinueOnFailure,
					Mutating: c.spec.Mutating, CapabilityKey: capKey,
					MaxAttempts: int32(orDefault(c.spec.ItemMaxAttempts, defaultMaxAttempts)), NextAttemptAt: now}); err != nil { // #nosec G115 -- small
					return err
				}
			}
		}
		if res.kind == kindApproval && !cancelled {
			a, err := q.InsertApproval(ctx, workflowdb.InsertApprovalParams{ID: db.NewID(), OrgID: run.OrgID, RunID: run.ID,
				StepID: step.ID, DiffSnapshot: marshal(res.diff)})
			if err != nil {
				return err
			}
			evType, msg = "APPROVAL_REQUESTED", "Waiting for approval of the changes."
			if err := e.event(ctx, q, run.OrgID, run.ID, &step.ID, evType, "info", msg, map[string]any{"approval_id": a.ID.String()}); err != nil {
				return err
			}
		} else if err := e.event(ctx, q, run.OrgID, run.ID, &step.ID, evType, level, msg,
			map[string]any{"step": step.StepKey, "status": step.Status}); err != nil {
			return err
		}
		if c.spec.Report && step.Status == StepSucceeded && len(step.Output) > 0 {
			if err := q.SetRunOutput(ctx, workflowdb.SetRunOutputParams{Output: step.Output, OrgID: run.OrgID, ID: run.ID}); err != nil {
				return err
			}
		}
		if _, err := e.syncRun(ctx, q, run); err != nil {
			return err
		}
		if step.Status == StepSucceeded || step.Status == StepSkipped || (step.Status == StepFailed && !step.Blocking) {
			next, err = e.runnable(ctx, q, run.OrgID, run.ID)
			return err
		}
		if step.Status == StepPending {
			next = []workflowdb.WorkflowStep{step}
		}
		return nil
	})
	if errors.Is(err, errStale) {
		return nil
	}
	if err != nil {
		return err
	}
	e.enqueue(ctx, next, retryDelay)
	return nil
}

var errStale = errors.New("stale lease")

func (e *Engine) attempt(ctx context.Context, q *workflowdb.Queries, step workflowdb.WorkflowStep, outcome string, errData any, started time.Time) error {
	var errJSON []byte
	if errData != nil {
		errJSON = marshal(errData)
	}
	return q.InsertAttempt(ctx, workflowdb.InsertAttemptParams{ID: db.NewID(), OrgID: step.OrgID, StepID: step.ID,
		AttemptNo: step.AttemptCount, Outcome: outcome, Error: errJSON, StartedAt: started, FinishedAt: e.now()})
}

func (e *Engine) event(ctx context.Context, q *workflowdb.Queries, orgID, runID uuid.UUID, stepID *uuid.UUID, typ, level, msg string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	return q.InsertEvent(ctx, workflowdb.InsertEventParams{ID: db.NewID(), OrgID: orgID, RunID: runID, StepID: stepID,
		Type: typ, Level: level, Message: truncate(msg, 1000), Data: marshal(data), CreatedAt: e.now()})
}

// syncRun recomputes the run status from its steps.
func (e *Engine) syncRun(ctx context.Context, q *workflowdb.Queries, run workflowdb.WorkflowRun) (workflowdb.WorkflowRun, error) {
	steps, err := q.ListSteps(ctx, workflowdb.ListStepsParams{OrgID: run.OrgID, RunID: run.ID})
	if err != nil {
		return run, err
	}
	views := make([]StepView, 0, len(steps))
	for _, s := range steps {
		views = append(views, StepView{Status: s.Status, Blocking: s.Blocking, Started: s.AttemptCount > 0})
	}
	next := DeriveRunStatus(views, run.CancelRequestedAt != nil)
	if next == run.Status {
		return run, nil
	}
	if !CanMoveRun(run.Status, next) {
		return run, fmt.Errorf("workflow: illegal run transition %s -> %s", run.Status, next)
	}
	now := e.now()
	p := workflowdb.SetRunStatusParams{Status: next, OrgID: run.OrgID, ID: run.ID}
	if next != RunPending {
		p.StartedAt = &now
	}
	if IsTerminalRun(next) {
		p.FinishedAt = &now
	}
	updated, err := q.SetRunStatus(ctx, p)
	if err != nil {
		return run, err
	}
	level := "info"
	if next == RunFailed || next == RunPartiallyCompleted {
		level = "warn"
	}
	return updated, e.event(ctx, q, run.OrgID, run.ID, nil, "RUN_STATUS_CHANGED", level, "",
		map[string]any{"from": run.Status, "to": next})
}

// backoff is full jitter: rand(0, min(max, base*2^attempt)).
func backoff(attempt int, base, max time.Duration) time.Duration {
	ceiling := max
	if attempt < 30 {
		if c := base << attempt; c > 0 && c < max {
			ceiling = c
		}
	}
	return time.Duration(rand.Int64N(int64(ceiling)) + 1) // #nosec G404 -- jitter, not security
}

func marshal(v any) []byte {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		b, _ = json.Marshal(map[string]string{"error": "unserializable output"})
	}
	return b
}

func jsonOrNil(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}

// safeMessage keeps error text short. Handlers must only return errors that
// are safe to show (the Shopee client never puts secrets into errors).
func safeMessage(err error) string {
	if err == nil {
		return ""
	}
	return truncate(err.Error(), 500)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func orDefault(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}

func orDefaultDur(v, d time.Duration) time.Duration {
	if v <= 0 {
		return d
	}
	return v
}

// Snapshot stores the configuration read from a shop during a run.
func (e *Engine) Snapshot(ctx context.Context, orgID, shopID, runID uuid.UUID, kind string, config any) error {
	return e.tx(ctx, orgID, uuid.Nil, func(q *workflowdb.Queries, _ pgx.Tx) error {
		return q.InsertSnapshot(ctx, workflowdb.InsertSnapshotParams{ID: db.NewID(), OrgID: orgID, ShopConnectionID: shopID,
			Kind: kind, WorkflowRunID: &runID, Config: marshal(config), CapturedAt: e.now()})
	})
}
